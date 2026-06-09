-- luacheck configuration for kasas community Lua plugins.
--
-- This is the complementary syntax/lint pass to the Go gate (which owns the
-- security-relevant bans on os/io/require/etc.). Here we teach luacheck about the
-- plugin execution model so it lints real mistakes without false positives:
--
--   * `kasas` is the read-only host API object injected by the runtime.
--   * Hook functions are defined as globals and called by the host, so assigning
--     them is intentional, not a "setting non-standard global" smell.

read_globals = { "kasas" }

globals = {
  "OnTransactionCreate",
  "OnTransactionUpdate",
  "OnSyncComplete",
  "OnUninstall",
  "OnPageRender",
  "OnPageAction",
}

max_line_length = 120

-- Plugins are small, single-file scripts; treat warnings as advisory but keep the
-- run green on style nits so the gate's hard rules remain the blocking authority.
codes = true
