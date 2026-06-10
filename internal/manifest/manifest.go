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
	// HookUninstall is a lifecycle hook (no triggering event) the host runs when a
	// plugin is uninstalled, so the plugin can undo anything it created. Every
	// community plugin must declare and implement it — cleanup is the plugin's
	// responsibility, and a listed plugin must be cleanly removable.
	HookUninstall Hook = "OnUninstall"
	// HookPageRender / HookPageAction back a plugin's dashboard page (the [ui]
	// block): request-driven hooks that RETURN a declarative page document the
	// host validates and the dashboard renders. They come as a unit with [ui]
	// and the ui:page capability (see validateUI).
	HookPageRender Hook = "OnPageRender"
	HookPageAction Hook = "OnPageAction"
)

var knownHooks = map[Hook]bool{
	HookTransactionCreate: true,
	HookTransactionUpdate: true,
	HookSyncComplete:      true,
	HookUninstall:         true,
	HookPageRender:        true,
	HookPageAction:        true,
}

// Capability is a permission the host grants and enforces. These mirror the host's
// plugins.Capability constants. Risk tiering for review lives in the gate package;
// this package only validates that a requested capability is one the host knows.
type Capability string

const (
	CapTransactionsRead Capability = "transactions:read"
	CapLabelsWrite      Capability = "labels:write"
	CapExtensionsWrite  Capability = "extensions:write"
	// CapUIPage lets a plugin expose a dashboard page (a sidebar entry plus a
	// declaratively rendered page). It mutates nothing itself, but it puts
	// plugin-authored content in front of the user, so the gate surfaces it.
	CapUIPage Capability = "ui:page"
)

var knownCapabilities = map[Capability]bool{
	CapTransactionsRead: true,
	CapLabelsWrite:      true,
	CapExtensionsWrite:  true,
	CapUIPage:           true,
}

// knownUIIcons is the curated sidebar icon set, mirroring the host
// (kasas/internal/plugins/manifest.go). Icons are picked by NAME only — the
// dashboard owns the SVG bytes — so a plugin can never inject markup through
// its sidebar entry.
var knownUIIcons = map[string]bool{
	"bell":     true,
	"calendar": true,
	"chart":    true,
	"coin":     true,
	"flag":     true,
	"gauge":    true,
	"heart":    true,
	"list":     true,
	"puzzle":   true,
	"star":     true,
}

// maxUITitleLen bounds the sidebar label, matching the host.
const maxUITitleLen = 40

// Runtime values accepted by the host. Each gates a different runtime-specific
// security pass in the gate package.
const (
	RuntimeLua  = "lua"
	RuntimeJS   = "js"
	RuntimeWasm = "wasm"
)

var knownRuntimes = map[string]bool{
	RuntimeLua:  true,
	RuntimeJS:   true,
	RuntimeWasm: true,
}

// DefaultEntrypoints is the per-runtime fallback entrypoint file when a manifest
// omits `entrypoint`, matching the host. For lua/js it is a source file; for wasm
// it is the compiled module.
var DefaultEntrypoints = map[string]string{
	RuntimeLua:  "main.lua",
	RuntimeJS:   "main.js",
	RuntimeWasm: "main.wasm",
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

	// UI, when present, declares the plugin's dashboard page (a sidebar entry
	// routing to /ext/<name>, rendered by the OnPageRender hook). Read by the
	// host; mirrored here so the gate enforces the same unit rule.
	UI *UIManifest `toml:"ui"`

	// Build, when present, declares that the entrypoint is a dependency bundle
	// produced from a linked source repository (ADR 0001). It records exactly
	// where that source lives and at what immutable commit, so the gate can
	// reproduce the bundle and prove the committed artifact is that source. The
	// host ignores this block: a bundled plugin drops into plugins.dir as one
	// self-contained file like any other. Registry-only.
	Build *BuildManifest `toml:"build"`

	// Registry-only metadata (ignored by the host).
	License  string `toml:"license"`
	Homepage string `toml:"homepage"`
}

