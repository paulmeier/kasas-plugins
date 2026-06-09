# kasas community plugins

The official registry of community-made plugins for
[kasas](https://github.com/paulmeier/kasas). The kasas dashboard reads this
repository's published index to **list** available plugins and **install** them
with one click — verifying integrity along the way.

A kasas plugin is third-party code that runs *inside* kasas, in a sandboxed
language VM, reacting to committed ledger events (the in-process counterpart to
webhooks). Because that code runs on a user's machine and touches their financial
data, every plugin listed here must clear a **submission gate** before it appears in
the catalog. The gate is what lets a user install a community plugin with
confidence.

## How it works

```text
author opens PR  ──►  Validate workflow (the gate)  ──►  merged to main  ──►  Publish workflow
   plugins/<name>/        manifest + per-runtime           registry/index.json     index.json on
                          security checks + luacheck        regenerated & checked   GitHub Pages + raw
                                                                                         │
                                              kasas dashboard fetches the index ◄────────┘
                                              shows catalog, installs + verifies hashes
```

- **`plugins/<name>/`** — one directory per plugin: a `plugin.toml` manifest, a
  single entrypoint (a source file, or a compiled wasm module), and a
  `README.md`. See
  [`plugins/coffee-budget`](plugins/coffee-budget) (Lua, with a dashboard page +
  settings form), [`plugins/large-expense-alert`](plugins/large-expense-alert)
  (TypeScript), and
  [`plugins/double-charge-detector`](plugins/double-charge-detector)
  (TypeScript, with a dashboard page + settings form) for templates.
- **The gate** (`cmd/kasas-plugins` + `internal/`) — Go tooling that validates the
  manifest and runs **language-specific** security checks, then generates the
  registry index. It is the single binary CI runs and the same one you run locally.
- **`registry/index.json`** — the generated, machine-readable catalog the dashboard
  consumes, with a per-file SHA-256 and an aggregate content hash for every plugin
  so the installer can verify exactly what it downloads.

## The submission gate

Submissions are gated by **language**, because the safe-and-confident bar differs by
runtime. All runtimes share structural rules (a single self-contained entrypoint, a
README, no nested dirs / symlinks / stray binaries, size limits, semver + license +
homepage metadata, and a required `OnUninstall` cleanup hook); on top of that:

| Runtime | Language-specific gating |
| ------- | ------------------------ |
| **Lua** | Static scan rejecting every escape hatch the host strips (`os`, `io`, `require`, `load`/`loadstring`, `dofile`, `debug`, `_G`/`_ENV`, …). Plus `luacheck` in CI for syntax/lint. |
| **JavaScript / TypeScript** | Static scan rejecting `eval`, the `Function` constructor, `require`/`import`, `process`, `fetch`/`XMLHttpRequest`/`WebSocket`, `WebAssembly`, `globalThis`, `__proto__`, … **and** a transpile with the *same esbuild* the host uses — so "it loads in kasas" is proven at submission time. |
| **WASM** | Structural verification of the compiled module (never executed): it must compile with the *same wazero* the host uses, import only from the `kasas` host module and (preopen-less) WASI preview1, and export the ABI surface — `_initialize`, `kasas_describe`, every declared hook, and its memory. The module gets its own size budget (8 MiB) in place of the text-file limits. |

Capabilities are risk-tiered: read-only plugins pass on the automated gate alone; any
plugin that can **write** a user's data (`labels:write`, `extensions:write`) is
surfaced in the PR and the index and requires maintainer sign-off (CODEOWNERS).

The gate never executes plugin code — it only reads the plugin directory — so it is
safe to run on untrusted submissions in CI.

## Submitting a plugin

1. Read [CONTRIBUTING.md](CONTRIBUTING.md) and the
   [submission guidelines](docs/submission-guidelines.md).
2. Add your plugin under `plugins/<name>/` (copy an example to start).
3. Run the gate locally and regenerate the index:
   ```sh
   go run ./cmd/kasas-plugins validate plugins/<name>
   go run ./cmd/kasas-plugins index           # updates registry/index.json
   ```
4. Open a PR and fill in the template. CI runs the same gate; a maintainer reviews.

## Tooling

```sh
go run ./cmd/kasas-plugins validate            # gate every plugin under ./plugins
go run ./cmd/kasas-plugins validate plugins/x  # gate one plugin
go run ./cmd/kasas-plugins index               # (re)write registry/index.json
go run ./cmd/kasas-plugins index --check       # verify the committed index is current (CI)
```

## Repository layout

```text
plugins/                community plugins, one directory each
registry/index.json     generated catalog the dashboard consumes
schema/plugin.schema.json JSON Schema for plugin.toml (editor tooling)
cmd/kasas-plugins/       the gate + index CLI
internal/manifest/       manifest parsing & validation (superset of the host's rules)
internal/gate/           structural + per-runtime security checks, capability tiering
internal/registry/       index generation with integrity hashes
docs/                    plugin spec, submission guidelines, registry schema
```

## Documentation

- [Plugin specification](docs/plugin-spec.md) — the `plugin.toml` manifest and the host contract.
- [Submission guidelines](docs/submission-guidelines.md) — every gate rule, by runtime, and how to pass it.
- [Registry index schema](docs/registry.md) — the `index.json` contract the dashboard consumes.
- [Security model](SECURITY.md) — the trust boundary and how to disclose a vulnerability.

## License

The registry tooling is MIT (see [LICENSE](LICENSE)). Each plugin is licensed
independently under the SPDX license declared in its manifest.
