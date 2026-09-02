// Package brief is the interview brief the factory issues and the live ledger
// state of one interview against it.
//
// ═══════════════════════════════════════════════════════════════════════════
//
//	★  THE CONDUCT WALL LIVES IN THIS FILE, AND IT IS STRUCTURAL.
//
// ═══════════════════════════════════════════════════════════════════════════
//
// A brief carries ITEMS ONLY: things the factory wants to know, in the order it
// would like them raised. It carries no wait time, no permission to interrupt,
// no instruction about what ghillie may disclose, and NO SUCH FIELD CAN BE
// EXPRESSED — the Go type has no place to put one and Decode uses
// DisallowUnknownFields, so a brief carrying `wait_time_ms`, `may_interrupt` or
// `explain_mechanism` FAILS TO PARSE AND IS REFUSED WHOLESALE.
//
// Why that is worth a wall rather than a code review note: the promise being
// sold is "this agent will not interrupt you, and here is the proof". If the
// facade can set the wait threshold, the promise is a setting, and a setting is
// changeable by whoever controls the facade — including whoever compromises it.
// Adding conduct to the brief later must cost a changed Go type in a reviewed
// commit, not a new field on a JSON object.
//
// ★ AND GHILLIE JUDGES NOTHING. This package tracks what was asked and what
// came back. It does not score, does not detect gaps, does not decide whether a
// spec is complete, and has no function that could be mistaken for one. The
// verbs it is entitled to are RECALL, PRESENT and ASK. The factory alone JUDGES,
// SCORES and DECIDES WHAT IS MISSING. Outstanding below counts states; it does
// not pass judgement, and nothing in ghillie may turn it into "your spec is
// ready".
package brief

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"sort"
	"strings"

	"github.com/tonygair/ghillie/internal/conduct"
)

// MaxBriefBytes bounds a fetched brief. A brief is a short list of questions; a
// megabyte of it is a fault or an attack, and either way it is refused before it
// is parsed.
const MaxBriefBytes = 1 << 20

// Item is one thing the factory wants to know.
//
// Want is CONTENT and it is the facade's to write. Nothing about how the
// question is put — how long ghillie waits, whether it may cut in, how many
// times it may return to the point — appears here or anywhere else in this file.
//
// ItemClass is CONTENT TOO, not conduct: it names WHAT the item is (an
// identity the factory stamps), never how it is put. It has no force of its
// own — it only gains meaning when the OWNER's standing fill rules name the
// same class (internal/fill), and a brief cannot create, change or invoke
// those rules. Untagged items simply cannot match any rule and are asked.
type Item struct {
	ID        int    `json:"id"`
	Want      string `json:"want"`
	ItemClass string `json:"item_class,omitempty"`
}

// Brief is one issued interview brief. Identity, version and items — nothing
// else, by construction.
//
// BriefID and Version are not conduct: they are how this brief is named and
// which revision it is, and they are checked against the signed frame that
// announced it (ArtifactRef and Version, ledger 119) so that the body fetched
// over an unsigned GET is provably the body the SIGNED frame referred to.
type Brief struct {
	BriefID string `json:"brief_id"`
	Version uint32 `json:"version"`
	Items   []Item `json:"items"`
}

// ErrConductInBrief reports a brief that tried to deliver CONDUCT. It is a
// distinct error from an ordinary malformed brief because it is a distinct
// event: an ordinary malformed brief is a bug at the factory, and this is
// someone trying to change how ghillie treats a person.
var ErrConductInBrief = errors.New("brief: CONDUCT DELIVERED OVER THE WIRE — refused wholesale")

// ErrMalformedBrief reports a brief that does not parse or does not validate.
var ErrMalformedBrief = errors.New("brief: malformed")

