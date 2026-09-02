package conduct

// This file mirrors Attempt_Bound_Pkg — when ghillie stops asking a given item.
//
// PROVEN AND ADMITTED — LEDGER 128. The core was proven 2026-07-30 (gnatprove
// level 2: 14 checks across 2 units, 0 unproved, 0 justified, 0 pragma Assume;
// solver grade 3/3 — CVC5, Z3 and Alt-Ergo EACH ALONE discharge all 7
// functional contracts; falsification battery 14/14 RED at `high:` with 2 GREEN
// controls) and admitted the same day via the off-seat chain: the admission machine
// independent re-proof + bypass scan, ledger entry 128, 2026-07-30T18:49:36Z,
// dep closure question_ledger_pkg.ads shipped.
//
// Source .ads sha256
// f1e69d97993b73d5798019d9af625cecbe0454ab9f3799bd49e37080dd2f447d, captured
// 2026-07-30. Proof evidence, on this machine:
//
//	<owner's vault>/cores/attempt_bound_pkg.ads                (factory output, unedited)
//	<owner's vault>/cores/attempt_bound_PROVENANCE.md          (full provenance)
//	~/ObVault/ghillie-voice/cores/attempt_bound_gnatprove-summary.txt  (proof summary)
//
// ★ IT WITHS THE LEDGER, IT DOES NOT FORK IT. The Ada takes
// Question_Ledger_Pkg.Question_State_Type and asks the ledger's OWN Is_Got and
// May_Move_On what the ledger holds, so the headline theorem — a let-lie item
// is NEVER reported as answered — bites against the real deployed notion of
// "answered", not a copy that could drift. This Go keeps exactly that
// relationship: it uses QuestionState, IsGot and MayMoveOn from
// question_ledger.go in this package and declares no parallel vocabulary.
//
// The Ada, verbatim:
//
//	subtype Attempt_Count_Type is Natural range 0 .. 32;
//
//	type Ask_Decision_Type is (Ask_Again, Let_It_Lie, Already_Have_It);
//
//	Max_Attempts : constant Attempt_Count_Type := 3;
//
//	function Exhausted (Attempts : Attempt_Count_Type) return Boolean is
//	  (Attempts >= Max_Attempts)
//
//	function Outstanding (State : Question_Ledger_Pkg.Question_State_Type) return Boolean is
//	  (not Question_Ledger_Pkg.Is_Got (State))
//
//	function Decide
//	  (Attempts : Attempt_Count_Type;
//	   State    : Question_Ledger_Pkg.Question_State_Type)
//	  return Ask_Decision_Type is
//	  (if not Outstanding (State => State) then
//	     Already_Have_It
//	   elsif Exhausted (Attempts => Attempts) then
//	     Let_It_Lie
//	   else
//	     Ask_Again)
//
//	function Attempts_Never_Revive_Asking
//	  (State          : Question_Ledger_Pkg.Question_State_Type;
//	   Fewer_Attempts : Attempt_Count_Type;
//	   More_Attempts  : Attempt_Count_Type)
//	  return Boolean is
//	  (Decide (Attempts => More_Attempts, State => State) >= Decide (Attempts => Fewer_Attempts, State => State))
//
// Theorems (the 14 named conjuncts): EXHAUSTION-IS-EXACTLY-THE-BOUND ·
// A-NEVER-ASKED-ITEM-IS-NEVER-EXHAUSTED ·
// OUTSTANDING-IS-EXACTLY-WHAT-THE-LEDGER-LACKS ·
// AN-INTERRUPTED-ITEM-IS-STILL-OUTSTANDING ·
// ALREADY-HAVE-IT-IS-EXACTLY-WHAT-THE-LEDGER-HAS ·
// ★ A-LET-LIE-ITEM-IS-NEVER-ANSWERED ·
// ★ A-LET-LIE-ITEM-NEVER-LICENCES-MOVING-ON ·
// AN-INTERRUPTED-ITEM-IS-NEVER-ALREADY-HAVE-IT · ★ ATTEMPTS-ARE-BOUNDED ·
// ASKING-AGAIN-IS-EXACTLY-OUTSTANDING-AND-UNEXHAUSTED ·
// LETTING-IT-LIE-IS-EXACTLY-OUTSTANDING-AND-EXHAUSTED ·
// AN-OUTSTANDING-ITEM-NEVER-FALLS-THROUGH ·
// A-NEVER-ASKED-OUTSTANDING-ITEM-IS-ALWAYS-ASKED ·
// MORE-ATTEMPTS-NEVER-RETURN-TO-ASKING.
//
// CALIBRATION. Max_Attempts = 3 is a calibrated default, not a law of nature —
// an original ask, a rephrase and a final confirm. The proof certifies the
// SHAPE of the decision for ANY bound of one or greater (calibration probe:
// GREEN at 1, 2, 3, 10 and 32; RED at 0, where the core would give up before
// asking even once). Only the number three is a judgement call, and it is
// COMPILED IN below like all conduct: no brief field can reach it.
//
// SCOPE BOUNDARY, restated from the .ads: this core decides ONE thing —
// whether ghillie asks this item again RIGHT NOW. Necessity is the FACTORY's
// judgement; letting an item lie is a decision about conversation, not about
// requirements. It reads no transcript, holds no state between calls, writes
// nothing back to the ledger, and its guarantees are conditional on the thin
// edge supplying an honest attempt count and an honest ledger state.
//
// HONEST LABEL: unproven shim, cross-checked against the proven core by
// exhaustive table test. Where this Go and the Ada disagree, THE ADA IS RIGHT.

