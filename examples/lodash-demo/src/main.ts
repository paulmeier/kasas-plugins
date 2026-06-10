// lodash-demo — the SOURCE of the `lodash-demo` kasas plugin.
//
// This file proves ADR 0001 end-to-end: it depends on a real third-party library
// (lodash) pulled from npm, and the registry's submission gate bundles it into the
// single `plugins/lodash-demo/main.js` entrypoint, then verifies that committed
// bundle is reproducibly this source. The library is pure computation, so it runs
// happily inside the sandbox — it can format a string, but it cannot reach the
// network, the filesystem, or a process, because those APIs are not present to call.
//
// The author never hand-writes the bundle: they push this source, pin the commit
// in plugins/lodash-demo/plugin.toml's [build] block, and run
//   go run ./cmd/kasas-plugins bundle plugins/lodash-demo
// which produces main.js. CI's `validate --verify-build` reproduces it and proves
// the artifact matches, byte for byte.

import round from "lodash/round";
import startCase from "lodash/startCase";
import toLower from "lodash/toLower";
import truncate from "lodash/truncate";

interface Transaction {
  id: string;
  amount: string; // decimal string, e.g. "-249.99"
  description: string;
  extensions?: Record<string, unknown>;
}

const SUMMARY = "lodash_demo.summary";

// OnTransactionCreate records a tidy, human-readable summary of each transaction,
// using lodash to do the string and number work that is annoying to get right by
// hand. Everything here is bundled from the lodash package — nothing is reimplemented.
export function OnTransactionCreate(txn: Transaction): void {
  const amount = parseFloat(txn.amount);
  if (Number.isNaN(amount)) {
    return;
  }
  // lodash doing real work: round to cents, and normalize the description into a
  // clean Title Case label, truncated to a sensible length.
  const magnitude = round(Math.abs(amount), 2);
  const title = truncate(startCase(toLower(txn.description)), { length: 40 });

  kasas.setExtension(txn.id, SUMMARY, { title, magnitude });
  kasas.log("info", "lodash-demo summarized a transaction", { id: txn.id, title });
}

// OnUninstall owns this plugin's cleanup: it removes the summary extension it set
// from every transaction that still carries it, leaving the ledger as it found it.
export function OnUninstall(): void {
  const all = kasas.search("", 1000) as Transaction[];
  for (const txn of all) {
    if (txn.extensions && SUMMARY in txn.extensions) {
      kasas.removeExtension(txn.id, SUMMARY);
    }
  }
  kasas.log("info", "lodash-demo removed its summaries on uninstall");
}
