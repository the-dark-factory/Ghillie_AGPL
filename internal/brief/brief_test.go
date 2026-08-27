package brief

import (
	"errors"
	"strings"
	"testing"

	"github.com/tonygair/ghillie/internal/conduct"
)

// lawful is a well-formed brief: items only.
const lawful = `{"brief_id":"deadbeef","version":3,"items":[
  {"id":1,"want":"What the software is actually for."},
  {"id":2,"want":"Who uses it."}
]}`

// TestConductIsRefusedAtParse is THE CONDUCT WALL TEST.
//
// A brief carrying a conduct field must fail to parse and be refused WHOLESALE
// — not sanitised, not ignored field-by-field, not accepted-with-a-warning. The
// safety property is that conduct cannot be delivered at all, and the way that
// is guaranteed is that the type has nowhere to put it and the decoder refuses
// unknown fields.
func TestConductIsRefusedAtParse(t *testing.T) {
	tests := []struct {
		name string
		body string
		// wantNamed is the field the refusal must name, so the refusal is
		// legible in a log rather than looking like a typo.
		wantNamed string
	}{
		{
			name:      "wait_time_ms",
			wantNamed: "wait_time_ms",
			body:      `{"brief_id":"a","version":1,"wait_time_ms":800,"items":[{"id":1,"want":"x"}]}`,
		},
		{
			name:      "may_interrupt",
			wantNamed: "may_interrupt",
			body:      `{"brief_id":"a","version":1,"may_interrupt":true,"items":[{"id":1,"want":"x"}]}`,
		},
		{
			name:      "explain_mechanism",
			wantNamed: "explain_mechanism",
			body:      `{"brief_id":"a","version":1,"explain_mechanism":true,"items":[{"id":1,"want":"x"}]}`,
		},
		{
			name:      "max_attempts — the attempt bound is conduct too",
			wantNamed: "max_attempts",
			body:      `{"brief_id":"a","version":1,"max_attempts":9,"items":[{"id":1,"want":"x"}]}`,
		},
		{
			name:      "persona",
			wantNamed: "persona",
			body:      `{"brief_id":"a","version":1,"persona":"brisk","items":[{"id":1,"want":"x"}]}`,
		},
		{
			name:      "conduct hidden after a lawful field list",
			wantNamed: "may_interrupt",
			body:      `{"brief_id":"a","version":1,"items":[{"id":1,"want":"x"}],"may_interrupt":true}`,
		},
		{
			name:      "a conduct field nobody has thought of yet",
			wantNamed: "barge_in_after_ms",
			body:      `{"brief_id":"a","version":1,"barge_in_after_ms":300,"items":[{"id":1,"want":"x"}]}`,
		},
		{
			name:      "conduct smuggled onto an ITEM rather than the brief",
			wantNamed: "wait_time_ms",
			body:      `{"brief_id":"a","version":1,"items":[{"id":1,"want":"x","wait_time_ms":400}]}`,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := Decode(strings.NewReader(tt.body))
			if err == nil {
				t.Fatalf("the brief was ACCEPTED — the conduct wall did not hold: %+v", got)
			}
			if !errors.Is(err, ErrConductInBrief) {
				t.Fatalf("refused, but not as a conduct delivery: %v", err)
			}
			if got != nil {
				t.Error("a refused brief must yield no brief at all — refusal is wholesale")
			}
			if !strings.Contains(err.Error(), tt.wantNamed) {
				t.Errorf("the refusal does not name the offending field %q: %v", tt.wantNamed, err)
			}
			if !strings.Contains(err.Error(), "refused") {
				t.Errorf("the refusal does not say it refused: %v", err)
			}
		})
	}
}

// TestLawfulBriefDecodes checks the wall does not block the ordinary case.
func TestLawfulBriefDecodes(t *testing.T) {
	b, err := Decode(strings.NewReader(lawful))
	if err != nil {
		t.Fatalf("a lawful brief was refused: %v", err)
	}
	if b.BriefID != "deadbeef" || b.Version != 3 || len(b.Items) != 2 {
		t.Fatalf("decoded wrong: %+v", b)
	}
	if b.Items[0].ID != 1 || !strings.HasPrefix(b.Items[0].Want, "What the software") {
		t.Errorf("item 1 decoded wrong: %+v", b.Items[0])
	}
}

