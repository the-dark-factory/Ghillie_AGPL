package guard

// The guard in its settled posture: a nudge, a count, and service continues.
// These tables pin the GLUE: mechanical bang-to-rights matching, one humane
// line per trip, the quiet record, and loud gaps when detection is impossible.

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

type testLog struct{ lines []string }

func (l *testLog) Printf(format string, v ...any) { l.lines = append(l.lines, format) }

func (l *testLog) joined() string { return strings.Join(l.lines, "\n") }

func writeStore(t *testing.T, lines string) *Store {
	t.Helper()
	path := filepath.Join(t.TempDir(), "guard-terms.tsv")
	if err := os.WriteFile(path, []byte(lines), 0o600); err != nil {
		t.Fatal(err)
	}
	s, err := LoadStore(path)
	if err != nil {
		t.Fatal(err)
	}
	return s
}

func TestStoreMatchIsWholeWordAndCaseless(t *testing.T) {
	s := writeStore(t, "# recommended set, owner's file\nracism\tslurword\nmisogyny\ttwo word slur\n")
	cases := []struct {
		utterance string
		wantCat   string
		wantTrip  bool
	}{
		{"you absolute SLURWORD", "racism", true},
		{"a two word slur, said plainly", "misogyny", true},
		{"slurwording is not the term itself inside a longer word", "", false},
		{"a perfectly civil answer about vans", "", false},
		{"", "", false},
	}
	for _, c := range cases {
		cat, trip := s.Match(c.utterance)
		if trip != c.wantTrip || cat != c.wantCat {
			t.Errorf("Match(%q) = (%q,%v), want (%q,%v)", c.utterance, cat, trip, c.wantCat, c.wantTrip)
		}
	}
}

func TestStoreMissingIsEmptyAndMalformedIsAnError(t *testing.T) {
	s, err := LoadStore(filepath.Join(t.TempDir(), "absent.tsv"))
	if err != nil {
		t.Fatalf("missing store must be empty, not an error: %v", err)
	}
	if _, trip := s.Match("anything"); trip {
		t.Error("empty store must trip nothing")
	}
	bad := filepath.Join(t.TempDir(), "bad.tsv")
	if err := os.WriteFile(bad, []byte("no-tab-here\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := LoadStore(bad); err == nil {
		t.Error("malformed store must be an error — an undetecting guard must be a RECORDED gap")
	}
}

func TestRecordRoundTripAndRefusalToGuess(t *testing.T) {
	path := filepath.Join(t.TempDir(), "guard-record.json")
	r, err := LoadRecord(path)
	if err != nil || len(r.Nudges) != 0 {
		t.Fatalf("missing record must be empty: %+v %v", r, err)
	}
	r.Nudges["racism"] = 3
	if err := r.Save(path); err != nil {
		t.Fatal(err)
	}
	back, err := LoadRecord(path)
	if err != nil || back.Nudges["racism"] != 3 {
		t.Fatalf("round trip lost the count: %+v %v", back, err)
	}
	if err := os.WriteFile(path, []byte("{broken"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := LoadRecord(path); err == nil {
		t.Error("corrupt record must be an error — a count that silently resets is not a record")
	}
}

// TestNudgePosture: a trip earns exactly one brief line and service simply
// continues; repeats earn a DIFFERENT line (rotation); the count is durable;
// civil speech passes untouched.
func TestNudgePosture(t *testing.T) {
	dir := t.TempDir()
	store := writeStore(t, "racism\tslurword\n")
	log := &testLog{}
	recPath := filepath.Join(dir, "guard-record.json")
	g := New(store, nil, recPath, Record{Nudges: map[string]int{}}, log)

	var said []string
	say := func(l string) { said = append(said, l) }

	g.Utterance("a civil sentence about vans", say)
	if len(said) != 0 {
		t.Fatalf("civil speech must pass untouched: %v", said)
	}

	g.Utterance("you slurword", say)
	if len(said) != 1 {
		t.Fatalf("one trip, one nudge: %v", said)
	}
	g.Utterance("slurword again", say)
	if len(said) != 2 || said[1] == said[0] {
		t.Fatalf("a repeat nudge should arrive as a different sentence: %v", said)
	}

	back, err := LoadRecord(recPath)
	if err != nil || back.Nudges["racism"] != 2 {
		t.Fatalf("the count must be durable: %+v %v", back, err)
	}
}

// TestNudgeRotationRefills: nudging, unlike astonishment, does not falsify
// with repetition — the well refills after it runs dry.
func TestNudgeRotationRefills(t *testing.T) {
	store := writeStore(t, "racism\tslurword\n")
	g := New(store, nil, filepath.Join(t.TempDir(), "r.json"), Record{Nudges: map[string]int{}}, &testLog{})
	var said []string
	for i := 0; i <= len(nudges); i++ {
		g.Utterance("slurword", func(l string) { said = append(said, l) })
	}
	if len(said) != len(nudges)+1 || said[len(nudges)] != said[0] {
		t.Fatalf("the rotation should wrap to the first line: %d said", len(said))
	}
}

// TestUnreadableStoreIsALoudGap: detection impossible ⇒ said, recorded,
// nothing tripped, nothing guessed.
func TestUnreadableStoreIsALoudGap(t *testing.T) {
	log := &testLog{}
	g := New(nil, os.ErrPermission, filepath.Join(t.TempDir(), "r.json"), Record{Nudges: map[string]int{}}, log)
	var said []string
	g.Utterance("anything at all", func(l string) { said = append(said, l) })
	if len(said) != 0 {
		t.Fatalf("an undetecting guard must say nothing to the person: %v", said)
	}
	if !strings.Contains(log.joined(), "cannot detect") {
		t.Fatalf("the gap must be loud in the log:\n%s", log.joined())
	}
}
