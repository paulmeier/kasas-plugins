package gate

import (
	"os"
	"path/filepath"
	"testing"
)

// --- minimal wasm binary assembly ---
//
// The wasm gate works on compiled binaries, so the tests assemble real (tiny)
// core modules by hand instead of checking in build artifacts. Only the pieces
// the gate inspects are modeled: function imports, exported functions with
// either the ()->() or (i32)->() signature, an exported memory, and an ignored
// custom section to inflate the file size.

type wasmExport struct {
	name    string
	payload bool // true: (i32)->() like a hook; false: ()->() like _initialize
}

type wasmModule struct {
	imports [][2]string  // (module, name) function imports, all typed ()->()
	exports []wasmExport // each backed by its own empty function body
	memory  bool         // define and export a one-page memory named "memory"
	padding int          // bytes of custom-section padding (valid, ignored)
}

func uleb(n int) []byte {
	var out []byte
	for {
		b := byte(n & 0x7f)
		n >>= 7
		if n != 0 {
			b |= 0x80
		}
		out = append(out, b)
		if n == 0 {
			return out
		}
	}
}

func wasmName(s string) []byte { return append(uleb(len(s)), s...) }

func wasmSection(id byte, payload []byte) []byte {
	return append([]byte{id}, append(uleb(len(payload)), payload...)...)
}

func (wm wasmModule) bytes() []byte {
	b := []byte{0x00, 0x61, 0x73, 0x6d, 0x01, 0x00, 0x00, 0x00} // "\0asm" v1

	// Type section: type 0 = ()->(), type 1 = (i32)->().
	b = append(b, wasmSection(1, append(uleb(2),
		append([]byte{0x60, 0x00, 0x00}, 0x60, 0x01, 0x7f, 0x00)...))...)

	if len(wm.imports) > 0 {
		p := uleb(len(wm.imports))
		for _, im := range wm.imports {
			p = append(p, wasmName(im[0])...)
			p = append(p, wasmName(im[1])...)
			p = append(p, 0x00) // kind: function
			p = append(p, uleb(0)...)
		}
		b = append(b, wasmSection(2, p)...)
	}

	// Function section: one defined function per export.
	p := uleb(len(wm.exports))
	for _, e := range wm.exports {
		ty := 0
		if e.payload {
			ty = 1
		}
		p = append(p, uleb(ty)...)
	}
	b = append(b, wasmSection(3, p)...)

	if wm.memory {
		b = append(b, wasmSection(5, []byte{0x01, 0x00, 0x01})...) // 1 memory, min 1 page
	}

	// Export section: the functions (indices follow the imports) and the memory.
	n := len(wm.exports)
	if wm.memory {
		n++
	}
	p = uleb(n)
	for i, e := range wm.exports {
		p = append(p, wasmName(e.name)...)
		p = append(p, 0x00) // kind: function
		p = append(p, uleb(len(wm.imports)+i)...)
	}
	if wm.memory {
		p = append(p, wasmName("memory")...)
		p = append(p, 0x02, 0x00) // kind: memory, index 0
	}
	b = append(b, wasmSection(7, p)...)

	// Code section: empty bodies.
	p = uleb(len(wm.exports))
	for range wm.exports {
		p = append(p, uleb(2)...)
		p = append(p, 0x00, 0x0b) // no locals, end
	}
	b = append(b, wasmSection(10, p)...)

	if wm.padding > 0 {
		b = append(b, wasmSection(0, append(wasmName("pad"), make([]byte, wm.padding)...))...)
	}
	return b
}

// goodWasmModule satisfies everything the gate demands of a plugin declaring
// OnTransactionCreate and OnUninstall, importing only host-provided modules.
func goodWasmModule() wasmModule {
	return wasmModule{
		imports: [][2]string{{"kasas", "host_call"}, {"wasi_snapshot_preview1", "fd_write"}},
		exports: []wasmExport{
			{name: "_initialize"},
			{name: "kasas_describe"},
			{name: "OnTransactionCreate", payload: true},
			{name: "OnUninstall", payload: true},
		},
		memory: true,
	}
}

const wasmManifest = `
name        = "gobudget"
version     = "1.0.0"
description = "labels transactions, compiled to wasm"
author      = "a"
runtime     = "wasm"
license     = "MIT"
homepage    = "https://example.com"
hooks        = ["OnTransactionCreate", "OnUninstall"]
capabilities = ["transactions:read", "labels:write"]
`

