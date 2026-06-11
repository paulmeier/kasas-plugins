# Security model & disclosure

This registry distributes third-party code that runs on users' machines and touches
their financial data. This document states what the registry guarantees, what it
does not, and how to report a problem.

## The trust boundary

A kasas plugin runs in-process inside a sandboxed runtime (gopher-lua for Lua,
goja for JS/TS, wazero for WASM). Its only sanctioned path to data is the
capability-checked `kasas` host API. The registry's submission gate is designed to
ensure every listed plugin respects that boundary:

1. **Single, reviewable entrypoint.** Each plugin is one self-contained entrypoint
   plus a manifest and README — no extra modules, no `node_modules`, no stray
   binaries, no nested directories, no symlinks. For hand-written Lua and JS/TS the
   entrypoint is source that fits on a reviewer's screen. For WASM it is one
   compiled module, and for a **bundled** JS/TS plugin
   ([ADR 0001](https://github.com/paulmeier/kasas/blob/main/docs/architecture/decisions/0001-plugin-dependency-bundling.md))
   it is one machine-generated bundle — neither is line-by-line human-reviewable, so
   the gate compensates with structural and provenance verification (see 2 and 3).
2. **No escape hatches.** For hand-written Lua and JS/TS, a static pass rejects
   every API the host strips at load (filesystem, network, process, dynamic code
   generation, module loading, environment/prototype manipulation). A bundled JS
   plugin's artifact is machine-generated and legitimately contains some of those
   constructs, so its guarantee is provenance, not a clean read (see 3) — and the
   sandbox is the backstop: bundled code runs in the same sealed goja VM with no
   filesystem, process, or network. For WASM, the gate verifies the module can only
   import from the `kasas` host module and WASI preview1 — which the host
   instantiates with no preopened directories and no sockets — so there is nothing
   else for it to call.
3. **Loads as published, and is provably its source.** JS/TS submissions are
   transpiled with the same esbuild the host uses, and WASM submissions are compiled
   (never run) with the same wazero the host uses — with the ABI's required exports
   checked — so a listed plugin is proven to load. For a **bundled** JS/TS plugin
   the gate additionally **reproduces the bundle**: it clones the source repository
   at the pinned commit, installs the locked dependencies with install scripts
   disabled, re-bundles with one fixed esbuild configuration, and requires the
   result to equal the committed entrypoint byte-for-byte. A mismatch is a hard
   rejection, so the artifact a user installs is provably the linked source.
4. **Least privilege, tiered, reviewed.** A plugin declares the capabilities it
   needs; the gate computes a [trust tier](docs/submission-guidelines.md#trust-tiers-adr-0003)
   from them and scales its posture to match
   ([ADR 0003](https://github.com/paulmeier/kasas/blob/main/docs/architecture/decisions/0003-marketplace-trust-tiers.md)).
   A **Verified** plugin (read/label/extension/page capabilities only) is statically
   sealed — it can reach neither the network nor the disk — and flows through the
   automated checks. A **Connected** plugin additionally declares `net:fetch`: it can
   make host-mediated, allowlisted outbound requests, so a maintainer reviews its
   declared egress hosts before it is listed and that list is recorded in the index.
   A plugin requesting anything outside that reviewed set is **Unlisted** — the gate
   refuses to publish it (sideload only). A maintainer also signs off before any
   plugin that can *write* a user's data is listed.
5. **Integrity to install.** The published index records a SHA-256 for every file
   and an aggregate content hash per plugin. The dashboard verifies these before
   writing anything into `plugins.dir`, so what a user installs is byte-for-byte what
   was reviewed here.

The gate itself never executes submitted code. For most checks it only reads
files; to reproduce a bundle it additionally runs `git` (which fetches files),
`npm ci --ignore-scripts` (which installs packages without running any lifecycle
hook), and esbuild (which parses and concatenates source without evaluating it) —
none of which runs the plugin's own logic, exactly as the WASM gate compiles but
never instantiates a module.

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
