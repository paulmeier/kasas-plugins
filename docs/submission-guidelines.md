# Submission guidelines (the gate, rule by rule)

Every plugin must clear the **submission gate** before it is listed. The gate is
implemented in `internal/gate` and run by `kasas-plugins validate`; this document
lists every rule and its finding code so you can read a CI failure and fix it fast.

Run it locally before opening a PR:

```sh
go run ./cmd/kasas-plugins validate plugins/<name>
```

A finding is either an **error** (blocks listing) or a **warning** (advisory,
surfaced to reviewers). A plugin is listed only if it has zero errors.

## Structural rules (all runtimes)

| Code | Rule |
| ---- | ---- |
| `manifest.missing` | `plugin.toml` must exist and be readable. |
| `manifest.invalid` | `plugin.toml` must parse and satisfy every field rule in [plugin-spec.md](plugin-spec.md), **including declaring the required `OnUninstall` hook**. |
| `manifest.name_mismatch` | `name` must equal the directory name. |
| `tree.entrypoint_missing` | The declared `entrypoint` file must exist. |
| `tree.readme_missing` | A `README.md` must be present. |
| `tree.extra_source` | Only the entrypoint may be a file of the runtime's language (`.lua`/`.js`/`.wasm`) — the host loads no other modules. |
| `tree.nested_dir` | A plugin is a single flat directory; no subdirectories (this also blocks `node_modules`, `.git`, …). |
| `tree.symlink` | No symlinks. |
| `tree.disallowed_file` | Only the entrypoint, `README.md`/`*.md`, `*.txt`, `*.toml`, `*.json`, and `*.d.ts` are allowed. No binaries/archives/scripts (a wasm plugin's compiled entrypoint is the one exception). |
| `tree.file_too_large` | No single file over 256 KiB (a wasm entrypoint, or a bundled JS entrypoint, has its own budget instead — see the WASM and `[build]` rules). |
| `tree.too_large` | The whole plugin must be under 1 MiB (not counting a wasm entrypoint, which has its own budget). |
| `tree.too_many_files` | At most 32 files. |

## Lua rules (`runtime = "lua"`)

| Code | Rule |
| ---- | ---- |
| `lua.banned_identifier` | No reference to a stripped/escape-hatch identifier: `os`, `io`, `package`, `require`, `dofile`, `loadfile`, `loadstring`, `load`, `loadlib`, `module`, `newproxy`, `collectgarbage`, `setfenv`, `getfenv`, `debug`, `_G`, `_ENV`. |

The scan strips comments and string literals first, so a banned word that appears
only inside a string or comment does **not** trip the gate — but using it as code
does. `luacheck` runs in CI as a complementary syntax/lint pass (see `.luacheckrc`).

Do all work through the `kasas` table. The host opens only `base`, `table`,
`string`, and `math`.

## JavaScript / TypeScript rules (`runtime = "js"`)

| Code | Rule |
| ---- | ---- |
| `js.dynamic_code` | No `eval(...)`, `new Function`, or `Function(...)`. |
| `js.module_loader` | No `require(...)`, `import` (static or dynamic), or `export`. |
| `js.host_env` | No `process`, `globalThis`, `__dirname`, `__filename`, `Deno`, `Bun`. |
| `js.network` | No global `fetch(...)`, `XMLHttpRequest`, `WebSocket`, `navigator`. The **member** call `kasas.fetch(...)` is allowed — it is the sanctioned, allowlisted host method gated by the `net:fetch` capability (see [Network access](#network-access-netfetch)); a bare global `fetch()` is not available and is rejected. |
| `js.wasm` | No `WebAssembly`. |
| `js.worker` | No `new Worker`. |
| `js.sandbox_escape` | No `__proto__` or `constructor.constructor`. |
| `js.transpile_failed` | The entrypoint must transpile with esbuild (same loader/target the host uses: loader by extension, ES2017). |

As with Lua, comments and string/template literals are stripped before scanning, so
a banned word in a log message is fine. Write `.ts` if you want types — esbuild
strips them at load; no build step or `node_modules`.

The escape-hatch scan above applies to **hand-written** entrypoints. A **bundled**
plugin (one with a `[build]` block, see below) ships a machine-generated artifact
that legitimately contains some of those constructs, so the scan is skipped for it
and the guarantee comes from reproducible-build verification instead. The
host-load transpile check (`js.transpile_failed`) still runs.

## Bundled dependencies (`[build]`, ADR 0001)

A JS/TS plugin may depend on third-party libraries if it **bundles them, at
submission time, into the single entrypoint**. Add a `[build]` block pointing at the
public source the bundle was produced from, at a pinned commit; the gate reproduces
the bundle and proves the committed entrypoint matches it byte-for-byte.

```toml
runtime    = "js"
entrypoint = "main.js"   # the committed BUNDLE

[build]
repository = "https://github.com/you/your-plugin"  # https git URL of the source
ref        = "<full 40-char commit SHA>"            # immutable pin (no branch/tag)
entry      = "src/main.ts"                          # bundler entry inside the repo
```

Your source must `export` its hook functions (`export function OnTransactionCreate
…`), and if it pulls npm packages it must commit a `package-lock.json` so the
install is deterministic. Produce the artifact with the registry tooling so it is
exactly what the gate will reproduce, then commit it:

```sh
go run ./cmd/kasas-plugins bundle plugins/<name>   # writes plugins/<name>/main.js
```

| Code | Rule |
| ---- | ---- |
| `build.not_verified` | *(warning)* `validate` ran without `--verify-build` (no git/npm/network), so the bundle's provenance was not checked. CI runs `validate --verify-build`, where this becomes a real check. |
| `build.clone_failed` | The `[build].repository` could not be cloned. |
| `build.checkout_failed` | The `[build].ref` commit was not found in the repository. |
| `build.no_lockfile` | The source has a `package.json` but no `package-lock.json`/`npm-shrinkwrap.json`; a committed lockfile is required for a deterministic install. |
| `build.install_failed` | `npm ci --ignore-scripts` failed. |
| `build.bundle_failed` | esbuild could not bundle the entry (e.g. an unresolved import, or a dependency on a Node builtin like `fs`). |
| `build.mismatch` | The committed entrypoint is not byte-for-byte the reproduced bundle. Regenerate it with `kasas-plugins bundle` and commit the result. |
| `js.bundle_too_large` | A bundled entrypoint is over the 2 MiB bundle budget (separate from the 256 KiB per-file limit, which still applies to hand-written JS). |

Build verification needs `git`, `npm`, and network, so it is **off by default**
locally (`validate` emits `build.not_verified`) and **on in CI**
(`validate --verify-build`).

## WASM rules (`runtime = "wasm"`)

The entrypoint is a **compiled** WASI preview1 reactor module (for Go:
`GOOS=wasip1 GOARCH=wasm go build -buildmode=c-shared -o main.wasm .` against the
[kasas plugin SDK](https://github.com/paulmeier/kasas/blob/main/docs/features/plugins.md#go-wasm)).
There is no source to scan, so the gate verifies the module's structure instead —
each rule mirrors a check the host performs at load, and the module is **compiled,
never instantiated or run**:

| Code | Rule |
| ---- | ---- |
| `wasm.not_wasm` | The entrypoint must be a core WebAssembly module: the `"\0asm"` magic plus binary version 1 (not source, not a component-model binary). |
| `wasm.entrypoint_too_large` | The compiled module must be under **8 MiB** (its own budget, separate from the per-file/total limits above — a standard-toolchain Go build is ~3.5 MiB). |
| `wasm.compile_failed` | The module must compile with the same wazero the host uses, so "it loads" is proven at submission time. |
| `wasm.unknown_import` | Every import must come from the `kasas` host module or `wasi_snapshot_preview1` — the only modules the host instantiates (and its WASI has no preopened dirs or sockets). Anything else cannot resolve at load. |
| `wasm.missing_export` | The module must export `_initialize` (the reactor initializer), `kasas_describe` (the ABI handshake), and one function per declared hook. |
| `wasm.bad_signature` | Hook exports take a single `i32` payload length; `_initialize` and `kasas_describe` take no parameters (ABI v1). |
| `wasm.no_memory` | The module must export its linear memory (a wasip1 reactor exports it as `"memory"`) — the ABI moves JSON through it. |

The SDK satisfies all of these automatically. Because reviewers cannot read a
binary, wasm review leans on this structural verification, the capability
sandbox (identical to Lua/JS — every host call is capability-checked), and the
registry's content hashes; your README and `homepage` should link the module's
source.

## Network access (`net:fetch`)

By default a plugin has **no network access** — that is what makes it safe to
install, and the per-runtime scans above reject every network construct. A plugin
that needs to talk to a service (an importer, an enricher, a notifier) declares the
`net:fetch` capability and a `[net]` block listing the **exact hosts** it may reach
([ADR 0002](https://github.com/paulmeier/kasas/blob/main/docs/architecture/decisions/0002-plugin-network-capability.md)):

```toml
capabilities = ["transactions:read", "net:fetch"]

[net]
# Egress is default-deny. Bare hostnames only — no scheme, port, or path. The host
# refuses any host not listed here; the registry records this list in the index.
allow = ["api.merchant.example.com", "paperless.lan"]
```

The plugin never opens a socket — it calls the host method `kasas.fetch(...)`, and
the host performs the allowlisted, SSRF-checked, logged request. So in JS/TS the
**member** call `kasas.fetch(...)` is allowed even though a bare global `fetch()`
is not. `net:fetch` without a non-empty `[net].allow` (or a `[net]` block without
`net:fetch`) is a manifest error.

`net:fetch` puts a plugin in the **Connected** trust tier (see below): egress turns
a bug into exfiltration, so the declared allowlist is reviewed by a maintainer
before the plugin is listed.

## Trust tiers (ADR 0003)

The gate computes an explicit **trust tier** from a plugin's declared capabilities
and records it in the index as `tier`. It is the at-a-glance "how far am I extending
trust" signal the dashboard groups and badges by:

| Tier | Capabilities | Posture |
| ---- | ------------ | ------- |
| `verified` | `transactions:read`, `labels:write`, `extensions:write`, `ui:page` | Statically sealed — reaches neither network nor disk. Auto-listed once the checks above pass. The default. |
| `connected` | the above **+ `net:fetch`** (with a `[net].allow` list) | A maintainer must review the declared egress hosts before listing; the host enables it with the same admin opt-in and collects any private/LAN grants. |
| `unlisted` | anything outside the auto-listable set (e.g. a future broader capability) | **Not auto-listed** — the gate fails it, sideload only. |

The tier is a *function of* your capabilities, not something you set — declaring
`net:fetch` *is* asking for Connected and its heavier review.

## Capability findings (advisory)

| Code | Meaning |
| ---- | ------- |
| `cap.write_requested` | The plugin requests `labels:write` or `extensions:write`. It can modify user data; a maintainer must sign off, and your README must justify it. |
| `cap.none` | The plugin requests no capabilities — its hooks can only log. Confirm that's intended. |
| `tier.connected` | *(warning)* The plugin requests `net:fetch`, placing it in the Connected tier. The finding names the declared egress hosts a maintainer must review and sign off on. Does not fail the gate by itself. |
| `tier.unlisted` | *(error)* The plugin requests a capability outside the registry's auto-listable set, so it cannot be published — install it by sideloading only. |

The advisory findings never fail the gate by themselves; they drive human review and
are reflected in the registry's `capability_tier` and `tier`. `tier.unlisted` is the
exception — it is an error, because an unreviewed capability surface must not land in
the published index.

## After it passes

Regenerate and commit the catalog:

```sh
go run ./cmd/kasas-plugins index
```

CI runs `kasas-plugins index --check` and fails if `registry/index.json` is stale,
so the published catalog always matches the plugins in the repo.
