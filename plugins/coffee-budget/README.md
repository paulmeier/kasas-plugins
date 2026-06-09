# coffee-budget

Automatically labels transactions whose description matches a keyword.

## What it does

On every `transaction.created` and `transaction.updated` event, the plugin checks
the transaction description (case-insensitive) for the configured keyword. If it
matches, it applies a `category` label.

It also provides a **Coffee Budget dashboard page** (sidebar entry, at
`/ext/coffee-budget`): a count of matching transactions, a table of recent
matches, a *Re-label matches* button that re-applies the category label on
demand, and a **Settings form** that edits the keyword and category right from
the page. The page is declarative — the plugin returns data blocks the
dashboard renders; it ships no frontend code.

## Capabilities

| Capability          | Why |
| ------------------- | --- |
| `transactions:read` | Read the triggering transaction's description; list matches for the page. |
| `labels:write`      | Apply the category label (on events and via the page's button). |
| `ui:page`           | Show the Coffee Budget dashboard page. |

It requests no other access. It performs no filesystem, network, or process
operations.

## Uninstalling

On `OnUninstall`, the plugin searches for transactions matching its keyword and
removes the `category` label it applied, so uninstalling cleans up after itself.

## Configuration

```toml
[config]
keyword  = "coffee"   # substring to match, lowercased
category = "food"     # value of the `category` label to apply
```

Both keys can be changed by the end user, two equivalent ways:

- **Dashboard**: the Settings form on the plugin's page (saved via
  `kasas.set_config`, which overwrites the config file below).
- **By hand**: edit `coffee-budget.config.toml` next to the plugin's directory
  in the kasas plugins folder, then reload the plugin:

  ```toml
  keyword  = "espresso"
  category = "coffee"
  ```

## License

MIT
