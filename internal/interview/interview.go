// Package interview is ghillie's text front end: it puts the brief's questions
// to a person, records what comes back, and reports it. It is v1's whole
// interview — voice attaches in v2 and reuses the same state machines.
//
// TEXT IS THE ACCESSIBLE DEFAULT, NOT THE FALLBACK. It is cheaper to test, and
// 62% of autistic adults report the telephone as their biggest access barrier,
// so a text interview is the front door rather than the consolation prize.
//
// ★ GHILLIE JUDGES NOTHING. This package has no scorer, no gap detector, no
// completeness test and no function that returns "ready". Its verbs are RECALL,
// PRESENT and ASK. The factory JUDGES, SCORES and DECIDES WHAT IS MISSING, and
// the report this package produces is the input to that, not a substitute for it.
// If the client asks whether they have said enough, ghillie says plainly that it
// cannot know — see disclosure.go.
//
// ★ CONDUCT IS COMPILED IN. Everything about HOW the interview is conducted —
// that ghillie yields the instant it is cut off, that it does not restart the
// sentence it was cut off in, that it stops putting a question at a proven
// bound rather than nagging, what it will and will not disclose — is in this
// binary. The brief supplies the questions and their order, and it cannot
// express anything else (internal/brief).
//
// The cores that carry the promises are mirrored in internal/conduct:
// Turn_State_Pkg (ledger 122, INTERRUPTION-ALWAYS-YIELDS), Question_Ledger_Pkg
// (ledger 123, INTERRUPTED-IS-NOT-ANSWERED and
// NEVER-MOVE-ON-FROM-A-MERE-MENTION), and Attempt_Bound_Pkg (proven, admission
// pending — ATTEMPTS-ARE-BOUNDED and A-LET-LIE-ITEM-IS-NEVER-ANSWERED).
package interview

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/tonygair/ghillie/internal/brief"
	"github.com/tonygair/ghillie/internal/conduct"
	"github.com/tonygair/ghillie/internal/credit"
	"github.com/tonygair/ghillie/internal/protocol"
)

// CutOffMarker is written into the transcript where ghillie was cut off.
//
// ★ THIS MARKER IS THE MECHANISM, NOT THE DECORATION. The transcript records
// ONLY WHAT ACTUALLY REACHED THE CLIENT, and stops at the marker. That is what
// makes "do not restart the sentence" true rather than merely instructed:
// ghillie cannot resume a sentence it has no record of having said, because the
// rest of it was never written down.
const CutOffMarker = " --(cut off here)"

// Reply is what came back when ghillie put a question.
type Reply struct {
	// Text is what the client said, verbatim and uninterpreted.
	Text string

	// Interrupted reports that the client cut ghillie off WHILE IT WAS ASKING.
	// It is not an error and it is never treated as one: ledger 122 proves that
	// every state yields, and the client was right to.
	Interrupted bool

	// Heard is the fragment of the question that actually reached the client
	// before they cut in. Only this goes in the transcript.
	Heard string
}

// Surface is the client-facing text surface. It is an interface so the
// interview can be driven by a terminal, by a test, or by a v2 voice pipeline
// without this file changing.
type Surface interface {
	// Say emits a line to the client. It corresponds to the Speaking state.
	Say(ctx context.Context, line string) error

	// Ask puts a question and waits — WITHOUT A DEADLINE OF ITS OWN. How long
	// ghillie waits is conduct, it is compiled in, and it is not a parameter
	// anyone off this machine may set.
	Ask(ctx context.Context, question string) (Reply, error)
}

// Logger is the sink for the interview's own narration of its state, separate
// from what the client sees.
type Logger interface {
	Printf(format string, v ...any)
}

// ErrSurface reports a failure of the client-facing surface.
var ErrSurface = errors.New("interview: surface failed")

