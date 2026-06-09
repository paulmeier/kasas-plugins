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
