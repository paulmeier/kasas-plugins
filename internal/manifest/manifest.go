// Package manifest parses and validates a community plugin's plugin.toml.
//
// The manifest is the contract between a plugin author, this registry, and the
// kasas host. The host's own parser (kasas/internal/plugins/manifest.go) is the
// source of truth for what kasas will accept at load time; the rules here are a
// strict SUPERSET of it. A manifest that passes here is guaranteed to load in the
// host, but the converse is not true: the registry additionally demands the
// metadata a marketplace needs (a semver version, a real description and author, a
// recognized license, and a homepage) so that every listed plugin can be presented
// to a user with provenance, not just executed.
//
// Keeping the field shape identical to the host means a submitted plugin directory
// drops into `plugins.dir` unchanged — the registry-only fields (license,
// homepage) are extra TOML keys the host simply ignores.
package manifest

import (
	"fmt"
	"net/url"
	"regexp"
	"sort"
	"strings"

	"github.com/pelletier/go-toml/v2"
)

// Hook is a lifecycle point a plugin subscribes to. These mirror the host's
// plugins.Hook constants exactly; an unknown hook is rejected so a typo can never
// silently subscribe a plugin to nothing.
type Hook string

const (
	HookTransactionCreate Hook = "OnTransactionCreate"
	HookTransactionUpdate Hook = "OnTransactionUpdate"
	HookSyncComplete      Hook = "OnSyncComplete"
)

var knownHooks = map[Hook]bool{
	HookTransactionCreate: true,
	HookTransactionUpdate: true,
	HookSyncComplete:      true,
}

// Capability is a permission the host grants and enforces. These mirror the host's
// plugins.Capability constants. Risk tiering for review lives in the gate package;
// this package only validates that a requested capability is one the host knows.
type Capability string

const (
	CapTransactionsRead Capability = "transactions:read"
	CapLabelsWrite      Capability = "labels:write"
	CapExtensionsWrite  Capability = "extensions:write"
)

var knownCapabilities = map[Capability]bool{
	CapTransactionsRead: true,
	CapLabelsWrite:      true,
	CapExtensionsWrite:  true,
}

// Runtime values accepted by the host. Each gates a different language-specific
// security pass in the gate package.
const (
	RuntimeLua = "lua"
	RuntimeJS  = "js"
)

var knownRuntimes = map[string]bool{
	RuntimeLua: true,
	RuntimeJS:  true,
}

// DefaultEntrypoints is the per-runtime fallback source file when a manifest omits
// `entrypoint`, matching the host.
var DefaultEntrypoints = map[string]string{
	RuntimeLua: "main.lua",
	RuntimeJS:  "main.js",
}

// licenseAllowlist is the set of SPDX identifiers the registry accepts. A
// permissive, well-understood license is part of giving users confidence: it keeps
// the legal footing of installing community code clear. Extend deliberately.
var licenseAllowlist = map[string]bool{
	"MIT":          true,
	"Apache-2.0":   true,
	"BSD-2-Clause": true,
	"BSD-3-Clause": true,
	"ISC":          true,
	"MPL-2.0":      true,
	"Unlicense":    true,
	"0BSD":         true,
}

// nameRE constrains a plugin name to a filesystem- and identity-safe slug. The
// name must also equal the plugin's directory name (the loader enforces that).
var nameRE = regexp.MustCompile(`^[a-z0-9][a-z0-9_-]*$`)

// semverRE is a pragmatic semantic-version check (MAJOR.MINOR.PATCH with optional
// pre-release / build metadata). The marketplace orders and dedupes by version, so
// a parseable version is mandatory.
var semverRE = regexp.MustCompile(`^(0|[1-9]\d*)\.(0|[1-9]\d*)\.(0|[1-9]\d*)(?:-[0-9A-Za-z-.]+)?(?:\+[0-9A-Za-z-.]+)?$`)

const (
	maxDescriptionLen = 200
	minDescriptionLen = 12
	maxAuthorLen      = 120
)

// Manifest is the parsed plugin.toml. The first block is the exact shape the host
// reads; License and Homepage are registry-only metadata the host ignores.
type Manifest struct {
	Name         string         `toml:"name"`
	Version      string         `toml:"version"`
	Description  string         `toml:"description"`
	Author       string         `toml:"author"`
	Runtime      string         `toml:"runtime"`
	Entrypoint   string         `toml:"entrypoint"`
	Hooks        []Hook         `toml:"hooks"`
	Capabilities []Capability   `toml:"capabilities"`
	Config       map[string]any `toml:"config"`

	// Registry-only metadata (ignored by the host).
	License  string `toml:"license"`
	Homepage string `toml:"homepage"`
}

// Parse decodes and validates a plugin.toml under the registry's strict rules,
// normalizing a few fields (defaulting the entrypoint) so the result is ready to
// use. Unknown TOML keys are tolerated, matching the host.
func Parse(data []byte) (Manifest, error) {
	var m Manifest
	if err := toml.Unmarshal(data, &m); err != nil {
		return Manifest{}, fmt.Errorf("parse manifest: %w", err)
	}
	if err := m.normalizeAndValidate(); err != nil {
		return Manifest{}, err
	}
	return m, nil
}

