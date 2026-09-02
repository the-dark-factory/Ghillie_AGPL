package dcpost

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
		{"Jean", "jean"},
		{"  JEAN  ", "jean"},
		{"robin_hood", "robin_hood"},
	}
	for _, tc := range cases {
		if got := Canonical(tc.in); got != tc.want {
			t.Errorf("Canonical(%q) = %q, want %q", tc.in, got, tc.want)
		}
	}
}

func TestLoadTableCanonicalizesKeys(t *testing.T) {
	p := filepath.Join(t.TempDir(), "grants.json")
	if err := os.WriteFile(p, []byte(`{"Jean": {"ceiling":"break_through","live":true}}`), 0o600); err != nil {
		t.Fatal(err)
	}
	table, err := LoadTable(p)
	if err != nil {
		t.Fatal(err)
	}
	if g, ok := table["jean"]; !ok || g.Ceiling != "break_through" {
		t.Fatalf("lookup by canonical username failed: %v %v", g, ok)
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

func TestLoadChannelsSkipsCommentsAndBlanks(t *testing.T) {
	p := filepath.Join(t.TempDir(), "channels.txt")
	if err := os.WriteFile(p, []byte("# the family channel\n123456789012345678\n\n  234567890123456789  \n"), 0o600); err != nil {
		t.Fatal(err)
	}
	ids, err := LoadChannels(p)
	if err != nil {
		t.Fatal(err)
	}
	if len(ids) != 2 || ids[0] != "123456789012345678" || ids[1] != "234567890123456789" {
		t.Fatalf("channels = %v", ids)
	}
}

func TestHigherSnowflake(t *testing.T) {
	// 19-digit id must sort after an 18-digit one — the case a lexical compare
	// gets wrong.
	if !HigherSnowflake("1000000000000000000", "999999999999999999") {
		t.Fatalf("numeric snowflake compare failed across the length boundary")
	}
	if HigherSnowflake("100", "200") {
		t.Fatalf("100 must not sort after 200")
	}
	if HigherSnowflake("not-a-number", "100") {
		t.Fatalf("an unparseable id must never advance the watermark")
	}
}

// sampleMessages is a GET /channels/{id}/messages reply: two authored messages
// (Discord returns newest first) and one with no author (a system message,
// nobody's arrival).
const sampleMessages = `[
	{"id":"200","content":"ship it \n please","timestamp":"2026-08-25T21:00:02Z","author":{"username":"Jean"}},
	{"id":"100","content":"the deploy is green","timestamp":"2026-08-25T21:00:00Z","author":{"username":"Robin"}},
	{"id":"50","content":"pinned","timestamp":"2026-08-25T20:59:00Z"}
]`

func TestParseMessages(t *testing.T) {
	arrivals, err := ParseMessages([]byte(sampleMessages), "chan-1")
	if err != nil {
		t.Fatalf("ParseMessages: %v", err)
	}
	if len(arrivals) != 2 {
		t.Fatalf("arrivals = %d, want 2 (the authorless message is nobody's arrival)", len(arrivals))
	}
	if arrivals[0].From != "Jean" || arrivals[0].Subject != "ship it   please" {
		t.Fatalf("first arrival = %+v", arrivals[0])
	}
	if arrivals[0].Channel != "chan-1" {
		t.Fatalf("arrival must carry its channel, got %q", arrivals[0].Channel)
	}
	if arrivals[0].At.UTC().Format(time.RFC3339) != "2026-08-25T21:00:02Z" {
		t.Fatalf("arrival time = %v", arrivals[0].At)
	}
}

func TestMessagesReadsWithAfterFilter(t *testing.T) {
	var sawQuery, sawAuth string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		sawQuery = r.URL.RawQuery
		sawAuth = r.Header.Get("Authorization")
		if _, err := w.Write([]byte(sampleMessages)); err != nil {
			t.Error(err)
		}
	}))
	t.Cleanup(srv.Close)
	c := &Client{BaseURL: srv.URL, Token: "bot-secret"}
	arrivals, err := c.Messages(context.Background(), "chan-1", "99", 25)
	if err != nil {
		t.Fatalf("Messages: %v", err)
	}
	if len(arrivals) != 2 {
		t.Fatalf("arrivals = %d", len(arrivals))
	}
	if !strings.Contains(sawQuery, "after=99") {
		t.Fatalf("watermark must ride as the after= snowflake, got %q", sawQuery)
	}
	if sawAuth != "Bot bot-secret" {
		t.Fatalf("bot token must ride as 'Bot <token>', got %q", sawAuth)
	}
}

func TestDiscordRefusalIsAnError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		http.Error(w, `{"message":"rate limited"}`, http.StatusTooManyRequests)
	}))
	t.Cleanup(srv.Close)
	c := &Client{BaseURL: srv.URL, Token: "t"}
	if _, err := c.Messages(context.Background(), "chan-1", "", 5); err == nil {
		t.Fatalf("a refused API call must surface, never read as an empty channel")
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

	a := Arrival{From: "Stranger", Subject: "hi", At: time.Now()}
	d, err := DecideArrival(context.Background(), post.Decider{}, post.Table{}, a, "free", false)
	if err != nil {
		t.Fatalf("DecideArrival: %v", err)
	}
	if d != post.Refuse {
		t.Fatalf("decision = %q", d)
	}
	raw, _ := os.ReadFile(argsFile)
	if got := string(raw); got != "decide false whenever whenever free false\n" {
		t.Fatalf("an unknown author must be asked with Grant_Live=false, got %q", got)
	}
}

func TestDecideArrivalUnwiredRecordsGapAndPresentsNothing(t *testing.T) {
	t.Setenv(post.EnvDecider, "")
	ledger := filepath.Join(t.TempDir(), "gaps.jsonl")
	d := post.Decider{Gaps: gapledger.Open(ledger)}

	a := Arrival{From: "Jean", Subject: "the deploy is green", At: time.Now()}
	_, err := DecideArrival(context.Background(), d, post.Table{}, a, "free", false)
	if !errors.Is(err, post.ErrDeciderUnwired) {
		t.Fatalf("err = %v, want ErrDeciderUnwired", err)
	}
	raw, rerr := os.ReadFile(ledger)
	if rerr != nil || len(raw) == 0 {
		t.Fatalf("an unwired decider must be recorded as a gap: %v", rerr)
	}
}
