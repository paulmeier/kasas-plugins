package gate

import (
	"bytes"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	esbuild "github.com/evanw/esbuild/pkg/api"
	"github.com/paulmeier/kasas-plugins/internal/manifest"
)

// ADR 0001 — reproducible dependency bundling.
//
// A plugin may depend on third-party libraries as long as those dependencies are
// bundled, at submission time, into the single entrypoint the registry reviews,
// hashes, and installs. The bundle is partly machine-generated, so a reviewer can
// no longer read every line; instead the gate proves the artifact IS the linked
// source by reproducing it. For a plugin with a [build] block it clones the source
// at the pinned commit, installs the locked dependencies with install scripts
// disabled, re-bundles with one fixed esbuild configuration, and requires the
// result to equal the committed entrypoint byte-for-byte.
//
// The gate still never executes the plugin's code: git fetches files, `npm ci
// --ignore-scripts` installs packages without running any lifecycle hook, and
// esbuild parses and concatenates source without evaluating it — exactly as the
// wasm gate compiles (never instantiates) a module. The sandbox remains the
// backstop: bundled pure-computation code runs under the same sealed VM as
// hand-written code, with no filesystem, process, or network.

// bundleGlobalName is the global the canonical bundle assigns its exported hooks
// to; the footer then copies them onto the global object so the host resolves
// each declared hook as a global function exactly as it does for a hand-written
// entrypoint (see kasas internal/plugins/js.go: Load).
const bundleGlobalName = "__kasasExports"

// bundleFooter is appended to every canonical bundle so the entry module's
// exported hook functions become globals in the host's goja VM.
var bundleFooter = "Object.assign(globalThis," + bundleGlobalName + ");"

// buildError carries a stable finding code alongside the underlying failure so
// verifyBundle can report which stage of the reproduction failed.
type buildError struct {
	code string
	err  error
}

func (e *buildError) Error() string { return e.err.Error() }
func (e *buildError) Unwrap() error { return e.err }

// verifyBundle implements the ADR 0001 reproduction check for a JS plugin with a
// [build] block. With verification off (the default for a local run) it records a
// single non-blocking warning so a contributor is not forced to have git, npm,
// and network to gate the rest of the plugin. CI runs with VerifyBuild on, where
// a mismatch — or any failure to reproduce — is a hard rejection.
func verifyBundle(r *Report, m manifest.Manifest, committed []byte, opts Options) {
	if m.Build == nil {
		return
	}
	if !opts.VerifyBuild {
		r.addWarningFile("build.not_verified",
			"bundle provenance not verified in this run (needs git, npm, and network); CI re-bundles the linked source and proves it matches", m.Entrypoint)
		return
	}

	bundle, err := ProduceBundle(*m.Build, opts.WorkDir)
	if err != nil {
		var be *buildError
		if errors.As(err, &be) {
			r.addErrorFile(be.code, "could not reproduce the bundle: "+be.Error(), m.Entrypoint)
		} else {
			r.addErrorFile("build.failed", "could not reproduce the bundle: "+err.Error(), m.Entrypoint)
		}
		return
	}
	if !bytes.Equal(bundle, committed) {
		r.addErrorFile("build.mismatch",
			fmt.Sprintf("the committed entrypoint is not a reproducible bundle of %s@%s (entry %s): committed %d bytes, rebuilt %d. Regenerate it with `kasas-plugins bundle` and commit the result.",
				m.Build.Repository, shortRef(m.Build.Ref), m.Build.Entry, len(committed), len(bundle)),
			m.Entrypoint)
	}
}

// ProduceBundle clones a plugin's linked source at its pinned commit, installs
// its locked dependencies (with install scripts disabled), and returns the
// canonical esbuild bundle of its entry point. It is the single source of the
// bundle bytes shared by build verification (which compares them to the committed
// artifact) and the `kasas-plugins bundle` command (which writes them), so an
// author who runs the command ships exactly what the gate will reproduce.
//
// workDir is the parent for the temporary clone (empty uses the OS temp dir); the
// clone is always removed before returning.
func ProduceBundle(b manifest.BuildManifest, workDir string) ([]byte, error) {
	tmp, err := os.MkdirTemp(workDir, "kasas-bundle-*")
	if err != nil {
		return nil, &buildError{"build.failed", fmt.Errorf("create work dir: %w", err)}
	}
	defer os.RemoveAll(tmp)

	repo := filepath.Join(tmp, "src")
	if out, err := run(tmp, "git", "clone", "--quiet", b.Repository, repo); err != nil {
		return nil, &buildError{"build.clone_failed", fmt.Errorf("git clone %s: %w%s", b.Repository, err, tail(out))}
	}
	if out, err := run(repo, "git", "-c", "advice.detachedHead=false", "checkout", "--quiet", b.Ref); err != nil {
		return nil, &buildError{"build.checkout_failed", fmt.Errorf("git checkout %s: %w%s", shortRef(b.Ref), err, tail(out))}
	}

	entry := filepath.Join(repo, filepath.FromSlash(b.Entry))
	if !withinDir(repo, entry) {
		return nil, &buildError{"build.bundle_failed", fmt.Errorf("entry %q escapes the repository", b.Entry)}
	}
	if !fileExists(entry) {
		return nil, &buildError{"build.bundle_failed", fmt.Errorf("entry %q does not exist in the repository at %s", b.Entry, shortRef(b.Ref))}
	}

	// The npm project is the nearest ancestor of the entry that has a package.json,
	// bounded by the repository root — so both a repo-root project and a source tree
	// kept in a subdirectory (a monorepo, or a plugin whose source lives under the
	// registry's own examples/) work. Install dependencies only when the source
	// actually declares them; a plugin that bundles only its own multi-file source
	// has no package.json and needs no install.
	baseDir := repo
	if pd, ok := nearestPackageJSON(repo, filepath.Dir(entry)); ok {
		baseDir = pd
		if !hasLockfile(baseDir) {
			return nil, &buildError{"build.no_lockfile",
				fmt.Errorf("source has a package.json but no package-lock.json/npm-shrinkwrap.json; a committed lockfile is required so dependencies install deterministically")}
		}
		if out, err := run(baseDir, "npm", "ci", "--ignore-scripts", "--no-audit", "--no-fund"); err != nil {
			return nil, &buildError{"build.install_failed", fmt.Errorf("npm ci: %w%s", err, tail(out))}
		}
	}

	entryRel, err := filepath.Rel(baseDir, entry)
	if err != nil {
		return nil, &buildError{"build.bundle_failed", fmt.Errorf("locate entry: %w", err)}
	}
	bundle, err := canonicalBundle(baseDir, filepath.ToSlash(entryRel))
	if err != nil {
		return nil, &buildError{"build.bundle_failed", err}
	}
	return bundle, nil
}

