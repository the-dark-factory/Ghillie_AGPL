package gapledger

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"
)

func TestRecordAndLoadRoundTrip(t *testing.T) {
	path := filepath.Join(t.TempDir(), "gaps.jsonl")
	l := Open(path)
	ctx := context.Background()
	if !l.Wired() {
		t.Fatal("a ledger opened on a real path reports itself unwired")
	}
	if err := l.Record(ctx, "DF_CALENDAR_DECIDER", "may I share my owner's calendar?", "asked by a caller"); err != nil {
		t.Fatalf("record: %v", err)
	}
	recs, err := Load(ctx, path)
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	if len(recs) != 1 {
		t.Fatalf("got %d records, want 1", len(recs))
	}
	if recs[0].Decider != "DF_CALENDAR_DECIDER" || recs[0].Question == "" || recs[0].At.IsZero() {
		t.Errorf("record round-tripped wrong: %+v", recs[0])
	}
}

// TestUnwiredRecordsNothingAndDoesNotFail: not watching is not a failure. A
// claw with no ledger configured must carry on exactly as before.
func TestUnwiredRecordsNothingAndDoesNotFail(t *testing.T) {
	l := Open("")
	if l.Wired() {
		t.Error("an empty path reports itself wired")
	}
	if err := l.Record(context.Background(), "D", "q", ""); err != nil {
		t.Errorf("an unwired ledger returned an error: %v", err)
	}
}

// TestBoundsRefused is table-driven over everything the writer must refuse,
// and the reader must refuse the same things.
func TestBoundsRefused(t *testing.T) {
	tests := []struct {
		name     string
		decider  string
		question string
		want     error
	}{
		{name: "no decider", decider: "  ", question: "q", want: ErrEmptyDecider},
		{name: "no question", decider: "D", question: "   ", want: ErrEmptyQuestion},
	}
	l := Open(filepath.Join(t.TempDir(), "gaps.jsonl"))
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			err := l.Record(context.Background(), tc.decider, tc.question, "")
			if !errors.Is(err, tc.want) {
				t.Fatalf("got %v, want %v", err, tc.want)
			}
		})
	}
}

// TestMalformedLineRefusesWholeLoad: a gap ledger you cannot trust is worse
// than none, because the COUNT is the entire point of it.
func TestMalformedLineRefusesWholeLoad(t *testing.T) {
	path := filepath.Join(t.TempDir(), "gaps.jsonl")
	lines := `{"at":"2026-08-24T10:00:00Z","decider":"D1","question":"q1"}
{"at":"2026-08-24T10:01:00Z","decider":"D2","question":"q2","surprise":"field"}
`
	if err := os.WriteFile(path, []byte(lines), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}
	if _, err := Load(context.Background(), path); !errors.Is(err, ErrMalformedLine) {
		t.Fatalf("got %v, want ErrMalformedLine — an unknown field was half-read", err)
	}
}

// TestMissingFileIsEmptyNotError: a claw that has met no gaps has nothing to
// say, and that is not a fault.
func TestMissingFileIsEmptyNotError(t *testing.T) {
	recs, err := Load(context.Background(), filepath.Join(t.TempDir(), "never-written.jsonl"))
	if err != nil {
		t.Fatalf("a missing ledger errored: %v", err)
	}
	if len(recs) != 0 {
		t.Errorf("got %d records from a missing file", len(recs))
	}
}

// TestNoDeduplicationOnWrite: frequency IS the signal. The same gap met three
// times must be three records, or the measurement is destroyed.
func TestNoDeduplicationOnWrite(t *testing.T) {
	path := filepath.Join(t.TempDir(), "gaps.jsonl")
	l := Open(path)
	ctx := context.Background()
	for i := 0; i < 3; i++ {
		if err := l.Record(ctx, "D", "the same question", ""); err != nil {
			t.Fatalf("record %d: %v", i, err)
		}
	}
	recs, err := Load(ctx, path)
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	if len(recs) != 3 {
		t.Fatalf("got %d records, want 3 — writes were deduplicated", len(recs))
	}
}

// TestTallies checks the read-side counting, including that distinct
// questions under one decider are kept so a reader can tell one real gap from
// several unrelated things sharing a name.
func TestTallies(t *testing.T) {
	path := filepath.Join(t.TempDir(), "gaps.jsonl")
	l := Open(path)
	ctx := context.Background()
	for _, q := range []string{"calendar?", "calendar?", "contacts?"} {
		if err := l.Record(ctx, "DF_SHARE_DECIDER", q, ""); err != nil {
			t.Fatalf("record: %v", err)
		}
	}
	if err := l.Record(ctx, "DF_OTHER", "something else", ""); err != nil {
		t.Fatalf("record: %v", err)
	}
	recs, err := Load(ctx, path)
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	ts := Tallies(recs)
	if len(ts) != 2 {
		t.Fatalf("got %d tallies, want 2", len(ts))
	}
	if ts[0].Decider != "DF_SHARE_DECIDER" || ts[0].Count != 3 {
		t.Errorf("most frequent tally wrong: %+v", ts[0])
	}
	if len(ts[0].Questions) != 2 {
		t.Errorf("got %d distinct questions, want 2 — dedup on read is lost", len(ts[0].Questions))
	}
	if ts[0].First.After(ts[0].Last) {
		t.Error("First is after Last")
	}
}
