// Command kasas-plugins is the registry's tooling: it gates community plugin
// submissions and builds the registry index the kasas dashboard consumes. It is the
// single binary CI runs, and the same binary a contributor runs locally before
// opening a PR.
//
// Usage:
//
//	kasas-plugins validate [--verify-build] [plugin-dir ...]  # gate one or more plugins (default: all)
//	kasas-plugins bundle <plugin-dir>                         # reproduce a bundled plugin's entrypoint (ADR 0001)
//	kasas-plugins index [--check] [--out f]                   # build registry/index.json (or verify it)
//
// validate exits non-zero if any plugin fails the gate. index --check exits non-zero
// if the committed index does not match the plugins directory, which is the CI gate
// that keeps the published index honest.
package main

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/paulmeier/kasas-plugins/internal/gate"
	"github.com/paulmeier/kasas-plugins/internal/manifest"
	"github.com/paulmeier/kasas-plugins/internal/registry"
)

const (
	repoURL          = "https://github.com/paulmeier/kasas-plugins"
	pluginsRelDir    = "plugins"
	defaultIndexPath = "registry/index.json"
)

func main() {
	if len(os.Args) < 2 {
		usage()
		os.Exit(2)
	}
	var err error
	switch os.Args[1] {
	case "validate":
		err = cmdValidate(os.Args[2:])
	case "bundle":
		err = cmdBundle(os.Args[2:])
	case "index":
		err = cmdIndex(os.Args[2:])
	case "-h", "--help", "help":
		usage()
		return
	default:
		fmt.Fprintf(os.Stderr, "unknown command %q\n\n", os.Args[1])
		usage()
		os.Exit(2)
	}
	if err != nil {
		fmt.Fprintln(os.Stderr, "error:", err)
		os.Exit(1)
	}
}

func usage() {
	fmt.Fprint(os.Stderr, `kasas-plugins — community plugin registry tooling

Usage:
  kasas-plugins validate [--verify-build] [plugin-dir ...]
        gate plugins (default: every plugin under ./plugins).
        --verify-build re-bundles every plugin with a [build] block from its
        linked source and proves the committed entrypoint matches (needs git+npm).
  kasas-plugins bundle <plugin-dir>
        reproduce a bundled plugin's entrypoint from its [build] source and write
        it into the plugin directory (ADR 0001).
  kasas-plugins index [--check] [--out f]
        build registry/index.json (--check verifies it is current).
`)
}

// cmdValidate gates the given plugin directories, or every plugin under ./plugins
// when none are given. It prints a per-plugin report and fails if any plugin has an
// error-severity finding.
func cmdValidate(args []string) error {
	var dirs []string
	opts := gate.Options{}
	for _, a := range args {
		switch a {
		case "--verify-build":
			opts.VerifyBuild = true
		default:
			if len(a) > 0 && a[0] == '-' {
				return fmt.Errorf("unknown flag %q", a)
			}
			dirs = append(dirs, a)
		}
	}
	if len(dirs) == 0 {
		var derr error
		dirs, derr = allPluginDirs(pluginsRelDir)
		if derr != nil {
			return derr
		}
		if len(dirs) == 0 {
			fmt.Println("no plugins found under", pluginsRelDir)
			return nil
		}
	}

	limits := gate.DefaultLimits()
	failed := 0
	for _, d := range dirs {
		rep := gate.CheckPluginWithOptions(d, limits, opts)
		printReport(rep)
		if !rep.OK() {
			failed++
		}
	}
	fmt.Printf("\n%d plugin(s) checked, %d failed.\n", len(dirs), failed)
	if failed > 0 {
		return fmt.Errorf("%d plugin(s) failed the submission gate", failed)
	}
	return nil
}

// cmdBundle reproduces a bundled plugin's entrypoint (ADR 0001) from the source
// its [build] block links and writes it into the plugin directory. An author runs
// it to produce exactly the bytes the gate will reproduce, so `validate
// --verify-build` then matches.
func cmdBundle(args []string) error {
	if len(args) != 1 {
		return fmt.Errorf("usage: kasas-plugins bundle <plugin-dir>")
	}
	dir := args[0]
	data, err := os.ReadFile(filepath.Join(dir, "plugin.toml"))
	if err != nil {
		return fmt.Errorf("read manifest: %w", err)
	}
	m, err := manifest.Parse(data)
	if err != nil {
		return fmt.Errorf("invalid manifest: %w", err)
	}
	if m.Build == nil {
		return fmt.Errorf("%s has no [build] block; nothing to bundle (only bundled plugins reproduce an entrypoint)", dir)
	}
	fmt.Printf("bundling %s from %s@%s (entry %s)…\n", m.Name, m.Build.Repository, shortRef(m.Build.Ref), m.Build.Entry)
	out, err := gate.ProduceBundle(*m.Build, "")
	if err != nil {
		return err
	}
	dest := filepath.Join(dir, m.Entrypoint)
	if err := os.WriteFile(dest, out, 0o644); err != nil {
		return fmt.Errorf("write %s: %w", dest, err)
	}
	fmt.Printf("wrote %s (%d bytes)\n", dest, len(out))
	return nil
}

