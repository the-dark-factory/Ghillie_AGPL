// Package credit separates the COURTESY balance ghillie shows the client from
// the AUTHORITY that decides whether an act may happen.
//
// ★ THE RULE THAT DECIDES THIS ARCHITECTURE: never trust a declaration computed
// on a machine we do not control. The OpenClaw client-trusted `senderIsOwner`
// field became CVE-2026-44118 for exactly this reason. The client's copy of a
// number is a display; it is not a permission.
//
// So the split is structural, not documentary:
//
//   - Courtesy is a number ghillie may DISPLAY. It has no method that returns a
//     permission, and there is nothing a caller can do with it except render it.
//     You cannot spend a Courtesy because there is no verb on it that spends.
//   - Decision comes back from the facade over the wire. It is the ONLY type in
//     this package that carries a verdict, and Authorise is the only function
//     that produces one.
//
// GHILLIE IS REQUIRED TO TALK ABOUT COST. That is in character, not a bolt-on:
// it is open with the client, says plainly when the factory needs to know more,
// and reminds them that describing the work precisely improves both how well it
// gets built and what it costs. The honest incentive alignment is real — a
// better interview is a cheaper build — so saying so is not a sales line.
package credit

import (
	"context"
	"errors"
	"fmt"
)

// ErrNoCredit reports that the FACADE declined the act for want of credit. It
// is never produced from a local number.
var ErrNoCredit = errors.New("credit: the factory reports insufficient credit for this act")

// ErrAuthorityUnreachable reports that the facade could not be asked. An act
// whose authority could not be consulted is NOT authorised — the failure is
// closed, not open.
var ErrAuthorityUnreachable = errors.New("credit: could not reach the factory to ask")

// Courtesy is a locally-held balance, shown to the client as a kindness so they
// are not surprised.
//
// ⚠ IT IS ADVISORY AND IT IS STRUCTURALLY INCAPABLE OF AUTHORISING ANYTHING.
// There is deliberately no Sufficient method, no Spend method and no comparison
// against a price. If you find yourself wanting one, the thing you actually want
// is Authorise, which asks the facade.
type Courtesy struct {
	// Units is the balance as this machine last understood it.
	Units int64
	// AsOf is a human-readable timestamp of when that understanding was formed.
	AsOf string
	// Stale marks a balance this machine has not refreshed. A stale courtesy is
	// still displayable — it is a courtesy either way.
	Stale bool
}

// Line renders the balance for the client, LABELLED AS ADVISORY every time. The
// label is part of the value, not decoration: a number shown without it would
// invite the client (and the next reader of this code) to treat it as authority.
func (c Courtesy) Line() string {
	staleness := ""
	if c.Stale {
		staleness = ", not refreshed this session"
	}
	return fmt.Sprintf("Credit on this machine: %d (a courtesy figure%s — the factory holds the real one and it is the factory that decides)", c.Units, staleness)
}

// CostGuidance is the sentence ghillie is required to be able to say. It is here
// rather than in the interview front end because it is a fact about how the
// factory charges, not a piece of interview content.
func CostGuidance() string {
	return "The more precisely you describe the work, the better it gets built and the less it costs — vagueness is the expensive part, not detail."
}

// Decision is the FACADE'S verdict on one act. It is the only thing in this
// package that authorises, and it can only be obtained from Authority.
type Decision struct {
	// Sufficient is the factory's answer. It arrives over the wire and is never
	// computed here.
	Sufficient bool `json:"sufficient"`
	// Note is the legible reason, shown to the client verbatim on a refusal.
	Note string `json:"note"`
	// Balance is what the FACADE says the balance is. It is reported for
	// display and reconciliation; it still does not authorise anything — the
	// Sufficient flag does.
	Balance int64 `json:"balance"`
}

// Authority is the facade, asked about one act. It is an interface so the
// terminal can be tested against a facade that says no.
type Authority interface {
	// Decide asks the factory whether this claw may perform this act now.
	Decide(ctx context.Context, act string) (Decision, error)
}

// Authorise asks the authority and turns its answer into a Go error, so that a
// refusal cannot be ignored by a caller that forgot to check a boolean.
//
// The two failure shapes are deliberately different errors: "the factory said
// no" and "the factory could not be asked" are different facts and the client
// deserves to be told which. Both refuse.
func Authorise(ctx context.Context, a Authority, act string) (Decision, error) {
	if a == nil {
		// No authority configured is not permission. It is the absence of an
		// answer, and the absence of an answer is a refusal.
		return Decision{}, fmt.Errorf("%w: no credit authority configured for %q", ErrAuthorityUnreachable, act)
	}
	d, err := a.Decide(ctx, act)
	if err != nil {
		return Decision{}, fmt.Errorf("%w: %s: %w", ErrAuthorityUnreachable, act, err)
	}
	if !d.Sufficient {
		note := d.Note
		if note == "" {
			note = "the factory declined this act"
		}
		return d, fmt.Errorf("%w: %s: %s", ErrNoCredit, act, note)
	}
	return d, nil
}
