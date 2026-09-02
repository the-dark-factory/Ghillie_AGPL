// Package gapledger records the moments a claw could not answer because it
// HAD NO RULE — as distinct from the moments it answered "no" because a rule
// said so.
//
// # Why the distinction is the whole point
//
// A claw that refuses is usually working correctly: the rule was asked and it
// said no. But a claw that refuses because no decider is wired has not applied
// a rule at all — it has run out of machinery. Those two refusals look the
// same to a user and are completely different facts about the claw. cvgate
// already separates them (ErrDeciderUnwired vs a decider answering false), and
// this package is what turns the first kind into a record.
//
// # What this is NOT
//
// It does not order anything, specify anything, or change what the claw may
// do. It writes down what the claw could not do. That is deliberate: the
// premise being tested is whether a claw can NOTICE its own gaps accurately,
// and that must be established before anything expensive is wired to the
// noticing. If the gaps it records turn out to be noise, better to learn that
// from a ledger than from a factory bill.
//
// # IDA contract: the gap JSONL pool
//
// Append-only JSONL, never rewritten. Each line is one JSON object with
// exactly these fields and no others (an unknown field is a hard error — a
// writer that knew something this reader does not must not be half-read):
//
//	at        string — RFC 3339 time the gap was met
//	decider   string — the env var / decider name that was unwired; non-empty
//	question  string — what was being decided, in the claw's own words
//	detail    string — optional context; may be empty
//
// # Invariants
//
//   - Append-only. Load never rewrites the file; Record appends one line.
//   - A malformed line refuses the WHOLE load. A gap ledger you cannot trust
//     is worse than none: the count is the entire point of it, and a skipped
//     line is a gap that silently never happened.
//   - Recording a gap NEVER fails the caller's own operation. The claw's
//     refusal already happened and stands on its own; if the ledger cannot be
//     written the refusal is unaffected. Callers are told, and carry on.
//   - No deduplication on write. The same gap met ten times is ten records —
//     frequency is the signal being measured, and collapsing it would destroy
//     the measurement. Counting is a READ-side concern.
package gapledger

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"sort"
	"strings"
	"time"
)

// Named errors. Each is a refusal to record or to load.
var (
	// ErrNoPath rejects an unset ledger path. There is no default: a claw
	// that silently wrote its gaps somewhere nobody looks would be worse
	// than one that did not record them.
	ErrNoPath = errors.New("gapledger: no ledger path configured")
	// ErrEmptyDecider rejects a record naming no decider — the decider is
	// what makes the gap actionable.
	ErrEmptyDecider = errors.New("gapledger: empty decider name")
	// ErrEmptyQuestion rejects a record with nothing to say what was being
	// decided. A gap with no question cannot be specified later.
	ErrEmptyQuestion = errors.New("gapledger: empty question")
	// ErrZeroTime rejects a record with no timestamp.
	ErrZeroTime = errors.New("gapledger: record has no timestamp")
	// ErrMalformedLine reports a journal line that is not a well-formed
	// record. It refuses the whole load.
	ErrMalformedLine = errors.New("gapledger: malformed ledger line — refusing the whole pool")
)

// Record is one gap: a moment the claw had no rule to apply.
type Record struct {
	At       time.Time `json:"at"`
	Decider  string    `json:"decider"`
	Question string    `json:"question"`
	Detail   string    `json:"detail,omitempty"`
}

// validate fails closed on anything the reader would refuse. What Record
// refuses to write, Load refuses to read.
func (r Record) validate() error {
	if strings.TrimSpace(r.Decider) == "" {
		return ErrEmptyDecider
	}
	if strings.TrimSpace(r.Question) == "" {
		return fmt.Errorf("%w (decider %q)", ErrEmptyQuestion, r.Decider)
	}
	if r.At.IsZero() {
		return fmt.Errorf("%w (decider %q)", ErrZeroTime, r.Decider)
	}
	return nil
}

// Ledger is an append-only gap ledger on disk. The zero value is unwired and
// records nothing, which is the honest state for a claw with no ledger
// configured — it is not an error, it is simply not watching.
type Ledger struct {
	path string
}

// Open returns a Ledger writing to path. An empty path yields an unwired
// Ledger that discards records and says so through Wired.
func Open(path string) *Ledger { return &Ledger{path: strings.TrimSpace(path)} }

// Wired reports whether a ledger path is configured.
func (l *Ledger) Wired() bool { return l != nil && l.path != "" }

// Path returns the configured path, empty when unwired.
func (l *Ledger) Path() string {
	if l == nil {
		return ""
	}
	return l.path
}

