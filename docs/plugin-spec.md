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

`kasas.log(...)` and `kasas.config` are always available and need no capability.
A **write** capability requires maintainer sign-off (see CONTRIBUTING.md).

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
