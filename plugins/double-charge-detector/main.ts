// double-charge-detector: surfaces possible double charges on a dashboard page.
//
// A "double charge" suspect is two POSTED expenses (negative amounts; pending
// transactions are skipped — a pending/posted pair is usually the same charge
// settling) with the same amount and the same merchant (payee, falling back to
// description) within a configurable window of days. The plugin only ever
// *flags* — it records a `double_charge.suspect` schema extension on the later
// transaction of a pair, pointing at the earlier one — and never modifies
// amounts, labels, or anything else; deciding whether a charge is really a
// duplicate stays with you.
//
// The dashboard page lists the current suspects, offers "Flag all" / "Clear
// flags" actions, and a Settings form: the window and the auto-flag toggle are
// saved with kasas.setConfig, which validates them against the [config]
// defaults and overwrites double-charge-detector.config.toml on the host — the
// same file an operator can edit by hand.
//
// It declares only `transactions:read`, `extensions:write`, and `ui:page`, and
// uses only the `kasas` host object — no network, filesystem, eval, require, or
// import — so esbuild transpiles it and goja runs it inside the sandbox
// unchanged. The TypeScript types below are stripped at load and exist only for
// authoring.

interface Transaction {
  id: string;
  amount: string; // decimal string, e.g. "-19.99"
  pending: boolean;
  date: Date;
  description: string;
  payee: string;
  extensions?: Record<string, unknown>;
}

interface PageRequest {
  plugin: string;
  action: string;
  params: Record<string, string>;
}

interface SuspectPair {
  first: Transaction; // the earlier charge
  second: Transaction; // the later charge (the one that gets flagged)
  daysApart: number;
}

const EXT = "double_charge.suspect";
const SCAN_LIMIT = 1000;
const DAY_MS = 24 * 60 * 60 * 1000;

// Parse a decimal money string into integer cents. Returns null if unparseable.
function toCents(amount: string): number | null {
  const m = /^(-?)(\d+)(?:\.(\d{1,2}))?$/.exec(amount.trim());
  if (!m) {
    return null;
  }
  const sign = m[1] === "-" ? -1 : 1;
  const whole = parseInt(m[2], 10);
  const frac = m[3] ? parseInt(m[3].padEnd(2, "0"), 10) : 0;
  return sign * (whole * 100 + frac);
}

function isExpense(txn: Transaction): boolean {
  const cents = toCents(txn.amount);
  return cents !== null && cents < 0;
}

// merchantKey identifies "the same merchant": the payee when present,
// otherwise the description, lowercased with whitespace collapsed.
function merchantKey(txn: Transaction): string {
  const name = (txn.payee && txn.payee.trim()) || txn.description || "";
  return name.trim().toLowerCase().replace(/\s+/g, " ");
}

// pairKey groups candidates: exact amount + merchant.
function pairKey(txn: Transaction): string {
  return txn.amount + "|" + merchantKey(txn);
}

function windowDays(): number {
  const n = Number(kasas.config.window_days);
  return isFinite(n) && n > 0 ? Math.floor(n) : 3;
}

function autoFlag(): boolean {
  return String(kasas.config.flag_new) === "true";
}

function isFlagged(txn: Transaction): boolean {
  return !!(txn.extensions && EXT in txn.extensions);
}

function day(d: Date): string {
  return new Date(d.getTime()).toISOString().slice(0, 10);
}

// findSuspects groups posted expenses by amount + merchant and pairs up
// consecutive charges within the window. A triple charge yields two pairs.
function findSuspects(txns: Transaction[]): SuspectPair[] {
  const windowMs = windowDays() * DAY_MS;
  const groups: Record<string, Transaction[]> = {};
  for (const txn of txns) {
    if (txn.pending || !isExpense(txn) || merchantKey(txn) === "") {
      continue;
    }
    const key = pairKey(txn);
    (groups[key] = groups[key] || []).push(txn);
  }

  const pairs: SuspectPair[] = [];
  for (const key in groups) {
    const group = groups[key];
    if (group.length < 2) {
      continue;
    }
    group.sort((a, b) => a.date.getTime() - b.date.getTime());
    for (let i = 1; i < group.length; i++) {
      const gapMs = group[i].date.getTime() - group[i - 1].date.getTime();
      if (gapMs <= windowMs) {
        pairs.push({
          first: group[i - 1],
          second: group[i],
          daysApart: Math.round(gapMs / DAY_MS),
        });
      }
    }
  }
  // Newest suspects first.
  pairs.sort((a, b) => b.second.date.getTime() - a.second.date.getTime());
  return pairs;
}

