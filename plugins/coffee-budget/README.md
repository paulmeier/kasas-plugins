# coffee-budget

Automatically labels transactions whose description matches a keyword.

## What it does

On every `transaction.created` and `transaction.updated` event, the plugin checks
the transaction description (case-insensitive) for the configured keyword. If it
matches, it applies a `category` label.

## Capabilities

| Capability          | Why |
| ------------------- | --- |
| `transactions:read` | Read the triggering transaction's description. |
| `labels:write`      | Apply the category label. |

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

## License

MIT
