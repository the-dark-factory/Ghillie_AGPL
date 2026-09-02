// Package post is ghillie's read-side ear on the owner's mail — the Gmail
// connector approved 2026-08-25 (proposal_connector_gmail: ROUTE A, the owner's
// own OAuth client, READ-ONLY v1).
//
// ★ THERE IS NO SEND PATH. Not disabled — ABSENT. The package holds no code
// that can transmit a byte toward a mailbox, and the OAuth scope requested is
// gmail.readonly, so a compromised or confused caller cannot send with the
// credentials this package holds. Send arrives, if ever, as its own gated work
// behind the disclosure ladder.
//
// ★ ARRIVING MAIL IS DATA, NEVER INSTRUCTION. Nothing here parses a body for
// meaning; v1 reads only From, Subject and the arrival time. No HTML, no
// links, no attachments — a message's CONTENT cannot reach anything that acts.
//
// ★ EVERY PRESENTATION DECISION IS THE PROVEN CORE'S. Whether an arrival is
// presented, queued, digested or refused is decided by Delivery_Policy_Pkg
// (ledger 167) through its front, in the cvgate/credit.FrontDecider shape:
// decider named by environment, NO LOCAL FALLBACK, absent-or-odd means
// nothing is presented. A sender with no grant is asked with Grant_Live =
// False, so even that refusal is proof-carried rather than glue's opinion.
package post

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"strings"

	"github.com/tonygair/ghillie/internal/gapledger"
)

// EnvDecider names the delivery_policy_front binary. Same convention as
// cvgate's CV_DISCLOSURE_DECIDER: env-named, exec'd, refusal on absence.
const EnvDecider = "DELIVERY_POLICY_DECIDER"

// Errors mirror the cvgate family so callers read one vocabulary.
var (
	ErrDeciderUnwired = errors.New("post: DELIVERY_POLICY_DECIDER unset — no proven decider, so nothing is presented")
	ErrDeciderMissing = errors.New("post: decider binary missing")
	ErrDeciderRun     = errors.New("post: decider failed to run")
	ErrDeciderVerdict = errors.New("post: decider spoke an unknown word")
)

// Grant is one sender's standing with the owner. Class words are passed to the
// proven core VERBATIM — this package deliberately holds no list of valid
// classes, because the enumeration is the core's and a copy here could drift;
// a wrong word in the table comes back as the front's exit 3 and fails closed.
type Grant struct {
	// Ceiling is the class the owner granted this sender.
	Ceiling string `json:"ceiling"`
	// Ask is the standing class this sender's mail asks at. Mail carries no
	// urgency field, so the owner sets it here per sender; empty means
	// "whenever" — the deliberately dumb default that stops a subject line
	// shouting its way to the front.
	Ask string `json:"ask,omitempty"`
	// Live is false for a revoked sender kept on file.
	Live bool `json:"live"`
}

// Table maps canonical addresses to grants. Loaded from the owner's grants
// file; absence of an address IS the no-grant case, not an error.
type Table map[string]Grant

// LoadTable reads the grants file. A missing file is an EMPTY table — a fresh
// ghillie refuses everything until the owner grants someone, which is the
// no-spam default working as designed.
func LoadTable(path string) (Table, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return Table{}, nil
		}
		return nil, fmt.Errorf("post: grants file: %w", err)
	}
	var t Table
	if err := json.Unmarshal(raw, &t); err != nil {
		return nil, fmt.Errorf("post: grants file %s: %w", path, err)
	}
	canon := make(Table, len(t))
	for addr, g := range t {
		canon[CanonicalAddress(addr)] = g
	}
	return canon, nil
}

// CanonicalAddress normalizes a mail address for lookup: lower-cased, angle
// brackets and display name stripped. "Jean Gair <Jean@Example.COM>" and
// "jean@example.com" are the same sender.
func CanonicalAddress(s string) string {
	s = strings.TrimSpace(s)
	if i := strings.LastIndexByte(s, '<'); i >= 0 {
		if j := strings.IndexByte(s[i:], '>'); j > 0 {
			s = s[i+1 : i+j]
		}
	}
	return strings.ToLower(strings.TrimSpace(s))
}

// Lookup finds the sender's grant. ok=false is the no-grant case.
func (t Table) Lookup(fromHeader string) (g Grant, ok bool) {
	g, ok = t[CanonicalAddress(fromHeader)]
	return g, ok
}

// Decision is the proven core's answer, verbatim and lower-case.
type Decision string

// The five words Delivery_Policy_Pkg can answer. Held as constants for
// callers' switch statements — comparison only, never a local re-decision.
const (
	Refuse     Decision = "refuse"
	Digest     Decision = "digest"
	QueueUntil Decision = "queue_until"
	Interrupt  Decision = "interrupt"
	PresentNow Decision = "present_now"
)

// Decider asks the proven front. It carries the gap ledger so an unwired
// decider is RECORDED as a gap (the self-enhancement mechanism's trigger)
// rather than silently swallowed.
type Decider struct {
	// Gaps records ErrDeciderUnwired occurrences. Unwired ledger = no record,
	// same as gapledger's own contract.
	Gaps *gapledger.Ledger
}

// Decide asks Delivery_Policy_Pkg through its front and returns its word.
//
// FAIL-CLOSED CONTRACT: any error means "present nothing". The front's own
// convention — a caller that sees output can trust it; a caller that sees
// none must refuse to present — is preserved exactly: non-zero exit yields an
// error here, never a Decision.
func (d Decider) Decide(ctx context.Context, grantLive bool, ceiling, asked, presence string, quiet bool) (Decision, error) {
	path := os.Getenv(EnvDecider)
	if path == "" {
		if d.Gaps.Wired() {
			// Recording the gap can fail (disk, perms); the refusal must not
			// depend on the recording, so the error is deliberately dropped.
			_ = d.Gaps.Record(ctx, "delivery_policy_front",
				"how should an arriving message be presented",
				"post: arrival with "+EnvDecider+" unset")
		}
		return "", ErrDeciderUnwired
	}
	if _, err := os.Stat(path); err != nil {
		return "", fmt.Errorf("%w: %s: %v", ErrDeciderMissing, path, err)
	}
	if asked == "" {
		asked = "whenever"
	}
	out, err := exec.CommandContext(ctx, path, "decide",
		boolWord(grantLive), ceiling, asked, presence, boolWord(quiet)).Output()
	if err != nil {
		return "", fmt.Errorf("%w: %s: %v", ErrDeciderRun, path, err)
	}
	word := Decision(strings.TrimSpace(string(out)))
	switch word {
	case Refuse, Digest, QueueUntil, Interrupt, PresentNow:
		return word, nil
	default:
		return "", fmt.Errorf("%w: %q", ErrDeciderVerdict, word)
	}
}

// DecideArrival is the whole read-side seam in one call: look the sender up,
// ask the core, return its word. An unknown sender is asked with Grant_Live =
// False and placeholder classes — the refusal that comes back is the PROVEN
// core's (its first theorem), not this package's.
func (d Decider) DecideArrival(ctx context.Context, t Table, fromHeader, presence string, quiet bool) (Decision, error) {
	g, ok := t.Lookup(fromHeader)
	if !ok {
		return d.Decide(ctx, false, "whenever", "whenever", presence, quiet)
	}
	return d.Decide(ctx, g.Live, g.Ceiling, g.Ask, presence, quiet)
}

func boolWord(b bool) string {
	if b {
		return "true"
	}
	return "false"
}
