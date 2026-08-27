package post

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

// gmailFixture serves the two read endpoints the client uses, recording what
// was asked for.
func gmailFixture(t *testing.T) (*Client, *[]string) {
	t.Helper()
	var paths []string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		paths = append(paths, r.URL.String())
		if r.Header.Get("Authorization") != "Bearer tok-1" {
			http.Error(w, "no token", http.StatusUnauthorized)
			return
		}
		switch {
		case strings.HasPrefix(r.URL.Path, "/gmail/v1/users/me/messages/m1"):
			if _, err := w.Write([]byte(`{"id":"m1","internalDate":"1756155600000",
				"payload":{"headers":[
					{"name":"From","value":"Jean Gair <jean@example.com>"},
					{"name":"Subject","value":"the school run"},
					{"name":"X-Ignored","value":"never fetched"}]}}`)); err != nil {
				t.Error(err)
			}
		case strings.HasPrefix(r.URL.Path, "/gmail/v1/users/me/messages"):
			if _, err := w.Write([]byte(`{"messages":[{"id":"m1"}]}`)); err != nil {
				t.Error(err)
			}
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(srv.Close)
	return &Client{
		BaseURL: srv.URL,
		TokenFn: func(context.Context) (string, error) { return "tok-1", nil },
	}, &paths
}

func TestListAndGetArrival(t *testing.T) {
	c, paths := gmailFixture(t)

	ids, err := c.ListNewIDs(context.Background(), time.Unix(1756150000, 0), 25)
	if err != nil {
		t.Fatalf("ListNewIDs: %v", err)
	}
	if len(ids) != 1 || ids[0] != "m1" {
		t.Fatalf("ids = %v", ids)
	}
	if !strings.Contains((*paths)[0], "q=after%3A1756150000") {
		t.Fatalf("the watermark must ride as Gmail's after: filter, got %s", (*paths)[0])
	}

	a, err := c.GetArrival(context.Background(), "m1")
	if err != nil {
		t.Fatalf("GetArrival: %v", err)
	}
	if a.From != "Jean Gair <jean@example.com>" || a.Subject != "the school run" {
		t.Fatalf("arrival = %+v", a)
	}
	if a.At.UnixMilli() != 1756155600000 {
		t.Fatalf("arrival time = %v", a.At)
	}
	// The metadata request must ask for headers only — bodies are never
	// fetched, which is the content-is-data rule enforced at the API.
	if !strings.Contains((*paths)[1], "format=metadata") {
		t.Fatalf("must fetch metadata only, got %s", (*paths)[1])
	}
}

func TestGmailRefusalIsAnError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		http.Error(w, "quota", http.StatusTooManyRequests)
	}))
	t.Cleanup(srv.Close)
	c := &Client{BaseURL: srv.URL, TokenFn: func(context.Context) (string, error) { return "t", nil }}
	if _, err := c.ListNewIDs(context.Background(), time.Time{}, 5); err == nil {
		t.Fatalf("a refused API call must surface, never read as an empty mailbox")
	}
}
