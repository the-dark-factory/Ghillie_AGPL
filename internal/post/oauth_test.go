package post

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestAccessTokenRefreshesThroughTheOwnersClient(t *testing.T) {
	var form map[string][]string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if err := r.ParseForm(); err != nil {
			t.Error(err)
		}
		form = r.PostForm
		if _, err := w.Write([]byte(`{"access_token":"fresh-1","expires_in":3600}`)); err != nil {
			t.Error(err)
		}
	}))
	t.Cleanup(srv.Close)

	store := TokenStore{Path: filepath.Join(t.TempDir(), "token.json")}
	if err := store.Save(Token{
		AccessToken:  "stale",
		RefreshToken: "refresh-1",
		Expiry:       time.Now().Add(-time.Hour),
	}); err != nil {
		t.Fatal(err)
	}
	c := ClientCredentials{ID: "owner-client", Secret: "s", TokenURI: srv.URL}

	tok, err := AccessToken(context.Background(), c, store)
	if err != nil {
		t.Fatalf("AccessToken: %v", err)
	}
	if tok != "fresh-1" {
		t.Fatalf("token = %q", tok)
	}
	if got := form["grant_type"]; len(got) != 1 || got[0] != "refresh_token" {
		t.Fatalf("grant_type = %v", form["grant_type"])
	}
	if got := form["client_id"]; len(got) != 1 || got[0] != "owner-client" {
		t.Fatalf("the refresh must go through the OWNER'S client, got %v", form["client_id"])
	}

	// The refresh token must survive a reply that omits it.
	stored, err := store.Load()
	if err != nil {
		t.Fatal(err)
	}
	if stored.RefreshToken != "refresh-1" {
		t.Fatalf("refresh token was dropped on refresh: %+v", stored)
	}
	// And the stored credential stays owner-only on disk.
	info, err := os.Stat(store.Path)
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm() != 0o600 {
		t.Fatalf("token file mode = %v, want 0600", info.Mode().Perm())
	}
}

func TestAccessTokenStillFreshSkipsTheNetwork(t *testing.T) {
	store := TokenStore{Path: filepath.Join(t.TempDir(), "token.json")}
	if err := store.Save(Token{
		AccessToken:  "live-token",
		RefreshToken: "r",
		Expiry:       time.Now().Add(time.Hour),
	}); err != nil {
		t.Fatal(err)
	}
	// TokenURI points nowhere; a network call would fail the test by itself.
	c := ClientCredentials{ID: "x", TokenURI: "http://127.0.0.1:1/refuse"}
	tok, err := AccessToken(context.Background(), c, store)
	if err != nil || tok != "live-token" {
		t.Fatalf("tok=%q err=%v", tok, err)
	}
}

func TestScopeIsReadOnlyByConstruction(t *testing.T) {
	// The constant IS the guarantee the proposal made; this test makes
	// changing it a visible act rather than a drive-by.
	if Scope != "https://www.googleapis.com/auth/gmail.readonly" {
		t.Fatalf("Scope = %q — the read-only scope is the approved contract; send is a separately gated v2", Scope)
	}
}