// Record appends one gap. It returns an error only to TELL the caller; the
// caller's own refusal already stands and must not be undone because the
// ledger could not be written. An unwired ledger records nothing and returns
// nil: not watching is not a failure.
func (l *Ledger) Record(ctx context.Context, decider, question, detail string) error {
	if !l.Wired() {
		return nil
	}
	if err := ctx.Err(); err != nil {
		return fmt.Errorf("gapledger: record cancelled: %w", err)
	}
	rec := Record{
		At:       time.Now().UTC().Truncate(time.Second),
		Decider:  strings.TrimSpace(decider),
		Question: strings.TrimSpace(question),
		Detail:   strings.TrimSpace(detail),
	}
	if err := rec.validate(); err != nil {
		return err
	}
	buf, err := json.Marshal(rec)
	if err != nil {
		return fmt.Errorf("gapledger: marshal record: %w", err)
	}
	f, err := os.OpenFile(l.path, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o644)
	if err != nil {
		return fmt.Errorf("gapledger: open %s for append: %w", l.path, err)
	}
	if _, werr := f.Write(append(buf, '\n')); werr != nil {
		return closeAnd(f, fmt.Errorf("gapledger: append to %s: %w", l.path, werr))
	}
	if serr := f.Sync(); serr != nil {
		return closeAnd(f, fmt.Errorf("gapledger: sync %s: %w", l.path, serr))
	}
	if cerr := f.Close(); cerr != nil {
		return fmt.Errorf("gapledger: close %s: %w", l.path, cerr)
	}
	return nil
}

// closeAnd closes f and returns err, noting a close failure alongside.
func closeAnd(f *os.File, err error) error {
	if cerr := f.Close(); cerr != nil {
		return fmt.Errorf("%w (and close failed: %v)", err, cerr)
	}
	return err
}

// Load replays the ledger. A missing file is an empty ledger, not an error:
// a claw that has met no gaps has nothing to say. One malformed line refuses
// the whole load.
func Load(ctx context.Context, path string) ([]Record, error) {
	if err := ctx.Err(); err != nil {
		return nil, fmt.Errorf("gapledger: load cancelled: %w", err)
	}
	if strings.TrimSpace(path) == "" {
		return nil, ErrNoPath
	}
	f, err := os.Open(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil, nil
		}
		return nil, fmt.Errorf("gapledger: open %s: %w", path, err)
	}
	var out []Record
	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 0, 64*1024), 1024*1024)
	line := 0
	for sc.Scan() {
		line++
		raw := bytes.TrimSpace(sc.Bytes())
		if len(raw) == 0 {
			continue
		}
		dec := json.NewDecoder(bytes.NewReader(raw))
		dec.DisallowUnknownFields()
		var rec Record
		if derr := dec.Decode(&rec); derr != nil {
			return nil, closeAnd(f, fmt.Errorf("%w: %s line %d: %w", ErrMalformedLine, path, line, derr))
		}
		if verr := rec.validate(); verr != nil {
			return nil, closeAnd(f, fmt.Errorf("%w: %s line %d: %w", ErrMalformedLine, path, line, verr))
		}
		out = append(out, rec)
	}
	if serr := sc.Err(); serr != nil {
		return nil, closeAnd(f, fmt.Errorf("gapledger: scan %s: %w", path, serr))
	}
	if cerr := f.Close(); cerr != nil {
		return nil, fmt.Errorf("gapledger: close %s: %w", path, cerr)
	}
	return out, nil
}

// Tally is one decider's gap count and the questions that produced it.
type Tally struct {
	// Decider is the unwired decider named by the records.
	Decider string
	// Count is how many times this gap was met — the frequency signal.
	Count int
	// First and Last bound when it was met.
	First, Last time.Time
	// Questions are the distinct questions seen, in first-seen order, so a
	// reader can judge whether the gap is one real gap or several
	// unrelated things sharing a decider name.
	Questions []string
}

// Tallies groups records by decider, most frequent first. Ties break on
// decider name so the order is total and stable — a report whose order
// wobbles between runs is one nobody trusts.
func Tallies(recs []Record) []Tally {
	byDecider := make(map[string]*Tally)
	seenQ := make(map[string]map[string]bool)
	for _, r := range recs {
		t, ok := byDecider[r.Decider]
		if !ok {
			t = &Tally{Decider: r.Decider, First: r.At, Last: r.At}
			byDecider[r.Decider] = t
			seenQ[r.Decider] = make(map[string]bool)
		}
		t.Count++
		if r.At.Before(t.First) {
			t.First = r.At
		}
		if r.At.After(t.Last) {
			t.Last = r.At
		}
		if !seenQ[r.Decider][r.Question] {
			seenQ[r.Decider][r.Question] = true
			t.Questions = append(t.Questions, r.Question)
		}
	}
	out := make([]Tally, 0, len(byDecider))
	for _, t := range byDecider {
		out = append(out, *t)
	}
	sort.SliceStable(out, func(i, j int) bool {
		if out[i].Count != out[j].Count {
			return out[i].Count > out[j].Count
		}
		return out[i].Decider < out[j].Decider
	})
	return out
}
