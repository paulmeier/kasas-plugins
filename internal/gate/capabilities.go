package gate

import "github.com/paulmeier/kasas-plugins/internal/manifest"

// Tier ranks a plugin by the most powerful capability it requests. The registry
// and the dashboard use it to decide how prominently to warn a user before they
// enable a plugin, and CI uses it to require human (CODEOWNERS) sign-off on
// anything that can mutate a user's ledger.
type Tier string

const (
	// TierReadOnly: the plugin can read transactions and run searches but cannot
	// change anything. The lowest-risk class.
	TierReadOnly Tier = "read-only"
	// TierWrite: the plugin can apply/remove labels or set/remove schema
	// extensions — it can modify a user's data. Requires reviewer sign-off.
	TierWrite Tier = "write"
)

// capabilityRisk maps each capability to whether it grants write power. ui:page
// is not a write: a page can only display what the plugin's OTHER capabilities
// let it read, and its actions mutate only through those same capabilities — so
// the tier is still decided by what the plugin can touch, not by having a page.
var capabilityWrite = map[manifest.Capability]bool{
	manifest.CapTransactionsRead: false,
	manifest.CapLabelsWrite:      true,
	manifest.CapExtensionsWrite:  true,
	manifest.CapUIPage:           false,
}

// tierFor returns the highest risk tier implied by a capability set. An empty set
// (a plugin that only logs) is read-only.
func tierFor(caps []manifest.Capability) Tier {
	for _, c := range caps {
		if capabilityWrite[c] {
			return TierWrite
		}
	}
	return TierReadOnly
}

// capabilityFindings emits advisory findings about a plugin's capability requests.
// They never block listing on their own (write capabilities are legitimate); they
// exist to surface, in the PR and in the registry, exactly what a plugin can do —
// so review and the user's install decision are informed. A plugin that requests no
// capabilities at all is also worth noting, since its hooks can then only log.
func capabilityFindings(m manifest.Manifest) []Finding {
	var out []Finding
	if len(m.Capabilities) == 0 {
		out = append(out, Finding{
			Severity: SeverityWarning,
			Code:     "cap.none",
			Message:  "plugin requests no capabilities; its hooks can only log (verify this is intended)",
		})
		return out
	}
	for _, c := range m.Capabilities {
		if capabilityWrite[c] {
			out = append(out, Finding{
				Severity: SeverityWarning,
				Code:     "cap.write_requested",
				Message:  "requests write capability " + string(c) + "; this plugin can modify user data and requires reviewer sign-off",
			})
		}
		if c == manifest.CapUIPage {
			out = append(out, Finding{
				Severity: SeverityWarning,
				Code:     "cap.ui_page",
				Message:  "provides a dashboard page (sidebar entry + rendered page); review the OnPageRender/OnPageAction hooks for what they show and do",
			})
		}
	}
	return out
}