func (m *Manifest) normalizeAndValidate() error {
	m.Name = strings.TrimSpace(m.Name)
	m.Version = strings.TrimSpace(m.Version)
	m.Description = strings.TrimSpace(m.Description)
	m.Author = strings.TrimSpace(m.Author)
	m.Runtime = strings.TrimSpace(m.Runtime)
	m.Entrypoint = strings.TrimSpace(m.Entrypoint)
	m.License = strings.TrimSpace(m.License)
	m.Homepage = strings.TrimSpace(m.Homepage)
	if m.Config == nil {
		m.Config = map[string]any{}
	}

	// --- Host-compatible fields (same rules the host enforces) ---
	if !nameRE.MatchString(m.Name) {
		return fmt.Errorf("invalid plugin name %q (must match %s)", m.Name, nameRE.String())
	}
	if !knownRuntimes[m.Runtime] {
		return fmt.Errorf("unsupported runtime %q (supported: %s)", m.Runtime, supportedRuntimes())
	}
	if m.Entrypoint == "" {
		m.Entrypoint = DefaultEntrypoints[m.Runtime]
	}
	if strings.ContainsAny(m.Entrypoint, `/\`) || m.Entrypoint == ".." {
		return fmt.Errorf("entrypoint %q must be a file name inside the plugin directory", m.Entrypoint)
	}
	if len(m.Hooks) == 0 {
		return fmt.Errorf("plugin must declare at least one hook")
	}
	for _, h := range m.Hooks {
		if !knownHooks[h] {
			return fmt.Errorf("unknown hook %q (known: %s)", h, strings.Join(KnownHookNames(), ", "))
		}
	}
	for _, c := range m.Capabilities {
		if !knownCapabilities[c] {
			return fmt.Errorf("unknown capability %q (known: %s)", c, strings.Join(KnownCapabilityNames(), ", "))
		}
	}

	// --- Registry-only metadata (stricter than the host) ---
	if !semverRE.MatchString(m.Version) {
		return fmt.Errorf("version %q is not a valid semantic version (MAJOR.MINOR.PATCH)", m.Version)
	}
	if l := len(m.Description); l < minDescriptionLen || l > maxDescriptionLen {
		return fmt.Errorf("description must be %d-%d characters (got %d)", minDescriptionLen, maxDescriptionLen, l)
	}
	if m.Author == "" {
		return fmt.Errorf("author is required")
	}
	if len(m.Author) > maxAuthorLen {
		return fmt.Errorf("author must be at most %d characters", maxAuthorLen)
	}
	if !licenseAllowlist[m.License] {
		return fmt.Errorf("license %q is not on the allowlist (allowed: %s)", m.License, AllowedLicenses())
	}
	if err := validateHomepage(m.Homepage); err != nil {
		return err
	}
	return nil
}

func validateHomepage(h string) error {
	if h == "" {
		return fmt.Errorf("homepage is required (a URL where users can read about the plugin)")
	}
	u, err := url.Parse(h)
	if err != nil {
		return fmt.Errorf("homepage %q is not a valid URL: %w", h, err)
	}
	if u.Scheme != "https" {
		return fmt.Errorf("homepage %q must use https", h)
	}
	if u.Host == "" {
		return fmt.Errorf("homepage %q must include a host", h)
	}
	return nil
}

// EntrypointExt returns the conventional source extension for the manifest's
// runtime (e.g. ".lua"). It is informational; the actual entrypoint filename is
// authoritative.
func EntrypointExt(runtime string) string {
	switch runtime {
	case RuntimeLua:
		return ".lua"
	case RuntimeJS:
		return ".js"
	default:
		return ""
	}
}

func supportedRuntimes() string {
	rs := make([]string, 0, len(knownRuntimes))
	for r := range knownRuntimes {
		rs = append(rs, r)
	}
	sort.Strings(rs)
	return strings.Join(rs, ", ")
}

// KnownHookNames returns the accepted hook names, sorted, for error messages and
// docs generation.
func KnownHookNames() []string {
	out := make([]string, 0, len(knownHooks))
	for h := range knownHooks {
		out = append(out, string(h))
	}
	sort.Strings(out)
	return out
}

// KnownCapabilityNames returns the accepted capability names, sorted.
func KnownCapabilityNames() []string {
	out := make([]string, 0, len(knownCapabilities))
	for c := range knownCapabilities {
		out = append(out, string(c))
	}
	sort.Strings(out)
	return out
}

// AllowedLicenses returns the SPDX allowlist as a sorted, comma-joined string.
func AllowedLicenses() string {
	out := make([]string, 0, len(licenseAllowlist))
	for l := range licenseAllowlist {
		out = append(out, l)
	}
	sort.Strings(out)
	return strings.Join(out, ", ")
}
