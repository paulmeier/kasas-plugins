# lodash-demo (source)

This is the **source** for the `lodash-demo` plugin under
[`plugins/lodash-demo`](../../plugins/lodash-demo). It exists to demonstrate
[ADR 0001](https://github.com/paulmeier/kasas/blob/main/docs/architecture/decisions/0001-plugin-dependency-bundling.md):
a plugin may depend on a real third-party library (here, [lodash](https://lodash.com))
as long as that dependency is **bundled into the single committed entrypoint** the
registry reviews, hashes, and installs.

```
examples/lodash-demo/
  package.json        # declares the lodash dependency
  package-lock.json   # pins it exactly, so npm ci is deterministic
  src/main.ts         # imports lodash; the bundler entry
```

The committed artifact lives at `plugins/lodash-demo/main.js`. It is **not** edited
by hand — it is produced from this source by the registry tooling:

```sh
# From the repo root, after pushing this source and pinning its commit in
# plugins/lodash-demo/plugin.toml's [build] block:
go run ./cmd/kasas-plugins bundle   plugins/lodash-demo   # writes plugins/lodash-demo/main.js
go run ./cmd/kasas-plugins validate --verify-build plugins/lodash-demo
```

`validate --verify-build` re-bundles this source at the pinned commit and proves
`plugins/lodash-demo/main.js` matches it byte-for-byte. `node_modules/` is never
committed — the verifier restores it from `package-lock.json`.
