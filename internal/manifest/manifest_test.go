package manifest

import "testing"

const validManifest = `
name        = "good-plugin"
version     = "1.2.3"
description = "A perfectly reasonable example plugin."
author      = "someone"
runtime     = "lua"
entrypoint  = "main.lua"
license     = "MIT"
homepage    = "https://example.com/good-plugin"
hooks        = ["OnTransactionCreate", "OnUninstall"]
capabilities = ["transactions:read"]
`

func TestParseValid(t *testing.T) {
	m, err := Parse([]byte(validManifest))
	if err != nil {
		t.Fatalf("expected valid manifest, got error: %v", err)
	}
	if m.Name != "good-plugin" || m.Version != "1.2.3" || m.Runtime != "lua" {
		t.Fatalf("unexpected parse result: %+v", m)
	}
}

func TestParseDefaultsEntrypoint(t *testing.T) {
	for runtime, want := range map[string]string{
		"lua":  "main.lua",
		"js":   "main.js",
		"wasm": "main.wasm",
	} {
		t.Run(runtime, func(t *testing.T) {
			src := `
name        = "p"
version     = "0.0.1"
description = "twelve chars."
author      = "a"
runtime     = "` + runtime + `"
license     = "MIT"
homepage    = "https://example.com"
hooks        = ["OnSyncComplete", "OnUninstall"]
`
			m, err := Parse([]byte(src))
			if err != nil {
				t.Fatalf("parse: %v", err)
			}
			if m.Entrypoint != want {
				t.Fatalf("expected entrypoint to default to %q, got %q", want, m.Entrypoint)
			}
		})
	}
}

func TestParseWasmManifest(t *testing.T) {
	src := `
name        = "go-budget"
version     = "1.0.0"
description = "A plugin compiled to WebAssembly."
author      = "someone"
runtime     = "wasm"
entrypoint  = "main.wasm"
license     = "MIT"
homepage    = "https://example.com/go-budget"
hooks        = ["OnTransactionCreate", "OnUninstall"]
capabilities = ["transactions:read", "labels:write"]
`
	m, err := Parse([]byte(src))
	if err != nil {
		t.Fatalf("expected valid wasm manifest, got error: %v", err)
	}
	if m.Runtime != RuntimeWasm || m.Entrypoint != "main.wasm" {
		t.Fatalf("unexpected parse result: %+v", m)
	}
}

func TestParseRejects(t *testing.T) {
	base := map[string]string{
		"name": `"good"`, "version": `"1.0.0"`, "description": `"a good description"`,
		"author": `"a"`, "runtime": `"lua"`, "license": `"MIT"`,
		"homepage": `"https://example.com"`, "hooks": `["OnTransactionCreate", "OnUninstall"]`,
	}
	build := func(overrides map[string]string, drop ...string) string {
		m := map[string]string{}
		for k, v := range base {
			m[k] = v
		}
		for k, v := range overrides {
			m[k] = v
		}
		for _, d := range drop {
			delete(m, d)
		}
		s := ""
		for k, v := range m {
			s += k + " = " + v + "\n"
		}
		return s
	}

	cases := []struct {
		name string
		src  string
	}{
		{"bad name", build(map[string]string{"name": `"Bad Name"`})},
		{"bad version", build(map[string]string{"version": `"v1"`})},
		{"unknown runtime", build(map[string]string{"runtime": `"python"`})},
		{"unknown hook", build(map[string]string{"hooks": `["OnWhatever", "OnUninstall"]`})},
		{"no hooks", build(map[string]string{"hooks": `[]`})},
		{"missing uninstall hook", build(map[string]string{"hooks": `["OnTransactionCreate"]`})},
		{"unknown capability", build(map[string]string{"capabilities": `["root:everything"]`})},
		{"short description", build(map[string]string{"description": `"short"`})},
		{"disallowed license", build(map[string]string{"license": `"GPL-3.0"`})},
		{"missing license", build(nil, "license")},
		{"missing homepage", build(nil, "homepage")},
		{"http homepage", build(map[string]string{"homepage": `"http://example.com"`})},
		{"missing author", build(nil, "author")},
		{"path entrypoint", build(map[string]string{"entrypoint": `"../evil.lua"`})},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if _, err := Parse([]byte(tc.src)); err == nil {
				t.Fatalf("expected rejection for %q, got none", tc.name)
			}
		})
	}
}