// conductFieldNames are the field names whose appearance is reported as an
// attempt to deliver conduct rather than as an ordinary unknown field.
//
// ⚠ THIS LIST IS NOT THE WALL. The wall is DisallowUnknownFields, which refuses
// every unknown field including ones nobody has thought of yet. This list only
// improves the SENTENCE the refusal is reported with, so that the interesting
// case is legible in a log instead of looking like a typo. Do not let anyone
// "simplify" the decoder to check this list instead.
var conductFieldNames = map[string]string{
	"wait_time_ms":      "how long ghillie waits before filling a silence",
	"wait_ms":           "how long ghillie waits before filling a silence",
	"may_interrupt":     "whether ghillie may cut the client off",
	"can_interrupt":     "whether ghillie may cut the client off",
	"explain_mechanism": "whether ghillie discloses how the factory judges a spec",
	"disclose_scoring":  "whether ghillie discloses how the factory judges a spec",
	"silence_fill":      "whether ghillie fills a silence",
	"max_attempts":      "how many times ghillie may put the same question",
	"attempt_bound":     "how many times ghillie may put the same question",
	"persona":           "how ghillie presents itself to the client",
	"tone":              "how ghillie presents itself to the client",
}

// Decode reads a brief from r under the strict decoder.
//
// The three things it does, in order, and all three matter:
//  1. bounds the body (MaxBriefBytes);
//  2. decodes with DisallowUnknownFields — THE WALL;
//  3. refuses trailing content, so a well-formed brief followed by a second
//     JSON document cannot smuggle anything past a decoder that stopped early.
func Decode(r io.Reader) (*Brief, error) {
	dec := json.NewDecoder(io.LimitReader(r, MaxBriefBytes+1))
	dec.DisallowUnknownFields()

	var b Brief
	if err := dec.Decode(&b); err != nil {
		if field, ok := unknownField(err); ok {
			if what, conduct := conductFieldNames[field]; conduct {
				return nil, fmt.Errorf("%w: the brief carried %q (%s). Conduct is compiled into ghillie and is not deliverable; the whole brief is refused", ErrConductInBrief, field, what)
			}
			return nil, fmt.Errorf("%w: the brief carried the unknown field %q. A brief carries items only; the whole brief is refused", ErrConductInBrief, field)
		}
		return nil, fmt.Errorf("%w: %w", ErrMalformedBrief, err)
	}

	// Trailing content. A brief is exactly one JSON document.
	if _, err := dec.Token(); !errors.Is(err, io.EOF) {
		if err == nil {
			return nil, fmt.Errorf("%w: trailing content after the brief", ErrMalformedBrief)
		}
		return nil, fmt.Errorf("%w: trailing content after the brief: %w", ErrMalformedBrief, err)
	}

	if err := b.validate(); err != nil {
		return nil, err
	}
	return &b, nil
}

// unknownField pulls the offending field name out of encoding/json's
// DisallowUnknownFields error, whose text is `json: unknown field "x"`.
//
// It is string-matching on another package's error text, which is ugly, and it
// is confined to producing a better MESSAGE — the refusal itself does not depend
// on it. If the text ever changes, the brief is still refused; only the sentence
// gets less specific.
func unknownField(err error) (string, bool) {
	const marker = "unknown field "
	msg := err.Error()
	i := strings.Index(msg, marker)
	if i < 0 {
		return "", false
	}
	rest := msg[i+len(marker):]
	rest = strings.TrimPrefix(rest, `"`)
	if j := strings.Index(rest, `"`); j >= 0 {
		rest = rest[:j]
	}
	if rest == "" {
		return "", false
	}
	return rest, true
}

// validate checks the shape a brief must have to be conducted at all.
func (b *Brief) validate() error {
	if strings.TrimSpace(b.BriefID) == "" {
		return fmt.Errorf("%w: no brief id", ErrMalformedBrief)
	}
	if len(b.Items) == 0 {
		return fmt.Errorf("%w: brief %s carries no items", ErrMalformedBrief, b.BriefID)
	}
	seen := make(map[int]bool, len(b.Items))
	for _, it := range b.Items {
		if it.ID <= 0 {
			return fmt.Errorf("%w: brief %s has an item with id %d (ids are positive)", ErrMalformedBrief, b.BriefID, it.ID)
		}
		if seen[it.ID] {
			return fmt.Errorf("%w: brief %s repeats item id %d", ErrMalformedBrief, b.BriefID, it.ID)
		}
		seen[it.ID] = true
		if strings.TrimSpace(it.Want) == "" {
			return fmt.Errorf("%w: brief %s item %d has nothing in it", ErrMalformedBrief, b.BriefID, it.ID)
		}
	}
	return nil
}