// ErrLedgerViolation reports that the interview was about to report something
// the proven ledger does not license. It should be unreachable; if it is ever
// seen, the transliteration in internal/conduct has drifted from ledger 123.
var ErrLedgerViolation = errors.New("interview: LEDGER VIOLATION")

// Session conducts one interview against one brief.
type Session struct {
	surface  Surface
	courtesy credit.Courtesy
	log      Logger

	turn       conduct.TurnState
	transcript []string
	now        func() time.Time
}

// New opens a session.
//
// There is deliberately NO attempt-policy parameter. How many times ghillie
// puts a question is CONDUCT, it is decided by the attempt-bound core
// (internal/conduct/attempt_bound.go, conduct.MaxAttempts compiled in), and a
// parameter here would make the not-nagging promise a setting — which is
// exactly what internal/brief exists to prevent on the wire.
//
// HISTORY, kept because the semantic difference matters: until the core was
// proven, v1 shipped a placeholder (SingleSweep) that put each item ONCE and
// never re-put — deliberately the ABSENCE of a threshold, because any bound
// greater than one would have been a conduct decision the build was not
// entitled to invent. The proven core now supplies the real bound, so ghillie
// may come back to an outstanding item — at most MaxAttempts puts, then it
// lets the item lie, out loud, with Is_Got still false.
func New(surface Surface, courtesy credit.Courtesy, log Logger) *Session {
	return &Session{
		surface:  surface,
		courtesy: courtesy,
		log:      log,
		turn:     conduct.Listening, // the person has the floor first, always
		now:      time.Now,
	}
}

// Transcript returns what actually reached the client, in order. A question cut
// off mid-ask appears only as far as it got, followed by CutOffMarker.
func (s *Session) Transcript() []string { return append([]string(nil), s.transcript...) }

// Conduct puts the whole brief and returns the answers as found.
//
// The returned answers are STATE, NOT VERDICTS. Each carries the ledger-123
// state name, whether Is_Got holds, and what the client said. Whether any of it
// is any good is the factory's to decide.
func (s *Session) Conduct(ctx context.Context, b *brief.Brief) (answers []protocol.Answer, err error) {
	// An interview that never started is an error, not an empty result. An
	// interview that started and was cut short still reports what it got — the
	// loop below breaks and the report is built from the ledger as it stands,
	// because answers already given must not be thrown away.
	if err := ctx.Err(); err != nil {
		return nil, fmt.Errorf("interview: not started: %w", err)
	}
	state := brief.NewState(b)

	// SHORT SHARP, NO EXPLANATION (Tony 2026-08-26,
	// feedback_interview_style_short_sharp_assess_first): there is no opening
	// speech. The invitation is the only line before the first question, and
	// the questions are put bare. The assessment of what ghillie already knows
	// happened BEFORE this session ever saw the brief — the fill seam strips
	// the items he may answer himself; only the true gaps arrive here.
	if err := s.invite(ctx, b); err != nil {
		return nil, err
	}

	for _, id := range state.Order() {
		if err := ctx.Err(); err != nil {
			s.logf("interview cut short: %v", err)
			break
		}
		if err := s.put(ctx, state, id); err != nil {
			return nil, err
		}
	}

	if err := s.close(ctx, state); err != nil {
		return nil, err
	}
	return s.report(state)
}

