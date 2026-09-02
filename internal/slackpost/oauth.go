package slackpost

// oauth.go — the owner's own Slack app, installed-app loopback consent,
// hand-rolled on the standard library, exactly as internal/post does for
// Google.
//
// WHY NO LIBRARY: the flow is two HTTP steps (authorize redirect, token
// exchange), and this is a credential path on a discretion-first product — the
// fewer hands the token passes through the better. Parsing hostile input earns
// a library; posting one well-formed request does not.
//
// THE SCOPES ARE THE READ-ONLY ONES, AS A CONSTANT. No write scope
// (chat:write and the rest) appears in this file or any other; a v2 that wants
// send must edit source in a reviewed change, not flip a config.
//
// USER TOKEN, NOT BOT TOKEN. The consent requests a USER scope (user_scope=)
// and stores the resulting xoxp token. Route A is the owner's own ears on
// everything the OWNER can already see; a bot token would see only the few
// channels a bot was invited to. The user token keeps the connector's view
// equal to the owner's, and the owner can revoke it from their Slack account
// at any time without asking anyone.

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"os"
	"strings"
	"time"
)

// UserScopes is the only set of Slack scopes this package will ever request:
// read history across channel types, and read user identity to resolve ids.
const UserScopes = "channels:history,groups:history,im:history,users:read"

// authorizeURL and tokenURL are Slack's OAuth v2 endpoints; tests do not need
// them because the login flow is exercised by hand, but they are named here so
// the request path is auditable in one place.
const (
	authorizeURL = "https://slack.com/oauth/v2/authorize"
	tokenURL     = "https://slack.com/api/oauth.v2.access"
)

// ClientCredentials is the owner's own Slack app, from a small JSON file the
// owner writes with the app's client id and secret (from their app's "Basic
// Information" page). Route A means the app itself is theirs, not ours.
type ClientCredentials struct {
	ID     string `json:"client_id"`
	Secret string `json:"client_secret"`
}

// LoadClientCredentials reads the {"client_id":...,"client_secret":...} file
// the owner downloads or writes from their Slack app.
func LoadClientCredentials(path string) (ClientCredentials, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return ClientCredentials{}, fmt.Errorf("slackpost: client credentials: %w", err)
	}
	var c ClientCredentials
	if err := json.Unmarshal(raw, &c); err != nil {
		return ClientCredentials{}, fmt.Errorf("slackpost: client credentials %s: %w", path, err)
	}
	if c.ID == "" || c.Secret == "" {
		return ClientCredentials{}, errors.New("slackpost: credentials file needs client_id and client_secret")
	}
	return c, nil
}

// Token is what the flow stores: a Slack user token (xoxp). Slack user tokens
// do not expire by default, so there is no refresh token to keep — the one
// string is the durable credential.
type Token struct {
	AccessToken string `json:"access_token"`
}

// TokenStore keeps the token beside the other owner-credentials on this
// machine, mode 0600.
type TokenStore struct{ Path string }

// Load reads the stored token; a missing file returns os.ErrNotExist for the
// caller to turn into "run login first".
func (s TokenStore) Load() (Token, error) {
	raw, err := os.ReadFile(s.Path)
	if err != nil {
		return Token{}, err
	}
	var t Token
	if err := json.Unmarshal(raw, &t); err != nil {
		return Token{}, fmt.Errorf("slackpost: token store %s: %w", s.Path, err)
	}
	return t, nil
}

// Save writes the token at 0600.
func (s TokenStore) Save(t Token) error {
	raw, err := json.Marshal(t)
	if err != nil {
		return fmt.Errorf("slackpost: encode token: %w", err)
	}
	if err := os.WriteFile(s.Path, raw, 0o600); err != nil {
		return fmt.Errorf("slackpost: token store: %w", err)
	}
	return nil
}

