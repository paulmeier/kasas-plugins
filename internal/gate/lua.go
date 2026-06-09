package gate

import (
	"fmt"
	"regexp"
	"strings"
)

// luaBanned maps a forbidden Lua identifier to the reason it is rejected. The host
// opens only the safe standard libraries (base, table, string, math) and removes
// every code-loading / environment escape hatch before a plugin runs. Referencing
// any of these names therefore cannot work at runtime — it would be a nil-index
// error inside the host — so the gate rejects it at submission time instead of
// letting it fail silently after install. Banning here also keeps the reviewable
// intent of the source honest: a plugin that "reaches for" io or os is not a plugin
// this registry lists.
var luaBanned = map[string]string{
	"os":             "operating-system access (time/exec/exit/env) is not available to plugins",
	"io":             "filesystem and stdio access is not available to plugins",
	"package":        "the module system is disabled; a plugin is a single self-contained file",
	"require":        "module loading is disabled; a plugin is a single self-contained file",
	"dofile":         "loading external code is disabled",
	"loadfile":       "loading external code is disabled",
	"loadstring":     "dynamic code loading is disabled",
	"load":           "dynamic code loading is disabled",
	"loadlib":        "native library loading is disabled",
	"module":         "the module system is disabled",
	"newproxy":       "metatable proxy creation is disabled",
	"collectgarbage": "garbage-collector control is not available to plugins",
	"setfenv":        "environment manipulation is disabled",
	"getfenv":        "environment manipulation is disabled",
	"debug":          "the debug library (which can defeat the sandbox) is not available",
	"_G":             "direct global-environment manipulation is not allowed",
	"_ENV":           "direct environment manipulation is not allowed",
}

// gateLua statically analyzes a Lua entrypoint: it strips comments and string
// literals (to avoid flagging the banned words when they appear as data or prose)
// and then rejects any reference to a forbidden identifier. luacheck in CI provides
// the complementary syntax/lint pass; this pass is the security-relevant one and is
// self-contained so it always runs.
func gateLua(r *Report, file string, src []byte) {
	scrubbed := stripLua(string(src))
	for ident, reason := range luaBanned {
		re := identRE(ident)
		if re.MatchString(scrubbed) {
			r.addErrorFile("lua.banned_identifier",
				fmt.Sprintf("use of %q is not allowed: %s", ident, reason), file)
		}
	}
}

// identReCache memoizes the per-identifier word-boundary regexes. Lua identifiers
// can contain underscores, so a plain \b is not enough (it would match inside
// my_os); we require the identifier not to be flanked by identifier characters.
var identReCache = map[string]*regexp.Regexp{}

func identRE(ident string) *regexp.Regexp {
	if re, ok := identReCache[ident]; ok {
		return re
	}
	re := regexp.MustCompile(`(^|[^A-Za-z0-9_])` + regexp.QuoteMeta(ident) + `($|[^A-Za-z0-9_])`)
	identReCache[ident] = re
	return re
}

// stripLua removes Lua comments and string literals so the banned-identifier scan
// sees only code. It is intentionally conservative: when a construct is ambiguous it
// errs toward keeping code visible (so a real reference is never hidden) rather than
// toward hiding it. Handles -- line comments, --[[ ]] / --[==[ ]==] long comments,
// '…' and "…" strings (with escapes), and [[ ]] long strings.
func stripLua(s string) string {
	var b strings.Builder
	b.Grow(len(s))
	i := 0
	n := len(s)
	for i < n {
		c := s[i]
		// Comments.
		if c == '-' && i+1 < n && s[i+1] == '-' {
			i += 2
			if lvl, ok := longBracketLevel(s, i); ok {
				i = skipLongBracket(s, i, lvl)
				b.WriteByte(' ')
				continue
			}
			// line comment to end of line
			for i < n && s[i] != '\n' {
				i++
			}
			continue
		}
		// Long-bracket strings [[ ]] / [=[ ]=].
		if c == '[' {
			if lvl, ok := longBracketLevel(s, i); ok {
				i = skipLongBracket(s, i, lvl)
				b.WriteByte('"')
				continue
			}
		}
		// Quoted strings.
		if c == '\'' || c == '"' {
			i++
			for i < n && s[i] != c {
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
			b.WriteByte('"')
			continue
		}
		b.WriteByte(c)
		i++
	}
	return b.String()
}

// longBracketLevel reports whether position i begins a Lua long bracket [[ , [=[ ,
// [==[ … and at what level (number of '='). i must point at '['.
func longBracketLevel(s string, i int) (int, bool) {
	if i >= len(s) || s[i] != '[' {
		return 0, false
	}
	j := i + 1
	lvl := 0
	for j < len(s) && s[j] == '=' {
		lvl++
		j++
	}
	if j < len(s) && s[j] == '[' {
		return lvl, true
	}
	return 0, false
}

// skipLongBracket returns the index just past a closing long bracket of the given
// level, starting at the opening '['. If unterminated, returns len(s).
func skipLongBracket(s string, i, lvl int) int {
	close := "]" + strings.Repeat("=", lvl) + "]"
	start := i + 2 + lvl // past the opening bracket
	idx := strings.Index(s[start:], close)
	if idx < 0 {
		return len(s)
	}
	return start + idx + len(close)
}
