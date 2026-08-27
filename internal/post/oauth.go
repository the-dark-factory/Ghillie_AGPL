package post

// oauth.go — the owner's own OAuth client, installed-app loopback flow,
// hand-rolled on the standard library.
//
// WHY NO LIBRARY: the flow is four HTTP calls, and this is a credential path
// on a discretion-first product — the fewer hands the tokens pass through the
// better (same reasoning that bought coder/websocket for FRAME PARSING kept
// dependencies OUT of the credential path: parsing hostile input earns a
// library; posting four well-formed requests does not).
//
// SCOPE IS THE READ-ONLY ONE, AS A CONSTANT. The send scope does not appear
// in this file or any other; a v2 that wants send must edit source in a
// reviewed change, not flip a config.

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

// Scope is the only Google scope this package will ever request.
const Scope = "https://www.googleapis.com/auth/gmail.readonly"

// ClientCredentials is the owner's own OAuth client, from the standard
// "installed" credentials JSON Google's console hands out.
type ClientCredentials struct {
	ID       string
	Secret   string
	AuthURI  string
	TokenURI string
}

// LoadClientCredentials reads a Google credentials file (the {"installed":
// {...}} shape). The owner downloads this from THEIR console — route A means
// the client itself is theirs, not ours.
func LoadClientCredentials(path string) (ClientCredentials, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return ClientCredentials{}, fmt.Errorf("post: client credentials: %w", err)
	}
	var f struct {
		Installed struct {
			ClientID     string `json:"client_id"`
			ClientSecret string `json:"client_secret"`
			AuthURI      string `json:"auth_uri"`
			TokenURI     string `json:"token_uri"`
		} `json:"installed"`
	}
	if err := json.Unmarshal(raw, &f); err != nil {
		return ClientCredentials{}, fmt.Errorf("post: client credentials %s: %w", path, err)
	}
	c := ClientCredentials{
		ID:       f.Installed.ClientID,
		Secret:   f.Installed.ClientSecret,
		AuthURI:  f.Installed.AuthURI,
		TokenURI: f.Installed.TokenURI,
	}
	if c.ID == "" || c.TokenURI == "" {
		return ClientCredentials{}, errors.New("post: credentials file is not an installed-app client (no installed.client_id)")
	}
	return c, nil
}

// Token is what the flow stores: the refresh token is the durable credential,
// the access token a short-lived derivative.
type Token struct {
	AccessToken  string    `json:"access_token"`
	RefreshToken string    `json:"refresh_token"`
	Expiry       time.Time `json:"expiry"`
}

// TokenStore keeps the token beside the device key, mode 0600, exactly as the
// other owner-credentials on this machine.
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
		return Token{}, fmt.Errorf("post: token store %s: %w", s.Path, err)
	}
	return t, nil
}

// Save writes the token at 0600.
func (s TokenStore) Save(t Token) error {
	raw, err := json.Marshal(t)
	if err != nil {
		return fmt.Errorf("post: encode token: %w", err)
	}
	if err := os.WriteFile(s.Path, raw, 0o600); err != nil {
		return fmt.Errorf("post: token store: %w", err)
	}
	return nil
}

// Login runs the installed-app loopback consent once: opens a local listener,
// prints the consent URL for the owner to open, waits for Google to redirect
// the code back, exchanges it, and stores the token. The CONSENT HAPPENS IN
// THE OWNER'S BROWSER against the OWNER'S client — ghillie never sees the
// password, only the resulting grant, which the owner can revoke at their
// Google account page without asking anyone.
func Login(ctx context.Context, c ClientCredentials, store TokenStore, announce func(url string)) error {
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return fmt.Errorf("post: loopback listener: %w", err)
	}
	defer func() {
		if cerr := l.Close(); cerr != nil && !errors.Is(cerr, net.ErrClosed) {
			// The listener outliving the flow is harmless; say nothing.
			_ = cerr
		}
	}()

	redirect := "http://" + l.Addr().String() + "/"
	consent := c.AuthURI + "?" + url.Values{
		"client_id":     {c.ID},
		"redirect_uri":  {redirect},
		"response_type": {"code"},
		"scope":         {Scope},
		"access_type":   {"offline"},
		"prompt":        {"consent"},
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
			if _, werr := io.WriteString(w, "ghillie has the post key. You can close this tab."); werr != nil {
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
		return fmt.Errorf("post: login: %w", ctx.Err())
	}

	tok, err := exchange(ctx, c, url.Values{
		"grant_type":   {"authorization_code"},
		"code":         {code},
		"redirect_uri": {redirect},
	})
	if err != nil {
		return err
	}
	if tok.RefreshToken == "" {
		return errors.New("post: consent returned no refresh token — remove the app's prior grant at the owner's Google account page and log in again")
	}
	return store.Save(tok)
}

// AccessToken returns a live access token, refreshing through the owner's
// client when the stored one is stale.
func AccessToken(ctx context.Context, c ClientCredentials, store TokenStore) (string, error) {
	tok, err := store.Load()
	if err != nil {
		return "", fmt.Errorf("post: no stored token (run login first): %w", err)
	}
	if time.Until(tok.Expiry) > time.Minute {
		return tok.AccessToken, nil
	}
	fresh, err := exchange(ctx, c, url.Values{
		"grant_type":    {"refresh_token"},
		"refresh_token": {tok.RefreshToken},
	})
	if err != nil {
		return "", err
	}
	if fresh.RefreshToken == "" {
		fresh.RefreshToken = tok.RefreshToken
	}
	if err := store.Save(fresh); err != nil {
		return "", err
	}
	return fresh.AccessToken, nil
}

// exchange posts to the token endpoint and reads the standard reply.
func exchange(ctx context.Context, c ClientCredentials, form url.Values) (Token, error) {
	form.Set("client_id", c.ID)
	form.Set("client_secret", c.Secret)
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.TokenURI,
		strings.NewReader(form.Encode()))
	if err != nil {
		return Token{}, fmt.Errorf("post: token request: %w", err)
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return Token{}, fmt.Errorf("post: token endpoint: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()
	body, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		return Token{}, fmt.Errorf("post: token reply: %w", err)
	}
	if resp.StatusCode != http.StatusOK {
		return Token{}, fmt.Errorf("post: token endpoint refused: %s: %s", resp.Status, strings.TrimSpace(string(body)))
	}
	var r struct {
		AccessToken  string `json:"access_token"`
		RefreshToken string `json:"refresh_token"`
		ExpiresIn    int    `json:"expires_in"`
	}
	if err := json.Unmarshal(body, &r); err != nil {
		return Token{}, fmt.Errorf("post: token reply: %w", err)
	}
	if r.AccessToken == "" {
		return Token{}, errors.New("post: token reply carried no access token")
	}
	return Token{
		AccessToken:  r.AccessToken,
		RefreshToken: r.RefreshToken,
		Expiry:       time.Now().Add(time.Duration(r.ExpiresIn) * time.Second),
	}, nil
}
