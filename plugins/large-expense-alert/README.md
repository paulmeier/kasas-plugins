# large-expense-alert

Flags transactions whose amount exceeds a configurable threshold by recording a
schema extension on them.

## What it does

On every `transaction.created` event, if the transaction is an expense (negative
amount) whose absolute value exceeds `threshold`, the plugin sets the extension
`large_expense_alert.flagged = true`. Downstream consumers (the event stream,
webhooks, the dashboard) can then react to the flag.

Amounts are compared in integer cents, parsed from kasas's decimal-string amounts,
so there is no floating-point rounding error.

## Uninstalling

On `OnUninstall`, the plugin searches the ledger and removes the
`large_expense_alert.flagged` extension from every transaction that still carries
it, so uninstalling leaves no trace.

## Capabilities

| Capability          | Why |
| ------------------- | --- |
| `extensions:write`  | Set (and, on uninstall, remove) the `large_expense_alert.flagged` extension. |
| `transactions:read` | Find flagged transactions during `OnUninstall` cleanup. |

It performs no filesystem, network, or process operations.

## Configuration

```toml
[config]
threshold = "200.00"   # flag expenses larger than this absolute value
```

## License

Apache-2.0
