package gate

import (
	"sort"
	"strings"

	"github.com/paulmeier/kasas-plugins/internal/manifest"
)

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

// TrustTier is the explicit, escalating trust ladder a marketplace plugin sits on
// (ADR 0003). It is orthogonal to [Tier] (read-only vs write, i.e. can it mutate
// data): the trust tier ranks how far the operator extends trust by the most
// powerful capability the plugin declares, scaling both the gate's posture and how
// loudly the dashboard warns. Like the capability tier it is a function of declared
// capabilities, computed here — never a label an author self-assigns.
type TrustTier string

const (
	// TrustVerified: the plugin declares only statically-sealed capabilities, so
	// the gate can prove it reaches neither the network nor the disk. Auto-listable
	// once the existing structural and per-runtime checks pass — the default tier.
	TrustVerified TrustTier = "verified"
	// TrustConnected: the plugin additionally declares net:fetch, so it can make
	// host-mediated outbound requests to its declared [net].allow hosts. Egress
	// turns a bug into exfiltration, so a maintainer must review the declared
	// allowlist before the plugin is listed; the allowlist is recorded in the index.
	TrustConnected TrustTier = "connected"
	// TrustUnlisted: the plugin declares a capability outside the registry's
	// reviewed, auto-listable set (e.g. a future broader capability). It is not
	// auto-listed — sideload only, with a "you are on your own" posture — so the
	// gate fails it for listing rather than publishing an unreviewed surface.
	TrustUnlisted TrustTier = "unlisted"
)

// verifiedCapabilities is the statically-sealed capability set: a plugin built
// only from these touches the ledger solely through the capability-checked host
// facade and can reach neither the network nor the disk, so the gate can prove it
// sealed. net:fetch is deliberately NOT here — it lifts a plugin to Connected.
var verifiedCapabilities = map[manifest.Capability]bool{
	manifest.CapTransactionsRead: true,
	manifest.CapLabelsWrite:      true,
	manifest.CapExtensionsWrite:  true,
	manifest.CapUIPage:           true,
}

// trustTierFor classifies a capability set onto the trust ladder. A capability
// outside the reviewed set drops the whole plugin to Unlisted (it is broader than
// what the registry auto-lists); net:fetch on top of an otherwise-verified set is
// Connected; an otherwise-verified set with no net:fetch is Verified. The function
// is forward-compatible: a future capability the host gains but the registry has
// not yet placed in verifiedCapabilities lands in Unlisted until reviewed.
func trustTierFor(caps []manifest.Capability) TrustTier {
	net := false
	for _, c := range caps {
		if c == manifest.CapNetFetch {
			net = true
			continue
		}
		if !verifiedCapabilities[c] {
			return TrustUnlisted
		}
	}
	if net {
		return TrustConnected
	}
	return TrustVerified
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

// trustTierFindings turns the computed trust tier (ADR 0003) into gate findings
// that scale effort with risk:
//
//   - Verified: no finding — it flows through the automated checks unchanged.
//   - Connected (net:fetch): a warning naming the declared egress hosts, so the
//     reviewer signs off on exactly what the plugin may reach. It does not block
//     the gate on its own; CODEOWNERS review is the gate for listing it.
//   - Unlisted: a blocking error — the plugin requests a capability outside the
//     auto-listable set, so it must not land in the published index (sideload only).
func trustTierFindings(m manifest.Manifest) []Finding {
	switch trustTierFor(m.Capabilities) {
	case TrustConnected:
		hosts := "(none declared)"
		if m.Net != nil && len(m.Net.Allow) > 0 {
			hosts = strings.Join(m.Net.Allow, ", ")
		}
		return []Finding{{
			Severity: SeverityWarning,
			Code:     "tier.connected",
			Message:  "requests net:fetch — Connected tier; egress is allowlisted to " + hosts + ", which a maintainer must review and sign off before listing",
		}}
	case TrustUnlisted:
		return []Finding{{
			Severity: SeverityError,
			Code:     "tier.unlisted",
			Message:  "requests a capability outside the registry's auto-listable set (" + capList(m.Capabilities) + "); this plugin is sideload-only and will not be listed",
		}}
	default:
		return nil
	}
}

// capList renders a capability set for a finding message, deterministically.
func capList(caps []manifest.Capability) string {
	ss := make([]string, len(caps))
	for i, c := range caps {
		ss[i] = string(c)
	}
	sort.Strings(ss)
	return strings.Join(ss, ", ")
}
