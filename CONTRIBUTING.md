# Contributing a plugin

Thank you for extending kasas! This document is the contract for getting a plugin
into the community registry. The goal of every rule here is one thing: a kasas user
should be able to install your plugin with **high confidence** that it does what it
says and cannot reach outside the sandbox.

If anything below is unclear, open a
[plugin submission proposal](https://github.com/paulmeier/kasas-plugins/issues/new?template=plugin_submission.yml)
to discuss before writing code.

## 1. Understand the model

A kasas plugin runs **in-process**, inside a sandboxed language VM, reacting to
committed events. It cannot block or corrupt a sync (it runs async, after commit),
and its **only** path to your data is the capability-checked `kasas` host API. Read
the host documentation first:
<https://github.com/paulmeier/kasas/blob/main/docs/features/plugins.md>.

Two runtimes are supported:

- **Lua** (`runtime = "lua"`)
- **JavaScript / TypeScript** (`runtime = "js"`; TypeScript types are stripped at
  load by esbuild — no build step)

## 2. Lay out your plugin

Create exactly this under `plugins/<name>/`:

```text
plugins/<name>/
  plugin.toml     # manifest (required)
  main.lua        # or main.js / main.ts — the single source entrypoint (required)
  README.md       # what it does and why it needs each capability (required)
```

- `<name>` is a lowercase slug (`^[a-z0-9][a-z0-9_-]*$`) and **must equal** the
  `name` in `plugin.toml`.
- A plugin is a **single self-contained source file**. The host loads only the
  entrypoint — there is no `require`/`import`, no `node_modules`, no second module.
- No binaries, archives, lockfiles, nested directories, or symlinks. Allowed
  companions are `README.md`, `*.md`, `LICENSE`/`*.txt`, `*.json` fixtures, and a
  `*.d.ts` ambient-types file for editor support.

See [`docs/plugin-spec.md`](docs/plugin-spec.md) for the full manifest reference and
[`docs/submission-guidelines.md`](docs/submission-guidelines.md) for every gate rule.

## 3. Write the manifest

```toml
name        = "my-plugin"            # == directory name
version     = "1.0.0"                # semver
description = "A clear one-liner."   # 12–200 chars
author      = "you or your handle"
runtime     = "lua"                  # or "js"
entrypoint  = "main.lua"             # defaults: main.lua / main.js

license  = "MIT"                     # SPDX, from the allowlist (see spec)
homepage  = "https://github.com/you/my-plugin"   # https

hooks        = ["OnTransactionCreate"]          # at least one
capabilities = ["transactions:read"]            # only what you use

[config]                              # optional, exposed as kasas.config
# ...
```

`license` and `homepage` are registry metadata the host ignores, so the same
directory drops into a kasas `plugins.dir` unchanged.

## 4. Stay inside the sandbox

The gate **rejects** any use of an escape hatch, per runtime:

- **Lua:** `os`, `io`, `require`, `package`, `load`, `loadstring`, `loadfile`,
  `dofile`, `module`, `debug`, `setfenv`/`getfenv`, `newproxy`, `collectgarbage`,
  `_G`, `_ENV`.
- **JS/TS:** `eval`, `new Function` / `Function(...)`, `require(...)`, `import`,
  `process`, `globalThis`, `fetch`, `XMLHttpRequest`, `WebSocket`, `WebAssembly`,
  `Worker`, `__proto__`, `constructor.constructor`, Node/Deno/Bun globals.

These aren't merely discouraged — the host removes them, so using one would fail at
runtime anyway. Do all of your work through the `kasas` host API.

## 5. Request the least capability

Declare only the capabilities you actually use:

| Capability          | Grants | Tier |
| ------------------- | ------ | ---- |
| `transactions:read` | read the triggering transaction, run searches | read-only |
| `labels:write`      | apply/remove labels | **write** |
| `extensions:write`  | set/remove schema extensions | **write** |

A **write** capability means your plugin can modify a user's ledger. That is
legitimate, but it requires a maintainer to sign off, and your README must explain
why each write capability is needed.

## 6. Run the gate locally

```sh
go run ./cmd/kasas-plugins validate plugins/<name>   # must pass
go run ./cmd/kasas-plugins index                     # regenerate the catalog
```

Commit the regenerated `registry/index.json` along with your plugin — CI enforces
that it is current with `index --check`.

## 7. Open the PR

Fill in the PR template. CI runs the full gate (tooling tests, the plugin gate,
luacheck for Lua, and the index check). A maintainer reviews for legitimacy and
signs off on any write capability. Once merged to `main`, the publish workflow
updates the catalog the dashboard serves.

## Updating an existing plugin

Bump `version` in `plugin.toml` (semver), make your change, regenerate the index,
and open a PR. The content hash in the index changes whenever any file changes, so
the dashboard can detect and offer updates.

## Provenance & licensing

Only submit code you wrote or have the right to redistribute under the declared
license, which must be on the SPDX allowlist in [`docs/plugin-spec.md`](docs/plugin-spec.md).
By submitting, you affirm you have that right.
