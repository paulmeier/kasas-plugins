// Package gate is the submission gate: the set of automated checks a community
// plugin must clear before it can be listed in the registry. The goal is to give a
// kasas user high confidence that an installed community plugin is what it claims
// to be and cannot reach outside the host's sandbox.
//
// The gate runs in three layers:
//
//  1. Structural checks (this file): the plugin directory is well-formed — a valid
//     manifest whose name matches the directory, a single declared entrypoint, a
//     README, no stray binaries / nested code / symlinks, and sane size limits (a
//     wasm plugin's compiled entrypoint is the one allowed binary, with its own
//     size budget).
//  2. A runtime-specific static pass keyed on the manifest runtime (lua.go, js.go,
//     wasm.go): lua/js source is scanned for the escape hatches the host strips at
//     load time. Using one of them is a guaranteed runtime failure in the host, so
//     it is a hard rejection here rather than a surprise after install. The JS/TS
//     pass additionally transpiles the entrypoint with the SAME esbuild the host
//     uses, and the wasm pass compiles the module with the SAME wazero the host
//     uses (plus import/export checks), so "it loads" is proven at submission time.
//  3. Capability risk tiering (capabilities.go): write capabilities are surfaced so
//     a human reviewer (CODEOWNERS) signs off before a plugin that can mutate a
//     user's ledger is listed.
//
// The gate is deterministic and reads only the plugin directory — it never
// executes plugin code — so it is safe to run on untrusted submissions in CI.
package gate

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/paulmeier/kasas-plugins/internal/manifest"
)

// Severity classifies a finding. An Error blocks listing; a Warning is advisory
// and surfaced to reviewers but does not by itself fail the gate.
type Severity string

const (
	SeverityError   Severity = "error"
	SeverityWarning Severity = "warning"
)

// Finding is one result from the gate: a stable machine code, a human message, and
// (when relevant) the file it concerns.
type Finding struct {
	Severity Severity
	Code     string
	Message  string
	File     string // optional, relative to the plugin dir
}

func (f Finding) String() string {
	loc := ""
	if f.File != "" {
		loc = " (" + f.File + ")"
	}
	return fmt.Sprintf("[%s] %s: %s%s", f.Severity, f.Code, f.Message, loc)
}

// Report is the gate's verdict for a single plugin directory.
type Report struct {
	Dir            string
	Name           string
	Manifest       manifest.Manifest // zero value if the manifest failed to parse
	ManifestOK     bool
	Findings       []Finding
	CapabilityTier Tier
}

// OK reports whether the plugin passed: no error-severity findings.
func (r *Report) OK() bool {
	for _, f := range r.Findings {
		if f.Severity == SeverityError {
			return false
		}
	}
	return true
}

// Errors returns just the blocking findings.
func (r *Report) Errors() []Finding {
	var out []Finding
	for _, f := range r.Findings {
		if f.Severity == SeverityError {
			out = append(out, f)
		}
	}
	return out
}

func (r *Report) addError(code, msg string)        { r.add(SeverityError, code, msg, "") }
func (r *Report) addErrorFile(code, msg, f string) { r.add(SeverityError, code, msg, f) }

func (r *Report) add(sev Severity, code, msg, file string) {
	r.Findings = append(r.Findings, Finding{Severity: sev, Code: code, Message: msg, File: file})
}

// Limits bound a submission so a single plugin cannot bloat the repository or hide
// payloads in oversized files. They are generous for real plugins and tight enough
// to make abuse obvious.
type Limits struct {
	MaxFileBytes  int64 // any single file
	MaxTotalBytes int64 // whole plugin directory
	MaxFiles      int   // entries in the directory tree

	// MaxWasmEntrypointBytes is the separate budget for a wasm plugin's compiled
	// entrypoint module, which is a binary several times larger than any source
	// file (a standard-toolchain Go build against the host SDK is ~3.5 MiB). The
	// module is exempt from MaxFileBytes and does not count toward MaxTotalBytes;
	// everything else in the plugin still obeys the normal limits.
	MaxWasmEntrypointBytes int64
}

// DefaultLimits are applied when CheckPlugin is called without overrides.
func DefaultLimits() Limits {
	return Limits{
		MaxFileBytes:           256 * 1024,      // 256 KiB
		MaxTotalBytes:          1 * 1024 * 1024, // 1 MiB
		MaxFiles:               32,
		MaxWasmEntrypointBytes: 8 * 1024 * 1024, // 8 MiB
	}
}

// allowedAuxFiles are non-source files a plugin may include alongside its manifest
// and entrypoint. Everything else (binaries, archives, lockfiles, nested code) is
// rejected so the reviewable surface stays small and obvious.
var allowedAuxExts = map[string]bool{
	".md":   true, // README and docs
	".txt":  true, // LICENSE / NOTICE
	".toml": true, // the manifest itself
	".d.ts": true, // dev-time ambient types for JS/TS plugins (never executed)
	".json": true, // small static data / fixtures
}

