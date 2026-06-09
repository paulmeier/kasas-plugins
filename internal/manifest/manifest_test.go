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
