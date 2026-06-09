package gate

import (
	"bytes"
	"context"
	"fmt"
	"sort"
	"strings"

	"github.com/tetratelabs/wazero"
	"github.com/tetratelabs/wazero/api"

	"github.com/paulmeier/kasas-plugins/internal/manifest"
)

// The host-side ABI surface (kasas internal/plugins/wasm.go, ABI v1): a plugin
// is a WASI preview1 reactor whose only importable modules are the
// capability-checked "kasas" host facade and WASI itself (which the host
// instantiates with no preopened directories and no sockets). At load the host
// runs the reactor initializer and the describe handshake, and requires one
// export per declared hook.
const (
	wasmHostModule     = "kasas"
	wasmWASIModule     = "wasi_snapshot_preview1"
	wasmExportInit     = "_initialize"
	wasmExportDescribe = "kasas_describe"
)

// wasmMagic/wasmVersion are the first 8 bytes of every core WebAssembly module:
// "\0asm" plus binary format version 1, the only format wazero (and therefore
// the host) runs. A component-model binary or any other blob differs here.
var (
	wasmMagic   = []byte{0x00, 0x61, 0x73, 0x6d}
	wasmVersion = []byte{0x01, 0x00, 0x00, 0x00}
)

// gateWasm statically analyzes a compiled wasm entrypoint. There is no source to
// scan, so the assurances are structural, mirroring what the host's load would
// enforce anyway (a guaranteed load failure is rejected here instead of after
// install):
//
//   - the file really is a core WebAssembly module (magic + version) and it
//     compiles with the SAME wazero the host uses — the binary analogue of the
//     JS pass's esbuild transpile, proving "it loads" at submission time;
//   - every import names the kasas host module or WASI preview1, the only two
//     modules the host instantiates. This is the wasm analogue of the lua/js
//     banned-identifier scans: the module physically cannot reference anything
//     but the capability-checked facade and the host's preopen-less WASI;
//   - the exports the host's load handshake demands are present with the ABI's
//     signatures: the reactor initializer, kasas_describe, one (i32 payload_len)
//     export per declared hook, and the linear memory the ABI moves JSON through
//     (a wasip1 reactor exports it as "memory").
//
// Compiling executes no guest code — start functions run only at instantiation,
// which the gate never performs — so the gate's never-runs-plugin-code property
// holds for wasm too.
func gateWasm(r *Report, m manifest.Manifest, bin []byte) {
	file := m.Entrypoint
	if len(bin) < 8 || !bytes.Equal(bin[:4], wasmMagic) {
		r.addErrorFile("wasm.not_wasm",
			`entrypoint is not a WebAssembly module (missing the "\0asm" magic); a wasm plugin ships the compiled module, not source`, file)
		return
	}
	if !bytes.Equal(bin[4:8], wasmVersion) {
		r.addErrorFile("wasm.not_wasm",
			"entrypoint is not a core wasm module (binary version is not 1); build a wasip1 core module, not e.g. a component-model binary", file)
		return
	}

	ctx := context.Background()
	// The interpreter config validates a module exactly like the host's compiling
	// runtime but skips native codegen, which the gate doesn't need.
	rt := wazero.NewRuntimeWithConfig(ctx, wazero.NewRuntimeConfigInterpreter())
	defer rt.Close(ctx)

	compiled, err := rt.CompileModule(ctx, bin)
	if err != nil {
		r.addErrorFile("wasm.compile_failed",
			fmt.Sprintf("module does not compile with the wazero the host uses: %v", err), file)
		return
	}

	// Imports: anything outside the two host-provided modules cannot resolve at
	// instantiation, so the plugin could never load.
	seen := map[string]bool{}
	for _, f := range compiled.ImportedFunctions() {
		mod, name, _ := f.Import()
		if mod == wasmHostModule || mod == wasmWASIModule || seen[mod+"\x00"+name] {
			continue
		}
		seen[mod+"\x00"+name] = true
		r.addErrorFile("wasm.unknown_import",
			fmt.Sprintf("imports %q from module %q; the host provides only the %q host module and %q", name, mod, wasmHostModule, wasmWASIModule), file)
	}
	for _, mem := range compiled.ImportedMemories() {
		mod, name, _ := mem.Import()
		r.addErrorFile("wasm.unknown_import",
			fmt.Sprintf("imports memory %q from module %q; the host provides no memory — the module must define and export its own", name, mod), file)
	}

	// Exports: what the host's load handshake will demand. Required hooks are the
	// declared ones; the initializer and describe export are unconditional.
	required := map[string][]api.ValueType{
		wasmExportInit:     {},
		wasmExportDescribe: {},
	}
	for _, h := range m.Hooks {
		required[string(h)] = []api.ValueType{api.ValueTypeI32} // (payload_len u32)
	}
	exports := compiled.ExportedFunctions()
	var missing, badSig []string
	for name, params := range required {
		def, ok := exports[name]
		if !ok {
			missing = append(missing, name)
			continue
		}
		if !valueTypesEqual(def.ParamTypes(), params) {
			badSig = append(badSig, fmt.Sprintf("%s (want params %s)", name, wasmSigString(params)))
		}
	}
	if len(missing) > 0 {
		sort.Strings(missing)
		r.addErrorFile("wasm.missing_export",
			fmt.Sprintf("module does not export %s — the host requires the WASI reactor initializer, the kasas_describe handshake, and one export per declared hook (for Go: build with -buildmode=c-shared against the kasas plugin SDK)", strings.Join(missing, ", ")), file)
	}
	if len(badSig) > 0 {
		sort.Strings(badSig)
		r.addErrorFile("wasm.bad_signature",
			fmt.Sprintf("exports with a non-ABI signature: %s — hooks take a single i32 payload length; %s and %s take no parameters", strings.Join(badSig, ", "), wasmExportInit, wasmExportDescribe), file)
	}
	if len(compiled.ExportedMemories()) == 0 {
		r.addErrorFile("wasm.no_memory",
			"module exports no linear memory; the ABI moves JSON through guest memory, so the host could never call it (a wasip1 reactor exports its memory as \"memory\")", file)
	}
}

func valueTypesEqual(a, b []api.ValueType) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

func wasmSigString(ts []api.ValueType) string {
	if len(ts) == 0 {
		return "()"
	}
	parts := make([]string, len(ts))
	for i, t := range ts {
		parts[i] = api.ValueTypeName(t)
	}
	return "(" + strings.Join(parts, ", ") + ")"
}