// State is the LIVE LEDGER of one interview against one brief. Every state
// transition in it goes through Question_Ledger_Pkg (ledger 123) — there is no
// second opinion and no shortcut, which is what makes
// INTERRUPTED-IS-NOT-ANSWERED and NEVER-MOVE-ON-FROM-A-MERE-MENTION hold of the
// interview and not merely of the transliterated function.
type State struct {
	brief    *Brief
	order    []int
	states   map[int]conduct.QuestionState
	attempts map[int]int
	replies  map[int]string
}

// NewState opens a fresh ledger over a brief. Every item starts Unasked, which
// is ledger 123's own empty state.
func NewState(b *Brief) *State {
	s := &State{
		brief:    b,
		order:    make([]int, 0, len(b.Items)),
		states:   make(map[int]conduct.QuestionState, len(b.Items)),
		attempts: make(map[int]int, len(b.Items)),
		replies:  make(map[int]string, len(b.Items)),
	}
	for _, it := range b.Items {
		s.order = append(s.order, it.ID)
		s.states[it.ID] = conduct.Unasked
	}
	return s
}

// Brief returns the brief this ledger is over.
func (s *State) Brief() *Brief { return s.brief }

// Order returns the item ids in the order the factory listed them. ORDER IS
// CONTENT and it is the factory's to choose.
func (s *State) Order() []int { return append([]int(nil), s.order...) }

// Want returns the text of an item.
func (s *State) Want(id int) string {
	for _, it := range s.brief.Items {
		if it.ID == id {
			return it.Want
		}
	}
	return ""
}

// StateOf returns an item's ledger state.
func (s *State) StateOf(id int) conduct.QuestionState { return s.states[id] }

// Attempts returns how many times an item has been put.
func (s *State) Attempts(id int) int { return s.attempts[id] }

// Reply returns whatever the client said in connection with an item. It is
// stored verbatim and NOT interpreted: whether it answers the item is the
// factory's judgement, and the ledger state records only what happened, not
// whether it was any good.
func (s *State) Reply(id int) string { return s.replies[id] }

// Ask records that ghillie put the item. It is Advance with Ask, and it counts
// the attempt.
func (s *State) Ask(id int) {
	s.states[id] = conduct.AdvanceQuestion(s.states[id], conduct.EventAsk)
	s.attempts[id]++
}

// Answer records that the client answered the item, storing what they said.
// It is Advance with Receive_Answer.
//
// ★ 2026-08-05 — SILENCE IS NOT AN ANSWER. This used to advance to Answered for
// ANY text, empty included, so a blank reply produced a ledger entry reading
// `state: Answered, text: null`. Two of those reached the coordinator's
// submission store on 2026-08-01 and sat in the record as answers that were
// never given.
//
// Nothing downstream was fooled — subratify's evidenceOf requires
// hasSubstance(Text) and scores such an item EvidenceAbsent — but a record that
// says a person answered when they did not is a lie whether or not anything acts
// on it, and it is the interviewer's record to keep straight. CutOff has always
// been careful here; this is the same care, one method over.
//
// Ledger 123 is NOT weakened: Advance still says Receive_Answer means Answered.
// What is decided here is whether an empty reply IS an answer received, which is
// the caller's question and not the core's. An item left un-advanced simply stays
// outstanding, so ghillie asks again — which is what should happen when someone
// says nothing.
func (s *State) Answer(id int, text string) {
	// The VERDICT is the core's, not ours. This line measures — does the reply
	// carry substance — and hands the Boolean to the proven Advance_On_Reply,
	// exactly as the glue does for every other core. It briefly lived here as a
	// hand-written early return, which is a decision in Go, which is the thing
	// the estate forges rather than writes.
	hasSubstance := strings.TrimSpace(text) != ""
	s.states[id] = conduct.AdvanceOnReply(s.states[id], hasSubstance)
	if hasSubstance {
		s.replies[id] = text
	}
}

