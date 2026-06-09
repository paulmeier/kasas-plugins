# Plugin specification

A community plugin is a directory under `plugins/<name>/` containing a `plugin.toml`
manifest, a single source entrypoint, and a `README.md`. This document is the
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
| `runtime` | yes | `lua` or `js`. |
| `entrypoint` | no | Filename inside the directory; no path separators. Defaults to `main.lua` / `main.js`. For TypeScript set `main.ts`. |
| `hooks` | yes | Non-empty; each one of the known hooks (below). |
| `capabilities` | no | Each one of the known capabilities (below). Declare only what you use. |
| `license` | yes | SPDX id from the allowlist (below). *Registry-only.* |
| `homepage` | yes | `https://` URL. *Registry-only.* |
| `[config]` | no | Arbitrary table exposed to the plugin as `kasas.config`. |
| `[ui]` | no | Declares a [dashboard page](#dashboard-pages-ui): `title` (≤40 chars) and an optional curated `icon` name. Requires the `OnPageRender` hook and the `ui:page` capability. |

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

`kasas.log(...)` and `kasas.config` are always available and need no capability.
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
(`{id, label, style?, params?}` buttons routed to `OnPageAction`), `divider`.
Scalar values may be strings, numbers, or booleans (normalized to strings).
Documents are bounded (≤256 KiB, ≤200 blocks, ≤1000 table rows) and an unknown
block type is a render error.

**Contract:** treat `OnPageRender` as read-only — it runs on every page view
(read-tier API). Put mutations in `OnPageAction` (write-tier API). Both run under
the host's per-hook timeout, serialized with your event hooks. See the
[host docs](https://github.com/paulmeier/kasas/blob/main/docs/features/plugins.md#dashboard-pages)
for the full schema and API surface.

## License allowlist

To keep the legal footing of installing community code clear, the manifest
`license` must be one of:

`0BSD`, `Apache-2.0`, `BSD-2-Clause`, `BSD-3-Clause`, `ISC`, `MIT`, `MPL-2.0`,
`Unlicense`.

To propose another, open an issue.

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

The full host API, data shapes, and the TypeScript `kasas.d.ts` ambient types are in
the [host plugin docs](https://github.com/paulmeier/kasas/blob/main/docs/features/plugins.md).
A `kasas.d.ts` you drop next to a TS plugin is allowed in the directory and never
executed.
