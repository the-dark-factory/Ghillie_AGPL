package conduct

import "testing"

// allQuestionStates and allLedgerEvents span the whole domain Advance is
// defined over: 4 × 3 = 12 cases.
var (
	allQuestionStates = []QuestionState{Unasked, Asked, Answered, Interrupted}
	allLedgerEvents   = []LedgerEvent{EventAsk, EventReceiveAnswer, EventCutOff}
)

// TestAdvanceQuestionExhaustive walks every (state × event) pair and asserts,
// for each, the five PROVED POSTCONDITIONS of Question_Ledger_Pkg.Advance.
//
// The postconditions are restated with literal enumeration comparisons rather
// than by calling AdvanceQuestion a second time, so a bug in the transliteration
// cannot hide by being shared between the code and its test.
func TestAdvanceQuestionExhaustive(t *testing.T) {
	cases := 0
	for _, current := range allQuestionStates {
		for _, event := range allLedgerEvents {
			cases++
			got := AdvanceQuestion(current, event)

			// Post: (if Event = Receive_Answer then Advance'Result = Answered)
			if event == EventReceiveAnswer && got != Answered {
				t.Errorf("Advance(%v, %v) = %v; Receive_Answer must yield Answered", current, event, got)
			}
			// Post: (if Current = Answered then Advance'Result = Answered)
			// AN INTERRUPTION IS NEVER DESTRUCTIVE.
			if current == Answered && got != Answered {
				t.Errorf("Advance(Answered, %v) = %v; an answer already held is never lost", event, got)
			}
			// Post: (if Advance'Result = Interrupted and then Current /= Interrupted
			//        then Event = Cut_Off and then Current = Asked)
			if got == Interrupted && current != Interrupted {
				if event != EventCutOff || current != Asked {
					t.Errorf("Advance(%v, %v) = Interrupted; only Cut_Off from Asked may enter Interrupted", current, event)
				}
			}
			// Post: (if Event = Ask and then Current /= Answered
			//        then Advance'Result = Asked)
			if event == EventAsk && current != Answered && got != Asked {
				t.Errorf("Advance(%v, Ask) = %v, want Asked", current, got)
			}
			// Post: (if Current = Unasked and then Event = Cut_Off
			//        then Advance'Result = Unasked)
			if current == Unasked && event == EventCutOff && got != Unasked {
				t.Errorf("Advance(Unasked, Cut_Off) = %v, want Unasked", got)
			}
		}
	}
	if want := len(allQuestionStates) * len(allLedgerEvents); cases != want {
		t.Fatalf("covered %d combinations, want %d", cases, want)
	}
	t.Logf("exhaustive: %d (state × event) combinations checked against ledger 123 postconditions", cases)
}

// TestAdvanceQuestionTable is the hand-derived transition table, read off the
// Ada if/elsif chain, kept separate from the postcondition sweep so a reviewer
// has concrete rows to check by eye.
func TestAdvanceQuestionTable(t *testing.T) {
	tests := []struct {
		name    string
		current QuestionState
		event   LedgerEvent
		want    QuestionState
	}{
		{"asking an untouched item", Unasked, EventAsk, Asked},
		{"an answer arrives out of the blue", Unasked, EventReceiveAnswer, Answered},
		{"cut off before a word was said", Unasked, EventCutOff, Unasked},

		{"re-asking an outstanding item", Asked, EventAsk, Asked},
		{"the answer lands", Asked, EventReceiveAnswer, Answered},
		{"cut off mid-ask", Asked, EventCutOff, Interrupted},

		{"asking something already answered", Answered, EventAsk, Answered},
		{"answering again", Answered, EventReceiveAnswer, Answered},
		{"cut off after it was answered", Answered, EventCutOff, Answered},

		{"returning to an interrupted question", Interrupted, EventAsk, Asked},
		{"the interrupted question is finally answered", Interrupted, EventReceiveAnswer, Answered},
		{"cut off again", Interrupted, EventCutOff, Interrupted},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := AdvanceQuestion(tt.current, tt.event); got != tt.want {
				t.Errorf("Advance(%v, %v) = %v, want %v", tt.current, tt.event, got, tt.want)
			}
		})
	}
}

// TestInterruptedIsNotAnswered states the first theorem on its own, because it
// is the one the interviewer is sold on: a question that was put into the air
// and cut off is not a question that was answered, and no route through the
// state machine may make it look like one.
func TestInterruptedIsNotAnswered(t *testing.T) {
	if got := AdvanceQuestion(Asked, EventCutOff); got != Interrupted {
		t.Fatalf("Advance(Asked, Cut_Off) = %v, want Interrupted", got)
	}
	if IsGot(Interrupted) {
		t.Error("Is_Got(Interrupted) = true — INTERRUPTED-IS-NOT-ANSWERED does not hold")
	}
	if MayMoveOn(Interrupted) {
		t.Error("May_Move_On(Interrupted) = true — the interviewer would move on from a question it never got")
	}
	// The only event that may produce Answered is Receive_Answer, from anywhere.
	for _, current := range allQuestionStates {
		for _, event := range allLedgerEvents {
			got := AdvanceQuestion(current, event)
			if got == Answered && current != Answered && event != EventReceiveAnswer {
				t.Errorf("Advance(%v, %v) = Answered without an answer being received", current, event)
			}
		}
	}
}

// TestNeverMoveOnFromAMereMention states the second theorem: May_Move_On is
// Is_Got exactly, so having merely raised a thing licenses nothing.
func TestNeverMoveOnFromAMereMention(t *testing.T) {
	for _, s := range allQuestionStates {
		// Post: ((May_Move_On'Result = True) = (Is_Got (State) = True))
		if MayMoveOn(s) != IsGot(s) {
			t.Errorf("May_Move_On(%v) = %v but Is_Got(%v) = %v — they must be the same function", s, MayMoveOn(s), s, IsGot(s))
		}
		// Post: ((Is_Got'Result = True) = (State = Answered))
		if IsGot(s) != (s == Answered) {
			t.Errorf("Is_Got(%v) = %v, want %v", s, IsGot(s), s == Answered)
		}
		// Post: (if State = Interrupted then Is_Got'Result = False)
		if s == Interrupted && IsGot(s) {
			t.Errorf("Is_Got(Interrupted) must be false")
		}
		// Post: (if State = Asked then May_Move_On'Result = False)
		if s == Asked && MayMoveOn(s) {
			t.Errorf("May_Move_On(Asked) must be false — asked is not got")
		}
	}
}

// TestAnAnswerIsNeverLost checks the core's recorded design decision: an
// interruption is never destructive. No sequence of asks and cut-offs can move
// an item out of Answered.
func TestAnAnswerIsNeverLost(t *testing.T) {
	sequences := [][]LedgerEvent{
		{EventCutOff},
		{EventAsk, EventCutOff},
		{EventCutOff, EventCutOff, EventAsk, EventCutOff},
		{EventAsk, EventAsk, EventCutOff, EventAsk},
	}
	for _, seq := range sequences {
		state := Answered
		for _, e := range seq {
			state = AdvanceQuestion(state, e)
			if state != Answered {
				t.Fatalf("after %v the state left Answered (now %v) — an interruption erased an answer", seq, state)
			}
		}
	}
}