// BuildManifest is the optional [build] block (ADR 0001). It turns the
// "single self-contained entrypoint" into a *reproducible* artifact: the
// author develops against third-party packages and ships a pre-bundled
// entrypoint, and this block points at the source the gate re-bundles to
// verify it byte-for-byte. Only JS/TS plugins use it (WASM links its
// dependencies statically by construction; Lua bundling is out of scope).
type BuildManifest struct {
	// Repository is the public https git URL of the plugin's source.
	Repository string `toml:"repository"`
	// Ref is the immutable full commit SHA the bundle was built from. A branch
	// or tag is rejected: only a pinned commit makes the build reproducible.
	Ref string `toml:"ref"`
	// Entry is the bundler entry point inside the repository (e.g. "src/main.ts"),
	// a clean repo-relative path to a JS/TS source file.
	Entry string `toml:"entry"`
}

// UIManifest is the optional [ui] block: the sidebar label and a curated icon
// name (defaulting to "puzzle").
type UIManifest struct {
	Title string `toml:"title"`
	Icon  string `toml:"icon"`
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
	declaresUninstall := false
	for _, h := range m.Hooks {
		if !knownHooks[h] {
			return fmt.Errorf("unknown hook %q (known: %s)", h, strings.Join(KnownHookNames(), ", "))
		}
		if h == HookUninstall {
			declaresUninstall = true
		}
	}
	// Every community plugin must clean up after itself: the host runs OnUninstall at
	// uninstall time, so a listed plugin is required to declare and implement it.
	if !declaresUninstall {
		return fmt.Errorf("plugin must declare the %q hook (and implement it) so it can clean up when uninstalled", HookUninstall)
	}
	for _, c := range m.Capabilities {
		if !knownCapabilities[c] {
			return fmt.Errorf("unknown capability %q (known: %s)", c, strings.Join(KnownCapabilityNames(), ", "))
		}
	}
	if err := m.validateUI(); err != nil {
		return err
	}
	if err := m.validateBuild(); err != nil {
		return err
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

// validateUI enforces the host's dashboard-page unit rule: the [ui] block, the
// OnPageRender hook, and the ui:page capability come together or not at all, so
// a listed plugin can never ship a sidebar entry without a renderer (or page
// hooks the host would refuse to load).
func (m *Manifest) validateUI() error {
	declares := func(h Hook) bool {
		for _, x := range m.Hooks {
			if x == h {
				return true
			}
		}
		return false
	}
	requests := func(c Capability) bool {
		for _, x := range m.Capabilities {
			if x == c {
				return true
			}
		}
		return false
	}

	if m.UI == nil {
		if declares(HookPageRender) || declares(HookPageAction) {
			return fmt.Errorf("hooks %s/%s require a [ui] block", HookPageRender, HookPageAction)
		}
		if requests(CapUIPage) {
			return fmt.Errorf("capability %q requires a [ui] block", CapUIPage)
		}
		return nil
	}

	m.UI.Title = strings.TrimSpace(m.UI.Title)
	m.UI.Icon = strings.TrimSpace(m.UI.Icon)
	if m.UI.Title == "" || len(m.UI.Title) > maxUITitleLen {
		return fmt.Errorf("ui.title is required and must be at most %d characters", maxUITitleLen)
	}
	if m.UI.Icon == "" {
		m.UI.Icon = "puzzle"
	}
	if !knownUIIcons[m.UI.Icon] {
		return fmt.Errorf("unknown ui.icon %q (supported: %s)", m.UI.Icon, KnownUIIconNames())
	}
	if !declares(HookPageRender) {
		return fmt.Errorf("a [ui] block requires the %s hook", HookPageRender)
	}
	if !requests(CapUIPage) {
		return fmt.Errorf("a [ui] block requires the %q capability", CapUIPage)
	}
	return nil
}

// commitRE matches a full git commit SHA (40 lowercase hex chars). The build
// ref must be a pinned commit — a branch or tag would let the upstream source
// change under a fixed artifact, defeating the whole point of reproducing it.
var commitRE = regexp.MustCompile(`^[0-9a-f]{40}$`)

// bundleEntryExts are the source extensions a bundler entry may have. The entry
// is JS/TS source the gate bundles; a compiled or data file is never an entry.
var bundleEntryExts = map[string]bool{
	".js": true, ".ts": true, ".jsx": true, ".tsx": true, ".mjs": true, ".cjs": true,
}

// validateBuild checks the optional [build] block (ADR 0001). The block makes
// the entrypoint a reproducible dependency bundle, so its fields must pin the
// source precisely: a JS/TS-only runtime, an https git repository, an immutable
// commit, and a clean in-repo entry path. Everything here is verified again at
// gate time by re-bundling; these are the cheap structural rules that let a bad
// block fail fast with a clear message.
func (m *Manifest) validateBuild() error {
	if m.Build == nil {
		return nil
	}
	b := m.Build
	b.Repository = strings.TrimSpace(b.Repository)
	b.Ref = strings.TrimSpace(b.Ref)
	b.Entry = strings.TrimSpace(b.Entry)

	// Bundling applies only to JS/TS. WASM statically links its dependencies into
	// the compiled module already; Lua bundling is deliberately out of scope.
	if m.Runtime != RuntimeJS {
		return fmt.Errorf("[build] is only valid for runtime %q (a %s plugin bundles dependencies differently)", RuntimeJS, m.Runtime)
	}
	if err := validateGitRepository(b.Repository); err != nil {
		return fmt.Errorf("build.repository: %w", err)
	}
	if !commitRE.MatchString(b.Ref) {
		return fmt.Errorf("build.ref %q must be a full 40-character commit SHA (a branch or tag is not reproducible)", b.Ref)
	}
	if b.Entry == "" {
		return fmt.Errorf("build.entry is required (the bundler entry point inside the repository, e.g. \"src/main.ts\")")
	}
	if err := validateCleanRelPath(b.Entry); err != nil {
		return fmt.Errorf("build.entry: %w", err)
	}
	if ext := strings.ToLower(pathExt(b.Entry)); !bundleEntryExts[ext] {
		return fmt.Errorf("build.entry %q must be a JS/TS source file (one of .js, .ts, .jsx, .tsx, .mjs, .cjs)", b.Entry)
	}
	return nil
}

// validateGitRepository accepts an https git URL (the only transport the gate
// clones from for a public submission).
func validateGitRepository(repo string) error {
	if repo == "" {
		return fmt.Errorf("a source repository URL is required so the gate can reproduce the bundle")
	}
	u, err := url.Parse(repo)
	if err != nil {
		return fmt.Errorf("%q is not a valid URL: %w", repo, err)
	}
	if u.Scheme != "https" {
		return fmt.Errorf("%q must use https", repo)
	}
	if u.Host == "" {
		return fmt.Errorf("%q must include a host", repo)
	}
	return nil
}

// validateCleanRelPath rejects absolute paths, parent-directory escapes, and
// backslashes, so an entry can only point inside the cloned repository.
func validateCleanRelPath(p string) error {
	if strings.ContainsRune(p, '\\') {
		return fmt.Errorf("path %q must use forward slashes", p)
	}
	if strings.HasPrefix(p, "/") {
		return fmt.Errorf("path %q must be relative to the repository root", p)
	}
	for _, seg := range strings.Split(p, "/") {
		if seg == ".." {
			return fmt.Errorf("path %q must not escape the repository with \"..\"", p)
		}
	}
	return nil
}

// pathExt returns the extension of a forward-slash path (filepath.Ext is
// OS-separator aware; build entries are always forward-slash).
func pathExt(p string) string {
	if i := strings.LastIndexByte(p, '.'); i >= 0 && i > strings.LastIndexByte(p, '/') {
		return p[i:]
	}
	return ""
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

// EntrypointExt returns the conventional entrypoint extension for the manifest's
// runtime (e.g. ".lua"; ".wasm" is a compiled module, not source). It is
// informational; the actual entrypoint filename is authoritative.
func EntrypointExt(runtime string) string {
	switch runtime {
	case RuntimeLua:
		return ".lua"
	case RuntimeJS:
		return ".js"
	case RuntimeWasm:
		return ".wasm"
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

// KnownUIIconNames returns the curated icon names as a sorted, comma-joined
// string for error messages and docs.
func KnownUIIconNames() string {
	out := make([]string, 0, len(knownUIIcons))
	for n := range knownUIIcons {
		out = append(out, n)
	}
	sort.Strings(out)
	return strings.Join(out, ", ")
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
