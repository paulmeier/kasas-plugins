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

-- OnPageRender backs the plugin's dashboard page ("Coffee Budget" in the
-- sidebar, at /ext/coffee-budget). It is read-only by contract: it returns a
-- declarative page document (stat / keyvalue / table / actions blocks) that the
-- host validates and the dashboard renders — the plugin ships no frontend code.
-- The host passes a request argument (plugin/action/params); this page is the
-- same for every request, so the parameter is omitted (Lua discards extra args).
function OnPageRender()
  local matches = kasas.search(kasas.config.keyword, 100)
  local rows = {}
  for _, txn in ipairs(matches) do
    rows[#rows + 1] = { txn.description, txn.amount }
  end
  return {
    title = "Coffee Budget",
    blocks = {
      { type = "stat", label = "Matching transactions", value = #matches,
        hint = 'description contains "' .. kasas.config.keyword .. '"' },
      { type = "heading", text = "Recent matches" },
      { type = "table", columns = { "Description", "Amount" }, rows = rows },
      { type = "actions", actions = {
          { id = "relabel", label = "Re-label matches", style = "primary" },
      } },
      { type = "divider" },
      -- Settings: the same keys an operator can edit by hand in
      -- coffee-budget.config.toml. Submitting runs OnPageAction with
      -- action = "save_settings" and the field values in req.params.
      { type = "heading", text = "Settings" },
      { type = "form", id = "save_settings", submit_label = "Save settings",
        fields = {
          { name = "keyword", label = "Keyword", value = kasas.config.keyword,
            help = "transactions whose description contains this are tagged" },
          { name = "category", label = "Category label", value = kasas.config.category,
            help = "value written to the category label on every match" },
      } },
    },
  }
end

-- OnPageAction handles the page's buttons and forms (mutations belong here,
-- not in OnPageRender). "relabel" re-applies the category label to every
-- current match; "save_settings" persists the form's values with
-- kasas.set_config, which validates them against the [config] defaults,
-- overwrites coffee-budget.config.toml on the host, and updates kasas.config
-- in place — so the re-render below already shows the new values.
function OnPageAction(req)
  if req.action == "relabel" then
    local matches = kasas.search(kasas.config.keyword, 1000)
    for _, txn in ipairs(matches) do
      kasas.apply_labels(txn.id, { category = kasas.config.category })
    end
    kasas.log("info", "coffee-budget re-labeled matches from the dashboard", { count = #matches })
  elseif req.action == "save_settings" then
    kasas.set_config({
      keyword  = string.lower(req.params.keyword or kasas.config.keyword),
      category = req.params.category or kasas.config.category,
    })
    kasas.log("info", "coffee-budget settings saved from the dashboard",
      { keyword = kasas.config.keyword, category = kasas.config.category })
  end
  return OnPageRender()
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