// CutOff records that the client cut ghillie off while it was putting the item.
// It is Advance with Cut_Off.
//
// The interjection is stored because it is what the person said and it belongs
// on the record — but the ITEM DOES NOT BECOME ANSWERED, because ledger 123 says
// so and because a passing mention is not an answer.
func (s *State) CutOff(id int, interjection string) {
	s.states[id] = conduct.AdvanceQuestion(s.states[id], conduct.EventCutOff)
	if strings.TrimSpace(interjection) != "" {
		s.replies[id] = interjection
	}
}

// IsGot reports whether ghillie actually holds this item's answer. It is ledger
// 123's Is_Got and nothing else.
func (s *State) IsGot(id int) bool { return conduct.IsGot(s.states[id]) }

// MayMoveOn reports whether ghillie may treat this item as done. Ledger 123's
// May_Move_On, which is Is_Got exactly.
func (s *State) MayMoveOn(id int) bool { return conduct.MayMoveOn(s.states[id]) }

// Outstanding counts the items ghillie does not hold an answer for.
//
// ⚠ THIS IS RECALL, NOT JUDGEMENT. It says how many items are not Answered. It
// does NOT say whether the spec is adequate, whether the factory has enough, or
// whether the client may stop. Ghillie cannot know those things and must never
// claim to; that is the factory's to decide from the report this ledger produces.
func (s *State) Outstanding() int {
	n := 0
	for _, id := range s.order {
		if !conduct.IsGot(s.states[id]) {
			n++
		}
	}
	return n
}

// Board renders the ledger for the person at the keyboard, so what ghillie
// believes it has is visible rather than hidden. Being able to see the machine's
// state is part of not being processed by it.
func (s *State) Board() string {
	var sb strings.Builder
	sb.WriteString("  ---- what ghillie has, and what it does not ----\n")
	for _, id := range s.order {
		mark := " "
		switch s.states[id] {
		case conduct.Answered:
			mark = "x"
		case conduct.Asked:
			mark = "?"
		case conduct.Interrupted:
			mark = "~"
		case conduct.Unasked:
			mark = " "
		}
		// ★ AN INTERRUPTED ITEM IS SHOWN WITHOUT ITS TEXT. The board is spoken
		// to the client, so putting the wording of a question ghillie was cut
		// off putting would be putting it again by the back door — and would
		// leave the unsaid remainder sitting in the record where it could be
		// resumed. The client is told THAT it happened, not told the question
		// over again.
		if s.states[id] == conduct.Interrupted {
			sb.WriteString(fmt.Sprintf("  [%s] %d %-56s %s\n", mark, id, "(the one I was cut off putting — not repeating it at you)", s.states[id]))
			continue
		}
		short := s.Want(id)
		if i := strings.Index(short, " — "); i > 0 {
			short = short[:i]
		}
		if i := strings.Index(short, " - "); i > 0 {
			short = short[:i]
		}
		if len([]rune(short)) > 52 {
			short = string([]rune(short)[:52]) + "..."
		}
		sb.WriteString(fmt.Sprintf("  [%s] %d %-56s %s\n", mark, id, short, s.states[id]))
	}
	sb.WriteString("  (ghillie does not score this and cannot tell you whether it is enough — the factory decides that)")
	return sb.String()
}

// SortedIDs returns the item ids in ascending numeric order, for deterministic
// reports. It is separate from Order because the factory's ORDER is content and
// must not be quietly re-sorted.
func (s *State) SortedIDs() []int {
	ids := append([]int(nil), s.order...)
	sort.Ints(ids)
	return ids
}