// nearestPackageJSON walks up from start (inclusive) to root (inclusive) and
// returns the first directory containing a package.json. start is assumed to be
// inside root.
func nearestPackageJSON(root, start string) (string, bool) {
	dir := start
	for {
		if fileExists(filepath.Join(dir, "package.json")) {
			return dir, true
		}
		if dir == root {
			return "", false
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			return "", false
		}
		dir = parent
	}
}

// canonicalBundle is THE bundle definition: one fixed esbuild configuration, so
// the same source always yields the same bytes. It bundles all imports into a
// single IIFE that assigns the entry's exported hooks to bundleGlobalName, then
// the footer lifts them onto the global object for the host. esbuild is pure Go
// and pinned as a dependency, so the gate and the `bundle` command produce
// identical output without the author having to match an external tool version.
//
// AbsWorkingDir is pinned to the repository root and the entry is given
// repo-relative, so the module-path comments esbuild emits are repo-relative
// ("src/main.ts") rather than absolute — the bundle is then identical no matter
// where the source was cloned, which is what makes the hash comparison stable.
func canonicalBundle(repoDir, entryRel string) ([]byte, error) {
	res := esbuild.Build(esbuild.BuildOptions{
		EntryPoints:   []string{entryRel},
		AbsWorkingDir: repoDir,
		Bundle:        true,
		Format:        esbuild.FormatIIFE,
		GlobalName:    bundleGlobalName,
		Footer:        map[string]string{"js": bundleFooter},
		Target:        esbuild.ES2017,
		Platform:      esbuild.PlatformBrowser,
		Charset:       esbuild.CharsetUTF8,
		LegalComments: esbuild.LegalCommentsNone,
		Write:         false,
		LogLevel:      esbuild.LogLevelSilent,
	})
	if len(res.Errors) > 0 {
		msgs := esbuild.FormatMessages(res.Errors, esbuild.FormatMessagesOptions{})
		return nil, fmt.Errorf("esbuild bundle failed: %s", strings.TrimSpace(strings.Join(msgs, "; ")))
	}
	if len(res.OutputFiles) != 1 {
		return nil, fmt.Errorf("esbuild produced %d output files, expected exactly 1", len(res.OutputFiles))
	}
	return res.OutputFiles[0].Contents, nil
}

// run executes a command in dir and returns its combined output. It never runs
// plugin code: callers invoke git, npm (with scripts disabled), and nothing that
// evaluates the submission's logic.
func run(dir, name string, args ...string) ([]byte, error) {
	cmd := exec.Command(name, args...)
	cmd.Dir = dir
	// A hermetic install: don't let a stray local config or auth leak in.
	cmd.Env = append(os.Environ(), "GIT_TERMINAL_PROMPT=0", "npm_config_audit=false", "npm_config_fund=false")
	return cmd.CombinedOutput()
}

func fileExists(p string) bool {
	info, err := os.Stat(p)
	return err == nil && !info.IsDir()
}

func hasLockfile(dir string) bool {
	return fileExists(filepath.Join(dir, "package-lock.json")) || fileExists(filepath.Join(dir, "npm-shrinkwrap.json"))
}

// withinDir reports whether path is inside root after resolving "..".
func withinDir(root, path string) bool {
	rel, err := filepath.Rel(root, path)
	if err != nil {
		return false
	}
	return rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator))
}

func shortRef(ref string) string {
	if len(ref) > 12 {
		return ref[:12]
	}
	return ref
}

// tail returns a short, indented excerpt of command output for an error message,
// so a failure shows why without dumping a whole log.
func tail(out []byte) string {
	s := strings.TrimSpace(string(out))
	if s == "" {
		return ""
	}
	const max = 500
	if len(s) > max {
		s = "…" + s[len(s)-max:]
	}
	return "\n    " + strings.ReplaceAll(s, "\n", "\n    ")
}
