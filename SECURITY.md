# Security model & disclosure

This registry distributes third-party code that runs on users' machines and touches
their financial data. This document states what the registry guarantees, what it
does not, and how to report a problem.

## The trust boundary

A kasas plugin runs in-process inside a sandboxed language VM (gopher-lua for Lua,
goja for JS/TS). Its only sanctioned path to data is the capability-checked `kasas`
host API. The registry's submission gate is designed to ensure every listed plugin
respects that boundary:

1. **Single, reviewable source.** Each plugin is one self-contained entrypoint plus
   a manifest and README — no extra modules, no `node_modules`, no binaries, no
   nested directories, no symlinks. The whole plugin fits on a reviewer's screen.
2. **No escape hatches.** A language-specific static pass rejects every API the host
   strips at load (filesystem, network, process, dynamic code generation, module
   loading, environment/prototype manipulation). Using one is a hard rejection, not
   a warning.
3. **Loads as published.** JS/TS submissions are transpiled with the same esbuild
   the host uses, so a listed plugin is proven to load.
4. **Least privilege, reviewed.** A plugin declares the capabilities it needs; the
   gate tiers them and a maintainer signs off before any plugin that can *write* a
   user's data is listed.
5. **Integrity to install.** The published index records a SHA-256 for every file
   and an aggregate content hash per plugin. The dashboard verifies these before
   writing anything into `plugins.dir`, so what a user installs is byte-for-byte what
   was reviewed here.

The gate itself never executes submitted code; it only reads files.

## What the registry does NOT guarantee

- **It is not a hard resource sandbox.** Like the host's v1 model, a buggy plugin
  can still loop or allocate within its per-hook timeout. The host bounds each hook;
  the registry does not add memory caps.
- **Static analysis is not proof of intent.** The gate proves a plugin cannot reach
  outside the sandbox; it cannot prove the in-sandbox logic is *useful* or
  bug-free. Human review and the plugin's README cover legitimacy, not the gate.
- **Enabling remains opt-in and admin-only in the host.** Installing a plugin makes
  it discoverable; an operator still has to enable it. The registry does not change
  that posture.

Treat an installed community plugin as you would any third-party extension: review
what it does and grant only the capabilities it needs.

## Reporting a vulnerability

Please report privately — do **not** open a public issue for a security problem:

- Open a [private security advisory](https://github.com/paulmeier/kasas-plugins/security/advisories/new), or
- If that is unavailable, email the maintainer listed on the kasas project.

Include the plugin name (or the tooling file), the impact, and steps to reproduce.

If the report concerns a **listed plugin**, we will unlist it from the registry
(removing it from the published index) while it is investigated. If it concerns the
**gate tooling** — e.g. a way to slip a banned construct past the static pass — we
treat it as a high-severity issue, since the gate is the guarantee the whole
registry rests on, and we will add a regression test alongside the fix.