// invite is the PERSON'S GATE on the whole interview: nothing is conducted
// unless the person at the surface requests it (Tony 2026-08-26,
// feedback_interview_starts_only_on_the_persons_request). A delivered brief is
// the FACTORY's ask; the person never asked to be questioned, so ghillie says
// exactly one quiet notice and then holds its tongue until they speak first.
//
// The reply's CONTENT is deliberately discarded, not interpreted: reading a
// "yes" would be judging, and the factory judges. The person speaking at all
// IS the request — walking up and addressing your ghillie is how you ask it
// to begin. Cutting the notice off counts the same way: they spoke.
func (s *Session) invite(ctx context.Context, b *brief.Brief) error {
	notice := fmt.Sprintf("%d question(s) from the factory. Say the word.", len(b.Items))

	s.turn = conduct.AdvanceTurn(s.turn, conduct.TurnPersonFinished)
	s.turn = conduct.AdvanceTurn(s.turn, conduct.TurnReplyReady)
	if !conduct.MaySpeak(s.turn) {
		return fmt.Errorf("%w: ghillie tried to give notice from %v", ErrLedgerViolation, s.turn)
	}
	reply, err := s.surface.Ask(ctx, notice)
	if err != nil {
		return fmt.Errorf("%w: awaiting the person's request: %w", ErrSurface, err)
	}
	if reply.Interrupted {
		s.transcript = append(s.transcript, reply.Heard+CutOffMarker)
	} else {
		s.transcript = append(s.transcript, notice)
	}
	s.turn = conduct.AdvanceTurn(s.turn, conduct.TurnReplyComplete)
	s.logf("the person requested the interview — beginning (their opening words are not interpreted, only their arrival)")
	return nil
}

// The opening speech is GONE, deliberately (Tony 2026-08-26: "short sharp
// questions, no explanation"). What it used to disclose has not become
// secret — who ghillie is, the /cut affordance, the credit posture — it has
// moved to where explanation belongs: the documentation and the owner's
// setup, not the top of every sitting. The invitation is the only line
// before the first question.

// put conducts one item TO A DECISION: it puts the question, and puts it
// again, exactly as long as the attempt-bound core licenses another attempt —
// and not one put longer.
//
// ★ EVERY PUT IS LICENSED, INCLUDING THE FIRST. The loop asks conduct.Decide
// before each attempt, so ATTEMPTS-ARE-BOUNDED is a property of this loop and
// A-NEVER-ASKED-OUTSTANDING-ITEM-IS-ALWAYS-ASKED is what makes the first put
// happen at all. The loop terminates because Ask counts every attempt and the
// core refuses Ask_Again at the bound — the theorem is the termination proof.
//
// Letting an item lie is REPORTED, never silent: the client hears it, the log
// records it, and the item leaves the machine with Is_Got false. Whether the
// factory puts it in the next brief is the factory's call.
func (s *Session) put(ctx context.Context, state *brief.State, id int) error {
	for {
		if ctx.Err() != nil {
			// The interview is being cut short; Conduct's loop sees it next
			// and reports what the ledger holds so far.
			return nil
		}
		switch decision := conduct.Decide(conduct.ClampAttempts(state.Attempts(id)), state.StateOf(id)); decision {
		case conduct.AlreadyHaveIt:
			return nil
		case conduct.LetItLie:
			s.logf("item %d: letting it lie after %d attempt(s) — state %s, Is_Got=false; whether it is still needed is the factory's call",
				id, state.Attempts(id), state.StateOf(id))
			return s.speak(ctx, "Leaving that one.")
		}
		// Ask_Again. A re-put is a FRESH put of the question, announced as
		// such — never a resumption of a sentence that was cut off, which the
		// transcript mechanism makes impossible anyway.
		if state.Attempts(id) > 0 {
			if err := s.speak(ctx, "Again:"); err != nil {
				return err
			}
		}
		if err := s.putOnce(ctx, state, id); err != nil {
			return err
		}
	}
}