// CheckPlugin runs the full gate against a plugin directory and returns a Report.
// It does not execute any plugin code.
func CheckPlugin(dir string, limits Limits) *Report {
	name := filepath.Base(dir)
	r := &Report{Dir: dir, Name: name}

	mfPath := filepath.Join(dir, "plugin.toml")
	data, err := os.ReadFile(mfPath)
	if err != nil {
		r.addError("manifest.missing", fmt.Sprintf("could not read plugin.toml: %v", err))
		return r
	}
	m, err := manifest.Parse(data)
	if err != nil {
		r.addError("manifest.invalid", err.Error())
		return r
	}
	r.Manifest = m
	r.ManifestOK = true

	if m.Name != name {
		r.addError("manifest.name_mismatch",
			fmt.Sprintf("manifest name %q must equal the directory name %q", m.Name, name))
	}

	r.checkTree(dir, m, limits)
	r.CapabilityTier = tierFor(m.Capabilities)
	for _, f := range capabilityFindings(m) {
		r.Findings = append(r.Findings, f)
	}

	// Runtime-specific static analysis on the entrypoint. Only run when the tree
	// check found the entrypoint, to avoid a confusing double error.
	entry := filepath.Join(dir, m.Entrypoint)
	if src, rerr := os.ReadFile(entry); rerr == nil {
		switch m.Runtime {
		case manifest.RuntimeLua:
			gateLua(r, m.Entrypoint, src)
		case manifest.RuntimeJS:
			gateJS(r, m.Entrypoint, src)
		case manifest.RuntimeWasm:
			gateWasm(r, m, src)
		}
	}
	return r
}

// checkTree walks the plugin directory and enforces the structural rules: a README
// is present, the entrypoint exists and is a regular file, there is exactly one
// runtime source file (the entrypoint), no symlinks or nested directories with
// code, only allowlisted auxiliary file types, and the size limits hold.
func (r *Report) checkTree(dir string, m manifest.Manifest, limits Limits) {
	srcExt := manifest.EntrypointExt(m.Runtime)
	var total int64
	var fileCount int
	var sawReadme, sawEntry bool
	var sourceFiles []string

	err := filepath.WalkDir(dir, func(path string, d os.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		rel, _ := filepath.Rel(dir, path)
		if path == dir {
			return nil
		}

		// Reject symlinks outright: they are a classic way to smuggle content or
		// escape the directory, and a plugin never needs one.
		info, ierr := d.Info()
		if ierr != nil {
			return ierr
		}
		if info.Mode()&os.ModeSymlink != 0 {
			r.addErrorFile("tree.symlink", "symlinks are not allowed in a plugin", rel)
			return nil
		}

		if d.IsDir() {
			// A single flat directory keeps the audit surface small. Nested
			// directories are rejected (this also prevents node_modules, .git, etc.).
			r.addErrorFile("tree.nested_dir", "nested directories are not allowed; a plugin is a single flat directory", rel)
			return filepath.SkipDir
		}

		fileCount++
		base := d.Name()

		// A wasm plugin's compiled entrypoint is a binary far larger than any
		// source file, so it gets its own budget instead of the per-file and
		// directory-total limits sized for text.
		if base == m.Entrypoint && m.Runtime == manifest.RuntimeWasm {
			if info.Size() > limits.MaxWasmEntrypointBytes {
				r.addErrorFile("wasm.entrypoint_too_large",
					fmt.Sprintf("compiled module is %d bytes, over the %d-byte wasm entrypoint limit", info.Size(), limits.MaxWasmEntrypointBytes), rel)
			}
		} else {
			total += info.Size()
			if info.Size() > limits.MaxFileBytes {
				r.addErrorFile("tree.file_too_large",
					fmt.Sprintf("file is %d bytes, over the %d-byte per-file limit", info.Size(), limits.MaxFileBytes), rel)
			}
		}
		switch {
		case base == "plugin.toml":
			// the manifest; already validated
		case strings.EqualFold(base, "README.md"):
			sawReadme = true
		case base == m.Entrypoint:
			sawEntry = true
		case srcExt != "" && strings.HasSuffix(base, srcExt):
			// A second source file of the runtime's language that is not the
			// entrypoint. The host loads only the entrypoint (no require/import),
			// so extra source is dead code that hides logic from review.
			sourceFiles = append(sourceFiles, rel)
		case isAllowedAux(base):
			// fine
		default:
			r.addErrorFile("tree.disallowed_file",
				fmt.Sprintf("file type not allowed in a plugin (allowed: source entrypoint, README.md, and %s)", allowedAuxList()), rel)
		}
		return nil
	})
	if err != nil {
		r.addError("tree.walk_failed", fmt.Sprintf("could not walk plugin directory: %v", err))
		return
	}

	if !sawEntry {
		r.addErrorFile("tree.entrypoint_missing", fmt.Sprintf("declared entrypoint %q not found", m.Entrypoint), m.Entrypoint)
	}
	if !sawReadme {
		r.addError("tree.readme_missing", "a README.md is required so users can understand what the plugin does")
	}
	for _, sf := range sourceFiles {
		r.addErrorFile("tree.extra_source",
			"only the single declared entrypoint may be a source file; the host does not load additional modules (no require/import)", sf)
	}
	if fileCount > limits.MaxFiles {
		r.addError("tree.too_many_files", fmt.Sprintf("%d files, over the limit of %d", fileCount, limits.MaxFiles))
	}
	if total > limits.MaxTotalBytes {
		r.addError("tree.too_large", fmt.Sprintf("plugin is %d bytes, over the %d-byte limit", total, limits.MaxTotalBytes))
	}
}

func isAllowedAux(base string) bool {
	lower := strings.ToLower(base)
	if strings.HasSuffix(lower, ".d.ts") {
		return true
	}
	return allowedAuxExts[strings.ToLower(filepath.Ext(base))]
}

func allowedAuxList() string {
	out := make([]string, 0, len(allowedAuxExts))
	for e := range allowedAuxExts {
		out = append(out, e)
	}
	sort.Strings(out)
	return strings.Join(out, ", ")
}