func writeWasmPlugin(t *testing.T, wm wasmModule) string {
	t.Helper()
	return writePlugin(t, "gobudget", map[string]string{
		"plugin.toml": wasmManifest,
		"main.wasm":   string(wm.bytes()), // the manifest's defaulted entrypoint
		"README.md":   "# gobudget\n",
	})
}

func TestGoodWasmPasses(t *testing.T) {
	r := CheckPlugin(writeWasmPlugin(t, goodWasmModule()), DefaultLimits())
	if !r.OK() {
		t.Fatalf("expected pass, got findings: %v", r.Errors())
	}
	if r.CapabilityTier != TierWrite {
		t.Fatalf("expected write tier, got %q", r.CapabilityTier)
	}
}

// TestWasmEntrypointOwnBudget checks the size carve-out: a compiled module is
// exempt from the per-file and directory-total limits sized for source text,
// and bounded by MaxWasmEntrypointBytes instead.
func TestWasmEntrypointOwnBudget(t *testing.T) {
	wm := goodWasmModule()
	wm.padding = 300 * 1024 // over MaxFileBytes (256 KiB) and total isn't charged

	dir := writeWasmPlugin(t, wm)
	r := CheckPlugin(dir, DefaultLimits())
	if !r.OK() {
		t.Fatalf("expected a 300 KiB module to pass under the wasm budget, got: %v", r.Errors())
	}

	small := DefaultLimits()
	small.MaxWasmEntrypointBytes = 1024
	r = CheckPlugin(dir, small)
	if r.OK() || !hasCode(r, "wasm.entrypoint_too_large") {
		t.Fatalf("expected wasm.entrypoint_too_large, got %v", r.Findings)
	}
	if hasCode(r, "tree.file_too_large") || hasCode(r, "tree.too_large") {
		t.Fatalf("wasm entrypoint must not double-trip the text-file limits: %v", r.Findings)
	}
}

func TestWasmNotWasm(t *testing.T) {
	dir := writePlugin(t, "gobudget", map[string]string{
		"plugin.toml": wasmManifest,
		"main.wasm":   "package main // source instead of a compiled module\n",
		"README.md":   "# gobudget\n",
	})
	r := CheckPlugin(dir, DefaultLimits())
	if r.OK() || !hasCode(r, "wasm.not_wasm") {
		t.Fatalf("expected wasm.not_wasm, got %v", r.Findings)
	}
}

func TestWasmUnknownImport(t *testing.T) {
	wm := goodWasmModule()
	wm.imports = append(wm.imports, [2]string{"env", "open_socket"})
	r := CheckPlugin(writeWasmPlugin(t, wm), DefaultLimits())
	if r.OK() || !hasCode(r, "wasm.unknown_import") {
		t.Fatalf("expected wasm.unknown_import, got %v", r.Findings)
	}
}

func TestWasmMissingHookExport(t *testing.T) {
	wm := goodWasmModule()
	wm.exports = wm.exports[:len(wm.exports)-2] // drop both hook exports
	r := CheckPlugin(writeWasmPlugin(t, wm), DefaultLimits())
	if r.OK() || !hasCode(r, "wasm.missing_export") {
		t.Fatalf("expected wasm.missing_export, got %v", r.Findings)
	}
}

func TestWasmBadSignature(t *testing.T) {
	wm := goodWasmModule()
	for i := range wm.exports {
		if wm.exports[i].name == "OnTransactionCreate" {
			wm.exports[i].payload = false // hooks must take the i32 payload length
		}
	}
	r := CheckPlugin(writeWasmPlugin(t, wm), DefaultLimits())
	if r.OK() || !hasCode(r, "wasm.bad_signature") {
		t.Fatalf("expected wasm.bad_signature, got %v", r.Findings)
	}
}

func TestWasmNoMemory(t *testing.T) {
	wm := goodWasmModule()
	wm.memory = false
	r := CheckPlugin(writeWasmPlugin(t, wm), DefaultLimits())
	if r.OK() || !hasCode(r, "wasm.no_memory") {
		t.Fatalf("expected wasm.no_memory, got %v", r.Findings)
	}
}

// TestWasmExtraModuleRejected checks the single-entrypoint rule applies to
// compiled modules like it does to source files.
func TestWasmExtraModuleRejected(t *testing.T) {
	dir := writeWasmPlugin(t, goodWasmModule())
	if err := os.WriteFile(filepath.Join(dir, "helper.wasm"), goodWasmModule().bytes(), 0o644); err != nil {
		t.Fatal(err)
	}
	r := CheckPlugin(dir, DefaultLimits())
	if r.OK() || !hasCode(r, "tree.extra_source") {
		t.Fatalf("expected tree.extra_source for a second .wasm module, got %v", r.Findings)
	}
}
