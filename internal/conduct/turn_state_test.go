package conduct

import "testing"

// allTurnStates and allTurnEvents span the whole domain Advance is defined
// over: 3 × 4 = 12 cases.
var (
	allTurnStates = []TurnState{Listening, Thinking, Speaking}
	allTurnEvents = []TurnEvent{TurnPersonFinished, TurnReplyReady, TurnReplyComplete, TurnCutOff}
)

// TestAdvanceTurnExhaustive walks every (state × event) pair and asserts the
// five PROVED POSTCONDITIONS of Turn_State_Pkg.Advance, restated with literals.
func TestAdvanceTurnExhaustive(t *testing.T) {
	cases := 0
	for _, current := range allTurnStates {
		for _, event := range allTurnEvents {
			cases++
			got := AdvanceTurn(current, event)

			// INTERRUPTION-ALWAYS-YIELDS
			if event == TurnCutOff && got != Listening {
				t.Errorf("Advance(%v, Cut_Off) = %v; every state must yield to Listening", current, got)
			}
			// THE-PERSON-GETS-THE-FIRST-WORD
			if got == Thinking && current != Thinking {
				if current != Listening || event != TurnPersonFinished {
					t.Errorf("Advance(%v, %v) = Thinking; only Person_Finished from Listening may enter Thinking", current, event)
				}
			}
			// NOTHING-IS-SAID-UNTHOUGHT
			if got == Speaking && current != Speaking {
				if current != Thinking || event != TurnReplyReady {
					t.Errorf("Advance(%v, %v) = Speaking; only Reply_Ready from Thinking may enter Speaking", current, event)
				}
			}
			// FINISHING-RETURNS-THE-FLOOR
			if current == Speaking && event == TurnReplyComplete && got != Listening {
				t.Errorf("Advance(Speaking, Reply_Complete) = %v, want Listening", got)
			}
			// NO-STATE-REFUSES-TO-YIELD
			if current == Speaking && event == TurnCutOff && got == Speaking {
				t.Error("Advance(Speaking, Cut_Off) stayed Speaking — the terminal refused to stop talking")
			}
		}
	}
	if want := len(allTurnStates) * len(allTurnEvents); cases != want {
		t.Fatalf("covered %d combinations, want %d", cases, want)
	}
	t.Logf("exhaustive: %d (state × event) combinations checked against ledger 122 postconditions", cases)
}

// TestAdvanceTurnTable is the hand-derived transition table, read off the Ada.
func TestAdvanceTurnTable(t *testing.T) {
	tests := []struct {
		name    string
		current TurnState
		event   TurnEvent
		want    TurnState
	}{
		{"the person stops talking", Listening, TurnPersonFinished, Thinking},
		{"a reply arrives while still listening", Listening, TurnReplyReady, Listening},
		{"a completion while listening", Listening, TurnReplyComplete, Listening},
		{"cut off while listening", Listening, TurnCutOff, Listening},

		{"still thinking", Thinking, TurnPersonFinished, Thinking},
		{"the reply is ready", Thinking, TurnReplyReady, Speaking},
		{"a completion while thinking", Thinking, TurnReplyComplete, Thinking},
		{"cut off while thinking", Thinking, TurnCutOff, Listening},

		{"the person talks over the reply", Speaking, TurnPersonFinished, Speaking},
		{"another reply while speaking", Speaking, TurnReplyReady, Speaking},
		{"the reply finishes", Speaking, TurnReplyComplete, Listening},
		{"cut off mid-sentence", Speaking, TurnCutOff, Listening},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := AdvanceTurn(tt.current, tt.event); got != tt.want {
				t.Errorf("Advance(%v, %v) = %v, want %v", tt.current, tt.event, got, tt.want)
			}
		})
	}
}

// TestInterruptionAlwaysYields states the headline theorem on its own: there is
// no state from which the terminal may decline to yield, and yielding leads
// nowhere except back to listening.
func TestInterruptionAlwaysYields(t *testing.T) {
	for _, s := range allTurnStates {
		if got := AdvanceTurn(s, TurnCutOff); got != Listening {
			t.Errorf("Advance(%v, Cut_Off) = %v — INTERRUPTION-ALWAYS-YIELDS does not hold", s, got)
		}
		if MaySpeak(AdvanceTurn(s, TurnCutOff)) {
			t.Errorf("after being cut off from %v the terminal may still speak", s)
		}
	}
}

// TestMaySpeakPostconditions checks the two proved postconditions of May_Speak.
func TestMaySpeakPostconditions(t *testing.T) {
	for _, s := range allTurnStates {
		// ONLY-SPEAKING-SPEAKS
		if MaySpeak(s) != (s == Speaking) {
			t.Errorf("May_Speak(%v) = %v, want %v", s, MaySpeak(s), s == Speaking)
		}
		// SILENCE-WHILE-LISTENING
		if s == Listening && MaySpeak(s) {
			t.Error("May_Speak(Listening) must be false")
		}
	}
}

// TestNoWayIntoSpeakingWithoutThinking checks the pair of entry theorems
// together: the only route to Speaking runs through Thinking, and the only
// route to Thinking runs through Listening. There is no shortcut from being cut
// off straight back into talking.
func TestNoWayIntoSpeakingWithoutThinking(t *testing.T) {
	state := Listening
	if got := AdvanceTurn(state, TurnReplyReady); got != Listening {
		t.Errorf("Reply_Ready from Listening reached %v — a reply was readied before the person finished", got)
	}
	state = AdvanceTurn(state, TurnPersonFinished)
	if state != Thinking {
		t.Fatalf("Person_Finished from Listening reached %v, want Thinking", state)
	}
	state = AdvanceTurn(state, TurnReplyReady)
	if state != Speaking {
		t.Fatalf("Reply_Ready from Thinking reached %v, want Speaking", state)
	}
	state = AdvanceTurn(state, TurnCutOff)
	if state != Listening {
		t.Fatalf("Cut_Off from Speaking reached %v, want Listening", state)
	}
	if got := AdvanceTurn(state, TurnReplyReady); got == Speaking {
		t.Error("the terminal resumed speaking straight after being cut off, without the person finishing")
	}
}
