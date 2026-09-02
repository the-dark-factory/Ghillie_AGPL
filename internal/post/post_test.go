package post

import (
	"context"
	"errors"
	"github.com/tonygair/ghillie/internal/gapledger"
	"os"
	"path/filepath"
	"testing"
)

func TestCanonicalAddress(t *testing.T) {
	cases := []struct{ in, want string }{
		{"jean@example.com", "jean@example.com"},
		{"Jean Gair <Jean@Example.COM>", "jean@example.com"},
		{"  \"The School\" <office@school.sch.uk>  ", "office@school.sch.uk"},
		{"UPPER@CASE.NET", "upper@case.net"},
		{"no-angle-brackets", "no-angle-brackets"},
	}
	for _, tc := range cases {
		if got := CanonicalAddress(tc.in); got != tc.want {
			t.Errorf("CanonicalAddress(%q) = %q, want %q", tc.in, got, tc.want)
		}
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
	if err := os.WriteFile(p, []byte(`{"Jean@Example.COM": {"ceiling":"break_through","live":true}}`), 0o600); err != nil {
		t.Fatal(err)
	}
	table, err := LoadTable(p)
	if err != nil {
		t.Fatal(err)
	}
	g, ok := table.Lookup("Jean Gair <jean@example.com>")
	if !ok || g.Ceiling != "break_through" {
		t.Fatalf("lookup after canonicalization failed: %v %v", g, ok)
	}
}

// stubDecider writes a fake front honoring the real CLI contract, so the exec
// seam is tested against the same shapes the Ada front produces.
func stubDecider(t *testing.T, script string) string {
	t.Helper()
	p := filepath.Join(t.TempDir(), "delivery_policy_front")
	if err := os.WriteFile(p, []byte("#!/bin/sh\n"+script), 0o755); err != nil {
		t.Fatal(err)
	}
	return p
}

func TestDecideWordsPassVerbatim(t *testing.T) {
	// The stub echoes its arguments to a file and answers queue_until, so the
	// test can assert EXACTLY what the proven front would be asked.
	dir := t.TempDir()
	argsFile := filepath.Join(dir, "args")
	p := stubDecider(t, `echo "$@" > `+argsFile+`; echo queue_until`)
	t.Setenv(EnvDecider, p)

	d, err := Decider{}.Decide(context.Background(), true, "working_hours", "", "occupied", false)
	if err != nil {
		t.Fatalf("Decide: %v", err)
	}
	if d != QueueUntil {
		t.Fatalf("decision = %q", d)
	}
	raw, err := os.ReadFile(argsFile)
	if err != nil {
		t.Fatal(err)
	}
	want := "decide true working_hours whenever occupied false\n"
	if string(raw) != want {
		t.Fatalf("front asked %q, want %q (empty ask must become whenever)", raw, want)
	}
}

func TestDecideFailsClosed(t *testing.T) {
	cases := []struct {
		name   string
		script string
	}{
		{"front exits 3 with nothing", "exit 3"},
		{"front exits 4 with nothing", "exit 4"},
		{"front prints an unknown word", "echo maybe"},
		{"front prints nothing and exits 0", "true"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Setenv(EnvDecider, stubDecider(t, tc.script))
			if _, err := (Decider{}).Decide(context.Background(), true, "whenever", "whenever", "free", false); err == nil {
				t.Fatalf("a decider that did not speak a decision word must yield an error, never a presentation")
			}
		})
	}
}

func TestDecideUnwiredRefusesAndRecordsTheGap(t *testing.T) {
	t.Setenv(EnvDecider, "")
	ledger := filepath.Join(t.TempDir(), "gaps.jsonl")
	d := Decider{Gaps: gapledger.Open(ledger)}
	_, err := d.Decide(context.Background(), true, "whenever", "whenever", "free", false)
	if !errors.Is(err, ErrDeciderUnwired) {
		t.Fatalf("err = %v, want ErrDeciderUnwired", err)
	}
	raw, rerr := os.ReadFile(ledger)
	if rerr != nil || len(raw) == 0 {
		t.Fatalf("the unwired decider must be recorded as a gap (self-enhancement trigger): %v", rerr)
	}
}

func TestDecideArrivalUnknownSenderIsAskedWithDeadGrant(t *testing.T) {
	dir := t.TempDir()
	argsFile := filepath.Join(dir, "args")
	t.Setenv(EnvDecider, stubDecider(t, `echo "$@" > `+argsFile+`; echo refuse`))

	d, err := (Decider{}).DecideArrival(context.Background(), Table{}, "stranger@cold.call", "free", false)
	if err != nil {
		t.Fatalf("DecideArrival: %v", err)
	}
	if d != Refuse {
		t.Fatalf("decision = %q", d)
	}
	raw, _ := os.ReadFile(argsFile)
	if got := string(raw); got != "decide false whenever whenever free false\n" {
		t.Fatalf("an unknown sender must be asked with Grant_Live=false so the refusal is the CORE's theorem, got %q", got)
	}
}

// TestAgainstTheRealFront exercises the seam against the actual Ada
// delivery_policy_front when this machine has one (the factory wu dir),
// skipping cleanly elsewhere — CI has stubs, Bill has the real thing.
func TestAgainstTheRealFront(t *testing.T) {
	real := os.Getenv("REAL_DELIVERY_POLICY_FRONT")
	if real == "" {
		home, _ := os.UserHomeDir()
		real = filepath.Join(home, "dev", "ada-factory", "wu-delivery-policy-front", "delivery_policy_front")
	}
	if _, err := os.Stat(real); err != nil {
		t.Skipf("no real front here: %v", err)
	}
	t.Setenv(EnvDecider, real)
	table := Table{
		"child@family.net":  {Ceiling: "break_through", Ask: "break_through", Live: true},
		"school@school.org": {Ceiling: "working_hours", Ask: "urgent", Live: true},
		"gone@revoked.com":  {Ceiling: "break_through", Live: false},
	}
	cases := []struct {
		from, presence string
		quiet          bool
		want           Decision
	}{
		{"The Bairn <child@family.net>", "asleep", true, PresentNow},
		// Capped: urgent ask on a working_hours ceiling is working_hours —
		// the seat first expected Interrupt here and the PROVEN CORE said no.
		{"school@school.org", "occupied", false, QueueUntil},
		{"school@school.org", "away", false, QueueUntil},
		{"gone@revoked.com", "free", false, Refuse},
		{"stranger@cold.call", "free", false, Refuse},
	}
	for _, tc := range cases {
		got, err := (Decider{}).DecideArrival(context.Background(), table, tc.from, tc.presence, tc.quiet)
		if err != nil {
			t.Fatalf("%s: %v", tc.from, err)
		}
		if got != tc.want {
			t.Errorf("%s (presence=%s quiet=%v) = %s, want %s", tc.from, tc.presence, tc.quiet, got, tc.want)
		}
	}
}
