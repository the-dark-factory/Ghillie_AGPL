package conduct

// This file mirrors Turn_State_Pkg (ada-factory ledger 122, source_sha256
// 3b4a5147c54702f48aa8ced9cab32cc7d2e76a4e2056b892e2cee631004f5c7b, captured
// 2026-07-29, gnatprove level 2 clean).
//
// The Ada, verbatim:
//
//	function Advance (Current : Turn_State_Type; Event : Turn_Event_Type)
//	   return Turn_State_Type
//	  is (if Event = Cut_Off then Listening
//	      elsif Current = Listening and then Event = Person_Finished then Thinking
//	      elsif Current = Thinking and then Event = Reply_Ready then Speaking
//	      elsif Current = Speaking and then Event = Reply_Complete then Listening
//	      else Current)
//
//	function May_Speak (State : Turn_State_Type) return Boolean
//	  is (State = Speaking)
//
// Theorems: INTERRUPTION-ALWAYS-YIELDS · THE-PERSON-GETS-THE-FIRST-WORD ·
// NOTHING-IS-SAID-UNTHOUGHT · FINISHING-RETURNS-THE-FLOOR ·
// NO-STATE-REFUSES-TO-YIELD · ONLY-SPEAKING-SPEAKS · SILENCE-WHILE-LISTENING.
//
// The core's own design decision, worth restating because it is the product:
// interruption is not an error condition and is not refused in any state. There
// is no state from which the terminal may decline to yield, and no state in
// which yielding leads anywhere except back to listening. A terminal that could
// refuse to stop talking would be a different and much worse product.
//
// v1 is TEXT, so "speaking" is a line printed and "listening" is a cursor
// waiting. The state machine is the same one the voice path will use in v2, and
// it is wired now precisely so the promise does not have to be re-established
// when audio arrives.

// TurnState is Turn_State_Pkg.Turn_State_Type, in the same order.
type TurnState uint8

// The three things the terminal can be doing at any moment.
const (
	Listening TurnState = iota
	Thinking
	Speaking
)

// TurnEvent is Turn_State_Pkg.Turn_Event_Type, in the same order.
type TurnEvent uint8

// The four things that move a turn along.
const (
	TurnPersonFinished TurnEvent = iota
	TurnReplyReady
	TurnReplyComplete
	TurnCutOff
)

// AdvanceTurn mirrors Turn_State_Pkg.Advance. The Cut_Off arm is first in the
// Ada and first here: yielding outranks every other transition, from every
// state, unconditionally.
func AdvanceTurn(current TurnState, event TurnEvent) TurnState {
	switch {
	case event == TurnCutOff:
		return Listening
	case current == Listening && event == TurnPersonFinished:
		return Thinking
	case current == Thinking && event == TurnReplyReady:
		return Speaking
	case current == Speaking && event == TurnReplyComplete:
		return Listening
	default:
		return current
	}
}

// MaySpeak mirrors Turn_State_Pkg.May_Speak.
func MaySpeak(state TurnState) bool { return state == Speaking }

// String renders a turn state as the name used in the Ada enumeration.
func (s TurnState) String() string {
	switch s {
	case Listening:
		return "Listening"
	case Thinking:
		return "Thinking"
	case Speaking:
		return "Speaking"
	default:
		return "UNKNOWN_TURN_STATE"
	}
}

// String renders a turn event as the name used in the Ada enumeration.
func (e TurnEvent) String() string {
	switch e {
	case TurnPersonFinished:
		return "Person_Finished"
	case TurnReplyReady:
		return "Reply_Ready"
	case TurnReplyComplete:
		return "Reply_Complete"
	case TurnCutOff:
		return "Cut_Off"
	default:
		return "UNKNOWN_TURN_EVENT"
	}
}
