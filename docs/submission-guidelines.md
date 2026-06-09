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
| `tree.extra_source` | Only the entrypoint may be a source file of the runtime's language — the host loads no other modules. |
| `tree.nested_dir` | A plugin is a single flat directory; no subdirectories (this also blocks `node_modules`, `.git`, …). |
| `tree.symlink` | No symlinks. |
| `tree.disallowed_file` | Only the entrypoint, `README.md`/`*.md`, `*.txt`, `*.toml`, `*.json`, and `*.d.ts` are allowed. No binaries/archives/scripts. |
| `tree.file_too_large` | No single file over 256 KiB. |
| `tree.too_large` | The whole plugin must be under 1 MiB. |
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
| `js.network` | No `fetch(...)`, `XMLHttpRequest`, `WebSocket`, `navigator`. |
| `js.wasm` | No `WebAssembly`. |
| `js.worker` | No `new Worker`. |
| `js.sandbox_escape` | No `__proto__` or `constructor.constructor`. |
| `js.transpile_failed` | The entrypoint must transpile with esbuild (same loader/target the host uses: loader by extension, ES2017). |

As with Lua, comments and string/template literals are stripped before scanning, so
a banned word in a log message is fine. Write `.ts` if you want types — esbuild
strips them at load; no build step or `node_modules`.

## Capability findings (advisory)

| Code | Meaning |
| ---- | ------- |
| `cap.write_requested` | The plugin requests `labels:write` or `extensions:write`. It can modify user data; a maintainer must sign off, and your README must justify it. |
| `cap.none` | The plugin requests no capabilities — its hooks can only log. Confirm that's intended. |

These never fail the gate by themselves; they drive human review and are reflected
in the registry's `capability_tier`.

## After it passes

Regenerate and commit the catalog:

```sh
go run ./cmd/kasas-plugins index
```

CI runs `kasas-plugins index --check` and fails if `registry/index.json` is stale,
so the published catalog always matches the plugins in the repo.