// AccessToken returns the stored user token. Unlike Google's, it does not
// refresh — Slack user tokens are durable — so a missing store is the only
// failure, turned into "run login first".
func AccessToken(_ context.Context, store TokenStore) (string, error) {
	tok, err := store.Load()
	if err != nil {
		return "", fmt.Errorf("slackpost: no stored token (run login first): %w", err)
	}
	if tok.AccessToken == "" {
		return "", errors.New("slackpost: stored token is empty (run login again)")
	}
	return tok.AccessToken, nil
}

// Login runs the installed-app loopback consent once: opens a local listener,
// prints the consent URL for the owner to open, waits for Slack to redirect
// the code back, exchanges it for a USER token, and stores it. The consent
// happens in the OWNER'S browser against the OWNER'S app — ghillie never sees
// the password, only the resulting grant.
func Login(ctx context.Context, c ClientCredentials, store TokenStore, announce func(url string)) error {
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return fmt.Errorf("slackpost: loopback listener: %w", err)
	}
	defer func() {
		if cerr := l.Close(); cerr != nil && !errors.Is(cerr, net.ErrClosed) {
			_ = cerr
		}
	}()

	redirect := "http://" + l.Addr().String() + "/"
	consent := authorizeURL + "?" + url.Values{
		"client_id":    {c.ID},
		"user_scope":   {UserScopes},
		"redirect_uri": {redirect},
	}.Encode()
	announce(consent)

	codeCh := make(chan string, 1)
	srv := &http.Server{
		ReadHeaderTimeout: 10 * time.Second,
		Handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			code := r.URL.Query().Get("code")
			if code == "" {
				http.Error(w, "no code in redirect", http.StatusBadRequest)
				return
			}
			if _, werr := io.WriteString(w, "ghillie has the read-only Slack key. You can close this tab."); werr != nil {
				_ = werr
			}
			codeCh <- code
		}),
	}
	go func() {
		if serr := srv.Serve(l); serr != nil && !errors.Is(serr, http.ErrServerClosed) && !errors.Is(serr, net.ErrClosed) {
			_ = serr
		}
	}()
	defer func() { _ = srv.Close() }()

	var code string
	select {
	case code = <-codeCh:
	case <-ctx.Done():
		return fmt.Errorf("slackpost: login: %w", ctx.Err())
	}

	tok, err := exchange(ctx, c, redirect, code)
	if err != nil {
		return err
	}
	if tok.AccessToken == "" {
		return errors.New("slackpost: consent returned no user token — check the app requested user scopes, then log in again")
	}
	return store.Save(tok)
}

// exchange posts the authorization code to Slack's oauth.v2.access and reads
// the USER token out of authed_user.access_token.
func exchange(ctx context.Context, c ClientCredentials, redirect, code string) (Token, error) {
	form := url.Values{
		"client_id":     {c.ID},
		"client_secret": {c.Secret},
		"code":          {code},
		"redirect_uri":  {redirect},
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, tokenURL, strings.NewReader(form.Encode()))
	if err != nil {
		return Token{}, fmt.Errorf("slackpost: token request: %w", err)
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return Token{}, fmt.Errorf("slackpost: token endpoint: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()
	body, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		return Token{}, fmt.Errorf("slackpost: token reply: %w", err)
	}
	if resp.StatusCode != http.StatusOK {
		return Token{}, fmt.Errorf("slackpost: token endpoint refused: %s: %s", resp.Status, strings.TrimSpace(string(body)))
	}
	var r struct {
		OK         bool   `json:"ok"`
		Error      string `json:"error"`
		AuthedUser struct {
			AccessToken string `json:"access_token"`
		} `json:"authed_user"`
	}
	if err := json.Unmarshal(body, &r); err != nil {
		return Token{}, fmt.Errorf("slackpost: token reply: %w", err)
	}
	if !r.OK {
		return Token{}, fmt.Errorf("slackpost: token endpoint refused: %s", r.Error)
	}
	return Token{AccessToken: r.AuthedUser.AccessToken}, nil
}
