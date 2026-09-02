package tgpost

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
		{"@Jean", "jean"},
		{"jean", "jean"},
		{"  @Jean  ", "jean"},
		{"First Name", "first name"},
	}
	for _, tc := range cases {
		if got := Canonical(tc.in); got != tc.want {
			t.Errorf("Canonical(%q) = %q, want %q", tc.in, got, tc.want)
		}
	}
}

func TestLoadTableCanonicalizesKeys(t *testing.T) {
	p := filepath.Join(t.TempDir(), "grants.json")
	if err := os.WriteFile(p, []byte(`{"@Jean": {"ceiling":"break_through","live":true}}`), 0o600); err != nil {
		t.Fatal(err)
	}
	table, err := LoadTable(p)
	if err != nil {
		t.Fatal(err)
	}
	if g, ok := table["jean"]; !ok || g.Ceiling != "break_through" {
		t.Fatalf("lookup by canonical handle failed: %v %v", g, ok)
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

// sampleUpdates is a getUpdates reply with three updates: a normal message, a
// non-message update (advances the cursor, is nobody's arrival), and a message
// with only a first name.
const sampleUpdates = `{"ok":true,"result":[
	{"update_id":1001,"message":{"date":1756155600,"from":{"username":"jean","first_name":"Jean"},"text":"the school run\nis at three"}},
	{"update_id":1002,"edited_channel_post":{"date":1756155601}},
	{"update_id":1003,"message":{"date":1756155602,"from":{"first_name":"Robin"},"text":"hi"}}
]}`

func TestParseUpdatesAndOffset(t *testing.T) {
	arrivals, next, err := ParseUpdates([]byte(sampleUpdates), 1000)
	if err != nil {
		t.Fatalf("ParseUpdates: %v", err)
	}
	if next != 1004 {
		t.Fatalf("next offset = %d, want 1004 (largest update_id + 1)", next)
	}
	if len(arrivals) != 2 {
		t.Fatalf("arrivals = %d, want 2 (the non-message update is nobody's arrival)", len(arrivals))
	}
	if arrivals[0].From != "jean" || arrivals[0].Subject != "the school run is at three" {
		t.Fatalf("first arrival = %+v (username preferred, text flattened)", arrivals[0])
	}
	if arrivals[0].At.Unix() != 1756155600 {
		t.Fatalf("arrival time = %v", arrivals[0].At)
	}
	if arrivals[1].From != "Robin" {
		t.Fatalf("second arrival must fall back to first name, got %q", arrivals[1].From)
	}
}

func TestParseUpdatesEmptyBatchKeepsOffset(t *testing.T) {
	_, next, err := ParseUpdates([]byte(`{"ok":true,"result":[]}`), 42)
	if err != nil {
		t.Fatal(err)
	}
	if next != 42 {
		t.Fatalf("empty batch must keep the offset, got %d", next)
	}
}

func TestParseUpdatesRefusalIsAnError(t *testing.T) {
	if _, _, err := ParseUpdates([]byte(`{"ok":false,"description":"unauthorized"}`), 0); err == nil {
		t.Fatalf("ok:false must surface as an error, never an empty batch")
	}
}

func TestGetUpdatesReadsFromTheBotPath(t *testing.T) {
	var sawPath string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		sawPath = r.URL.Path
		if _, err := w.Write([]byte(sampleUpdates)); err != nil {
			t.Error(err)
		}
	}))
	t.Cleanup(srv.Close)
	c := &Client{BaseURL: srv.URL, Token: "bot-secret"}
	arrivals, next, err := c.GetUpdates(context.Background(), 1000, 0, 25)
	if err != nil {
		t.Fatalf("GetUpdates: %v", err)
	}
	if next != 1004 || len(arrivals) != 2 {
		t.Fatalf("GetUpdates parsed wrong: next=%d arrivals=%d", next, len(arrivals))
	}
	if !strings.HasPrefix(sawPath, "/botbot-secret/getUpdates") {
		t.Fatalf("token must ride in the path, got %q", sawPath)
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

	a := Arrival{From: "stranger", Subject: "hi", At: time.Now()}
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

	a := Arrival{From: "jean", Subject: "the school run", At: time.Now()}
	_, err := DecideArrival(context.Background(), d, post.Table{}, a, "free", false)
	if !errors.Is(err, post.ErrDeciderUnwired) {
		t.Fatalf("err = %v, want ErrDeciderUnwired", err)
	}
	raw, rerr := os.ReadFile(ledger)
	if rerr != nil || len(raw) == 0 {
		t.Fatalf("an unwired decider must be recorded as a gap: %v", rerr)
	}
}
