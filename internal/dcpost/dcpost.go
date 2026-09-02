// Package dcpost is ghillie's read-side ear on the owner's Discord — a
// connector cut to the same pattern as internal/post (Gmail) and
// internal/wapost (WhatsApp): route A, the owner's OWN bot token, READ-ONLY v1.
//
// ★ THERE IS NO SEND PATH. Not disabled — ABSENT. The package holds no code
// that can transmit a byte toward Discord: POST /channels/{id}/messages and
// every other write appear NOWHERE. The ONLY endpoint this connector calls is
// GET /channels/{id}/messages — a read. The connection is plain REST polling;
// no gateway socket is opened, so the bot cannot even be pushed a write path.
//
// ★ ARRIVING CONTENT IS DATA, NEVER INSTRUCTION. v1 reads only the author's
// username, a short content preview for the owner's eye, and the arrival time.
// No links are followed, no embeds parsed for meaning, no attachment fetched —
// a message's CONTENT cannot reach anything that acts. A refused author's words
// appear NOWHERE: refuse counts, it does not quote.
//
// ★ EVERY PRESENTATION DECISION IS THE PROVEN CORE'S — the same
// Delivery_Policy_Pkg seam as internal/post, reused directly through
// post.Decider and DELIVERY_POLICY_DECIDER: one presentation law for
// everything that wants the owner's attention. Fail closed: an unwired or odd
// decider presents nothing and records a gap.
package dcpost

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/tonygair/ghillie/internal/post"
)

// Arrival is what v1 knows about one Discord message: who, what about, when.
// No body is kept — Subject is a short preview for the owner, not content for a
// machine to act on.
type Arrival struct {
	// ID is the message snowflake; it doubles as the per-channel watermark
	// (snowflakes sort in arrival order) and dedup key.
	ID string
	// Channel is the channel snowflake this message arrived in — carried so the
	// poller can persist a per-channel watermark.
	Channel string
	// From is the author's username. Identity only; the grant lookup keys on
	// its canonical form.
	From string
	// Subject is the first ~80 characters of the message content, newlines
	// flattened: enough for the owner to recognise the author, never parsed.
	Subject string
	// At is the arrival time, from the message timestamp (RFC 3339).
	At time.Time
}

// Canonical normalizes a Discord username for grant lookup: lower-cased,
// surrounding space trimmed. Discord usernames are case-insensitive handles.
func Canonical(username string) string {
	return strings.ToLower(strings.TrimSpace(username))
}

// LoadTable reads the same grants-file shape as internal/post, keyed by Discord
// username instead of mail address. A missing file is the EMPTY table: a fresh
// ghillie refuses everyone until the owner grants someone, which is the
// no-spam default working as designed.
func LoadTable(path string) (post.Table, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return post.Table{}, nil
		}
		return nil, fmt.Errorf("dcpost: grants file: %w", err)
	}
	var t map[string]post.Grant
	if err := json.Unmarshal(raw, &t); err != nil {
		return nil, fmt.Errorf("dcpost: grants file %s: %w", path, err)
	}
	canon := make(post.Table, len(t))
	for u, g := range t {
		canon[Canonical(u)] = g
	}
	return canon, nil
}

// LoadChannels reads the newline-separated list of channel snowflakes the owner
// wants watched (a "# comment" and blank lines allowed). A missing file is the
// empty list: nothing is polled until the owner names a channel, the no-spam
// default working as designed.
func LoadChannels(path string) ([]string, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, fmt.Errorf("dcpost: channels file: %w", err)
	}
	var ids []string
	for _, line := range strings.Split(string(raw), "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		ids = append(ids, line)
	}
	return ids, nil
}

// ParseMessages reads arrivals out of a GET /channels/{id}/messages reply body
// — identity and time ONLY. It stamps each arrival with the channel it came
// from. Messages without an author are ignored: they are nobody's arrival.
// Errors are wrapped, never swallowed as an empty batch.
func ParseMessages(body []byte, channel string) (arrivals []Arrival, err error) {
	var msgs []struct {
		ID        string `json:"id"`
		Content   string `json:"content"`
		Timestamp string `json:"timestamp"`
		Author    *struct {
			Username string `json:"username"`
		} `json:"author"`
	}
	if uerr := json.Unmarshal(body, &msgs); uerr != nil {
		return nil, fmt.Errorf("dcpost: messages reply: %w", uerr)
	}
	for _, m := range msgs {
		if m.Author == nil || m.Author.Username == "" {
			continue
		}
		at, terr := time.Parse(time.RFC3339, m.Timestamp)
		if terr != nil {
			// An unparseable timestamp yields the zero time, which the poller's
			// ordering treats conservatively — never a spurious present.
			at = time.Time{}
		}
		arrivals = append(arrivals, Arrival{
			ID:      m.ID,
			Channel: channel,
			From:    m.Author.Username,
			Subject: preview(m.Content),
			At:      at.UTC(),
		})
	}
	return arrivals, nil
}

// DecideArrival asks the proven core about one arrival, per-sender grants, the
// same fail-closed contract as post: any error means nothing is presented. An
// unknown author is asked with Grant_Live = False, so even that refusal is the
// CORE's theorem rather than this package's opinion.
func DecideArrival(ctx context.Context, d post.Decider, t post.Table, a Arrival, presence string, quiet bool) (post.Decision, error) {
	g, ok := t[Canonical(a.From)]
	if !ok {
		return d.Decide(ctx, false, "whenever", "whenever", presence, quiet)
	}
	return d.Decide(ctx, g.Live, g.Ceiling, g.Ask, presence, quiet)
}

// preview flattens and trims message content to a short subject line. It reads
// the content ONLY to shorten it for the owner's eye — no meaning is extracted.
func preview(content string) string {
	content = strings.TrimSpace(strings.ReplaceAll(strings.ReplaceAll(content, "\n", " "), "\r", " "))
	const max = 80
	if len([]rune(content)) <= max {
		return content
	}
	return string([]rune(content)[:max]) + "…"
}