// AttemptCount is Attempt_Bound_Pkg.Attempt_Count_Type: Natural range 0 .. 32.
type AttemptCount uint8

// AttemptCountLast is the subtype's upper bound ('Last in the Ada).
const AttemptCountLast AttemptCount = 32

// AskDecision is Attempt_Bound_Pkg.Ask_Decision_Type, IN THE SAME ORDER.
//
// ⚠ THE ORDER IS LOAD-BEARING, exactly as the Ada's Design Decision One says:
// the monotonicity theorem is a comparison on this order, and Ask_Again is
// declared FIRST so that "the decision never decreases as the attempt count
// rises" means exactly "the machine never returns to asking". Reorder these
// constants and AttemptsNeverReviveAsking silently changes meaning.
type AskDecision uint8

// The three decisions, in declaration order.
const (
	AskAgain AskDecision = iota
	LetItLie
	AlreadyHaveIt
)

// MaxAttempts is Attempt_Bound_Pkg.Max_Attempts: the compiled-in bound on how
// many times ghillie puts one item. Conduct is compiled in, never delivered —
// internal/brief refuses a brief that tries to carry this number.
const MaxAttempts AttemptCount = 3

// The calibration floor, enforced at construction — and the construction of
// compiled-in conduct is COMPILATION. The probe proved the theorems for any
// bound of one or greater and showed them provably FALSE at zero (a zero bound
// gives up before asking once), so a zero MaxAttempts must be unrepresentable:
// if it were ever edited to 0, the constant expression below underflows and
// the build fails. The second guard pins the bound inside the subtype.
const (
	_ = uint8(MaxAttempts - 1)
	_ = uint8(AttemptCountLast - MaxAttempts)
)

// exhaustedAt, outstandingAt and decideAt are the CALIBRATION FAMILY: the
// core's expression functions over an arbitrary bound, exactly as the
// calibration probe re-proved them at bounds 1, 2, 3, 10 and 32. The exported
// functions below pin the shipped calibration (MaxAttempts) and contain no
// second copy of the logic, so shipped decision and tested family cannot
// drift. The tests walk this family at the boundary — bound 1 legal, bound 0
// demonstrably perverse — which is why it exists as a named thing.
func exhaustedAt(bound, attempts AttemptCount) bool { return attempts >= bound }

func decideAt(bound, attempts AttemptCount, state QuestionState) AskDecision {
	switch {
	case !Outstanding(state):
		return AlreadyHaveIt
	case exhaustedAt(bound, attempts):
		return LetItLie
	default:
		return AskAgain
	}
}

// Exhausted mirrors Attempt_Bound_Pkg.Exhausted: whether this item has been
// asked as many times as it is going to be.
func Exhausted(attempts AttemptCount) bool { return exhaustedAt(MaxAttempts, attempts) }

// Outstanding mirrors Attempt_Bound_Pkg.Outstanding: whether the ledger is
// still without an answer for this item. It is NOT a new notion of "answered"
// — it is the negation of the ledger's own IsGot, so Interrupted is
// outstanding and a mere mention is outstanding, by ledger 123's theorems.
func Outstanding(state QuestionState) bool { return !IsGot(state) }

// Decide mirrors Attempt_Bound_Pkg.Decide. The arms are in the Ada's own
// order, because the Ada is an if/elsif chain and the order is part of the
// meaning: what the ledger already holds outranks exhaustion, and exhaustion
// outranks asking.
//
// The theorem this core exists for: if Decide returns LetItLie, then
// IsGot(state) is false and MayMoveOn(state) is false — an item ghillie has
// let lie is never, under any input, reported as answered, and giving up is
// never a licence to move on. Letting an item lie is therefore REPORTED,
// never silent; whether the item is still needed is the factory's judgement.
func Decide(attempts AttemptCount, state QuestionState) AskDecision {
	return decideAt(MaxAttempts, attempts, state)
}

// AttemptsNeverReviveAsking mirrors Attempt_Bound_Pkg.Attempts_Never_Revive_Asking
// — the monotonicity theorem, stated as an operation: with more attempts on
// the clock the decision never moves back towards asking.
func AttemptsNeverReviveAsking(state QuestionState, fewerAttempts, moreAttempts AttemptCount) bool {
	return Decide(moreAttempts, state) >= Decide(fewerAttempts, state)
}

// ClampAttempts is the THIN EDGE between the interview's ordinary int counter
// and the core's subtype. The Ada would refuse an out-of-range count with a
// Constraint_Error at the call; Go has no subtypes, so the edge pins the range
// here instead:
//
//   - below zero cannot arise from the ledger's own counter (it only
//     increments from zero) and maps to zero;
//   - above AttemptCountLast saturates at the subtype top, which is at or
//     above MaxAttempts by the guard constants, so by
//     MORE-ATTEMPTS-NEVER-RETURN-TO-ASKING saturation can never turn an
//     exhausted item back into one that gets asked.
func ClampAttempts(n int) AttemptCount {
	switch {
	case n < 0:
		return 0
	case n > int(AttemptCountLast):
		return AttemptCountLast
	default:
		return AttemptCount(n)
	}
}

// String renders an ask decision as the name used in the Ada enumeration, for
// the reports a human reads and the factory scores.
func (d AskDecision) String() string {
	switch d {
	case AskAgain:
		return "Ask_Again"
	case LetItLie:
		return "Let_It_Lie"
	case AlreadyHaveIt:
		return "Already_Have_It"
	default:
		return "UNKNOWN_ASK_DECISION"
	}
}
