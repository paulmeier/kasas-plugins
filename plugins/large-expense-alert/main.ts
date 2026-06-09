// large-expense-alert: a minimal, safe TypeScript example plugin.
//
// When a transaction is created, if it is an expense (negative amount) whose
// absolute value exceeds the configured threshold, it records a schema extension
// flagging it as a large expense. Amounts are decimal strings (cents are never
// floats), so we compare in integer cents to avoid floating-point error.
//
// It declares only `extensions:write` and uses only the `kasas` host object — no
// network, filesystem, eval, require, or import — so esbuild transpiles it and goja
// runs it inside the sandbox unchanged. The TypeScript types below are stripped at
// load by esbuild and exist only for authoring.

interface Transaction {
  id: string;
  amount: string; // decimal string, e.g. "-249.99"
  description: string;
}

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

function OnTransactionCreate(txn: Transaction): void {
  const amountCents = toCents(txn.amount);
  const thresholdCents = toCents(String(kasas.config.threshold));
  if (amountCents === null || thresholdCents === null) {
    return;
  }
  // Expenses are negative; compare absolute values.
  if (amountCents < 0 && -amountCents > thresholdCents) {
    kasas.setExtension(txn.id, "large_expense_alert.flagged", true);
    kasas.log("info", "flagged large expense", { id: txn.id, amount: txn.amount });
  }
}
