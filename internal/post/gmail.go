package post

// gmail.go — the minimal read-only Gmail REST client: list new message ids,
// fetch From/Subject/arrival-time metadata. Nothing else. format=metadata
// with named headers means BODIES ARE NEVER FETCHED — the API is asked only
// for what v1 presents, so the content-is-data rule is enforced by what is
// requested, not merely by what is read.

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

// DefaultBaseURL is Gmail's REST root; tests substitute their own.
const DefaultBaseURL = "https://gmail.googleapis.com"

// Client reads the owner's mailbox metadata. TokenFn supplies a live access
// token per call so the poller never holds a stale one.
type Client struct {
	BaseURL string
	TokenFn func(ctx context.Context) (string, error)
	// HTTP defaults to http.DefaultClient.
	HTTP *http.Client
}

// Arrival is what v1 knows about one message: who, what about, when. No body.
type Arrival struct {
	ID      string
	From    string
	Subject string
	At      time.Time
}

// ListNewIDs returns message ids arrived after the watermark, oldest last
// (Gmail lists newest first; callers reverse if order matters). The after
// query is Gmail's own epoch-seconds filter.
func (c *Client) ListNewIDs(ctx context.Context, after time.Time, max int) (ids []string, err error) {
	q := url.Values{"maxResults": {strconv.Itoa(max)}}
	if !after.IsZero() {
		q.Set("q", "after:"+strconv.FormatInt(after.Unix(), 10))
	}
	var r struct {
		Messages []struct {
			ID string `json:"id"`
		} `json:"messages"`
	}
	if err := c.get(ctx, "/gmail/v1/users/me/messages?"+q.Encode(), &r); err != nil {
		return nil, err
	}
	for _, m := range r.Messages {
		ids = append(ids, m.ID)
	}
	return ids, nil
}

// GetArrival fetches one message's metadata — From, Subject, internalDate —
// and nothing more.
func (c *Client) GetArrival(ctx context.Context, id string) (Arrival, error) {
	path := "/gmail/v1/users/me/messages/" + url.PathEscape(id) +
		"?format=metadata&metadataHeaders=From&metadataHeaders=Subject"
	var r struct {
		ID           string `json:"id"`
		InternalDate string `json:"internalDate"`
		Payload      struct {
			Headers []struct {
				Name  string `json:"name"`
				Value string `json:"value"`
			} `json:"headers"`
		} `json:"payload"`
	}
	if err := c.get(ctx, path, &r); err != nil {
		return Arrival{}, err
	}
	a := Arrival{ID: r.ID}
	for _, h := range r.Payload.Headers {
		switch strings.ToLower(h.Name) {
		case "from":
			a.From = h.Value
		case "subject":
			a.Subject = h.Value
		}
	}
	if ms, perr := strconv.ParseInt(r.InternalDate, 10, 64); perr == nil {
		a.At = time.UnixMilli(ms)
	}
	return a, nil
}

// get performs one authorized read and decodes the JSON reply.
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
		return fmt.Errorf("post: gmail request: %w", err)
	}
	req.Header.Set("Authorization", "Bearer "+token)
	client := c.HTTP
	if client == nil {
		client = http.DefaultClient
	}
	resp, err := client.Do(req)
	if err != nil {
		return fmt.Errorf("post: gmail: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()
	body, err := io.ReadAll(io.LimitReader(resp.Body, 4<<20))
	if err != nil {
		return fmt.Errorf("post: gmail reply: %w", err)
	}
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("post: gmail refused: %s: %s", resp.Status, strings.TrimSpace(string(body)))
	}
	if err := json.Unmarshal(body, into); err != nil {
		return fmt.Errorf("post: gmail reply: %w", err)
	}
	return nil
}
