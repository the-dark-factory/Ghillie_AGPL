// Package wapost is ghillie's read-side ear on the owner's WhatsApp — the
// connector approved 2026-08-25 (proposal_connector_whatsapp: ROUTE A, the
// linked-device protocol).
//
// ⚠ ROUTE A IS DISCLOSED, NOT HIDDEN: the linked-device protocol is not an
// official API, automating a personal account violates WhatsApp's terms, and
// the ACCOUNT-BAN RISK IS THE OWNER'S. The catalogue entry carries this above
// the fold, and cmd/ghillie-wa says it at startup. What route A buys in
// exchange is discretion: the end-to-end keys live on this machine and no
// Meta-side business proxy ever holds the owner's messages.
//
// ★ MESSAGE CONTENT IS NEVER READ. Stronger than the Gmail connector, which
// at least reads a Subject: WhatsApp has none, so v1 touches NOTHING but the
// sender identity and the arrival time. The *waE2E.Message payload is not
// opened anywhere in this package — a presentation is "a message from X",
// full stop. Media, stickers, captions: unread, undownloaded.
//
// ★ EVERY PRESENTATION DECISION IS THE PROVEN CORE'S — the same
// Delivery_Policy_Pkg seam as internal/post, reused directly: one presentation
// law for everything that wants the owner's attention.
package wapost

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/tonygair/ghillie/internal/post"

	"go.mau.fi/whatsmeow/types"
	"go.mau.fi/whatsmeow/types/events"
)

// Arrival is what v1 knows about one message: who and when. There is no
// subject field because there is no subject; there is no body field because
// bodies are never read.
type Arrival struct {
	// Sender is the canonical number (digits only, country code first).
	Sender string
	// PushName is the sender's self-declared display name — UNTRUSTED, shown
	// only beside the number, never used for the grant lookup.
	PushName string
	// Chat is the canonical group number when the message arrived in a group,
	// empty for direct messages. Grants are PER-SENDER in v1 either way.
	Chat string
	At   time.Time
}

// CanonicalNumber reduces a phone-number-ish string to digits: "+44 7700
// 900123", "447700900123" and a JID user "447700900123" all key the same
// grant.
func CanonicalNumber(s string) string {
	var b strings.Builder
	for _, r := range s {
		if r >= '0' && r <= '9' {
			b.WriteRune(r)
		}
	}
	return b.String()
}

// LoadTable reads the same grants-file shape as internal/post, keyed by
// number instead of address. A missing file is the empty table: a fresh
// ghillie refuses everyone, which is the no-spam default working.
func LoadTable(path string) (post.Table, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return post.Table{}, nil
		}
		return nil, fmt.Errorf("wapost: grants file: %w", err)
	}
	var t map[string]post.Grant
	if err := json.Unmarshal(raw, &t); err != nil {
		return nil, fmt.Errorf("wapost: grants file %s: %w", path, err)
	}
	canon := make(post.Table, len(t))
	for num, g := range t {
		canon[CanonicalNumber(num)] = g
	}
	return canon, nil
}

// FromEvent reads the arrival out of a whatsmeow message event — identity and
// time ONLY. It returns ok=false for the messages v1 ignores entirely: the
// owner's own sends echoed back, and anything with no sender.
func FromEvent(evt *events.Message) (a Arrival, ok bool) {
	if evt == nil || evt.Info.IsFromMe || evt.Info.Sender.User == "" {
		return Arrival{}, false
	}
	a = Arrival{
		Sender:   CanonicalNumber(evt.Info.Sender.User),
		PushName: evt.Info.PushName,
		At:       evt.Info.Timestamp,
	}
	if evt.Info.IsGroup {
		a.Chat = CanonicalNumber(evt.Info.Chat.User)
	}
	return a, true
}

// GroupJID reports whether a JID is a group, exposed for callers that log.
func GroupJID(j types.JID) bool { return j.Server == types.GroupServer }

// DecideArrival asks the proven core about one arrival, per-sender grants,
// same fail-closed contract as post: any error means nothing is presented.
func DecideArrival(ctx context.Context, d post.Decider, t post.Table, a Arrival, presence string, quiet bool) (post.Decision, error) {
	g, ok := t[a.Sender]
	if !ok {
		return d.Decide(ctx, false, "whenever", "whenever", presence, quiet)
	}
	return d.Decide(ctx, g.Live, g.Ceiling, g.Ask, presence, quiet)
}