// shortRef abbreviates a commit SHA for human-facing output.
func shortRef(ref string) string {
	if len(ref) > 12 {
		return ref[:12]
	}
	return ref
}

func printReport(rep *gate.Report) {
	status := "PASS"
	if !rep.OK() {
		status = "FAIL"
	}
	fmt.Printf("\n%s  %s", status, rep.Name)
	if rep.ManifestOK {
		fmt.Printf("  (%s, %s, tier=%s, capability=%s)", rep.Manifest.Version, rep.Manifest.Runtime, rep.TrustTier, rep.CapabilityTier)
	}
	fmt.Println()
	for _, f := range rep.Findings {
		fmt.Println("   " + f.String())
	}
}

// cmdIndex builds the registry index. With --out it writes the file; with --check
// it instead compares against the committed file (ignoring the generated_at
// timestamp) and fails on any difference. A plugin that fails the gate is fatal:
// the index only ever lists plugins that pass.
func cmdIndex(args []string) error {
	out := defaultIndexPath
	check := false
	for i := 0; i < len(args); i++ {
		switch args[i] {
		case "--check":
			check = true
		case "--out":
			if i+1 >= len(args) {
				return fmt.Errorf("--out requires a path")
			}
			i++
			out = args[i]
		default:
			return fmt.Errorf("unknown flag %q", args[i])
		}
	}

	idx, failures, err := registry.Build(repoURL, pluginsRelDir, pluginsRelDir, gate.DefaultLimits())
	if err != nil {
		return err
	}
	if len(failures) > 0 {
		fmt.Fprintln(os.Stderr, "cannot build index: the following plugins fail the gate:")
		for _, rep := range failures {
			printReport(rep)
		}
		return fmt.Errorf("%d plugin(s) fail the gate; fix them before regenerating the index", len(failures))
	}
	idx.GeneratedAt = time.Now().UTC()

	data, err := idx.Marshal()
	if err != nil {
		return err
	}

	if check {
		return checkIndex(out, data)
	}

	if err := os.MkdirAll(filepath.Dir(out), 0o755); err != nil {
		return err
	}
	if err := os.WriteFile(out, data, 0o644); err != nil {
		return err
	}
	fmt.Printf("wrote %s (%d plugin(s))\n", out, len(idx.Plugins))
	return nil
}

// checkIndex compares freshly generated index bytes against the committed file,
// ignoring generated_at so the timestamp alone never fails CI.
func checkIndex(path string, fresh []byte) error {
	committed, err := os.ReadFile(path)
	if err != nil {
		return fmt.Errorf("read committed index %s: %w (run `kasas-plugins index` and commit it)", path, err)
	}
	if bytes.Equal(stripGeneratedAt(committed), stripGeneratedAt(fresh)) {
		fmt.Printf("%s is up to date.\n", path)
		return nil
	}
	return fmt.Errorf("%s is out of date; run `kasas-plugins index` and commit the result", path)
}

// stripGeneratedAt blanks the generated_at line so index --check compares only
// substantive content. The line is on its own in the indented JSON.
func stripGeneratedAt(b []byte) []byte {
	lines := bytes.Split(b, []byte("\n"))
	out := lines[:0]
	for _, ln := range lines {
		if bytes.Contains(ln, []byte(`"generated_at"`)) {
			continue
		}
		out = append(out, ln)
	}
	return bytes.Join(out, []byte("\n"))
}

// allPluginDirs lists immediate subdirectories of root that contain a plugin.toml.
func allPluginDirs(root string) ([]string, error) {
	entries, err := os.ReadDir(root)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}
	var out []string
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		if _, err := os.Stat(filepath.Join(root, e.Name(), "plugin.toml")); err == nil {
			out = append(out, filepath.Join(root, e.Name()))
		}
	}
	return out, nil
}
