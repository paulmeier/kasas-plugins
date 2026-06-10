package gate

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/paulmeier/kasas-plugins/internal/manifest"
)

// initRepo creates a throwaway git repository containing files and returns its
// path and the HEAD commit SHA. Build verification clones from any git URL, so a
// local repository lets these tests exercise the full clone→checkout→bundle path
// hermetically — no network, no npm.
func initRepo(t *testing.T, files map[string]string) (string, string) {
	t.Helper()
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not available")
	}
	repo := t.TempDir()
	git(t, repo, "init", "-q", "-b", "main")
	for rel, content := range files {
		p := filepath.Join(repo, rel)
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	git(t, repo, "add", "-A")
	git(t, repo, "-c", "user.email=t@example.com", "-c", "user.name=t", "-c", "commit.gpgsign=false", "commit", "-q", "-m", "snapshot")
	sha := strings.TrimSpace(git(t, repo, "rev-parse", "HEAD"))
	return repo, sha
}

func git(t *testing.T, dir string, args ...string) string {
	t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git %v: %v\n%s", args, err, out)
	}
	return string(out)
}

// source is a two-file TypeScript plugin: the entry imports a relative helper and
// exports its hooks. It uses no third-party package, so it bundles with no npm
// install — which keeps the test hermetic while still proving multi-file bundling.
var bundleSource = map[string]string{
	"src/money.ts": "export function toCents(a: string): number { return Math.round(parseFloat(a) * 100); }\n",
	"src/main.ts": `import { toCents } from "./money";
export function OnTransactionCreate(txn: { id: string; amount: string }): void {
  if (toCents(txn.amount) < 0) { kasas.setExtension(txn.id, "flagged", true); }
}
export function OnUninstall(): void { kasas.log("info", "bye"); }
`,
}

func TestProduceBundleIsReproducible(t *testing.T) {
	repo, sha := initRepo(t, bundleSource)
	b := manifest.BuildManifest{Repository: repo, Ref: sha, Entry: "src/main.ts"}

	first, err := ProduceBundle(b, t.TempDir())
	if err != nil {
		t.Fatalf("ProduceBundle: %v", err)
	}
	second, err := ProduceBundle(b, t.TempDir())
	if err != nil {
		t.Fatalf("ProduceBundle (again): %v", err)
	}
	if string(first) != string(second) {
		t.Fatal("bundle is not byte-for-byte reproducible across runs")
	}
	// The footer must lift the entry's exports onto the global object, and the
	// relative dependency must be inlined (no leftover import).
	out := string(first)
	if !strings.Contains(out, "Object.assign(globalThis,"+bundleGlobalName+")") {
		t.Fatalf("bundle missing the global-export footer:\n%s", out)
	}
	if !strings.Contains(out, "toCents") {
		t.Fatalf("relative dependency was not bundled in:\n%s", out)
	}
}

func TestVerifyBundleMatch(t *testing.T) {
	repo, sha := initRepo(t, bundleSource)
	b := &manifest.BuildManifest{Repository: repo, Ref: sha, Entry: "src/main.ts"}
	committed, err := ProduceBundle(*b, t.TempDir())
	if err != nil {
		t.Fatal(err)
	}

	r := &Report{Name: "demo"}
	m := manifest.Manifest{Name: "demo", Runtime: manifest.RuntimeJS, Entrypoint: "main.js", Build: b}
	verifyBundle(r, m, committed, Options{VerifyBuild: true})
	if !r.OK() {
		t.Fatalf("a faithfully reproduced bundle must verify, got: %v", r.Errors())
	}
}