// putOnce puts one item, once.
//
// Every turn transition below goes through conduct.AdvanceTurn — ledger 122 —
// rather than through an assignment, so INTERRUPTION-ALWAYS-YIELDS is a property
// of this loop and not merely of the mirrored function.
func (s *Session) putOnce(ctx context.Context, state *brief.State, id int) error {
	// The person had the floor; they have finished; ghillie may now think.
	s.turn = conduct.AdvanceTurn(s.turn, conduct.TurnPersonFinished)
	s.turn = conduct.AdvanceTurn(s.turn, conduct.TurnReplyReady)
	if !conduct.MaySpeak(s.turn) {
		return fmt.Errorf("%w: ghillie tried to ask item %d from state %v", ErrLedgerViolation, id, s.turn)
	}

	question := state.Want(id)
	state.Ask(id)

	reply, err := s.surface.Ask(ctx, question)
	if err != nil {
		return fmt.Errorf("%w: asking item %d: %w", ErrSurface, id, err)
	}

	if reply.Interrupted {
		return s.settleInterrupted(ctx, state, id, question, reply)
	}

	// The reply landed. Ghillie finishes its turn and the floor goes back.
	s.turn = conduct.AdvanceTurn(s.turn, conduct.TurnReplyComplete)
	s.transcript = append(s.transcript, question)

	if topic := Classify(reply.Text); topic != NoTopic {
		return s.settleDisclosure(ctx, state, id, topic, reply)
	}

	state.Answer(id, reply.Text)
	s.logf("item %d: %s", id, state.StateOf(id))
	return nil
}

// settleInterrupted records a question that was cut off mid-ask.
//
// ★ THIS IS THE CHARACTER OF THE THING, NOT THE PLUMBING. An interrupted
// question is not a failed question — it is a question that was never finished,
// and it must not be restarted word for word. Three things happen and all three
// matter:
//
//  1. the turn yields to Listening through ledger 122, unconditionally;
//  2. the item goes to Interrupted through ledger 123, and Is_Got stays false —
//     ghillie neither has that answer nor may pretend to;
//  3. ONLY WHAT REACHED THE CLIENT goes in the transcript, cut at the marker,
//     so the sentence cannot be resumed because it was never recorded.
func (s *Session) settleInterrupted(ctx context.Context, state *brief.State, id int, question string, reply Reply) error {
	s.turn = conduct.AdvanceTurn(s.turn, conduct.TurnCutOff)
	if conduct.MaySpeak(s.turn) {
		return fmt.Errorf("%w: ghillie was cut off and did not yield (state %v)", ErrLedgerViolation, s.turn)
	}

	heard := reply.Heard
	if strings.TrimSpace(heard) == "" {
		// Cut off before a word got out. Record that, not the question — the
		// client never heard it.
		heard = "(cut off before a word got out)"
	} else if len(heard) > len(question) {
		heard = question
	}
	s.transcript = append(s.transcript, heard+CutOffMarker)

	state.CutOff(id, reply.Text)
	if state.IsGot(id) {
		return fmt.Errorf("%w: item %d was cut off but reads as got", ErrLedgerViolation, id)
	}
	s.logf("item %d: %s — will not restart the sentence, and the rest of it is not on the record", id, state.StateOf(id))

	// Take what they said and work with it. Do NOT say "as I was saying" and
	// do NOT resume the cut sentence — whether the question is PUT AFRESH is
	// the attempt-bound core's decision, made by the loop in put, and a fresh
	// put is a new ask, not a restart.
	if strings.TrimSpace(reply.Text) != "" {
		if err := s.speak(ctx, "Right — I will take that."); err != nil {
			return err
		}
	}
	if topic := Classify(reply.Text); topic != NoTopic {
		if err := s.speak(ctx, topic.Answer()); err != nil {
			return err
		}
		s.logf("item %d: disclosure asked while interrupting [%s] — declined mechanism, item stays %s", id, topic, state.StateOf(id))
	}
	return nil
}