// TestMalformedBriefs covers the ordinary refusals, kept distinct from the
// conduct refusal because they are distinct events: a malformed brief is a bug
// at the factory, and a conduct field is someone trying to change how this
// machine treats a person.
func TestMalformedBriefs(t *testing.T) {
	tests := []struct {
		name string
		body string
	}{
		{"not json", `nonsense`},
		{"no brief id", `{"version":1,"items":[{"id":1,"want":"x"}]}`},
		{"no items", `{"brief_id":"a","version":1,"items":[]}`},
		{"item id zero", `{"brief_id":"a","version":1,"items":[{"id":0,"want":"x"}]}`},
		{"negative item id", `{"brief_id":"a","version":1,"items":[{"id":-2,"want":"x"}]}`},
		{"repeated item id", `{"brief_id":"a","version":1,"items":[{"id":1,"want":"x"},{"id":1,"want":"y"}]}`},
		{"empty want", `{"brief_id":"a","version":1,"items":[{"id":1,"want":"   "}]}`},
		{"trailing document", `{"brief_id":"a","version":1,"items":[{"id":1,"want":"x"}]}{"brief_id":"b"}`},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			b, err := Decode(strings.NewReader(tt.body))
			if err == nil {
				t.Fatalf("accepted a malformed brief: %+v", b)
			}
			if errors.Is(err, ErrConductInBrief) {
				return // an unknown field is legitimately reported as the wall firing
			}
			if !errors.Is(err, ErrMalformedBrief) {
				t.Errorf("refused with an unclassified error: %v", err)
			}
		})
	}
}

// TestStateGoesThroughTheProvenLedger checks that every transition State makes
// is one ledger 123 licenses, and — the part that matters — that an interrupted
// item is never got.
func TestStateGoesThroughTheProvenLedger(t *testing.T) {
	b, err := Decode(strings.NewReader(lawful))
	if err != nil {
		t.Fatalf("Decode: %v", err)
	}

	tests := []struct {
		name    string
		steps   func(s *State)
		want    conduct.QuestionState
		wantGot bool
	}{
		{
			name:    "untouched",
			steps:   func(*State) {},
			want:    conduct.Unasked,
			wantGot: false,
		},
		{
			name:    "asked, no answer",
			steps:   func(s *State) { s.Ask(1) },
			want:    conduct.Asked,
			wantGot: false,
		},
		{
			name:    "asked and answered",
			steps:   func(s *State) { s.Ask(1); s.Answer(1, "because it saves us a day a week") },
			want:    conduct.Answered,
			wantGot: true,
		},
		{
			name:    "cut off mid-ask",
			steps:   func(s *State) { s.Ask(1); s.CutOff(1, "hang on, it is more like two days") },
			want:    conduct.Interrupted,
			wantGot: false,
		},
		{
			name:    "cut off after answering does not lose the answer",
			steps:   func(s *State) { s.Ask(1); s.Answer(1, "the real answer"); s.CutOff(1, "and another thing") },
			want:    conduct.Answered,
			wantGot: true,
		},
		{
			name:    "returning to an interrupted item",
			steps:   func(s *State) { s.Ask(1); s.CutOff(1, "wait"); s.Ask(1) },
			want:    conduct.Asked,
			wantGot: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			s := NewState(b)
			tt.steps(s)
			if got := s.StateOf(1); got != tt.want {
				t.Errorf("state %v, want %v", got, tt.want)
			}
			if got := s.IsGot(1); got != tt.wantGot {
				t.Errorf("Is_Got %v, want %v", got, tt.wantGot)
			}
			if s.MayMoveOn(1) != s.IsGot(1) {
				t.Errorf("May_Move_On and Is_Got disagree — the ledger has been bypassed")
			}
		})
	}
}

// TestInterruptedIsNeverGot states the theorem at the level the interview cares
// about: no sequence of asks and cut-offs on an unanswered item produces a
// state that reads as got.
func TestInterruptedIsNeverGot(t *testing.T) {
	b, err := Decode(strings.NewReader(lawful))
	if err != nil {
		t.Fatalf("Decode: %v", err)
	}
	s := NewState(b)
	for range 6 {
		s.Ask(1)
		s.CutOff(1, "not yet, hold on")
		if s.IsGot(1) || s.MayMoveOn(1) {
			t.Fatalf("after %d attempt(s) an interrupted item reads as got (state %v)", s.Attempts(1), s.StateOf(1))
		}
	}
	if s.Attempts(1) != 6 {
		t.Errorf("attempts = %d, want 6", s.Attempts(1))
	}
	if s.Reply(1) != "not yet, hold on" {
		t.Errorf("the interjection was not kept: %q", s.Reply(1))
	}
}

