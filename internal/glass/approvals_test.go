package glass

import (
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/tonygair/ghillie/internal/gate"
)

// TestNoDialogForStructuralRefusals — the property that matters. An act refused
// for a reason no human may override must never reach a reviewer. Showing a
// prompt that will be refused regardless misrepresents who decides, and teaches
// reviewers to click through.
func TestNoDialogForStructuralRefusals(t *testing.T) {
	cases := []struct {
		name      string
		c         gate.Command
		ceiling   gate.Command
		authentic bool
	}{
		{"not authentic", gate.InstallArtifact, gate.RunLocalCode, false},
		{"over ceiling", gate.RunLocalCode, gate.OfferCatalogue, true},
		{"local only", gate.RequestSpecUpload, gate.RunLocalCode, true},
		{"local only, even at max ceiling with consent to come", gate.RequestSpecUpload, gate.RunLocalCode, true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			allowed, askable := Askable(tc.c, tc.ceiling, gate.NoConsent, tc.authentic)
			if allowed {
				t.Fatalf("gate should refuse this act")
			}
			if askable {
				t.Errorf("a dialog must NOT be raised: fresh consent cannot rescue this")
			}
			if _, err := testServer().NewPendingApproval(tc.c, tc.ceiling, gate.NoConsent, tc.authentic, time.Minute); err == nil {
				t.Errorf("NewPendingApproval must refuse to build a dialog for a structural refusal")
			}
		})
	}
}

// TestDialogOnlyWhenConsentIsTheOnlyThingMissing
func TestDialogOnlyWhenConsentIsTheOnlyThingMissing(t *testing.T) {
	// Install requires consent; ceiling permits it; authentic. Consent absent.
	allowed, askable := Askable(gate.InstallArtifact, gate.RunLocalCode, gate.NoConsent, true)
	if allowed {
		t.Fatalf("should not already be allowed without consent")
	}
	if !askable {
		t.Fatalf("consent is the only thing missing; this should be askable")
	}
	a, err := testServer().NewPendingApproval(gate.InstallArtifact, gate.RunLocalCode, gate.NoConsent, true, time.Minute)
	if err != nil {
		t.Fatalf("NewPendingApproval: %v", err)
	}
	if a.Status != "pending" {
		t.Errorf("status = %q, want pending", a.Status)
	}
	if a.ExpiresAtMs <= a.CreatedAtMs {
		t.Errorf("expiry must be after creation")
	}
}

// TestDenyIsAlwaysOffered — ApprovalAllowedDecisionsSchema requires that "deny"
// is always among the decisions. A reviewer must always be able to refuse.
func TestDenyIsAlwaysOffered(t *testing.T) {
	a, err := testServer().NewPendingApproval(gate.InstallArtifact, gate.RunLocalCode, gate.NoConsent, true, time.Minute)
	if err != nil {
		t.Fatalf("NewPendingApproval: %v", err)
	}
	var found bool
	for _, d := range a.Presentation.AllowedDecisions {
		if d == string(Deny) {
			found = true
		}
	}
	if !found {
		t.Errorf("allowedDecisions %v does not contain deny", a.Presentation.AllowedDecisions)
	}
}

// TestNoAlreadyPermittedDialog — asking about something already allowed is
// consent theatre.
func TestNoAlreadyPermittedDialog(t *testing.T) {
	if _, err := testServer().NewPendingApproval(gate.ReportStatus, gate.RunLocalCode, gate.NoConsent, true, time.Minute); err == nil {
		t.Errorf("must refuse to raise a dialog for an act the gate already permits")
	}
}

// TestApprovalCannotExceedTheCeiling — THE attack this seam exists to stop. A
// reviewer clicking allow-always must not widen anything: the gate is asked
// again, and the ceiling still refuses.
func TestApprovalCannotExceedTheCeiling(t *testing.T) {
	for _, d := range []Decision{AllowOnce, AllowAlways} {
		allowed, consent := Resolve(gate.RunLocalCode, gate.OfferCatalogue, true, d)
		if allowed {
			t.Errorf("%s let an act past its ceiling", d)
		}
		if consent != gate.FreshExplicit {
			t.Errorf("consent should still be recorded as fresh explicit, got %v", consent)
		}
	}
}

// TestUnknownDecisionFailsClosed — an approval surface that treated an
// unrecognised verdict as permission would be the whole vulnerability.
func TestUnknownDecisionFailsClosed(t *testing.T) {
	for _, d := range []Decision{"", "yes", "ALLOW-ONCE", "allow", "true", "deny "} {
		if allowed, _ := Resolve(gate.InstallArtifact, gate.RunLocalCode, true, d); allowed {
			t.Errorf("decision %q was treated as permission", d)
		}
	}
}

// TestResolveGrantsWhenTheGateAgrees
func TestResolveGrantsWhenTheGateAgrees(t *testing.T) {
	allowed, consent := Resolve(gate.InstallArtifact, gate.RunLocalCode, true, AllowOnce)
	if !allowed {
		t.Errorf("gate should permit install with fresh explicit consent under a sufficient ceiling")
	}
	if consent != gate.FreshExplicit {
		t.Errorf("consent = %v, want FreshExplicit", consent)
	}
}

// TestResolveOverTheWire drives approval.resolve end to end.
func TestResolveOverTheWire(t *testing.T) {
	s := testServer()
	a, err := s.NewPendingApproval(gate.InstallArtifact, gate.RunLocalCode, gate.NoConsent, true, time.Minute)
	if err != nil {
		t.Fatalf("NewPendingApproval: %v", err)
	}
	params, err := json.Marshal(approvalResolveParams{ID: a.ID, Decision: string(AllowOnce)})
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	frame, err := json.Marshal(requestFrame{Type: "req", ID: "r7", Method: "approval.resolve", Params: params})
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	out, fatal := s.handle(frame)
	if fatal {
		t.Fatalf("resolve should not be fatal")
	}
	if !strings.Contains(string(out), `"status":"allowed"`) {
		t.Errorf("want allowed, got %s", out)
	}
	if !strings.Contains(string(out), "ledger-112") {
		t.Errorf("the verdict should be attributed to the proven core, got %s", out)
	}

	// Replay: the id is consumed, so a second resolve cannot re-grant.
	out2, _ := s.handle(frame)
	if !strings.Contains(string(out2), "not_found") {
		t.Errorf("an approval must not be resolvable twice, got %s", out2)
	}
}
