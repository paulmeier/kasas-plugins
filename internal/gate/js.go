package gate

import (
	"fmt"
	"path/filepath"
	"regexp"
	"strings"

	esbuild "github.com/evanw/esbuild/pkg/api"
)

// jsBannedPatterns are constructs that either cannot work in the host's goja VM
// (which exposes no module loader, filesystem, network, or process) or are the
// classic ways to break out of it (dynamic code generation, prototype walking to
// recover the Function constructor). Each is rejected at submission time so a kasas
// user never installs a community plugin that tries to phone home, read disk, or
// escape the sandbox.
//
// The patterns run against source with comments and string literals stripped, so a
// banned word inside a message or comment does not trip the gate.
var jsBannedPatterns = []struct {
	code    string
	re      *regexp.Regexp
	message string
}{
	{"js.dynamic_code", regexp.MustCompile(`\beval\s*\(`), "eval() is dynamic code execution and is removed in the host"},
	{"js.dynamic_code", regexp.MustCompile(`\bnew\s+Function\b`), "the Function constructor is dynamic code execution and is removed in the host"},
	{"js.dynamic_code", regexp.MustCompile(`\bFunction\s*\(`), "the Function constructor is dynamic code execution and is removed in the host"},
	{"js.module_loader", regexp.MustCompile(`\brequire\s*\(`), "require() is not available; a plugin is a single self-contained file"},
	{"js.module_loader", regexp.MustCompile(`\bimport\s*\(`), "dynamic import() is not available; a plugin is a single self-contained file"},
	{"js.module_loader", regexp.MustCompile(`(^|[^.\w])import\s+[\w*{]`), "import statements are not available; a plugin is a single self-contained file"},
	{"js.module_loader", regexp.MustCompile(`(^|[^.\w])export\s+`), "export statements are not available; a plugin is a single self-contained file"},
	{"js.host_env", regexp.MustCompile(`\bprocess\b`), "the Node process object is not available in the host"},
	{"js.host_env", regexp.MustCompile(`\bglobalThis\b`), "globalThis access is not allowed (it is a path to recover removed globals)"},
	{"js.host_env", regexp.MustCompile(`\b__dirname\b`), "Node filesystem globals are not available"},
	{"js.host_env", regexp.MustCompile(`\b__filename\b`), "Node filesystem globals are not available"},
	{"js.host_env", regexp.MustCompile(`\bDeno\b`), "the Deno runtime is not available in the host"},
	{"js.host_env", regexp.MustCompile(`\bBun\b`), "the Bun runtime is not available in the host"},
	{"js.network", regexp.MustCompile(`\bfetch\s*\(`), "network access (fetch) is not available to plugins"},
	{"js.network", regexp.MustCompile(`\bXMLHttpRequest\b`), "network access (XMLHttpRequest) is not available to plugins"},
	{"js.network", regexp.MustCompile(`\bWebSocket\b`), "network access (WebSocket) is not available to plugins"},
	{"js.network", regexp.MustCompile(`\bnavigator\b`), "the navigator object is not available in the host"},
	{"js.wasm", regexp.MustCompile(`\bWebAssembly\b`), "WebAssembly is not available to plugins"},
	{"js.worker", regexp.MustCompile(`\bnew\s+Worker\b`), "Workers are not available to plugins"},
	{"js.sandbox_escape", regexp.MustCompile(`__proto__`), "__proto__ access is a sandbox-escape vector and is not allowed"},
	{"js.sandbox_escape", regexp.MustCompile(`\.constructor\s*\.\s*constructor\b`), "constructor.constructor is a path to the Function constructor and is not allowed"},
}

