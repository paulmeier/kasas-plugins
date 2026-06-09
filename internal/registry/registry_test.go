package registry

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/paulmeier/kasas-plugins/internal/gate"
)

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

func writePlugin(t *testing.T, root, name string, files map[string]string) {
	t.Helper()
	dir := filepath.Join(root, name)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	for rel, content := range files {
		if err := os.WriteFile(filepath.Join(dir, rel), []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}
}

func TestBuildIndex(t *testing.T) {
	plugins := t.TempDir()
	writePlugin(t, plugins, "coffee", map[string]string{
		"plugin.toml": luaManifest,
		"main.lua":    "function OnTransactionCreate(t) kasas.apply_labels(t.id, {x=\"y\"}) end\n",
		"README.md":   "# coffee\n",
	})

	idx, failures, err := Build("https://example.com/repo", plugins, "plugins", gate.DefaultLimits())
	if err != nil {
		t.Fatal(err)
	}
	if len(failures) != 0 {
		t.Fatalf("unexpected gate failures: %v", failures)
	}
	if len(idx.Plugins) != 1 {
		t.Fatalf("expected 1 plugin, got %d", len(idx.Plugins))
	}
	p := idx.Plugins[0]
	if p.Name != "coffee" || p.CapabilityTier != string(gate.TierWrite) {
		t.Fatalf("unexpected plugin entry: %+v", p)
	}
	if p.Path != "plugins/coffee" {
		t.Fatalf("expected repo-relative path, got %q", p.Path)
	}
	if !strings.HasPrefix(p.ContentHash, "sha256:") {
		t.Fatalf("expected content hash, got %q", p.ContentHash)
	}
	if len(p.Files) != 3 {
		t.Fatalf("expected 3 files, got %d", len(p.Files))
	}
	// Files must be sorted for deterministic output.
	for i := 1; i < len(p.Files); i++ {
		if p.Files[i-1].Path > p.Files[i].Path {
			t.Fatalf("files not sorted: %v", p.Files)
		}
	}
}

func TestBuildExcludesFailing(t *testing.T) {
	plugins := t.TempDir()
	// A plugin that uses os.* must not be listed.
	writePlugin(t, plugins, "evil", map[string]string{
		"plugin.toml": strings.Replace(luaManifest, `"coffee"`, `"evil"`, 1),
		"main.lua":    "function OnTransactionCreate(t) os.exit() end\n",
		"README.md":   "# evil\n",
	})
	idx, failures, err := Build("https://example.com/repo", plugins, "plugins", gate.DefaultLimits())
	if err != nil {
		t.Fatal(err)
	}
	if len(idx.Plugins) != 0 {
		t.Fatalf("failing plugin must not be listed, got %d", len(idx.Plugins))
	}
	if len(failures) != 1 {
		t.Fatalf("expected 1 failure report, got %d", len(failures))
	}
}

func TestMarshalDeterministic(t *testing.T) {
	plugins := t.TempDir()
	writePlugin(t, plugins, "coffee", map[string]string{
		"plugin.toml": luaManifest,
		"main.lua":    "function OnTransactionCreate(t) kasas.log(\"info\",\"x\") end\n",
		"README.md":   "# coffee\n",
	})
	idx, _, err := Build("https://example.com/repo", plugins, "plugins", gate.DefaultLimits())
	if err != nil {
		t.Fatal(err)
	}
	a, err := idx.Marshal()
	if err != nil {
		t.Fatal(err)
	}
	b, err := idx.Marshal()
	if err != nil {
		t.Fatal(err)
	}
	if string(a) != string(b) {
		t.Fatal("marshal is not deterministic")
	}
	if a[len(a)-1] != '\n' {
		t.Fatal("expected trailing newline")
	}
}

// TestBuildIndexCarriesUI checks a page-bearing plugin's index entry includes
// the ui metadata so the marketplace can badge it.
func TestBuildIndexCarriesUI(t *testing.T) {
	plugins := t.TempDir()
	mf := `
name="pager"
version="1.0.0"
description="shows a dashboard page"
author="a"
runtime="lua"
license="MIT"
homepage="https://example.com"
hooks=["OnPageRender","OnUninstall"]
capabilities=["ui:page"]
[ui]
title="Pager"
icon="chart"
`
	writePlugin(t, plugins, "pager", map[string]string{
		"plugin.toml": mf,
		"main.lua":    "function OnPageRender(req) return { blocks = { { type = \"text\", text = \"hi\" } } } end\nfunction OnUninstall() end\n",
		"README.md":   "# pager\n",
	})
	idx, failures, err := Build("https://example.com/repo", plugins, "plugins", gate.DefaultLimits())
	if err != nil {
		t.Fatal(err)
	}
	if len(failures) != 0 {
		t.Fatalf("unexpected gate failures: %v", failures)
	}
	if len(idx.Plugins) != 1 {
		t.Fatalf("expected 1 plugin, got %d", len(idx.Plugins))
	}
	ui := idx.Plugins[0].UI
	if ui == nil || ui.Title != "Pager" || ui.Icon != "chart" {
		t.Fatalf("expected ui metadata in the index, got %+v", ui)
	}
}
