package conduct

import "testing"

// allAttemptCounts spans the whole subtype: Natural range 0 .. 32, 33 values.
// With the four question states that makes Decide's entire input domain
// 33 × 4 = 132 cases, and the monotonicity operation's 4 × 33 × 33 = 4356.
func allAttemptCounts() []AttemptCount {
	out := make([]AttemptCount, 0, 33)
	for n := AttemptCount(0); n <= AttemptCountLast; n++ {
		out = append(out, n)
	}
	return out
}

// The expectations below restate the proven postconditions with LITERALS —
// the bound is written 3, "the ledger has it" is written state == Answered —
// rather than by calling MaxAttempts, IsGot or a second run of the function
// under test, so a bug in the transliteration cannot hide by being shared
// between the code and its test.

// TestExhaustedExhaustive walks all 33 attempt counts against the two proved
// postconditions of Attempt_Bound_Pkg.Exhausted.
func TestExhaustedExhaustive(t *testing.T) {
	cases := 0
	for _, attempts := range allAttemptCounts() {
		cases++
		got := Exhausted(attempts)

		// Post: ((Exhausted'Result = True) = (Attempts >= Max_Attempts))
		// EXHAUSTION-IS-EXACTLY-THE-BOUND, with the bound as a literal.
		if got != (attempts >= 3) {
			t.Errorf("Exhausted(%d) = %v, want %v", attempts, got, attempts >= 3)
		}
		// Post: (if Attempts = 0 then Exhausted'Result = False)
		// A-NEVER-ASKED-ITEM-IS-NEVER-EXHAUSTED.
		if attempts == 0 && got {
			t.Error("Exhausted(0) = true — a never-asked item reads as exhausted")
		}
	}
	if cases != 33 {
		t.Fatalf("covered %d attempt counts, want 33", cases)
	}
	t.Logf("exhaustive: %d attempt counts checked against the Exhausted postconditions", cases)
}

// TestOutstandingExhaustive walks all four question states against the two
// proved postconditions of Attempt_Bound_Pkg.Outstanding.
func TestOutstandingExhaustive(t *testing.T) {
	for _, state := range allQuestionStates {
		got := Outstanding(state)

		// Post: ((Outstanding'Result = True) = (Is_Got (State) = False)),
		// with Is_Got restated literally as State = Answered.
		if got != (state != Answered) {
			t.Errorf("Outstanding(%v) = %v, want %v", state, got, state != Answered)
		}
		// Post: (if State = Interrupted then Outstanding'Result = True)
		// AN-INTERRUPTED-ITEM-IS-STILL-OUTSTANDING — 123's theorem survives.
		if state == Interrupted && !got {
			t.Error("Outstanding(Interrupted) = false — an interrupted item reads as held")
		}
	}
}