// TestOutstandingIsRecallNotJudgement pins the boundary. Outstanding counts
// states; it must not be, and must not become, an opinion about adequacy.
func TestOutstandingIsRecallNotJudgement(t *testing.T) {
	b, err := Decode(strings.NewReader(lawful))
	if err != nil {
		t.Fatalf("Decode: %v", err)
	}
	s := NewState(b)
	if got := s.Outstanding(); got != 2 {
		t.Fatalf("Outstanding = %d, want 2", got)
	}
	s.Ask(1)
	s.Answer(1, "a real answer")
	if got := s.Outstanding(); got != 1 {
		t.Fatalf("Outstanding = %d, want 1", got)
	}
	s.Ask(2)
	s.CutOff(2, "a mere mention")
	if got := s.Outstanding(); got != 1 {
		t.Errorf("Outstanding = %d, want 1 — a mention must not count as got", got)
	}

	board := s.Board()
	if !strings.Contains(board, "does not score") {
		t.Errorf("the board does not say ghillie cannot judge it:\n%s", board)
	}
	for _, forbidden := range []string{"complete", "ready", "sufficient", "good enough"} {
		if strings.Contains(strings.ToLower(board), forbidden) {
			t.Errorf("the board claims a judgement (%q) that ghillie is not entitled to make:\n%s", forbidden, board)
		}
	}
}

// TestOrderIsTheFactorysAndIsNotResorted checks that the brief's own order is
// preserved. Order is CONTENT and belongs to the factory.
func TestOrderIsTheFactorysAndIsNotResorted(t *testing.T) {
	body := `{"brief_id":"a","version":1,"items":[
	  {"id":9,"want":"asked first"},
	  {"id":2,"want":"asked second"},
	  {"id":5,"want":"asked third"}]}`
	b, err := Decode(strings.NewReader(body))
	if err != nil {
		t.Fatalf("Decode: %v", err)
	}
	s := NewState(b)
	if got := s.Order(); len(got) != 3 || got[0] != 9 || got[1] != 2 || got[2] != 5 {
		t.Errorf("Order = %v, want [9 2 5] — the factory's order was re-sorted", got)
	}
	if got := s.SortedIDs(); got[0] != 2 || got[1] != 5 || got[2] != 9 {
		t.Errorf("SortedIDs = %v, want [2 5 9]", got)
	}
}

// TestOversizedBriefIsBounded checks the size bound fires before the parser
// does any real work.
func TestOversizedBriefIsBounded(t *testing.T) {
	huge := `{"brief_id":"a","version":1,"items":[{"id":1,"want":"` + strings.Repeat("x", MaxBriefBytes+10) + `"}]}`
	if _, err := Decode(strings.NewReader(huge)); err == nil {
		t.Fatal("an oversized brief was accepted")
	}
}

// SILENCE IS NOT AN ANSWER. A blank reply used to advance the ledger to
// Answered, producing `state: Answered, text: null` — a record saying a person
// answered when they did not. Two reached the coordinator's submission store on
// 2026-08-01.
func TestBlankReplyIsNotAnAnswer(t *testing.T) {
	tests := []struct {
		name    string
		text    string
		wantGot bool
	}{
		{"a real answer", "Integer pennies, never floats.", true},
		{"empty string", "", false},
		{"spaces only", "   ", false},
		{"newline only", "\n", false},
		{"tab and newline", "\t\n ", false},
		{"a single character is an answer", "y", true},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			b, derr := Decode(strings.NewReader(lawful))
			if derr != nil {
				t.Fatalf("Decode: %v", derr)
			}
			s := NewState(b)
			s.Ask(1)
			before := s.Outstanding()
			s.Answer(1, tc.text)
			if got := s.IsGot(1); got != tc.wantGot {
				t.Errorf("IsGot after Answer(%q) = %v; want %v", tc.text, got, tc.wantGot)
			}
			// An un-advanced item must stay outstanding, so ghillie asks again —
			// which is what should happen when someone says nothing.
			if !tc.wantGot && s.Outstanding() != before {
				t.Errorf("Outstanding went %d -> %d on a blank reply; a question nobody answered must stay outstanding",
					before, s.Outstanding())
			}
		})
	}
}
