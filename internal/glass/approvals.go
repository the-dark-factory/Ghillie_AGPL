package glass

import (
	"encoding/json"
	"fmt"
	"time"

	"github.com/tonygair/ghillie/internal/gate"
)

// Approvals — the seam where the third-party glass meets the proven gate.
//
// THE LOAD-BEARING PROPERTY: a human cannot approve what the gate forbids.
//
// An approval dialog supplies exactly one thing — fresh explicit consent — and
// consent is only ever an INPUT to Facade_Command_Pkg.May_Command (ada-factory
// ledger 112), never a substitute for it. The ceiling, the local-only rule and
// the authenticity requirement are unaffected by anything the reviewer clicks.
// So the gate is asked twice: once to find out whether consent is the ONLY
// thing missing, and once again afterwards with the consent the human actually
// gave. The second answer is the gate's, not the UI's.
//
// A corollary worth stating because it is easy to get wrong: if an act would be
// refused even WITH fresh explicit consent, no dialog is raised at all. Asking a
// person to approve something that will be refused regardless is a lie about who
// is deciding, and it trains reviewers to click through prompts — which is the
// habit every approval-fatigue attack is built on.

// Decision mirrors ApprovalDecisionSchema.
type Decision string

const (
	AllowOnce   Decision = "allow-once"
	AllowAlways Decision = "allow-always"
	Deny        Decision = "deny"
)

// execPresentation mirrors ExecApprovalPresentationSchema. Their description is
// explicit that runtime cwd, environment and execution plan are excluded — the
// reviewer sees what they are deciding about and nothing that would leak the
// machine's state to a surface that does not need it.
type execPresentation struct {
	Kind             string   `json:"kind"`
	CommandText      string   `json:"commandText"`
	CommandPreview   *string  `json:"commandPreview,omitempty"`
	WarningText      *string  `json:"warningText,omitempty"`
	AllowedDecisions []string `json:"allowedDecisions"`
}

// pendingApproval mirrors PendingApprovalSnapshotSchema
// (ApprovalRecordCommonFields + status).
type pendingApproval struct {
	ID           string           `json:"id"`
	URLPath      string           `json:"urlPath"`
	CreatedAtMs  int64            `json:"createdAtMs"`
	ExpiresAtMs  int64            `json:"expiresAtMs"`
	Presentation execPresentation `json:"presentation"`
	Status       string           `json:"status"`
}

// Askable reports whether an act should raise an approval dialog at all, and is
// the first half of the two-question rule.
//
//	allowed   — the gate already permits it; do not ask, just proceed.
//	askable   — refused ONLY for want of fresh explicit consent; a dialog can help.
//	otherwise — refused for a reason no human may override; refuse silently.
func Askable(c gate.Command, ceiling gate.Command, k gate.Consent, authentic bool) (allowed, askable bool) {
	if gate.MayCommand(c, ceiling, k, authentic) {
		return true, false
	}
	// Would the strongest possible consent rescue it? If not, the refusal is
	// structural (ceiling, local-only, inauthentic) and is none of the
	// reviewer's business.
	return false, gate.MayCommand(c, ceiling, gate.FreshExplicit, authentic)
}

// Resolve turns a reviewer decision into a verdict by ASKING THE GATE AGAIN.
// It does not consult the decision except to derive the consent level, so a
// malformed or hostile decision string can only ever fail closed.
func Resolve(c gate.Command, ceiling gate.Command, authentic bool, d Decision) (allowed bool, consent gate.Consent) {
	switch d {
	case AllowOnce, AllowAlways:
		consent = gate.FreshExplicit
	default:
		// Deny, and anything unrecognised. An approval surface that treated an
		// unknown verdict as permission would be the whole vulnerability.
		return false, gate.NoConsent
	}
	return gate.MayCommand(c, ceiling, consent, authentic), consent
}

// NewPendingApproval builds the reviewer-facing snapshot for an askable act.
// It returns an error rather than a dialog when the act is not askable, so the
// only way to show a prompt is to have passed the gate's own test.
func (s *Server) NewPendingApproval(c gate.Command, ceiling gate.Command, k gate.Consent, authentic bool, ttl time.Duration) (approval pendingApproval, err error) {
	allowed, askable := Askable(c, ceiling, k, authentic)
	if allowed {
		return pendingApproval{}, fmt.Errorf("glass: %s is already permitted; no approval to raise", c)
	}
	if !askable {
		_, reason := gate.Evaluate(c, ceiling, k, authentic)
		return pendingApproval{}, fmt.Errorf("glass: %s is refused for %s, which no reviewer may override", c, reason)
	}

	now := s.Now()
	id := s.NewConnID() + "-" + c.String()
	expires := now.Add(ttl).UnixMilli()
	warning := "Granting this supplies consent only. The proven gate still decides."

	// Remember the act SERVER-SIDE. The reviewer's later frame carries only an
	// id; the command it authorises is read from here, never from the wire.
	if s.pending == nil {
		s.pending = map[string]pendingAct{}
	}
	s.pending[id] = pendingAct{command: c, ceiling: ceiling, authentic: authentic, expiresAtMs: expires}

	return pendingApproval{
		ID:          id,
		URLPath:     "/approvals/" + id,
		CreatedAtMs: now.UnixMilli(),
		ExpiresAtMs: expires,
		Status:      "pending",
		Presentation: execPresentation{
			Kind:        "exec",
			CommandText: c.String(),
			WarningText: &warning,
			// ApprovalAllowedDecisionsSchema requires that "deny" is always
			// among the offered decisions. A reviewer must always be able to
			// refuse, so this is never computed from context.
			AllowedDecisions: []string{string(AllowOnce), string(Deny)},
		},
	}, nil
}

// approvalResolveParams mirrors the reviewer's inbound decision.
type approvalResolveParams struct {
	ID       string `json:"id"`
	Decision string `json:"decision"`
}

// handleApprovalResolve answers "approval.resolve".
func (s *Server) handleApprovalResolve(req requestFrame) (out []byte, fatal bool) {
	var p approvalResolveParams
	if len(req.Params) > 0 {
		if err := json.Unmarshal(req.Params, &p); err != nil {
			return s.encodeErr(req.ID, "bad_request", "approval.resolve params are not valid JSON"), false
		}
	}
	if p.ID == "" {
		return s.encodeErr(req.ID, "bad_request", "approval.resolve requires an id"), false
	}

	pend, ok := s.pending[p.ID]
	if !ok {
		return s.encodeErr(req.ID, "not_found", "no such pending approval"), false
	}
	if s.Now().UnixMilli() > pend.expiresAtMs {
		delete(s.pending, p.ID)
		return s.encodeErr(req.ID, "expired", "the approval window closed"), false
	}
	delete(s.pending, p.ID)

	allowed, consent := Resolve(pend.command, pend.ceiling, pend.authentic, Decision(p.Decision))
	payload := map[string]any{
		"id":        p.ID,
		"status":    map[bool]string{true: "allowed", false: "denied"}[allowed],
		"decidedBy": "facade_command_pkg/ledger-112",
		"consent":   consent.String(),
	}
	body, err := json.Marshal(responseFrame{Type: "res", ID: req.ID, OK: true, Payload: payload})
	if err != nil {
		return s.encodeErr(req.ID, "internal", "could not encode approval result"), false
	}
	return body, false
}
