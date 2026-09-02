package dcpost

// discord.go — the minimal READ-ONLY Discord REST client: poll one channel's
// recent messages by snowflake, and read the bot's own identity to validate
// the token at setup. Those two reads are the ONLY endpoints named in this
// file. POST /channels/{id}/messages and every write are absent by
// construction; no gateway socket is opened. The no-send rule is enforced by
// WHAT IS CALLED, not merely by intent.

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

// DefaultBaseURL is Discord's REST root (v10); tests substitute their own.
const DefaultBaseURL = "https://discord.com/api/v10"

// Client reads the owner's configured channels. Token is the bot token; it
// rides in the Authorization header as "Bot <token>", the scheme Discord
// requires for a bot credential.
type Client struct {
	BaseURL string
	Token   string
	// HTTP defaults to http.DefaultClient.
	HTTP *http.Client
}

// Messages reads up to limit messages in the channel that arrived after the
// given snowflake, and parses arrivals out of the reply. An empty after asks
// for the channel's most recent messages (first run). This is a GET — the only
// verb this connector ever uses against a channel.
func (c *Client) Messages(ctx context.Context, channel, after string, limit int) (arrivals []Arrival, err error) {
	q := url.Values{"limit": {strconv.Itoa(limit)}}
	if after != "" {
		q.Set("after", after)
	}
	body, err := c.get(ctx, "/channels/"+url.PathEscape(channel)+"/messages?"+q.Encode())
	if err != nil {
		return nil, err
	}
	return ParseMessages(body, channel)
}

// Username validates the token and returns the bot's own username, for the
// -login setup path. It is a read (GET /users/@me) and touches no channel.
func (c *Client) Username(ctx context.Context) (string, error) {
	body, err := c.get(ctx, "/users/@me")
	if err != nil {
		return "", err
	}
	var r struct {
		Username string `json:"username"`
	}
	if uerr := json.Unmarshal(body, &r); uerr != nil {
		return "", fmt.Errorf("dcpost: users/@me reply: %w", uerr)
	}
	return r.Username, nil
}

// get performs one authorized REST read and returns the raw JSON body.
func (c *Client) get(ctx context.Context, path string) ([]byte, error) {
	base := c.BaseURL
	if base == "" {
		base = DefaultBaseURL
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, base+path, nil)
	if err != nil {
		return nil, fmt.Errorf("dcpost: request: %w", err)
	}
	req.Header.Set("Authorization", "Bot "+c.Token)
	client := c.HTTP
	if client == nil {
		client = http.DefaultClient
	}
	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("dcpost: discord: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()
	body, err := io.ReadAll(io.LimitReader(resp.Body, 4<<20))
	if err != nil {
		return nil, fmt.Errorf("dcpost: discord reply: %w", err)
	}
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("dcpost: discord refused: %s: %s", resp.Status, strings.TrimSpace(string(body)))
	}
	return body, nil
}

// HigherSnowflake reports whether a sorts after b as a Discord snowflake
// (numeric, so it is correct across the id-length boundary that a lexical
// compare gets wrong). An unparseable id is treated as lower, so the watermark
// only ever advances on a genuine, well-formed id.
func HigherSnowflake(a, b string) bool {
	ai, aerr := strconv.ParseUint(a, 10, 64)
	if aerr != nil {
		return false
	}
	bi, berr := strconv.ParseUint(b, 10, 64)
	if berr != nil {
		return true
	}
	return ai > bi
}