// OnTransactionCreate flags an incoming posted expense that duplicates an
// existing one (same amount + merchant within the window), when auto-flagging
// is enabled in the settings.
function OnTransactionCreate(txn: Transaction): void {
  if (!autoFlag() || txn.pending || !isExpense(txn) || merchantKey(txn) === "") {
    return;
  }
  const key = pairKey(txn);
  const windowMs = windowDays() * DAY_MS;
  for (const other of kasas.search("", SCAN_LIMIT) as Transaction[]) {
    if (other.id === txn.id || other.pending || !isExpense(other)) {
      continue;
    }
    if (pairKey(other) !== key) {
      continue;
    }
    if (Math.abs(txn.date.getTime() - other.date.getTime()) <= windowMs) {
      kasas.setExtension(txn.id, EXT, other.id);
      kasas.log("info", "flagged possible double charge", {
        id: txn.id,
        duplicate_of: other.id,
        amount: txn.amount,
      });
      return;
    }
  }
}

// OnPageRender backs the "Double Charges" dashboard page. It is read-only by
// contract: it scans for suspects and returns a declarative page document;
// flagging and settings changes happen in OnPageAction.
function OnPageRender() {
  const txns = kasas.search("", SCAN_LIMIT) as Transaction[];
  const suspects = findSuspects(txns);

  const rows: (string | number)[][] = [];
  for (const p of suspects) {
    rows.push([
      merchantKey(p.second),
      p.second.amount,
      day(p.first.date),
      day(p.second.date),
      p.daysApart === 0 ? "same day" : p.daysApart + "d apart",
      isFlagged(p.second) ? "yes" : "",
    ]);
  }

  return {
    title: "Double Charges",
    blocks: [
      {
        type: "stat",
        label: "Possible double charges",
        value: suspects.length,
        hint: "same amount + merchant within " + windowDays() + " day(s)",
      },
      { type: "heading", text: "Suspects" },
      {
        type: "table",
        columns: ["Merchant", "Amount", "First", "Second", "Gap", "Flagged"],
        rows: rows,
      },
      {
        type: "actions",
        actions: [
          { id: "flag_all", label: "Flag all suspects", style: "primary" },
          { id: "clear_flags", label: "Clear all flags", style: "danger" },
        ],
      },
      { type: "divider" },
      { type: "heading", text: "Settings" },
      {
        type: "form",
        id: "save_settings",
        submit_label: "Save settings",
        fields: [
          {
            name: "window_days",
            label: "Window (days)",
            kind: "number",
            value: windowDays(),
            help: "two charges this many days apart (or fewer) count as a suspect",
          },
          {
            name: "flag_new",
            label: "Flag new duplicates automatically",
            kind: "toggle",
            value: autoFlag(),
            help: "mark incoming duplicates with the " + EXT + " extension as they arrive",
          },
        ],
      },
    ],
  };
}

// OnPageAction handles the page's buttons and the settings form (mutations
// belong here, not in OnPageRender), then re-renders.
function OnPageAction(req: PageRequest) {
  if (req.action === "flag_all") {
    const suspects = findSuspects(kasas.search("", SCAN_LIMIT) as Transaction[]);
    let flagged = 0;
    for (const p of suspects) {
      if (!isFlagged(p.second)) {
        kasas.setExtension(p.second.id, EXT, p.first.id);
        flagged++;
      }
    }
    kasas.log("info", "flagged double-charge suspects from the dashboard", { count: flagged });
  } else if (req.action === "clear_flags") {
    let cleared = 0;
    for (const txn of kasas.search("", SCAN_LIMIT) as Transaction[]) {
      if (isFlagged(txn)) {
        kasas.removeExtension(txn.id, EXT);
        cleared++;
      }
    }
    kasas.log("info", "cleared double-charge flags from the dashboard", { count: cleared });
  } else if (req.action === "save_settings") {
    // The host validates against the [config] defaults and coerces the string
    // form params to their default types (number / boolean), then overwrites
    // double-charge-detector.config.toml and refreshes kasas.config in place.
    kasas.setConfig({
      window_days: req.params.window_days,
      flag_new: req.params.flag_new,
    });
    kasas.log("info", "double-charge-detector settings saved", {
      window_days: kasas.config.window_days,
      flag_new: kasas.config.flag_new,
    });
  }
  return OnPageRender();
}

// OnUninstall runs once when the plugin is uninstalled. This plugin owns its
// cleanup, so it removes every double_charge.suspect extension it set, leaving
// the ledger as it found it.
function OnUninstall(): void {
  for (const txn of kasas.search("", SCAN_LIMIT) as Transaction[]) {
    if (isFlagged(txn)) {
      kasas.removeExtension(txn.id, EXT);
    }
  }
  kasas.log("info", "double-charge-detector removed its flags on uninstall");
}
