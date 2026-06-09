# double-charge-detector

Surfaces possible **double charges** — the same amount at the same merchant
within a few days — on its own dashboard page, and can flag them as they arrive.

## What it does

A suspect is a pair of **posted** expenses (pending transactions are skipped,
since a pending/posted pair is usually the same charge settling) with the same
amount and the same merchant — the payee, falling back to the description —
whose dates are at most `window_days` apart.

The **Double Charges dashboard page** (sidebar entry, at
`/ext/double-charge-detector`) shows a count and a table of current suspect
pairs (merchant, amount, both dates, gap), with two actions:

- **Flag all suspects** — records a `double_charge.suspect` schema extension on
  the *later* charge of each pair, whose value is the id of the earlier charge.
- **Clear all flags** — removes every flag the plugin has set.

With **Flag new duplicates automatically** enabled (the default), the
`OnTransactionCreate` hook also flags an incoming posted expense the moment it
duplicates an existing one.

The plugin only ever *flags*: it never changes amounts or labels — whether a
suspect is really a duplicate (and what to do about it) stays with you. Flags
are ordinary [schema extensions](https://github.com/paulmeier/kasas/blob/main/docs/features/schema-extensions.md),
so you can search them (`ext:double_charge.suspect`) or consume them from rules
and webhooks.

## Capabilities

| Capability          | Why |
| ------------------- | --- |
| `transactions:read` | Scan transactions for matching amount/merchant pairs. |
| `extensions:write`  | Record / remove the `double_charge.suspect` flag. |
| `ui:page`           | Show the Double Charges dashboard page. |

It requests no other access. It performs no filesystem, network, or process
operations.

## Configuration

```toml
[config]
window_days = 3     # max days between two charges to count as a suspect
flag_new    = true  # flag incoming duplicates automatically
```

Both keys can be changed by the end user, two equivalent ways:

- **Dashboard**: the Settings form on the plugin's page (saved via
  `kasas.setConfig`, which overwrites the config file below).
- **By hand**: edit `double-charge-detector.config.toml` next to the plugin's
  directory in the kasas plugins folder, then reload the plugin:

  ```toml
  window_days = 5
  flag_new    = false
  ```

## Uninstalling

On `OnUninstall`, the plugin removes every `double_charge.suspect` extension it
set, so uninstalling cleans up after itself.

## License

MIT
