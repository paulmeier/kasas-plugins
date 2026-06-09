-- coffee-budget: a minimal, safe example plugin.
--
-- When a transaction is created or updated, if its description contains the
-- configured keyword (case-insensitive), apply a category label. It reads only the
-- triggering transaction and writes only labels, matching its declared
-- capabilities. It touches no filesystem, network, or os/io library — so it loads
-- and runs inside the kasas sandbox unchanged.

local function classify(txn)
  local desc = string.lower(txn.description or "")
  if string.find(desc, kasas.config.keyword, 1, true) then
    kasas.apply_labels(txn.id, { category = kasas.config.category })
    kasas.log("info", "coffee-budget tagged transaction", { id = txn.id })
  end
end

function OnTransactionCreate(txn)
  classify(txn)
end

function OnTransactionUpdate(txn)
  classify(txn)
end

-- OnUninstall runs once when the plugin is uninstalled. This plugin is responsible
-- for undoing what it created, so it removes the category label from the
-- transactions it would have matched, leaving no trace behind.
function OnUninstall()
  local matches = kasas.search(kasas.config.keyword, 1000)
  for _, txn in ipairs(matches) do
    kasas.remove_labels(txn.id, { "category" })
  end
  kasas.log("info", "coffee-budget removed its category labels on uninstall")
end