const uiManifestBase = `
name        = "pager"
version     = "1.0.0"
description = "A plugin with a dashboard page."
author      = "someone"
runtime     = "lua"
license     = "MIT"
homepage    = "https://example.com/pager"
hooks        = ["OnPageRender", "OnPageAction", "OnUninstall"]
capabilities = ["ui:page"]
`

func TestParseUIValid(t *testing.T) {
	m, err := Parse([]byte(uiManifestBase + "[ui]\ntitle = \"Pager\"\nicon = \"chart\"\n"))
	if err != nil {
		t.Fatalf("expected valid ui manifest, got error: %v", err)
	}
	if m.UI == nil || m.UI.Title != "Pager" || m.UI.Icon != "chart" {
		t.Fatalf("unexpected ui block: %+v", m.UI)
	}
}

func TestParseUIIconDefaults(t *testing.T) {
	m, err := Parse([]byte(uiManifestBase + "[ui]\ntitle = \"Pager\"\n"))
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if m.UI.Icon != "puzzle" {
		t.Fatalf("expected icon to default to puzzle, got %q", m.UI.Icon)
	}
}

func TestParseUIRejects(t *testing.T) {
	cases := []struct {
		name string
		src  string
	}{
		{"page hook without ui block", `
name = "pager"
version = "1.0.0"
description = "A plugin with a dashboard page."
author = "someone"
runtime = "lua"
license = "MIT"
homepage = "https://example.com"
hooks = ["OnPageRender", "OnUninstall"]
capabilities = ["ui:page"]
`},
		{"ui capability without ui block", `
name = "pager"
version = "1.0.0"
description = "A plugin with a dashboard page."
author = "someone"
runtime = "lua"
license = "MIT"
homepage = "https://example.com"
hooks = ["OnTransactionCreate", "OnUninstall"]
capabilities = ["ui:page"]
`},
		{"ui block without render hook", `
name = "pager"
version = "1.0.0"
description = "A plugin with a dashboard page."
author = "someone"
runtime = "lua"
license = "MIT"
homepage = "https://example.com"
hooks = ["OnTransactionCreate", "OnUninstall"]
capabilities = ["ui:page"]
[ui]
title = "Pager"
`},
		{"ui block without capability", `
name = "pager"
version = "1.0.0"
description = "A plugin with a dashboard page."
author = "someone"
runtime = "lua"
license = "MIT"
homepage = "https://example.com"
hooks = ["OnPageRender", "OnUninstall"]
[ui]
title = "Pager"
`},
		{"missing title", uiManifestBase + "[ui]\nicon = \"chart\"\n"},
		{"unknown icon", uiManifestBase + "[ui]\ntitle = \"Pager\"\nicon = \"sparkles\"\n"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if _, err := Parse([]byte(tc.src)); err == nil {
				t.Fatalf("expected rejection for %q, got none", tc.name)
			}
		})
	}
}

// --- [build] block (ADR 0001) ---

const jsBundleManifestHead = `
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
`

func TestParseValidBuildBlock(t *testing.T) {
	src := jsBundleManifestHead + `
[build]
repository = "https://github.com/example/bundled"
ref        = "1111111111111111111111111111111111111111"
entry      = "src/main.ts"
`
	m, err := Parse([]byte(src))
	if err != nil {
		t.Fatalf("expected valid build block, got: %v", err)
	}
	if m.Build == nil || m.Build.Entry != "src/main.ts" {
		t.Fatalf("build block not parsed: %+v", m.Build)
	}
}