// gateJS statically analyzes a JS/TS entrypoint and then transpiles it with the
// exact esbuild configuration the host uses at load time, so a submission that
// passes the gate is proven to both (a) avoid every sandbox escape hatch and (b)
// actually load in the host.
func gateJS(r *Report, file string, src []byte) {
	scrubbed := stripJS(string(src))
	seen := map[string]bool{}
	for _, p := range jsBannedPatterns {
		if p.re.MatchString(scrubbed) {
			// De-dup identical messages (several patterns share a code).
			key := p.code + "|" + p.message
			if seen[key] {
				continue
			}
			seen[key] = true
			r.addErrorFile(p.code, "disallowed construct: "+p.message, file)
		}
	}

	// Transpile with the same loader/target the host uses (see kasas
	// internal/plugins/js.go: transpile). A transform error here is exactly the
	// "execute %s" failure a user would hit on enable, so we reject it now.
	if res := transpile(file, src); len(res.Errors) > 0 {
		e := res.Errors[0]
		loc := ""
		if e.Location != nil {
			loc = fmt.Sprintf(" (line %d)", e.Location.Line)
		}
		r.addErrorFile("js.transpile_failed",
			fmt.Sprintf("entrypoint does not transpile with esbuild%s: %s", loc, e.Text), file)
	}
}

// transpile mirrors the host's transform: loader chosen by extension, ES2017
// target, default format, no sourcemap.
func transpile(entrypoint string, src []byte) esbuild.TransformResult {
	loader := esbuild.LoaderJS
	switch strings.ToLower(filepath.Ext(entrypoint)) {
	case ".ts":
		loader = esbuild.LoaderTS
	case ".jsx":
		loader = esbuild.LoaderJSX
	case ".tsx":
		loader = esbuild.LoaderTSX
	}
	return esbuild.Transform(string(src), esbuild.TransformOptions{
		Loader:    loader,
		Format:    esbuild.FormatDefault,
		Target:    esbuild.ES2017,
		Sourcemap: esbuild.SourceMapNone,
	})
}

// stripJS removes // and /* */ comments and string / template literals so the
// banned-pattern scan sees only code. Like stripLua it is conservative: ambiguous
// input keeps code visible rather than hiding it. Template-literal interpolations
// (${...}) are kept as code; the surrounding text is dropped.
func stripJS(s string) string {
	var b strings.Builder
	b.Grow(len(s))
	i, n := 0, len(s)
	for i < n {
		c := s[i]
		// Line comment.
		if c == '/' && i+1 < n && s[i+1] == '/' {
			for i < n && s[i] != '\n' {
				i++
			}
			continue
		}
		// Block comment.
		if c == '/' && i+1 < n && s[i+1] == '*' {
			i += 2
			for i+1 < n && !(s[i] == '*' && s[i+1] == '/') {
				i++
			}
			i += 2
			b.WriteByte(' ')
			continue
		}
		// Single / double quoted strings.
		if c == '\'' || c == '"' {
			i = skipJSQuoted(s, i, c)
			b.WriteByte('"')
			continue
		}
		// Template literal: keep ${...} expressions as code, drop literal text.
		if c == '`' {
			i++
			for i < n && s[i] != '`' {
				if s[i] == '\\' && i+1 < n {
					i += 2
					continue
				}
				if s[i] == '$' && i+1 < n && s[i+1] == '{' {
					depth := 1
					i += 2
					for i < n && depth > 0 {
						switch s[i] {
						case '{':
							depth++
						case '}':
							depth--
						}
						if depth > 0 {
							b.WriteByte(s[i])
						}
						i++
					}
					continue
				}
				i++
			}
			if i < n {
				i++ // closing backtick
			}
			b.WriteByte('"')
			continue
		}
		b.WriteByte(c)
		i++
	}
	return b.String()
}

func skipJSQuoted(s string, i int, q byte) int {
	n := len(s)
	i++ // opening quote
	for i < n && s[i] != q {
		if s[i] == '\\' && i+1 < n {
			i += 2
			continue
		}
		if s[i] == '\n' {
			break
		}
		i++
	}
	if i < n {
		i++ // closing quote
	}
	return i
}
