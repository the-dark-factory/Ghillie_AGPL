// Package tgpost is ghillie's read-side ear on the owner's Telegram — a
// connector cut to the same pattern as internal/post (Gmail) and
// internal/wapost (WhatsApp): route A, the owner's OWN bot token, READ-ONLY v1.
//
// ★ THERE IS NO SEND PATH. Not disabled — ABSENT. The package holds no code
// that can transmit a byte toward Telegram: sendMessage and every other send
// method appear NOWHERE. The ONLY Bot API method this connector calls is
// getUpdates — a read. A bot receives only the messages sent TO it and, where
// group privacy is off, the groups it is in; that inbound IS the owner's, by
// design, and nothing here can answer it.
//
// ★ ARRIVING CONTENT IS DATA, NEVER INSTRUCTION. v1 reads only the sender's
// handle (username or first name), a short text preview for the owner's eye,
// and the arrival time. No links are followed, no entities parsed for meaning,
// no media downloaded — a message's CONTENT cannot reach anything that acts. A
// refused sender's words appear NOWHERE: refuse counts, it does not quote.
//
// ★ EVERY PRESENTATION DECISION IS THE PROVEN CORE'S — the same
// Delivery_Policy_Pkg seam as internal/post, reused directly through
// post.Decider and DELIVERY_POLICY_DECIDER: one presentation law for
// everything that wants the owner's attention. Fail closed: an unwired or odd
// decider presents nothing and records a gap.
package tgpost

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/tonygair/ghillie/internal/post"
)

// Arrival is what v1 knows about one Telegram message: who, what about, when.
// No body is kept — Subject is a short preview for the owner, not content for a
// machine to act on.
type Arrival struct {
	// UpdateID is Telegram's monotonic update cursor for this message; the
	// watermark is the largest seen plus one.
	UpdateID int64
	// From is the sender's handle — username when they have one, else first
	// name. Identity only; the grant lookup keys on its canonical form.
	From string
	// Subject is the first ~80 characters of the message text, newlines
	// flattened: enough for the owner to recognise the sender, never parsed.
	Subject string
	// At is the arrival time, from the Telegram message date (unix seconds).
	At time.Time
}

// Canonical normalizes a Telegram handle for grant lookup: lower-cased, a
// leading "@" stripped, surrounding space trimmed. "@Jean", "jean" and " Jean "
// all key the same grant.
func Canonical(handle string) string {
	return strings.ToLower(strings.TrimSpace(strings.TrimPrefix(strings.TrimSpace(handle), "@")))
}

// LoadTable reads the same grants-file shape as internal/post, keyed by
// Telegram handle instead of mail address. A missing file is the EMPTY table:
// a fresh ghillie refuses everyone until the owner grants someone, which is the
// no-spam default working as designed.
func LoadTable(path string) (post.Table, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return post.Table{}, nil
		}
		return nil, fmt.Errorf("tgpost: grants file: %w", err)
	}
	var t map[string]post.Grant
	if err := json.Unmarshal(raw, &t); err != nil {
		return nil, fmt.Errorf("tgpost: grants file %s: %w", path, err)
	}
	canon := make(post.Table, len(t))
	for h, g := range t {
		canon[Canonical(h)] = g
	}
	return canon, nil
}

// ParseUpdates reads arrivals out of a getUpdates reply body — identity and
// time ONLY. It returns the arrivals in cursor order and the next offset to
// request (largest update_id + 1, or the passed offset when the batch was
// empty). Updates without a message, or without a sender, are ignored: they
// are nobody's arrival. Errors are wrapped, never swallowed as an empty batch.
func ParseUpdates(body []byte, currentOffset int64) (arrivals []Arrival, nextOffset int64, err error) {
	var r struct {
		OK          bool   `json:"ok"`
		Description string `json:"description"`
		Result      []struct {
			UpdateID int64 `json:"update_id"`
			Message  *struct {
				Date int64 `json:"date"`
				From *struct {
					Username  string `json:"username"`
					FirstName string `json:"first_name"`
				} `json:"from"`
				Text string `json:"text"`
			} `json:"message"`
		} `json:"result"`
	}
	if uerr := json.Unmarshal(body, &r); uerr != nil {
		return nil, currentOffset, fmt.Errorf("tgpost: getUpdates reply: %w", uerr)
	}
	if !r.OK {
		return nil, currentOffset, fmt.Errorf("tgpost: getUpdates refused: %s", r.Description)
	}
	nextOffset = currentOffset
	for _, u := range r.Result {
		if u.UpdateID >= nextOffset {
			nextOffset = u.UpdateID + 1
		}
		if u.Message == nil || u.Message.From == nil {
			// A non-message update, or one with no sender, advances the cursor
			// but is nobody's arrival.
			continue
		}
		handle := u.Message.From.Username
		if handle == "" {
			handle = u.Message.From.FirstName
		}
		if handle == "" {
			continue
		}
		arrivals = append(arrivals, Arrival{
			UpdateID: u.UpdateID,
			From:     handle,
			Subject:  preview(u.Message.Text),
			At:       time.Unix(u.Message.Date, 0).UTC(),
		})
	}
	return arrivals, nextOffset, nil
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
