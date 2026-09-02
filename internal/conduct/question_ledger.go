// Package conduct holds ghillie's CONDUCT: how it behaves while interviewing a
// person, as distinct from what it asks them about.
//
// ★ CONDUCT IS COMPILED IN, NEVER DELIVERED. Content — which questions, in what
// order, with what probes — arrives in a brief over the wire. Conduct — wait
// time, whether it may interrupt, whether it fills silence, what it will and
// will not disclose — is compiled into this binary and cannot be set by anyone
// on the other end of a socket.
//
// That separation is a SAFETY PROPERTY, not a tidiness preference. The promise
// being sold is "this agent will not interrupt you, and here is the proof". If
// the facade could set the wait threshold, the promise would be a setting, and a
// setting is changeable by whoever controls the facade. Conduct delivered over
// the wire is conduct an attacker can rewrite. So the wall is structural: see
// internal/brief, whose type cannot express a conduct field and whose decoder
// refuses a brief that carries one.
//
// Everything in this package is a transliteration of a proven SPARK core, in
// the same style internal/gate uses for ledgers 112 and 120: no policy of its
// own, kept line-for-line with the .ads so a diff is a readable exercise.
//
// HONEST LABEL. These are UNPROVEN SHIMS cross-checked against the proven cores
// by table test. The proven core is the authority: WHERE THIS GO AND THE ADA
// SPEC DISAGREE, THE ADA IS RIGHT.
package conduct

// This file mirrors Question_Ledger_Pkg (ada-factory ledger 123, source_sha256
// 292b3528db5d1ab984f778254f6b30a0602c907b09dbe139b5ab5b35904d4a55, captured
// 2026-07-29, gnatprove level 2 clean).
//
// The Ada, verbatim:
//
//	function Advance (Current : Question_State_Type; Event : Ledger_Event_Type)
//	  return Question_State_Type
//	  is (if Event = Receive_Answer then Answered
//	      elsif Current = Answered then Answered
//	      elsif Event = Cut_Off and then Current = Asked then Interrupted
//	      elsif Event = Ask and then Current /= Answered then Asked
//	      elsif Current = Unasked and then Event = Cut_Off then Unasked
//	      else Current)
//
//	function Is_Got (State : Question_State_Type) return Boolean
//	  is (State = Answered)
//
//	function May_Move_On (State : Question_State_Type) return Boolean
//	  is (Is_Got (State))
//
// The two theorems this core exists for:
//
//   - INTERRUPTED-IS-NOT-ANSWERED. A question that was put into the air and cut
//     off before it landed is not a question that was answered. The interviewer
//     neither has that answer nor may pretend to.
//   - NEVER-MOVE-ON-FROM-A-MERE-MENTION. May_Move_On is Is_Got exactly, so
//     having merely raised a thing the factory needs licenses nothing.

// QuestionState is Question_Ledger_Pkg.Question_State_Type, in the same order.
type QuestionState uint8

// The four states of one thing the factory wants to know.
const (
	Unasked QuestionState = iota
	Asked
	Answered
	Interrupted
)

// LedgerEvent is Question_Ledger_Pkg.Ledger_Event_Type, in the same order.
type LedgerEvent uint8

// The three things that can happen to a question.
const (
	EventAsk LedgerEvent = iota
	EventReceiveAnswer
	EventCutOff
)

// AdvanceQuestion mirrors Question_Ledger_Pkg.Advance. The arms are in the
// Ada's own order, because the Ada is an if/elsif chain and the order is part of
// the meaning.
//
// Note the design decision the core records: an interruption is NEVER
// DESTRUCTIVE. Being cut off while asking loses only the asking, never a
// previously received answer — which is why the Answered arm sits above the
// Cut_Off arm. A person who interrupts to add something cannot thereby erase
// what they already said.
func AdvanceQuestion(current QuestionState, event LedgerEvent) QuestionState {
	switch {
	case event == EventReceiveAnswer:
		return Answered
	case current == Answered:
		return Answered
	case event == EventCutOff && current == Asked:
		return Interrupted
	case event == EventAsk && current != Answered:
		return Asked
	case current == Unasked && event == EventCutOff:
		return Unasked
	default:
		return current
	}
}

// IsGot mirrors Question_Ledger_Pkg.Is_Got: whether the interviewer actually
// HAS this answer. Interrupted is not got, and that is the whole point of the
// state existing.
func IsGot(state QuestionState) bool { return state == Answered }

// MayMoveOn mirrors Question_Ledger_Pkg.May_Move_On, which is Is_Got exactly.
//
// It is a separate function in the Ada, and it is kept separate here, because
// the temptation it guards against is real: an interviewer that moved on from
// something on the strength of having mentioned it would look fluent and would
// be lying to the factory about what it holds.
func MayMoveOn(state QuestionState) bool { return IsGot(state) }

// String renders a question state as the name used in the Ada enumeration, for
// the reports a human reads and the factory scores.
func (s QuestionState) String() string {
	switch s {
	case Unasked:
		return "Unasked"
	case Asked:
		return "Asked"
	case Answered:
		return "Answered"
	case Interrupted:
		return "Interrupted"
	default:
		return "UNKNOWN_QUESTION_STATE"
	}
}

// String renders a ledger event as the name used in the Ada enumeration.
func (e LedgerEvent) String() string {
	switch e {
	case EventAsk:
		return "Ask"
	case EventReceiveAnswer:
		return "Receive_Answer"
	case EventCutOff:
		return "Cut_Off"
	default:
		return "UNKNOWN_LEDGER_EVENT"
	}
}

// AdvanceOnReply mirrors Question_Ledger_Pkg.Advance_On_Reply, added to the core
// on 2026-08-05:
//
//	function Advance_On_Reply (Current : Question_State_Type;
//	                           Reply_Has_Substance : Boolean)
//	  return Question_State_Type
//	  is (if Reply_Has_Substance then Advance (Current, Receive_Answer)
//	      else Current)
//
// SILENCE IS NOT AN ANSWER. Receiving an answer means Answered — that was never
// in question. What was in question is whether an EMPTY reply is an answer
// RECEIVED at all, and the terminal assumed it was: any reply advanced the
// ledger, so a blank one produced a record reading Answered with no text. Two
// such records reached the factory's submission store on 2026-08-01 and sat
// there as answers nobody had given.
//
// That decision briefly lived in brief.go as a hand-written `if
// strings.TrimSpace(text) == ""`. Whether a reply is an answer is exactly what
// this core exists to settle, so it is settled there and mirrored here. What
// stays outside is whether the reply HAS substance: that is measurement, and the
// caller counts characters and hands in a Boolean, as the glue does for every
// other core.
//
// The four theorems: SUBSTANCE-IS-AN-ANSWER, SILENCE-IS-NOT-AN-ANSWER,
// SILENCE-NEVER-GETS-WHAT-WAS-NOT-GIVEN, AN-ANSWER-ALREADY-HELD-SURVIVES-SILENCE.
func AdvanceOnReply(current QuestionState, replyHasSubstance bool) QuestionState {
	if replyHasSubstance {
		return AdvanceQuestion(current, EventReceiveAnswer)
	}
	return current
}
