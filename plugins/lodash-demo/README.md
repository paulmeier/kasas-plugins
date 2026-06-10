# lodash-demo

A small plugin that **demonstrates bundled dependencies** ([ADR 0001](https://github.com/paulmeier/kasas/blob/main/docs/architecture/decisions/0001-plugin-dependency-bundling.md)).
On each new transaction it records a tidy summary extension — a rounded amount and a
clean Title Case label — computed with the [lodash](https://lodash.com) library.

What makes it notable is not what it does but **how it ships**: `main.js` is not
hand-written. It is a **bundle** produced from the source at
[`examples/lodash-demo`](../../examples/lodash-demo), with lodash folded in. The
registry gate reproduces that bundle from the pinned commit in `plugin.toml`'s
`[build]` block and proves the committed `main.js` is exactly that source — so a
third-party library is used without ever loosening the sandbox.

## Capabilities

- `transactions:read` — `OnUninstall` searches for the extensions it set so it can
  remove them.
- `extensions:write` — records (and on uninstall removes) the `lodash_demo.summary`
  extension.

## Config

None — it summarizes every transaction.