func TestParseRejectsBadBuildBlocks(t *testing.T) {
	cases := map[string]string{
		"non-https repo": `
[build]
repository = "git@github.com:example/bundled.git"
ref        = "1111111111111111111111111111111111111111"
entry      = "src/main.ts"
`,
		"branch ref not pinned": `
[build]
repository = "https://github.com/example/bundled"
ref        = "main"
entry      = "src/main.ts"
`,
		"short ref": `
[build]
repository = "https://github.com/example/bundled"
ref        = "1111111"
entry      = "src/main.ts"
`,
		"entry escapes repo": `
[build]
repository = "https://github.com/example/bundled"
ref        = "1111111111111111111111111111111111111111"
entry      = "../secrets.ts"
`,
		"entry not source": `
[build]
repository = "https://github.com/example/bundled"
ref        = "1111111111111111111111111111111111111111"
entry      = "dist/main.wasm"
`,
	}
	for name, block := range cases {
		t.Run(name, func(t *testing.T) {
			if _, err := Parse([]byte(jsBundleManifestHead + block)); err == nil {
				t.Fatalf("expected rejection for %q", name)
			}
		})
	}
}

func TestParseRejectsBuildOnNonJSRuntime(t *testing.T) {
	src := `
name        = "bundled"
version     = "1.0.0"
description = "a bundled example plugin"
author      = "a"
runtime     = "lua"
license     = "MIT"
homepage    = "https://example.com"
hooks        = ["OnTransactionCreate", "OnUninstall"]
capabilities = ["labels:write"]

[build]
repository = "https://github.com/example/bundled"
ref        = "1111111111111111111111111111111111111111"
entry      = "src/main.ts"
`
	if _, err := Parse([]byte(src)); err == nil {
		t.Fatal("expected [build] on a lua plugin to be rejected")
	}
}

// --- [net] block + net:fetch capability (ADR 0002/0003) ---

const netManifestBase = `
name        = "enricher"
version     = "1.0.0"
description = "Enriches transactions from a merchant API."
author      = "someone"
runtime     = "lua"
license     = "MIT"
homepage    = "https://example.com/enricher"
hooks        = ["OnTransactionCreate", "OnUninstall"]
capabilities = ["transactions:read", "net:fetch"]
`

func TestParseNetValid(t *testing.T) {
	m, err := Parse([]byte(netManifestBase + "[net]\nallow = [\"API.Merchant.Example.com\", \"paperless.lan\"]\n"))
	if err != nil {
		t.Fatalf("expected valid net manifest, got error: %v", err)
	}
	// Hosts are lowercased and de-duplicated, preserving order.
	want := []string{"api.merchant.example.com", "paperless.lan"}
	if len(m.Net.Allow) != len(want) {
		t.Fatalf("unexpected allow list: %+v", m.Net.Allow)
	}
	for i, h := range want {
		if m.Net.Allow[i] != h {
			t.Fatalf("allow[%d] = %q, want %q", i, m.Net.Allow[i], h)
		}
	}
}

func TestParseNetRejects(t *testing.T) {
	cases := []struct {
		name string
		src  string
	}{
		{"net:fetch without [net] block", netManifestBase},
		{"net:fetch with empty allow", netManifestBase + "[net]\nallow = []\n"},
		{"[net] block without net:fetch capability", `
name = "enricher"
version = "1.0.0"
description = "Enriches transactions from a merchant API."
author = "someone"
runtime = "lua"
license = "MIT"
homepage = "https://example.com"
hooks = ["OnTransactionCreate", "OnUninstall"]
capabilities = ["transactions:read"]
[net]
allow = ["api.example.com"]
`},
		{"host with scheme", netManifestBase + "[net]\nallow = [\"https://api.example.com\"]\n"},
		{"host with port", netManifestBase + "[net]\nallow = [\"api.example.com:443\"]\n"},
		{"host with path", netManifestBase + "[net]\nallow = [\"api.example.com/x\"]\n"},
		{"empty host", netManifestBase + "[net]\nallow = [\"\"]\n"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if _, err := Parse([]byte(tc.src)); err == nil {
				t.Fatalf("expected rejection for %q, got none", tc.name)
			}
		})
	}
}