// TestDecideExhaustive walks the entire 132-case input domain of Decide and
// asserts, for each case, all eight PROVED POSTCONDITIONS of
// Attempt_Bound_Pkg.Decide, restated with literals.
func TestDecideExhaustive(t *testing.T) {
	cases := 0
	for _, state := range allQuestionStates {
		for _, attempts := range allAttemptCounts() {
			cases++
			got := Decide(attempts, state)

			// Post: ((Decide'Result = Already_Have_It) = (Is_Got (State) = True))
			// ALREADY-HAVE-IT-IS-EXACTLY-WHAT-THE-LEDGER-HAS.
			if (got == AlreadyHaveIt) != (state == Answered) {
				t.Errorf("Decide(%d, %v) = %v; Already_Have_It must hold exactly when the state is Answered", attempts, state, got)
			}
			// Post: (if Decide'Result = Let_It_Lie then Is_Got (State) = False)
			// ★ A-LET-LIE-ITEM-IS-NEVER-ANSWERED — the dangerous confusion.
			if got == LetItLie && state == Answered {
				t.Errorf("Decide(%d, Answered) = Let_It_Lie — a let-lie item reads as answered", attempts)
			}
			// Post: (if Decide'Result = Let_It_Lie then May_Move_On (State) = False)
			// ★ A-LET-LIE-ITEM-NEVER-LICENCES-MOVING-ON. May_Move_On is Is_Got
			// exactly (ledger 123), restated literally.
			if got == LetItLie && state == Answered {
				t.Errorf("Decide(%d, Answered) = Let_It_Lie — giving up licensed moving on", attempts)
			}
			// Post: (if State = Interrupted then Decide'Result /= Already_Have_It)
			// AN-INTERRUPTED-ITEM-IS-NEVER-ALREADY-HAVE-IT.
			if state == Interrupted && got == AlreadyHaveIt {
				t.Errorf("Decide(%d, Interrupted) = Already_Have_It", attempts)
			}
			// Post: (if Attempts >= Max_Attempts then Decide'Result /= Ask_Again)
			// ★ ATTEMPTS-ARE-BOUNDED, with the bound as a literal.
			if attempts >= 3 && got == AskAgain {
				t.Errorf("Decide(%d, %v) = Ask_Again past the bound — ghillie badgers forever", attempts, state)
			}
			// Post: ((Decide'Result = Ask_Again) =
			//        (Outstanding (State) and then not Exhausted (Attempts)))
			// ASKING-AGAIN-IS-EXACTLY-OUTSTANDING-AND-UNEXHAUSTED.
			if (got == AskAgain) != (state != Answered && attempts < 3) {
				t.Errorf("Decide(%d, %v) = %v; Ask_Again must hold exactly when unanswered and under 3 attempts", attempts, state, got)
			}
			// Post: ((Decide'Result = Let_It_Lie) =
			//        (Outstanding (State) and then Exhausted (Attempts)))
			// LETTING-IT-LIE-IS-EXACTLY-OUTSTANDING-AND-EXHAUSTED.
			if (got == LetItLie) != (state != Answered && attempts >= 3) {
				t.Errorf("Decide(%d, %v) = %v; Let_It_Lie must hold exactly when unanswered and at 3 or more attempts", attempts, state, got)
			}
			// Post: (if Outstanding (State) then
			//        (Decide'Result = Ask_Again or else Decide'Result = Let_It_Lie))
			// AN-OUTSTANDING-ITEM-NEVER-FALLS-THROUGH.
			if state != Answered && got != AskAgain && got != LetItLie {
				t.Errorf("Decide(%d, %v) = %v — an outstanding item fell through", attempts, state, got)
			}
			// Post: (if Attempts = 0 and then Outstanding (State)
			//        then Decide'Result = Ask_Again)
			// A-NEVER-ASKED-OUTSTANDING-ITEM-IS-ALWAYS-ASKED — the liveness
			// conjunct that kills the degenerate always-give-up core.
			if attempts == 0 && state != Answered && got != AskAgain {
				t.Errorf("Decide(0, %v) = %v — ghillie gave up before asking once", state, got)
			}
		}
	}
	if want := len(allQuestionStates) * 33; cases != want {
		t.Fatalf("covered %d combinations, want %d", cases, want)
	}
	t.Logf("exhaustive: %d (attempts × state) combinations checked against the Decide postconditions", cases)
}

// TestDecideTable is the hand-derived decision table, read off the Ada
// if/elsif chain, kept separate from the postcondition sweep so a reviewer has
// concrete rows to check by eye.
func TestDecideTable(t *testing.T) {
	tests := []struct {
		name     string
		attempts AttemptCount
		state    QuestionState
		want     AskDecision
	}{
		{"an untouched item is asked", 0, Unasked, AskAgain},
		{"asked once, no answer yet — ask again", 1, Asked, AskAgain},
		{"asked twice, no answer yet — one more", 2, Asked, AskAgain},
		{"asked three times, no answer — let it lie", 3, Asked, LetItLie},
		{"far past the bound — still let it lie", 32, Asked, LetItLie},

		{"interrupted once — still outstanding, ask again", 1, Interrupted, AskAgain},
		{"interrupted at the bound — let it lie, never 'have it'", 3, Interrupted, LetItLie},

		{"answered on the first put", 1, Answered, AlreadyHaveIt},
		{"answered — exhaustion is irrelevant", 32, Answered, AlreadyHaveIt},
		{"an answer that arrived before any ask", 0, Answered, AlreadyHaveIt},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := Decide(tt.attempts, tt.state); got != tt.want {
				t.Errorf("Decide(%d, %v) = %v, want %v", tt.attempts, tt.state, got, tt.want)
			}
		})
	}
}

// TestALetLieItemIsNeverAnswered states the ★ theorem on its own and makes it
// bite the way the Ada does: against the REAL IsGot and MayMoveOn of ledger
// 123 in this package, not against a literal restatement. The core withs the
// ledger precisely so "answered" cannot drift between the two packages, and
// this test is that relationship, executed.
func TestALetLieItemIsNeverAnswered(t *testing.T) {
	for _, state := range allQuestionStates {
		for _, attempts := range allAttemptCounts() {
			if Decide(attempts, state) != LetItLie {
				continue
			}
			if IsGot(state) {
				t.Errorf("Decide(%d, %v) = Let_It_Lie with Is_Got true — a let-lie item was reported as answered", attempts, state)
			}
			if MayMoveOn(state) {
				t.Errorf("Decide(%d, %v) = Let_It_Lie with May_Move_On true — giving up became a licence to move on", attempts, state)
			}
		}
	}
}

