package slackpost

// slack.go — the minimal READ-ONLY Slack Web API client: list the
// conversations the owner's token can see, then read message metadata newer
// than the watermark. Nothing else. The only endpoints named in this file are
// conversations.list and conversations.history — both reads. chat.postMessage
// and every write method are absent by construction, so the content-is-data
// and no-send rules are enforced by WHAT IS CALLED, not merely by intent.

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"
)

// DefaultBaseURL is Slack's Web API root; tests substitute their own.
const DefaultBaseURL = "https://slack.com/api"

// idSep joins channel and ts into an Arrival.ID. A raw byte unlikely to occur
// in either half keeps the split unambiguous.
const idSep = "|"

// Client reads the owner's Slack message metadata. TokenFn supplies a live
// token (xoxp user token or xoxb bot token) per call so the poller never holds
// a stale one.
type Client struct {
	BaseURL string
	TokenFn func(ctx context.Context) (string, error)
	// HTTP defaults to http.DefaultClient.
	HTTP *http.Client
}

// ListNewIDs returns composite "channel|ts" ids for messages that arrived
// after the watermark, across every conversation the token can see. It reads
// conversations.list once, then conversations.history per conversation with
// Slack's own oldest= filter. The total is capped at max to bound one poll.
func (c *Client) ListNewIDs(ctx context.Context, after time.Time, max int) (ids []string, err error) {
	channels, err := c.conversations(ctx, max)
	if err != nil {
		return nil, err
	}
	oldest := ""
	if !after.IsZero() {
		oldest = slackTS(after)
	}
	for _, ch := range channels {
		q := url.Values{"channel": {ch}, "limit": {strconv.Itoa(max)}}
		if oldest != "" {
			q.Set("oldest", oldest)
		}
		var r struct {
			Messages []struct {
				TS string `json:"ts"`
			} `json:"messages"`
		}
		if herr := c.get(ctx, "/conversations.history?"+q.Encode(), &r); herr != nil {
			return nil, herr
		}
		for _, m := range r.Messages {
			if m.TS == "" {
				continue
			}
			ids = append(ids, ch+idSep+m.TS)
			if len(ids) >= max {
				return ids, nil
			}
		}
	}
	return ids, nil
}

// GetArrival re-reads one message's metadata — sender, text preview, ts — by
// its composite id. It asks conversations.history for exactly that ts
// (latest=oldest, inclusive), so no more than the addressed message is fetched.
func (c *Client) GetArrival(ctx context.Context, id string) (Arrival, error) {
	channel, ts, ok := strings.Cut(id, idSep)
	if !ok {
		return Arrival{}, fmt.Errorf("slackpost: malformed arrival id %q", id)
	}
	q := url.Values{
		"channel":   {channel},
		"latest":    {ts},
		"oldest":    {ts},
		"inclusive": {"true"},
		"limit":     {"1"},
	}
	var r struct {
		Messages []struct {
			User string `json:"user"`
			Text string `json:"text"`
			TS   string `json:"ts"`
		} `json:"messages"`
	}
	if err := c.get(ctx, "/conversations.history?"+q.Encode(), &r); err != nil {
		return Arrival{}, err
	}
	if len(r.Messages) == 0 {
		return Arrival{}, fmt.Errorf("slackpost: message %q not found on re-read", id)
	}
	m := r.Messages[0]
	return Arrival{
		ID:      id,
		From:    m.User,
		Subject: preview(m.Text),
		At:      tsToTime(m.TS),
	}, nil
}

// conversations lists the ids of channels, groups and DMs the token can read.
// It requests only the read-scoped types; a token without a given scope simply
// sees fewer conversations, never gains a write.
func (c *Client) conversations(ctx context.Context, limit int) (ids []string, err error) {
	q := url.Values{
		"types": {"public_channel,private_channel,im,mpim"},
		"limit": {strconv.Itoa(limit)},
	}
	var r struct {
		Channels []struct {
			ID string `json:"id"`
		} `json:"channels"`
	}
	if err := c.get(ctx, "/conversations.list?"+q.Encode(), &r); err != nil {
		return nil, err
	}
	for _, ch := range r.Channels {
		ids = append(ids, ch.ID)
	}
	return ids, nil
}

// get performs one authorized read and decodes the JSON reply. Slack answers
// 200 with {"ok":false,"error":"..."} for logical failures, so this checks the
// envelope's ok field too — a refusal must surface, never read as empty.
func (c *Client) get(ctx context.Context, path string, into interface{}) error {
	base := c.BaseURL
	if base == "" {
		base = DefaultBaseURL
	}
	token, err := c.TokenFn(ctx)
	if err != nil {
		return err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, base+path, nil)
	if err != nil {
		return fmt.Errorf("slackpost: request: %w", err)
	}
	req.Header.Set("Authorization", "Bearer "+token)
	client := c.HTTP
	if client == nil {
		client = http.DefaultClient
	}
	resp, err := client.Do(req)
	if err != nil {
		return fmt.Errorf("slackpost: slack: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()
	body, err := io.ReadAll(io.LimitReader(resp.Body, 4<<20))
	if err != nil {
		return fmt.Errorf("slackpost: slack reply: %w", err)
	}
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("slackpost: slack refused: %s: %s", resp.Status, strings.TrimSpace(string(body)))
	}
	var env struct {
		OK    bool   `json:"ok"`
		Error string `json:"error"`
	}
	if err := json.Unmarshal(body, &env); err != nil {
		return fmt.Errorf("slackpost: slack reply: %w", err)
	}
	if !env.OK {
		return fmt.Errorf("slackpost: slack refused: %s", env.Error)
	}
	if err := json.Unmarshal(body, into); err != nil {
		return fmt.Errorf("slackpost: slack reply: %w", err)
	}
	return nil
}

// slackTS renders a time as a Slack ts (seconds.microseconds) for the oldest=
// filter.
func slackTS(t time.Time) string {
	return fmt.Sprintf("%d.%06d", t.Unix(), t.Nanosecond()/1000)
}

// tsToTime parses a Slack ts ("1756155600.001500") into a time. An unparseable
// ts yields the zero time, which the poll loop's After() guard treats as "not
// newer than the watermark" — safe, never a spurious present.
func tsToTime(ts string) time.Time {
	secPart, microPart, _ := strings.Cut(ts, ".")
	sec, err := strconv.ParseInt(secPart, 10, 64)
	if err != nil {
		return time.Time{}
	}
	var micro int64
	if microPart != "" {
		if micro, err = strconv.ParseInt(microPart, 10, 64); err != nil {
			micro = 0
		}
	}
	return time.Unix(sec, micro*1000).UTC()
}
