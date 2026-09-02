package tgpost

// telegram.go — the minimal READ-ONLY Telegram Bot API client: long-poll
// getUpdates for messages sent to the owner's bot, and getMe to validate the
// token at setup. Those two reads are the ONLY methods named in this file.
// sendMessage and every write method are absent by construction, so the
// no-send rule is enforced by WHAT IS CALLED, not merely by intent.

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
)

// DefaultBaseURL is Telegram's Bot API root; tests substitute their own.
const DefaultBaseURL = "https://api.telegram.org"

// Client reads the owner's bot inbound. Token is the BotFather token; it rides
// in the path segment "/bot<token>/" as the Bot API requires, never as a query
// parameter.
type Client struct {
	BaseURL string
	Token   string
	// HTTP defaults to http.DefaultClient.
	HTTP *http.Client
}

// GetUpdates long-polls for messages newer than offset and parses arrivals out
// of the reply. It returns the arrivals and the next offset to persist as the
// watermark. Telegram has no time filter — its cursor is the update_id — so
// the watermark here is that integer, not an arrival time.
func (c *Client) GetUpdates(ctx context.Context, offset int64, timeout, limit int) (arrivals []Arrival, nextOffset int64, err error) {
	q := url.Values{
		"offset":  {strconv.FormatInt(offset, 10)},
		"timeout": {strconv.Itoa(timeout)},
		"limit":   {strconv.Itoa(limit)},
		// Ask only for message updates: the connector reads inbound messages,
		// nothing else the Bot API could deliver.
		"allowed_updates": {`["message"]`},
	}
	body, err := c.get(ctx, "getUpdates?"+q.Encode())
	if err != nil {
		return nil, offset, err
	}
	return ParseUpdates(body, offset)
}

// Username validates the token and returns the bot's own username, for the
// -login setup path. It is a read (getMe) and touches no message.
func (c *Client) Username(ctx context.Context) (string, error) {
	body, err := c.get(ctx, "getMe")
	if err != nil {
		return "", err
	}
	var r struct {
		OK          bool   `json:"ok"`
		Description string `json:"description"`
		Result      struct {
			Username string `json:"username"`
		} `json:"result"`
	}
	if uerr := json.Unmarshal(body, &r); uerr != nil {
		return "", fmt.Errorf("tgpost: getMe reply: %w", uerr)
	}
	if !r.OK {
		return "", fmt.Errorf("tgpost: getMe refused: %s", r.Description)
	}
	return r.Result.Username, nil
}

// get performs one Bot API read and returns the raw JSON body. The token is in
// the path, so this URL must never be logged; callers log the method, not this.
func (c *Client) get(ctx context.Context, method string) ([]byte, error) {
	base := c.BaseURL
	if base == "" {
		base = DefaultBaseURL
	}
	u := base + "/bot" + c.Token + "/" + method
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
	if err != nil {
		return nil, fmt.Errorf("tgpost: request: %w", err)
	}
	client := c.HTTP
	if client == nil {
		client = http.DefaultClient
	}
	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("tgpost: telegram: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()
	body, err := io.ReadAll(io.LimitReader(resp.Body, 4<<20))
	if err != nil {
		return nil, fmt.Errorf("tgpost: telegram reply: %w", err)
	}
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("tgpost: telegram refused: %s: %s", resp.Status, strings.TrimSpace(string(body)))
	}
	return body, nil
}
