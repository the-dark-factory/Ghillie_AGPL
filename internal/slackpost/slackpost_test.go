package slackpost

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/tonygair/ghillie/internal/gapledger"
	"github.com/tonygair/ghillie/internal/post"
)

func TestCanonical(t *testing.T) {
	cases := []struct{ in, want string }{
		{"U0123ABCD", "U0123ABCD"},
		{"  U0123ABCD  ", "U0123ABCD"},
		{"", ""},
	}
	for _, tc := range cases {
		if got := Canonical(tc.in); got != tc.want {
			t.Errorf("Canonical(%q) = %q, want %q", tc.in, got, tc.want)
		}
	}
}

func TestPreviewTrimsAndFlattens(t *testing.T) {
	if got := preview("first line\nsecond line"); got != "first line second line" {
		t.Fatalf("preview flattened wrong: %q", got)
	}
	long := strings.Repeat("x", 200)
	got := preview(long)
	if len([]rune(got)) != 81 { // 80 runes + the ellipsis
		t.Fatalf("preview did not cap to 80 runes: %d runes", len([]rune(got)))
	}
}

func TestLoadTableMissingFileIsEmptyTable(t *testing.T) {
	table, err := LoadTable(filepath.Join(t.TempDir(), "absent.json"))
	if err != nil {
		t.Fatalf("missing grants file must be the empty table, got %v", err)
	}
	if len(table) != 0 {
		t.Fatalf("table = %v", table)
	}
}

func TestLoadTableCanonicalizesKeys(t *testing.T) {
	p := filepath.Join(t.TempDir(), "grants.json")
	if err := os.WriteFile(p, []byte(`{"  U0123ABCD  ": {"ceiling":"break_through","live":true}}`), 0o600); err != nil {
		t.Fatal(err)
	}
	table, err := LoadTable(p)
	if err != nil {
		t.Fatal(err)
	}
	if g, ok := table["U0123ABCD"]; !ok || g.Ceiling != "break_through" {
		t.Fatalf("lookup by canonical id failed: %v %v", g, ok)
	}
}

// slackFixture serves the read endpoints the client uses, recording the paths
// asked for, so a test can assert only reads are ever issued.
func slackFixture(t *testing.T) (*Client, *[]string) {
	t.Helper()
	var paths []string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		paths = append(paths, r.URL.String())
		if r.Header.Get("Authorization") != "Bearer xoxp-test" {
			http.Error(w, `{"ok":false,"error":"not_authed"}`, http.StatusOK)
			return
		}
		switch r.URL.Path {
		case "/conversations.list":
			if _, err := w.Write([]byte(`{"ok":true,"channels":[{"id":"C1"}]}`)); err != nil {
				t.Error(err)
			}
		case "/conversations.history":
			q := r.URL.Query()
			if q.Get("latest") != "" {
				// single-message re-read for GetArrival
				if _, err := w.Write([]byte(`{"ok":true,"messages":[
					{"user":"U0123ABCD","text":"the deploy is green\nship it","ts":"1756155600.001500"}]}`)); err != nil {
					t.Error(err)
				}
				return
			}
			if _, err := w.Write([]byte(`{"ok":true,"messages":[{"user":"U0123ABCD","text":"the deploy is green","ts":"1756155600.001500"}]}`)); err != nil {
				t.Error(err)
			}
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(srv.Close)
	return &Client{
		BaseURL: srv.URL,
		TokenFn: func(context.Context) (string, error) { return "xoxp-test", nil },
	}, &paths
}

func TestListAndGetArrival(t *testing.T) {
	c, paths := slackFixture(t)

	ids, err := c.ListNewIDs(context.Background(), time.Unix(1756150000, 0), 25)
	if err != nil {
		t.Fatalf("ListNewIDs: %v", err)
	}
	if len(ids) != 1 || ids[0] != "C1|1756155600.001500" {
		t.Fatalf("ids = %v", ids)
	}
	// The watermark must ride as Slack's oldest= filter.
	var sawOldest bool
	for _, p := range *paths {
		if strings.Contains(p, "conversations.history") && strings.Contains(p, "oldest=1756150000.000000") {
			sawOldest = true
		}
	}
	if !sawOldest {
		t.Fatalf("watermark did not become the oldest= filter, paths=%v", *paths)
	}

	a, err := c.GetArrival(context.Background(), ids[0])
	if err != nil {
		t.Fatalf("GetArrival: %v", err)
	}
	if a.From != "U0123ABCD" {
		t.Fatalf("arrival from = %q", a.From)
	}
	if a.Subject != "the deploy is green ship it" {
		t.Fatalf("subject preview = %q", a.Subject)
	}
	if a.At.Unix() != 1756155600 {
		t.Fatalf("arrival time = %v", a.At)
	}
}

func TestSlackLogicalErrorIsAnError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		// 200 OK but ok:false — Slack's way of refusing; must not read as empty.
		if _, err := w.Write([]byte(`{"ok":false,"error":"ratelimited"}`)); err != nil {
			t.Error(err)
		}
	}))
	t.Cleanup(srv.Close)
	c := &Client{BaseURL: srv.URL, TokenFn: func(context.Context) (string, error) { return "xoxp-test", nil }}
	if _, err := c.ListNewIDs(context.Background(), time.Time{}, 5); err == nil {
		t.Fatalf("an ok:false reply must surface as an error, never an empty history")
	}
}

// stubDecider writes a fake front honoring the real CLI contract.
func stubDecider(t *testing.T, script string) string {
	t.Helper()
	p := filepath.Join(t.TempDir(), "delivery_policy_front")
	if err := os.WriteFile(p, []byte("#!/bin/sh\n"+script), 0o755); err != nil {
		t.Fatal(err)
	}
	return p
}

func TestDecideArrivalUnknownSenderIsAskedWithDeadGrant(t *testing.T) {
	dir := t.TempDir()
	argsFile := filepath.Join(dir, "args")
	t.Setenv(post.EnvDecider, stubDecider(t, `echo "$@" > `+argsFile+`; echo refuse`))

	a := Arrival{From: "U999STRANGER", Subject: "hi", At: time.Now()}
	d, err := DecideArrival(context.Background(), post.Decider{}, post.Table{}, a, "free", false)
	if err != nil {
		t.Fatalf("DecideArrival: %v", err)
	}
	if d != post.Refuse {
		t.Fatalf("decision = %q", d)
	}
	raw, _ := os.ReadFile(argsFile)
	if got := string(raw); got != "decide false whenever whenever free false\n" {
		t.Fatalf("an unknown sender must be asked with Grant_Live=false, got %q", got)
	}
}

func TestDecideArrivalUnwiredRecordsGapAndPresentsNothing(t *testing.T) {
	t.Setenv(post.EnvDecider, "")
	ledger := filepath.Join(t.TempDir(), "gaps.jsonl")
	d := post.Decider{Gaps: gapledger.Open(ledger)}

	a := Arrival{From: "U0123ABCD", Subject: "the deploy is green", At: time.Now()}
	_, err := DecideArrival(context.Background(), d, post.Table{}, a, "free", false)
	if !errors.Is(err, post.ErrDeciderUnwired) {
		t.Fatalf("err = %v, want ErrDeciderUnwired", err)
	}
	raw, rerr := os.ReadFile(ledger)
	if rerr != nil || len(raw) == 0 {
		t.Fatalf("an unwired decider must be recorded as a gap: %v", rerr)
	}
}