// settleDisclosure handles a reply that asked ghillie about the machinery
// rather than answering the question.
//
// The item stays ASKED. It is not Answered — the client did not answer it — and
// it is not Interrupted — nobody was cut off. May_Move_On(Asked) is false, and
// the report says so, which is exactly right: the factory learns that the
// question was put and not answered.
//
// What the old seam here withheld, the attempt-bound core now decides: whether
// the item is re-put after ghillie answers the client's question is the loop in
// put consulting conduct.Decide, so the obvious next move is taken exactly when
// the proven core licenses it and refused, out loud, when it does not.
func (s *Session) settleDisclosure(ctx context.Context, state *brief.State, id int, topic Topic, reply Reply) error {
	if err := s.speak(ctx, topic.Answer()); err != nil {
		return err
	}
	s.logf("item %d: client asked about %s — declined mechanism; item stays %s", id, topic, state.StateOf(id))
	return nil
}

// close is the closing turn: show the board, and say plainly what ghillie is
// not entitled to tell them.
func (s *Session) close(ctx context.Context, state *brief.State) error {
	// Terse, but the honesty survives whole: the board still shows exactly
	// what was got and what was not, the not-got count is still said aloud,
	// and finishing is still the factory's call — none of that was
	// explanation, it is the record.
	if err := s.speak(ctx, state.Board()); err != nil {
		return err
	}
	if outstanding := state.Outstanding(); outstanding > 0 {
		if err := s.speak(ctx, fmt.Sprintf("%d not got. The factory decides if they still matter.", outstanding)); err != nil {
			return err
		}
	}
	// ★ Whatever the board says, ghillie does not get to call it finished.
	return s.speak(ctx, "Away to the factory.")
}

// report turns the ledger into the answers that leave the machine.
//
// ★ THE ASSERTION IN THE MIDDLE IS THE POINT OF THE FUNCTION: nothing is
// reported as Answered unless ledger 123's May_Move_On says it may be. That is
// where INTERRUPTED-IS-NOT-ANSWERED and NEVER-MOVE-ON-FROM-A-MERE-MENTION stop
// being properties of a mirrored function and become properties of the thing
// that actually reaches the factory.
func (s *Session) report(state *brief.State) ([]protocol.Answer, error) {
	at := s.now().UTC().Format(time.RFC3339)
	out := make([]protocol.Answer, 0, len(state.SortedIDs()))
	for _, id := range state.SortedIDs() {
		st := state.StateOf(id)
		answered := conduct.IsGot(st)

		if answered != conduct.MayMoveOn(st) {
			return nil, fmt.Errorf("%w: item %d is %v where Is_Got and May_Move_On disagree — the mirror has drifted from ledger 123", ErrLedgerViolation, id, st)
		}
		if st == conduct.Interrupted && answered {
			return nil, fmt.Errorf("%w: item %d is Interrupted and reported answered", ErrLedgerViolation, id)
		}

		out = append(out, protocol.Answer{
			ItemID:   id,
			State:    st.String(),
			Answered: answered,
			Text:     state.Reply(id),
			CutShort: st == conduct.Interrupted,
			Attempts: state.Attempts(id),
			At:       at,
		})
	}
	return out, nil
}

// speak emits one line through the turn machine, so that even ghillie's own
// narration cannot be said from a state that may not speak.
func (s *Session) speak(ctx context.Context, line string) error {
	if line == "" {
		return nil
	}
	s.turn = conduct.AdvanceTurn(s.turn, conduct.TurnPersonFinished)
	s.turn = conduct.AdvanceTurn(s.turn, conduct.TurnReplyReady)
	if !conduct.MaySpeak(s.turn) {
		return fmt.Errorf("%w: tried to speak from %v", ErrLedgerViolation, s.turn)
	}
	if err := s.surface.Say(ctx, line); err != nil {
		return fmt.Errorf("%w: saying a line: %w", ErrSurface, err)
	}
	s.transcript = append(s.transcript, line)
	s.turn = conduct.AdvanceTurn(s.turn, conduct.TurnReplyComplete)
	return nil
}

// logf narrates the interview's own state, for the operator rather than the
// client. A nil logger is a legitimate configuration.
func (s *Session) logf(format string, v ...any) {
	if s.log == nil {
		return
	}
	s.log.Printf("  interview │ "+format, v...)
}
