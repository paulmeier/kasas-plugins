<!--
Submitting a plugin? Thank you! Please read CONTRIBUTING.md first and fill in the
checklist below. CI (the "Validate" workflow) runs the same gate locally available
via `go run ./cmd/kasas-plugins validate` — run it before opening the PR.
-->

## What kind of change is this?

- [ ] New plugin submission
- [ ] Update to an existing plugin (bumps `version` in `plugin.toml`)
- [ ] Tooling / docs / registry infrastructure

## Plugin submissions

**Plugin name:** <!-- must equal the directory name under plugins/ -->
**Runtime:** <!-- lua | js -->
**Capabilities requested:** <!-- e.g. transactions:read, labels:write -->

### What does it do, in one paragraph?

<!-- A reviewer should understand the plugin's full behavior from this + the source. -->

### Checklist

- [ ] The plugin lives in `plugins/<name>/` and `<name>` matches `name` in `plugin.toml`.
- [ ] `plugin.toml` declares `version` (semver), `description`, `author`, `license`, and `homepage`.
- [ ] There is a `README.md` explaining what the plugin does and why it needs each capability.
- [ ] The plugin is a **single source file** (the entrypoint) plus the manifest and README — no extra modules, no `node_modules`, no binaries.
- [ ] It uses **only** the `kasas` host API — no filesystem, network, `os`/`io`, `eval`, `require`/`import`, or `process`.
- [ ] I ran `go run ./cmd/kasas-plugins validate plugins/<name>` and it passes.
- [ ] I ran `go run ./cmd/kasas-plugins index` and committed the updated `registry/index.json`.
- [ ] If the plugin requests a **write** capability (`labels:write` / `extensions:write`), I have explained why above (this requires maintainer sign-off).

### Provenance

- [ ] I am the author, or I have the right to submit this code under the declared license.
