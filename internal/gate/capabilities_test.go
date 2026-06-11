package gate

import (
	"testing"

	"github.com/paulmeier/kasas-plugins/internal/manifest"
)

func TestTrustTierFor(t *testing.T) {
	cases := []struct {
		name string
		caps []manifest.Capability
		want TrustTier
	}{
		{"empty (log-only)", nil, TrustVerified},
		{"read-only", []manifest.Capability{manifest.CapTransactionsRead}, TrustVerified},
		{"write", []manifest.Capability{manifest.CapTransactionsRead, manifest.CapLabelsWrite}, TrustVerified},
		{"ui page", []manifest.Capability{manifest.CapTransactionsRead, manifest.CapUIPage}, TrustVerified},
		{"all sealed caps", []manifest.Capability{manifest.CapTransactionsRead, manifest.CapLabelsWrite, manifest.CapExtensionsWrite, manifest.CapUIPage}, TrustVerified},
		{"net:fetch", []manifest.Capability{manifest.CapTransactionsRead, manifest.CapNetFetch}, TrustConnected},
		{"net:fetch + write", []manifest.Capability{manifest.CapExtensionsWrite, manifest.CapNetFetch}, TrustConnected},
		// A capability the host gains in future but the registry has not yet placed
		// in the verified set drops the plugin to Unlisted, even alongside net:fetch.
		{"future capability", []manifest.Capability{manifest.Capability("transactions:create")}, TrustUnlisted},
		{"future capability + net:fetch", []manifest.Capability{manifest.CapNetFetch, manifest.Capability("process:spawn")}, TrustUnlisted},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := trustTierFor(tc.caps); got != tc.want {
				t.Fatalf("trustTierFor(%v) = %q, want %q", tc.caps, got, tc.want)
			}
		})
	}
}

// TestVerifiedCapabilitiesCoverKnown guards the forward-compatibility contract: a
// capability the host/registry knows must be deliberately classified. Every known
// capability is either statically sealed (in verifiedCapabilities) or net:fetch
// (Connected) — a new known capability that is neither would silently become
// Unlisted, which this test surfaces so the classification is a conscious choice.
func TestVerifiedCapabilitiesCoverKnown(t *testing.T) {
	for _, name := range manifest.KnownCapabilityNames() {
		c := manifest.Capability(name)
		if c == manifest.CapNetFetch {
			continue
		}
		if !verifiedCapabilities[c] {
			t.Errorf("known capability %q is unclassified: add it to verifiedCapabilities (or treat it as a new tier) so it is not silently Unlisted", name)
		}
	}
}

// TestTrustTierFindings checks the gate's posture per tier: Connected draws a
// non-blocking review warning naming its hosts; Unlisted is a blocking error.
func TestTrustTierFindings(t *testing.T) {
	connected := manifest.Manifest{
		Capabilities: []manifest.Capability{manifest.CapTransactionsRead, manifest.CapNetFetch},
		Net:          &manifest.NetManifest{Allow: []string{"api.example.com"}},
	}
	fs := trustTierFindings(connected)
	if len(fs) != 1 || fs[0].Severity != SeverityWarning || fs[0].Code != "tier.connected" {
		t.Fatalf("expected one tier.connected warning, got %+v", fs)
	}

	unlisted := manifest.Manifest{Capabilities: []manifest.Capability{manifest.Capability("process:spawn")}}
	fs = trustTierFindings(unlisted)
	if len(fs) != 1 || fs[0].Severity != SeverityError || fs[0].Code != "tier.unlisted" {
		t.Fatalf("expected one tier.unlisted error, got %+v", fs)
	}

	verified := manifest.Manifest{Capabilities: []manifest.Capability{manifest.CapLabelsWrite}}
	if fs := trustTierFindings(verified); len(fs) != 0 {
		t.Fatalf("expected no findings for a verified plugin, got %+v", fs)
	}
}
