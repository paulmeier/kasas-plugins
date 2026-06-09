package gate

import (
	"os"
	"path/filepath"
	"testing"
)

// writePlugin materializes a plugin directory named after dir's base from a map of
// relative path -> contents, and returns the directory path.
func writePlugin(t *testing.T, name string, files map[string]string) string {
	t.Helper()
	root := t.TempDir()
	dir := filepath.Join(root, name)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	for rel, content := range files {
		p := filepath.Join(dir, rel)
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	return dir
}

const luaManifest = `
name        = "coffee"
version     = "1.0.0"
description = "tags coffee transactions"
author      = "a"
runtime     = "lua"
license     = "MIT"
homepage    = "https://example.com"
hooks        = ["OnTransactionCreate", "OnUninstall"]
capabilities = ["transactions:read", "labels:write"]
`

const goodLua = `
function OnTransactionCreate(txn)
  kasas.apply_labels(txn.id, { category = "food" })
end
`

func hasCode(r *Report, code string) bool {
	for _, f := range r.Findings {
		if f.Code == code {
			return true
		}
	}
	return false
}

func TestGoodLuaPasses(t *testing.T) {
	dir := writePlugin(t, "coffee", map[string]string{
		"plugin.toml": luaManifest,
		"main.lua":    goodLua,
		"README.md":   "# coffee\n\nTags coffee.\n",
	})
	r := CheckPlugin(dir, DefaultLimits())
	if !r.OK() {
		t.Fatalf("expected pass, got findings: %v", r.Errors())
	}
	if r.CapabilityTier != TierWrite {
		t.Fatalf("expected write tier, got %q", r.CapabilityTier)
	}
}

func TestLuaBannedIdentifiers(t *testing.T) {
	bodies := map[string]string{
		"os":      `function OnTransactionCreate(t) os.exit() end`,
		"io":      `function OnTransactionCreate(t) io.open("/etc/passwd") end`,
		"require": `local x = require("socket")` + "\nfunction OnTransactionCreate(t) end",
		"load":    `function OnTransactionCreate(t) load("evil")() end`,
	}
	for name, body := range bodies {
		t.Run(name, func(t *testing.T) {
			dir := writePlugin(t, "coffee", map[string]string{
				"plugin.toml": luaManifest, "main.lua": body, "README.md": "# x\n",
			})
			r := CheckPlugin(dir, DefaultLimits())
			if r.OK() {
				t.Fatalf("expected rejection for banned %q", name)
			}
			if !hasCode(r, "lua.banned_identifier") {
				t.Fatalf("expected lua.banned_identifier, got %v", r.Findings)
			}
		})
	}
}

func TestLuaBannedWordInStringIsAllowed(t *testing.T) {
	// "os" appears only inside a string and a comment, never as code.
	body := `
-- this plugin does not use os at all
function OnTransactionCreate(txn)
  kasas.log("info", "the word os and io are fine in strings")
end
`
	dir := writePlugin(t, "coffee", map[string]string{
		"plugin.toml": luaManifest, "main.lua": body, "README.md": "# x\n",
	})
	r := CheckPlugin(dir, DefaultLimits())
	if !r.OK() {
		t.Fatalf("expected pass (banned words only in strings/comments), got: %v", r.Errors())
	}
}

const jsManifest = `
name        = "flagger"
version     = "1.0.0"
description = "flags large expenses"
author      = "a"
runtime     = "js"
entrypoint  = "main.js"
license     = "MIT"
homepage    = "https://example.com"
hooks        = ["OnTransactionCreate", "OnUninstall"]
capabilities = ["extensions:write"]
`

func TestGoodJSPasses(t *testing.T) {
	dir := writePlugin(t, "flagger", map[string]string{
		"plugin.toml": jsManifest,
		"main.js":     "function OnTransactionCreate(txn) { kasas.setExtension(txn.id, \"flagged\", true); }\n",
		"README.md":   "# flagger\n",
	})
	r := CheckPlugin(dir, DefaultLimits())
	if !r.OK() {
		t.Fatalf("expected pass, got: %v", r.Errors())
	}
}

func TestJSBannedConstructs(t *testing.T) {
	bodies := map[string]string{
		"eval":     `function OnTransactionCreate(t){ eval("1+1"); }`,
		"require":  `const fs = require("fs"); function OnTransactionCreate(t){}`,
		"fetch":    `function OnTransactionCreate(t){ fetch("https://evil.example"); }`,
		"import":   `import fs from "fs";` + "\nfunction OnTransactionCreate(t){}",
		"Function": `function OnTransactionCreate(t){ new Function("return 1")(); }`,
		"process":  `function OnTransactionCreate(t){ process.exit(1); }`,
	}
	for name, body := range bodies {
		t.Run(name, func(t *testing.T) {
			dir := writePlugin(t, "flagger", map[string]string{
				"plugin.toml": jsManifest, "main.js": body, "README.md": "# x\n",
			})
			r := CheckPlugin(dir, DefaultLimits())
			if r.OK() {
				t.Fatalf("expected rejection for %q, got pass", name)
			}
		})
	}
}

func TestJSBannedWordInStringAllowed(t *testing.T) {
	body := `function OnTransactionCreate(t){ kasas.log("info", "do not eval or require this"); }`
	dir := writePlugin(t, "flagger", map[string]string{
		"plugin.toml": jsManifest, "main.js": body, "README.md": "# x\n",
	})
	r := CheckPlugin(dir, DefaultLimits())
	if !r.OK() {
		t.Fatalf("expected pass (banned words only in a string), got: %v", r.Errors())
	}
}

func TestJSTranspileFailure(t *testing.T) {
	dir := writePlugin(t, "flagger", map[string]string{
		"plugin.toml": jsManifest,
		"main.js":     "function OnTransactionCreate(txn) { this is not valid js",
		"README.md":   "# x\n",
	})
	r := CheckPlugin(dir, DefaultLimits())
	if r.OK() || !hasCode(r, "js.transpile_failed") {
		t.Fatalf("expected js.transpile_failed, got %v", r.Findings)
	}
}

func TestTypeScriptTranspiles(t *testing.T) {
	dir := writePlugin(t, "flagger", map[string]string{
		"plugin.toml": `
name="flagger"
version="1.0.0"
description="flags large expenses"
author="a"
runtime="js"
entrypoint="main.ts"
license="MIT"
homepage="https://example.com"
hooks=["OnTransactionCreate","OnUninstall"]
capabilities=["extensions:write"]
`,
		"main.ts":   "interface T { id: string }\nfunction OnTransactionCreate(txn: T): void { kasas.setExtension(txn.id, \"x\", true); }\n",
		"README.md": "# x\n",
	})
	r := CheckPlugin(dir, DefaultLimits())
	if !r.OK() {
		t.Fatalf("expected TS to pass, got: %v", r.Errors())
	}
}

func TestStructuralRejections(t *testing.T) {
	t.Run("name mismatch", func(t *testing.T) {
		dir := writePlugin(t, "wrongname", map[string]string{
			"plugin.toml": luaManifest, "main.lua": goodLua, "README.md": "# x\n",
		})
		r := CheckPlugin(dir, DefaultLimits())
		if r.OK() || !hasCode(r, "manifest.name_mismatch") {
			t.Fatalf("expected name mismatch, got %v", r.Findings)
		}
	})

	t.Run("missing readme", func(t *testing.T) {
		dir := writePlugin(t, "coffee", map[string]string{
			"plugin.toml": luaManifest, "main.lua": goodLua,
		})
		r := CheckPlugin(dir, DefaultLimits())
		if r.OK() || !hasCode(r, "tree.readme_missing") {
			t.Fatalf("expected readme_missing, got %v", r.Findings)
		}
	})

	t.Run("extra source file", func(t *testing.T) {
		dir := writePlugin(t, "coffee", map[string]string{
			"plugin.toml": luaManifest, "main.lua": goodLua,
			"helper.lua": "local x = 1\n", "README.md": "# x\n",
		})
		r := CheckPlugin(dir, DefaultLimits())
		if r.OK() || !hasCode(r, "tree.extra_source") {
			t.Fatalf("expected extra_source, got %v", r.Findings)
		}
	})

	t.Run("nested directory", func(t *testing.T) {
		dir := writePlugin(t, "coffee", map[string]string{
			"plugin.toml": luaManifest, "main.lua": goodLua, "README.md": "# x\n",
			"sub/data.json": "{}",
		})
		r := CheckPlugin(dir, DefaultLimits())
		if r.OK() || !hasCode(r, "tree.nested_dir") {
			t.Fatalf("expected nested_dir, got %v", r.Findings)
		}
	})

	t.Run("disallowed file type", func(t *testing.T) {
		dir := writePlugin(t, "coffee", map[string]string{
			"plugin.toml": luaManifest, "main.lua": goodLua, "README.md": "# x\n",
			"payload.sh": "#!/bin/sh\nrm -rf /\n",
		})
		r := CheckPlugin(dir, DefaultLimits())
		if r.OK() || !hasCode(r, "tree.disallowed_file") {
			t.Fatalf("expected disallowed_file, got %v", r.Findings)
		}
	})

	t.Run("file too large", func(t *testing.T) {
		big := make([]byte, 2048)
		for i := range big {
			big[i] = 'x'
		}
		dir := writePlugin(t, "coffee", map[string]string{
			"plugin.toml": luaManifest, "main.lua": goodLua, "README.md": "# x\n" + string(big),
		})
		r := CheckPlugin(dir, Limits{MaxFileBytes: 100, MaxTotalBytes: 1 << 20, MaxFiles: 32})
		if r.OK() || !hasCode(r, "tree.file_too_large") {
			t.Fatalf("expected file_too_large, got %v", r.Findings)
		}
	})

	t.Run("symlink rejected", func(t *testing.T) {
		dir := writePlugin(t, "coffee", map[string]string{
			"plugin.toml": luaManifest, "main.lua": goodLua, "README.md": "# x\n",
		})
		if err := os.Symlink("/etc/passwd", filepath.Join(dir, "link.txt")); err != nil {
			t.Skipf("symlink unsupported: %v", err)
		}
		r := CheckPlugin(dir, DefaultLimits())
		if r.OK() || !hasCode(r, "tree.symlink") {
			t.Fatalf("expected symlink finding, got %v", r.Findings)
		}
	})
}

func TestReadOnlyTier(t *testing.T) {
	mf := `
name="reader"
version="1.0.0"
description="reads transactions"
author="a"
runtime="lua"
license="MIT"
homepage="https://example.com"
hooks=["OnSyncComplete","OnUninstall"]
capabilities=["transactions:read"]
`
	dir := writePlugin(t, "reader", map[string]string{
		"plugin.toml": mf,
		"main.lua":    "function OnSyncComplete(s) kasas.log(\"info\", \"done\") end\n",
		"README.md":   "# reader\n",
	})
	r := CheckPlugin(dir, DefaultLimits())
	if !r.OK() {
		t.Fatalf("expected pass, got %v", r.Errors())
	}
	if r.CapabilityTier != TierReadOnly {
		t.Fatalf("expected read-only tier, got %q", r.CapabilityTier)
	}
}

// TestUIPagePluginPasses checks a plugin with a dashboard page passes the gate,
// stays read-only-tiered when its other capabilities are reads, and surfaces the
// ui-page advisory finding for reviewers.
func TestUIPagePluginPasses(t *testing.T) {
	mf := `
name="pager"
version="1.0.0"
description="shows a dashboard page"
author="a"
runtime="lua"
license="MIT"
homepage="https://example.com"
hooks=["OnPageRender","OnPageAction","OnUninstall"]
capabilities=["transactions:read","ui:page"]
[ui]
title="Pager"
icon="chart"
`
	dir := writePlugin(t, "pager", map[string]string{
		"plugin.toml": mf,
		"main.lua": `function OnPageRender(req) return { blocks = { { type = "text", text = "hi" } } } end
function OnPageAction(req) return OnPageRender(req) end
function OnUninstall() end
`,
		"README.md": "# pager\n",
	})
	r := CheckPlugin(dir, DefaultLimits())
	if !r.OK() {
		t.Fatalf("expected pass, got %v", r.Errors())
	}
	if r.CapabilityTier != TierReadOnly {
		t.Fatalf("ui:page alone must not raise the tier; got %q", r.CapabilityTier)
	}
	if !hasCode(r, "cap.ui_page") {
		t.Fatalf("expected the cap.ui_page advisory finding, got %v", r.Findings)
	}
}

// TestUIBlockWithoutHookRejected checks the manifest unit rule is enforced at
// the gate (a sidebar entry with no renderer can never be listed).
func TestUIBlockWithoutHookRejected(t *testing.T) {
	mf := `
name="pager"
version="1.0.0"
description="shows a dashboard page"
author="a"
runtime="lua"
license="MIT"
homepage="https://example.com"
hooks=["OnTransactionCreate","OnUninstall"]
capabilities=["ui:page"]
[ui]
title="Pager"
`
	dir := writePlugin(t, "pager", map[string]string{
		"plugin.toml": mf,
		"main.lua":    "function OnTransactionCreate(t) end\nfunction OnUninstall() end\n",
		"README.md":   "# pager\n",
	})
	r := CheckPlugin(dir, DefaultLimits())
	if r.OK() || !hasCode(r, "manifest.invalid") {
		t.Fatalf("expected manifest.invalid, got %v", r.Findings)
	}
}