// TestAttemptsNeverReviveAskingExhaustive walks the monotonicity operation's
// entire 4356-case domain against its proved postcondition, and pins the
// declaration order it relies on with literal values first.
func TestAttemptsNeverReviveAskingExhaustive(t *testing.T) {
	// The order is load-bearing: Ask_Again < Let_It_Lie < Already_Have_It.
	if AskAgain != 0 || LetItLie != 1 || AlreadyHaveIt != 2 {
		t.Fatalf("AskDecision order is %d/%d/%d, want 0/1/2 — MORE-ATTEMPTS-NEVER-RETURN-TO-ASKING has silently changed meaning",
			AskAgain, LetItLie, AlreadyHaveIt)
	}
	cases := 0
	for _, state := range allQuestionStates {
		for _, fewer := range allAttemptCounts() {
			for _, more := range allAttemptCounts() {
				cases++
				got := AttemptsNeverReviveAsking(state, fewer, more)

				// Post: (if More_Attempts >= Fewer_Attempts then 'Result = True)
				if more >= fewer && !got {
					t.Errorf("AttemptsNeverReviveAsking(%v, %d, %d) = false — more attempts revived asking", state, fewer, more)
				}
				// The function is its own definition: restate it literally.
				if got != (Decide(more, state) >= Decide(fewer, state)) {
					t.Errorf("AttemptsNeverReviveAsking(%v, %d, %d) disagrees with the comparison it is defined as", state, fewer, more)
				}
			}
		}
	}
	if want := len(allQuestionStates) * 33 * 33; cases != want {
		t.Fatalf("covered %d combinations, want %d", cases, want)
	}
	t.Logf("exhaustive: %d (state × fewer × more) combinations checked against the monotonicity postcondition", cases)
}

// TestCalibrationBoundaryTheorem walks the calibration family at the boundary
// the probe established: every bound from 1 to 32 is legal — the key conjuncts
// hold across the full domain — and a bound of ZERO is perverse: it gives up
// before asking once, which is exactly why the probe went RED at 0 and why
// MaxAttempts = 0 is unrepresentable at construction (the guard constants in
// attempt_bound.go make a zero bound a COMPILE ERROR — for compiled-in
// conduct, construction is compilation).
func TestCalibrationBoundaryTheorem(t *testing.T) {
	if MaxAttempts != 3 {
		t.Fatalf("MaxAttempts = %d, want the shipped calibration 3", MaxAttempts)
	}

	// Max_Attempts = 1 up to 32: legal, and the shape holds everywhere.
	for bound := AttemptCount(1); bound <= AttemptCountLast; bound++ {
		for _, state := range allQuestionStates {
			for _, attempts := range allAttemptCounts() {
				got := decideAt(bound, attempts, state)
				if (got == AlreadyHaveIt) != (state == Answered) {
					t.Fatalf("bound %d: decideAt(%d, %v) = %v breaks ALREADY-HAVE-IT-IS-EXACTLY-WHAT-THE-LEDGER-HAS", bound, attempts, state, got)
				}
				if attempts >= bound && got == AskAgain {
					t.Fatalf("bound %d: decideAt(%d, %v) = Ask_Again past the bound", bound, attempts, state)
				}
				if attempts == 0 && state != Answered && got != AskAgain {
					t.Fatalf("bound %d: a never-asked outstanding item was not asked", bound)
				}
				if got == LetItLie && state == Answered {
					t.Fatalf("bound %d: a let-lie item reads as answered", bound)
				}
			}
		}
	}

	// Max_Attempts = 0: A-NEVER-ASKED-ITEM-IS-NEVER-EXHAUSTED is FALSE — the
	// core would let an item lie before asking even once. This is the probe's
	// RED row, reproduced, and the reason zero is refused at build time.
	if !exhaustedAt(0, 0) {
		t.Fatal("exhaustedAt(0, 0) = false — the zero-bound perversity this test documents has vanished; re-read the calibration probe")
	}
	if got := decideAt(0, 0, Unasked); got != LetItLie {
		t.Fatalf("decideAt(bound 0, 0 attempts, Unasked) = %v, want Let_It_Lie — the zero bound no longer demonstrates giving up before asking", got)
	}
}

// TestClampAttempts pins the thin edge between the interview's int counter and
// the subtype, including that saturation at the top can never revive asking.
func TestClampAttempts(t *testing.T) {
	tests := []struct {
		in   int
		want AttemptCount
	}{
		{-1, 0}, {0, 0}, {1, 1}, {3, 3}, {32, 32}, {33, 32}, {1 << 20, 32},
	}
	for _, tt := range tests {
		if got := ClampAttempts(tt.in); got != tt.want {
			t.Errorf("ClampAttempts(%d) = %d, want %d", tt.in, got, tt.want)
		}
	}
	// Saturation is invisible to the decision: above the subtype top the item
	// is exhausted either way, for every state.
	for _, state := range allQuestionStates {
		if Decide(ClampAttempts(1<<20), state) != Decide(32, state) {
			t.Errorf("saturating a huge count changed the decision for %v", state)
		}
	}
}
