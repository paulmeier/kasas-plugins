# Plugin specification

A community plugin is a directory under `plugins/<name>/` containing a `plugin.toml`
manifest, a single entrypoint (a source file — or, for `runtime = "wasm"`, one
compiled WebAssembly module), and a `README.md`. This document is the
authoritative reference for the manifest and the registry's requirements.

The manifest shape is identical to the one the kasas host reads
([host docs](https://github.com/paulmeier/kasas/blob/main/docs/features/plugins.md)),
plus two registry-only keys (`license`, `homepage`) that the host ignores. So a
plugin that passes the registry also drops into a kasas `plugins.dir` unchanged.

## `plugin.toml`

```toml
name        = "coffee-budget"
version     = "0.1.0"
description = "Labels transactions whose description matches a keyword."
author      = "kasas community"
runtime     = "lua"
entrypoint  = "main.lua"

license  = "MIT"
homepage = "https://github.com/paulmeier/kasas-plugins/tree/main/plugins/coffee-budget"

hooks        = ["OnTransactionCreate", "OnTransactionUpdate"]
capabilities = ["transactions:read", "labels:write"]

[config]
keyword  = "coffee"
category = "food"
```

### Fields

| Field | Required | Rules |
| ----- | -------- | ----- |
| `name` | yes | Lowercase slug `^[a-z0-9][a-z0-9_-]*$`. **Must equal the directory name.** |
| `version` | yes | Semantic version `MAJOR.MINOR.PATCH` (optional `-prerelease`/`+build`). |
| `description` | yes | 12–200 characters. Shown in the dashboard catalog. |
| `author` | yes | Non-empty, ≤120 characters. |
| `runtime` | yes | `lua`, `js`, or `wasm`. |
| `entrypoint` | no | Filename inside the directory; no path separators. Defaults to `main.lua` / `main.js` / `main.wasm`. For TypeScript set `main.ts`. |
| `hooks` | yes | Non-empty; each one of the known hooks (below). |
| `capabilities` | no | Each one of the known capabilities (below). Declare only what you use. |
| `license` | yes | SPDX id from the allowlist (below). *Registry-only.* |
| `homepage` | yes | `https://` URL. *Registry-only.* |
| `[config]` | no | The plugin's configurable keys **and their defaults**, exposed as `kasas.config`. This block is also the schema of what an end user may override (see [Configuration](#configuration)). |
| `[ui]` | no | Declares a [dashboard page](#dashboard-pages-ui): `title` (≤40 chars) and an optional curated `icon` name. Requires the `OnPageRender` hook and the `ui:page` capability. |
| `[build]` | no | Marks the JS/TS entrypoint as a reproducible dependency **bundle** (ADR 0001) and links the source the gate re-bundles to verify it. *Registry-only.* See [Bundled dependencies](#bundled-dependencies-build). |

Unknown keys are tolerated (the host ignores them), but don't rely on them — they
carry no meaning to either the host or the registry.

## Hooks

A hook is a global function the host calls. **Event hooks** fire when the matching
event commits; the **`OnUninstall` lifecycle hook** runs once when the plugin is
uninstalled. Declare the ones you implement; the host requires a matching global
function for each.

| Hook | Kind | Fires on | Argument |
| ---- | ---- | -------- | -------- |
| `OnTransactionCreate` | event | `transaction.created` | the transaction |
| `OnTransactionUpdate` | event | `transaction.updated` | the transaction |
| `OnSyncComplete` | event | `sync.completed` | a sync summary |
| `OnUninstall` | lifecycle | uninstall (no event) | none |
| `OnPageRender` | request | viewing the plugin's [dashboard page](#dashboard-pages-ui) | a page request; **returns** a page document |
| `OnPageAction` | request | a button press on the page | a page request with `action`/`params`; **returns** a page document |

**`OnUninstall` is required.** Every plugin must declare and implement it so it can
clean up anything it created (labels, extensions) when removed — cleanup is the
plugin's responsibility, and kasas runs the hook at uninstall (best-effort: a
failing hook does not block removal). It runs with the plugin's granted
capabilities, so it can call the same `kasas` write methods it used to create data.

```lua
function OnUninstall()
  -- undo what this plugin created; runs with the plugin's granted capabilities
  kasas.log("info", "cleaning up")
end
```

## Capabilities

A capability is a permission the host enforces at the `kasas` API boundary.

| Capability | Grants | Tier |
| ---------- | ------ | ---- |
| `transactions:read` | `kasas.get_transaction`, `kasas.search` | read-only |
| `labels:write` | `kasas.apply_labels`, `kasas.remove_labels` | **write** |
| `extensions:write` | `kasas.set_extension`, `kasas.remove_extension` | **write** |
| `ui:page` | a [dashboard page](#dashboard-pages-ui) (sidebar entry + rendered page) | read-only* |

`kasas.log(...)`, `kasas.config`, and `kasas.set_config(...)` are always
available and need no capability (`set_config` only writes the plugin's own
configuration, never ledger data).
A **write** capability requires maintainer sign-off (see CONTRIBUTING.md).
\* `ui:page` itself mutates nothing — a page can only show what the plugin's other
capabilities let it read, and its actions write only through those capabilities —
but the gate surfaces it as a finding so reviewers look at what the page shows
and does.

## Dashboard pages (`[ui]`)

A plugin with a `[ui]` block contributes its **own sidebar entry and page** to
the kasas dashboard (at `/ext/<name>`), without shipping any frontend code. The
`OnPageRender` hook **returns a declarative page document** — a `title` plus a
list of typed blocks — which the host validates, bounds-checks, and renders with
its own components. Plugin strings are always treated as text (no markup, no
scripts). The `[ui]` block, the `OnPageRender` hook, and the `ui:page` capability
are a unit: a manifest with any of the three missing is rejected.

```toml
hooks        = ["OnPageRender", "OnPageAction", "OnUninstall"]
capabilities = ["transactions:read", "ui:page"]

[ui]
title = "Coffee Budget"  # sidebar label, ≤40 chars
icon  = "coin"           # one of: bell, calendar, chart, coin, flag, gauge,
                         #         heart, list, puzzle (default), star
```

```lua
function OnPageRender(req)   -- req.plugin, req.action (""), req.params
  return {
    title = "Coffee Budget",
    blocks = {
      { type = "stat", label = "Matches", value = 42, hint = "this month" },
      { type = "keyvalue", items = { { key = "Keyword", value = "coffee" } } },
      { type = "table", columns = { "Description", "Amount" }, rows = { { "Latte", "-4.50" } } },
      { type = "actions", actions = { { id = "relabel", label = "Re-label", style = "primary" } } },
    },
  }
end

function OnPageAction(req)   -- req.action == "relabel"
  -- mutate (within your granted capabilities), then re-render:
  return OnPageRender(req)
end
```

**Block types:** `heading`/`text` (`text`), `stat` (`label`, `value`, `hint?`),
`keyvalue` (`items` of `{key, value}`), `table` (`columns`, `rows`), `actions`
(`{id, label, style?, params?}` buttons routed to `OnPageAction`), `form`
(`{id, fields, submit_label?}` inputs routed to `OnPageAction` — see
[Configuration](#configuration)), `divider`.
Scalar values may be strings, numbers, or booleans (normalized to strings).
Documents are bounded (≤256 KiB, ≤200 blocks, ≤1000 table rows, ≤16 form
fields) and an unknown block type is a render error.

**Contract:** treat `OnPageRender` as read-only — it runs on every page view
(read-tier API). Put mutations in `OnPageAction` (write-tier API). Both run under
the host's per-hook timeout, serialized with your event hooks. See the
[host docs](https://github.com/paulmeier/kasas/blob/main/docs/features/plugins.md#dashboard-pages)
for the full schema and API surface.

## Configuration

The manifest's `[config]` block declares every configurable key with its
default, and the host treats it as the **schema** of what the end user may
change. Users configure an installed plugin two ways, both backed by the same
file on the kasas host:

1. **By hand**: editing `<plugins.dir>/<name>.config.toml` (a sibling of the
   plugin's directory, so updates never wipe it) and reloading the plugin.
2. **From your dashboard page**: render a `form` block and persist the
   submitted values in `OnPageAction` with `kasas.set_config` (JS:
   `kasas.setConfig`). The host validates each key against your `[config]`
   defaults, coerces values to the default's type (form params arrive as
   strings), **overwrites the config file**, updates the live `kasas.config`,
   and returns the new effective config.

```lua
function OnPageRender(req)
  return {
    blocks = {
      { type = "heading", text = "Settings" },
      { type = "form", id = "save_settings", submit_label = "Save",
        fields = {
          { name = "keyword",  label = "Keyword",  value = kasas.config.keyword },
          { name = "category", label = "Category", value = kasas.config.category },
        } },
    },
  }
end

function OnPageAction(req)
  if req.action == "save_settings" then
    kasas.set_config({ keyword = req.params.keyword, category = req.params.category })
  end
  return OnPageRender(req)
end
```

Form fields are `{name, label, kind?, value?, placeholder?, help?, options?}`
with `kind` one of `text` (default), `number`, `toggle` (submits
`"true"`/`"false"`), or `select` (requires `options`). On submit the host calls
`OnPageAction` with the form's `id` as `req.action` and every field's value in
`req.params`.

Design your plugin so every tunable lives in `[config]` with a sensible
default — that single block is what makes it configurable from both the TOML
file and your page.

## License allowlist

To keep the legal footing of installing community code clear, the manifest
`license` must be one of:

`0BSD`, `Apache-2.0`, `BSD-2-Clause`, `BSD-3-Clause`, `ISC`, `MIT`, `MPL-2.0`,
`Unlicense`.

To propose another, open an issue.

## Bundled dependencies (`[build]`) {#bundled-dependencies-build}

By default a JS/TS plugin is a single hand-written file. Per
[ADR 0001](https://github.com/paulmeier/kasas/blob/main/docs/architecture/decisions/0001-plugin-dependency-bundling.md)
it may instead depend on third-party libraries, **provided they are bundled into
the single committed entrypoint**. You add a `[build]` block that links the public
source the bundle was produced from; the gate clones that source at the pinned
commit, installs its locked dependencies (install scripts disabled), re-bundles with
one fixed esbuild configuration, and requires the result to equal your committed
entrypoint byte-for-byte. The host still sees, hashes, and runs exactly one file,
and the sandbox is unchanged — bundled code gets no new powers.

```toml
runtime    = "js"
entrypoint = "main.js"   # the committed bundle

[build]
repository = "https://github.com/you/your-plugin"
ref        = "0f1c2b3a4d5e6f7081920a1b2c3d4e5f60718293"  # full 40-char commit SHA
entry      = "src/main.ts"                                # bundler entry in the repo
```

| Field | Required | Rules |
| ----- | -------- | ----- |
| `repository` | yes | `https://` git URL of the plugin's source. |
| `ref` | yes | A full 40-character commit SHA. A branch or tag is rejected — only a pinned commit is reproducible. |
| `entry` | yes | Clean repo-relative path to the bundler entry, a `.ts`/`.js`/`.tsx`/`.jsx`/`.mjs`/`.cjs` file. No `..`, no leading `/`. |

`[build]` is **JS/TS only**: a WASM module statically links its dependencies
already, and Lua bundling is out of scope.

Requirements on the source:

- The entry **`export`s its hook functions** (`export function OnTransactionCreate
  …`, `export function OnUninstall …`). The canonical bundle lifts those exports
  onto the global object so the host resolves them like a hand-written entrypoint.
- If it imports npm packages, it commits a **`package-lock.json`** (or
  `npm-shrinkwrap.json`) so `npm ci` installs deterministically. A plugin that
  bundles only its own multi-file source needs no lockfile.
- Dependencies must be **pure computation** that bundles cleanly: a dependency on a
  Node builtin (`fs`, `net`, …) fails to bundle, and even if it bundled it could not
  call out — the sandbox grants no filesystem, process, or network. Libraries that
  detect the global object via `global`/`self` work: the canonical bundler resolves
  both to `globalThis` (which the host provides), so a library never falls back to
  the `Function` constructor the sandbox removes.

A worked example lives at [`examples/lodash-demo`](../examples/lodash-demo) (source)
and [`plugins/lodash-demo`](../plugins/lodash-demo) (the bundled plugin): it depends
on lodash, pulled from npm and folded into a single verified `main.js`.

Produce the artifact with the registry tooling so it is exactly what the gate
reproduces, then commit it alongside `plugin.toml`:

```sh
go run ./cmd/kasas-plugins bundle plugins/<name>   # writes plugins/<name>/main.js
go run ./cmd/kasas-plugins validate --verify-build plugins/<name>
```

See [submission-guidelines.md](submission-guidelines.md) for the `build.*` finding
codes and the bundle size budget.

## The host API (summary)

| Lua | JavaScript/TypeScript | Capability |
| --- | --------------------- | ---------- |
| `kasas.get_transaction(id)` | `kasas.getTransaction(id)` | `transactions:read` |
| `kasas.search(query, limit?)` | `kasas.search(query, limit?)` | `transactions:read` |
| `kasas.apply_labels(id, {k=v})` | `kasas.applyLabels(id, {k:v})` | `labels:write` |
| `kasas.remove_labels(id, {k})` | `kasas.removeLabels(id, [k])` | `labels:write` |
| `kasas.set_extension(id, key, value)` | `kasas.setExtension(id, key, value)` | `extensions:write` |
| `kasas.remove_extension(id, key)` | `kasas.removeExtension(id, key)` | `extensions:write` |
| `kasas.log(level, msg, {fields})` | `kasas.log(level, msg, {fields})` | — |
| `kasas.config` | `kasas.config` | — |
| `kasas.set_config({k=v})` | `kasas.setConfig({k:v})` | — |

For `wasm` plugins the first-party Go SDK
(`github.com/paulmeier/kasas/pluginsdk/kasas`) mirrors this API one-to-one in
PascalCase (`kasas.ApplyLabels`, `kasas.SetExtension`, …) with the same
capability gates; any other language can implement the ABI directly — see
[Go (WASM) in the host docs](https://github.com/paulmeier/kasas/blob/main/docs/features/plugins.md#go-wasm).

The full host API, data shapes, and the TypeScript `kasas.d.ts` ambient types are in
the [host plugin docs](https://github.com/paulmeier/kasas/blob/main/docs/features/plugins.md).
A `kasas.d.ts` you drop next to a TS plugin is allowed in the directory and never
executed.