func TestVerifyBundleMismatch(t *testing.T) {
	repo, sha := initRepo(t, bundleSource)
	b := &manifest.BuildManifest{Repository: repo, Ref: sha, Entry: "src/main.ts"}
	committed, err := ProduceBundle(*b, t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	tampered := append([]byte("/* sneaky */\n"), committed...)

	r := &Report{Name: "demo"}
	m := manifest.Manifest{Name: "demo", Runtime: manifest.RuntimeJS, Entrypoint: "main.js", Build: b}
	verifyBundle(r, m, tampered, Options{VerifyBuild: true})
	if r.OK() || !hasCode(r, "build.mismatch") {
		t.Fatalf("a tampered entrypoint must fail with build.mismatch, got: %v", r.Findings)
	}
}

func TestVerifyBundlePinnedToCommit(t *testing.T) {
	repo, sha := initRepo(t, bundleSource)
	b := &manifest.BuildManifest{Repository: repo, Ref: sha, Entry: "src/main.ts"}
	atFirst, err := ProduceBundle(*b, t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	// Advance the source: the pinned ref must still reproduce the OLD bundle.
	if err := os.WriteFile(filepath.Join(repo, "src/money.ts"),
		[]byte("export function toCents(a: string): number { return 999; }\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	git(t, repo, "add", "-A")
	git(t, repo, "-c", "user.email=t@example.com", "-c", "user.name=t", "-c", "commit.gpgsign=false", "commit", "-q", "-m", "change")

	r := &Report{Name: "demo"}
	m := manifest.Manifest{Name: "demo", Runtime: manifest.RuntimeJS, Entrypoint: "main.js", Build: b}
	verifyBundle(r, m, atFirst, Options{VerifyBuild: true})
	if !r.OK() {
		t.Fatalf("the pinned commit must still reproduce its bundle after HEAD moved, got: %v", r.Errors())
	}
}

func TestVerifyBundleBadRefFails(t *testing.T) {
	repo, _ := initRepo(t, bundleSource)
	b := &manifest.BuildManifest{
		Repository: repo,
		Ref:        "0000000000000000000000000000000000000000",
		Entry:      "src/main.ts",
	}
	r := &Report{Name: "demo"}
	m := manifest.Manifest{Name: "demo", Runtime: manifest.RuntimeJS, Entrypoint: "main.js", Build: b}
	verifyBundle(r, m, []byte("whatever"), Options{VerifyBuild: true})
	if r.OK() || !hasCode(r, "build.checkout_failed") {
		t.Fatalf("an unknown commit must fail at checkout, got: %v", r.Findings)
	}
}

// TestBundledPluginUnverifiedWarnsAndSkipsStaticBans drives the full gate over a
// plugin directory with a [build] block but with verification OFF (the local
// default). The machine-generated bundle legitimately contains constructs the
// hand-written scan bans (globalThis, __proto__, Function), which must NOT be
// flagged; instead the gate records a single non-blocking provenance warning.
func TestBundledPluginUnverifiedWarnsAndSkipsStaticBans(t *testing.T) {
	mf := `
name        = "bundled"
version     = "1.0.0"
description = "a bundled example plugin"
author      = "a"
runtime     = "js"
entrypoint  = "main.js"
license     = "MIT"
homepage    = "https://example.com"
hooks        = ["OnTransactionCreate", "OnUninstall"]
capabilities = ["extensions:write"]

[build]
repository = "https://github.com/example/bundled"
ref        = "1111111111111111111111111111111111111111"
entry      = "src/main.ts"
`
	// Stand-in for a real bundle: valid JS that trips several hand-written bans.
	bundle := `var __kasasExports=(()=>{
  var o={__proto__:null};
  var g=Function("return this")();
  function OnTransactionCreate(t){ kasas.setExtension(t.id,"x",true); }
  function OnUninstall(){}
  return {OnTransactionCreate:OnTransactionCreate,OnUninstall:OnUninstall};
})();
Object.assign(globalThis,__kasasExports);
`
	dir := writePlugin(t, "bundled", map[string]string{
		"plugin.toml": mf, "main.js": bundle, "README.md": "# bundled\n",
	})
	r := CheckPluginWithOptions(dir, DefaultLimits(), Options{VerifyBuild: false})
	if !r.OK() {
		t.Fatalf("expected pass (unverified bundle is a warning, bans skipped), got: %v", r.Errors())
	}
	if !hasCode(r, "build.not_verified") {
		t.Fatalf("expected the build.not_verified warning, got: %v", r.Findings)
	}
	for _, f := range r.Findings {
		if strings.HasPrefix(f.Code, "js.") && f.Code != "js.transpile_failed" {
			t.Fatalf("static escape-hatch ban must be skipped for a bundle, got: %v", f)
		}
	}
}
