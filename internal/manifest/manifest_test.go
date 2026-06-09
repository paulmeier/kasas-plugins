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
hooks        = ["OnTransactionCreate"]
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
	src := `
name        = "p"
version     = "0.0.1"
description = "twelve chars."
author      = "a"
runtime     = "js"
license     = "MIT"
homepage    = "https://example.com"
hooks        = ["OnSyncComplete"]
`
	m, err := Parse([]byte(src))
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if m.Entrypoint != "main.js" {
		t.Fatalf("expected entrypoint to default to main.js, got %q", m.Entrypoint)
	}
}

func TestParseRejects(t *testing.T) {
	base := map[string]string{
		"name": `"good"`, "version": `"1.0.0"`, "description": `"a good description"`,
		"author": `"a"`, "runtime": `"lua"`, "license": `"MIT"`,
		"homepage": `"https://example.com"`, "hooks": `["OnTransactionCreate"]`,
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
		{"unknown hook", build(map[string]string{"hooks": `["OnWhatever"]`})},
		{"no hooks", build(map[string]string{"hooks": `[]`})},
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
