// Package slackpost is ghillie's read-side ear on the owner's Slack — a
// connector cut to the same pattern as internal/post (Gmail) and
// internal/wapost (WhatsApp): route A, the owner's OWN token, READ-ONLY v1.
//
// ★ THERE IS NO SEND PATH. Not disabled — ABSENT. The package holds no code
// that can transmit a byte toward Slack: chat.postMessage and every other
// write method appear NOWHERE. The OAuth scopes requested are the history/read
// ones only (channels:history, groups:history, im:history, users:read), so a
// confused or compromised caller cannot post with the token this package holds.
// Send arrives, if ever, as its own gated work behind the disclosure ladder.
//
// ★ ARRIVING CONTENT IS DATA, NEVER INSTRUCTION. v1 reads only the sender's
// user id, a short text preview for the owner's eye, and the arrival time. No
// links are followed, no blocks parsed for meaning, no attachment fetched — a
// message's CONTENT cannot reach anything that acts. A refused sender's words
// appear NOWHERE: refuse counts, it does not quote.
//
// ★ EVERY PRESENTATION DECISION IS THE PROVEN CORE'S — the same
// Delivery_Policy_Pkg seam as internal/post, reused directly through
// post.Decider and DELIVERY_POLICY_DECIDER: one presentation law for
// everything that wants the owner's attention. Fail closed: an unwired or odd
// decider presents nothing and records a gap.
package slackpost

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/tonygair/ghillie/internal/post"
)

// Arrival is what v1 knows about one Slack message: who, what about, when. No
// body is kept — Subject is a short preview for the owner, not content for a
// machine to act on.
type Arrival struct {
	// ID is the composite "channel|ts" that addresses this one message for a
	// re-read; ts alone is not unique across conversations.
	ID string
	// From is the sender's Slack user id (e.g. "U0123ABCD"). Identity only —
	// the grant lookup keys on this, never on a display name the sender picks.
	From string
	// Subject is the first ~80 characters of the message text, newlines
	// flattened: enough for the owner to recognise the thread, never parsed.
	Subject string
	// At is the arrival time, from the Slack message ts.
	At time.Time
}

// Canonical normalizes a Slack user id for grant lookup. Slack ids are
// case-sensitive opaque handles, so this only trims surrounding space — it
// deliberately does NOT lower-case, which would collide distinct users.
func Canonical(userID string) string {
	return strings.TrimSpace(userID)
}

// LoadTable reads the same grants-file shape as internal/post, keyed by Slack
// user id instead of mail address. A missing file is the EMPTY table: a fresh
// ghillie refuses everyone until the owner grants someone, which is the
// no-spam default working as designed.
func LoadTable(path string) (post.Table, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return post.Table{}, nil
		}
		return nil, fmt.Errorf("slackpost: grants file: %w", err)
	}
	var t map[string]post.Grant
	if err := json.Unmarshal(raw, &t); err != nil {
		return nil, fmt.Errorf("slackpost: grants file %s: %w", path, err)
	}
	canon := make(post.Table, len(t))
	for id, g := range t {
		canon[Canonical(id)] = g
	}
	return canon, nil
}

// DecideArrival asks the proven core about one arrival, per-sender grants, the
// same fail-closed contract as post: any error means nothing is presented. An
// unknown sender is asked with Grant_Live = False, so even that refusal is the
// CORE's theorem rather than this package's opinion.
func DecideArrival(ctx context.Context, d post.Decider, t post.Table, a Arrival, presence string, quiet bool) (post.Decision, error) {
	g, ok := t[Canonical(a.From)]
	if !ok {
		return d.Decide(ctx, false, "whenever", "whenever", presence, quiet)
	}
	return d.Decide(ctx, g.Live, g.Ceiling, g.Ask, presence, quiet)
}

// preview flattens and trims message text to a short subject line. It reads the
// text ONLY to shorten it for the owner's eye — no meaning is extracted.
func preview(text string) string {
	text = strings.TrimSpace(strings.ReplaceAll(strings.ReplaceAll(text, "\n", " "), "\r", " "))
	const max = 80
	if len([]rune(text)) <= max {
		return text
	}
	return string([]rune(text)[:max]) + "…"
}
