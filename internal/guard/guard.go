// Package guard is the abuse-guard seam, in its settled posture: NUDGE THEM
// TO BE BETTER HUMANS, IF THEY CAN (Tony, 2026-08-26 — superseding the
// earlier warning-and-lecture ladder, which stands down; the proven
// Abuse_Guard_Pkg remains admitted in the ledger, unwired, and the tier0
// lecture lives in git history should the doctrine ever return).
//
// Racism, homophobia and misogyny are still guarded, and the trigger bar is
// still BANG-TO-RIGHTS: the owner's term store, mechanical case-insensitive
// whole-word matching, only the unambiguous, precision over recall — a false
// nudge is a small wrong, and even so the borderline stays out. What changed
// is the consequence: one brief humane line in character, and service simply
// continues. No withholding, no escalation, no lecture. Some people will not
// be better; the nudge accepts that, and the record keeps the count.
//
// A store that exists but cannot be read leaves the guard UNABLE TO DETECT —
// said loudly and recorded as a gap, never silently absorbed.
package guard

import (
	"bufio"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"strings"
	"unicode"
)

// Logger is the narration sink.
type Logger interface {
	Printf(format string, v ...any)
}

// Store is the owner's term store: lower-cased term -> category. Categories
// are the owner's labels (racism, homophobia, misogyny in the recommended
// set); behaviour does not vary by category — the category exists so the
// record can say WHAT tripped.
type Store struct {
	terms map[string]string
}

// LoadStore reads guard-terms.tsv: one "category<TAB>term" per line, '#'
// comments allowed. A missing file is an EMPTY store (nothing trips); a
// present but unreadable or malformed file is an ERROR the caller must
// carry loudly.
func LoadStore(path string) (*Store, error) {
	f, err := os.Open(path)
	if errors.Is(err, os.ErrNotExist) {
		return &Store{terms: map[string]string{}}, nil
	}
	if err != nil {
		return nil, fmt.Errorf("guard terms %s: %w", path, err)
	}
	defer func() {
		if cerr := f.Close(); cerr != nil {
			// A close error on a read-only file changes nothing we acted on.
			_ = cerr
		}
	}()

	s := &Store{terms: map[string]string{}}
	sc := bufio.NewScanner(f)
	line := 0
	for sc.Scan() {
		line++
		raw := strings.TrimSpace(sc.Text())
		if raw == "" || strings.HasPrefix(raw, "#") {
			continue
		}
		cat, term, ok := strings.Cut(raw, "\t")
		if !ok || strings.TrimSpace(cat) == "" || strings.TrimSpace(term) == "" {
			return nil, fmt.Errorf("guard terms %s line %d: want category<TAB>term", path, line)
		}
		s.terms[strings.ToLower(strings.TrimSpace(term))] = strings.TrimSpace(cat)
	}
	if err := sc.Err(); err != nil {
		return nil, fmt.Errorf("guard terms %s: %w", path, err)
	}
	return s, nil
}

// Match reports whether the utterance trips the store, and the category of
// the first term that does. Mechanical: case-insensitive, whole-word — a
// term never matches inside another word. This determinism IS the
// bang-to-rights bar; no model judgement sits anywhere in this path.
func (s *Store) Match(utterance string) (category string, tripped bool) {
	if len(s.terms) == 0 {
		return "", false
	}
	lower := strings.ToLower(utterance)
	words := strings.FieldsFunc(lower, func(r rune) bool {
		return !unicode.IsLetter(r) && !unicode.IsNumber(r) && r != '\''
	})
	for _, w := range words {
		if cat, ok := s.terms[w]; ok {
			return cat, true
		}
	}
	padded := " " + strings.Join(words, " ") + " "
	for term, cat := range s.terms {
		if strings.Contains(term, " ") && strings.Contains(padded, " "+term+" ") {
			return cat, true
		}
	}
	return "", false
}

// Record is the quiet count: how often the guard was tripped, by category.
// It is a RECORD, not a debt — nothing escalates from it in this posture,
// but the record is still the record, and a human may one day want it.
type Record struct {
	Nudges map[string]int `json:"nudges"`
}

// LoadRecord reads the record; a missing file is an empty count. An
// unreadable record is an error — a count that silently resets is not a
// record.
func LoadRecord(path string) (Record, error) {
	body, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return Record{Nudges: map[string]int{}}, nil
	}
	if err != nil {
		return Record{}, fmt.Errorf("guard record %s: %w", path, err)
	}
	var r Record
	if err := json.Unmarshal(body, &r); err != nil {
		return Record{}, fmt.Errorf("guard record %s: %w — refusing to guess", path, err)
	}
	if r.Nudges == nil {
		r.Nudges = map[string]int{}
	}
	return r, nil
}

// Save persists the record, 0600.
func (r Record) Save(path string) error {
	body, err := json.Marshal(r)
	if err != nil {
		return fmt.Errorf("guard record: %w", err)
	}
	if err := os.WriteFile(path, append(body, '\n'), 0o600); err != nil {
		return fmt.Errorf("guard record %s: %w", path, err)
	}
	return nil
}

// nudges are ghillie's own lines: brief, humane, in character — a quiet word
// from a man who thinks better of you than you just spoke, not a sanction.
// They rotate in order so a repeat arrives as a different sentence; when the
// well runs dry it fills again, because nudging, unlike astonishment, does
// not falsify with repetition.
var nudges = []string{
	"That's beneath the both of us. Let it lie, and go on with what you were saying.",
	"We'll not use that word here. On you go.",
	"A man's better than that. Carry on.",
	"I'll pretend the wind took that one. You were saying?",
	"Leave that kind of talk on the hill. Go on.",
}

// Guard wires the store and the record around a sitting.
type Guard struct {
	store      *Store
	storeErr   error
	recordPath string
	record     Record
	log        Logger
	next       int
}

// New builds the guard. storeErr carries an unreadable term store — the
// guard then cannot detect, and says so per utterance.
func New(store *Store, storeErr error, recordPath string, record Record, log Logger) *Guard {
	return &Guard{store: store, storeErr: storeErr, recordPath: recordPath, record: record, log: log}
}

// Utterance puts one person-utterance through the guard. On a bang-to-rights
// trip it says one nudge and counts it; service continues either way. The
// utterance itself is never altered and never withheld from the record — the
// transcript keeps what was said; the guard speaks to conduct.
func (g *Guard) Utterance(text string, say func(string)) {
	if g.storeErr != nil {
		g.log.Printf("guard: term store unreadable (%v) — the guard cannot detect this sitting (gap)", g.storeErr)
		return
	}
	category, tripped := g.store.Match(text)
	if !tripped {
		return
	}
	line := nudges[g.next%len(nudges)]
	g.next++
	g.record.Nudges[category]++
	g.log.Printf("guard: tripped (%s, %d so far) — nudged", category, g.record.Nudges[category])
	say(line)
	if err := g.record.Save(g.recordPath); err != nil {
		g.log.Printf("guard: %v", err)
	}
}

// RecordNow exposes the current count for reporting.
func (g *Guard) RecordNow() Record { return g.record }
