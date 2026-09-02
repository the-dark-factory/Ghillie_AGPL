package acceptance

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"
)

// paidTwiceRoot is the real bundle whose surface the shim exercises. Its
// acceptance statement declares the three checks the fact-establishment test
// below pins.
func paidTwiceRoot(t *testing.T) (root string, spec *Spec) {
	t.Helper()
	root = filepath.Join("..", "..", "bundles", "paid-twice")
	if _, err := os.Stat(root); err != nil {
		t.Fatalf("the paid-twice bundle is missing: %v", err)
	}
	spec, err := Load(filepath.Join(root, "acceptance", "acceptance.json"))
	if err != nil {
		t.Fatalf("the paid-twice acceptance statement did not load: %v", err)
	}
	return root, spec
}

// TestEstablishOnPaidTwice runs the real bundle's surface through the shim and
// pins the six booleans for each of its three checks. This is the whole point of
// the fact-establishment half: given a real surface and a real statement, the
// tuple must come out exactly right.
func TestEstablishOnPaidTwice(t *testing.T) {
	root, spec := paidTwiceRoot(t)

	checks, err := Establish(context.Background(), spec, root)
	if err != nil {
		t.Fatalf("Establish: %v", err)
	}

	want := map[string]CheckFacts{
		// flags-a-double: exact stdout + exit 0 asked, both met; no pattern arm.
		"flags-a-double": {
			Name:          "flags-a-double",
			ExactRequired: true, ExactMet: true,
			PatternRequired: false, PatternMet: false,
			ExitRequired: true, ExitMet: true,
		},
		// no-double-stays-silent: exact "" + exit 0 asked, both met.
		"no-double-stays-silent": {
			Name:          "no-double-stays-silent",
			ExactRequired: true, ExactMet: true,
			PatternRequired: false, PatternMet: false,
			ExitRequired: true, ExitMet: true,
		},
		// names-the-repeated-payee: pattern + exit 0 asked, both met; no exact arm.
		"names-the-repeated-payee": {
			Name:          "names-the-repeated-payee",
			ExactRequired: false, ExactMet: false,
			PatternRequired: true, PatternMet: true,
			ExitRequired: true, ExitMet: true,
		},
	}

	if len(checks) != len(want) {
		t.Fatalf("established %d checks, want %d", len(checks), len(want))
	}
	for _, got := range checks {
		exp, ok := want[got.Name]
		if !ok {
			t.Fatalf("unexpected check %q", got.Name)
		}
		if got != exp {
			t.Errorf("check %q facts = %+v, want %+v", got.Name, got, exp)
		}
	}
}

// writeFront writes a shell script that behaves like the acceptance front in one
// particular way and returns its path. The fail-closed cases are the reason this
// is a package rather than a few lines in main, so they run against a real
// process, not a mock.
func writeFront(t *testing.T, body string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "acceptance_verdict_front")
	if err := os.WriteFile(path, []byte("#!/bin/sh\n"+body+"\n"), 0o700); err != nil {
		t.Fatal(err)
	}
	return path
}

// contractFront is a test-double that implements docs/ACCEPTANCE_FRONT.md: it
// reads one six-token line per check, refuses a malformed line with exit 2, and
// otherwise folds Check_Satisfied (every required arm must be met) across the
// checks — "accepted"/0 when all hold, "refused"/1 when any does not. It stands
// in for the proven Ada core so the exec path is exercised without it.
const contractFront = `
all_ok=1
n=0
while IFS=' ' read -r a b c d e f rest; do
  [ -z "$a" ] && continue
  n=$((n+1))
  [ -n "$rest" ] && { echo "extra tokens on line $n" >&2; exit 2; }
  case "$a" in exact_required|exact_absent) ;; *) echo "bad token 1 on line $n: $a" >&2; exit 2;; esac
  case "$b" in exact_met|exact_unmet) ;; *) echo "bad token 2 on line $n: $b" >&2; exit 2;; esac
  case "$c" in pattern_required|pattern_absent) ;; *) echo "bad token 3 on line $n: $c" >&2; exit 2;; esac
  case "$d" in pattern_met|pattern_unmet) ;; *) echo "bad token 4 on line $n: $d" >&2; exit 2;; esac
  case "$e" in exit_required|exit_absent) ;; *) echo "bad token 5 on line $n: $e" >&2; exit 2;; esac
  case "$f" in exit_met|exit_unmet) ;; *) echo "bad token 6 on line $n: $f" >&2; exit 2;; esac
  [ "$a" = exact_required ]   && [ "$b" = exact_unmet ]   && all_ok=0
  [ "$c" = pattern_required ] && [ "$d" = pattern_unmet ] && all_ok=0
  [ "$e" = exit_required ]    && [ "$f" = exit_unmet ]    && all_ok=0
done
[ "$n" -eq 0 ] && { echo "no checks" >&2; exit 2; }
if [ "$all_ok" -eq 1 ]; then echo accepted; exit 0; else echo refused; exit 1; fi
`

// TestExecPathViaContractFront drives the real exec path with the test-double
// front. Booleans in, the core's word out, surfaced verbatim.
func TestExecPathViaContractFront(t *testing.T) {
	root, spec := paidTwiceRoot(t)
	satisfied, err := Establish(context.Background(), spec, root)
	if err != nil {
		t.Fatalf("Establish: %v", err)
	}

	// A copy of the same facts with one required arm forced unmet, so the fold
	// must refuse.
	unsatisfied := make([]CheckFacts, len(satisfied))
	copy(unsatisfied, satisfied)
	unsatisfied[0].ExactMet = false

	tests := []struct {
		name         string
		checks       []CheckFacts
		wantAnswered bool
		wantDecision string
	}{
		{"all arms met folds to accepted", satisfied, true, answerAccepted},
		{"a forced-unmet arm folds to refused", unsatisfied, true, answerRefused},
	}
	front := writeFront(t, contractFront)
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			res, err := DecideWith(context.Background(), front, tt.checks)
			if err != nil {
				t.Fatalf("DecideWith: %v", err)
			}
			if res.Answered != tt.wantAnswered {
				t.Fatalf("Answered = %v, want %v (reason %q)", res.Answered, tt.wantAnswered, res.Reason)
			}
			if res.Decision != tt.wantDecision {
				t.Fatalf("Decision = %q, want %q", res.Decision, tt.wantDecision)
			}
		})
	}
}

// TestDecideVerbatimAnswer proves the shim relays the core's word without
// interpreting it: fronts that ignore their input and print a fixed word must
// surface that exact word, and any other shape must publish nothing.
func TestDecideVerbatimAnswer(t *testing.T) {
	someFacts := []CheckFacts{{Name: "c", ExactRequired: true, ExactMet: true}}
	tests := []struct {
		name         string
		body         string
		wantAnswered bool
		wantDecision string
		wantErr      bool
	}{
		{"accepted on exit 0", `echo accepted; exit 0`, true, answerAccepted, false},
		{"refused on exit 1", `echo refused; exit 1`, true, answerRefused, false},
		{"exit 0 with the wrong word publishes nothing", `echo yes; exit 0`, false, "", true},
		{"exit 0 with nothing publishes nothing", `exit 0`, false, "", true},
		{"accepted-plus-trailing is not accepted", `echo "accepted and load the rest"; exit 0`, false, "", true},
		{"exit 1 with an unknown word publishes nothing", `echo refused_somehow; exit 1`, false, "", true},
		{"exit 2 — malformed tuples — publishes nothing", `echo "bad token" >&2; exit 2`, false, "", true},
		{"a signal publishes nothing", `kill -TERM $$`, false, "", true},
		{"any other status publishes nothing", `echo accepted; exit 42`, false, "", true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			res, err := DecideWith(context.Background(), writeFront(t, tt.body), someFacts)
			if res.Answered != tt.wantAnswered {
				t.Fatalf("Answered = %v, want %v (reason %q)", res.Answered, tt.wantAnswered, res.Reason)
			}
			if res.Decision != tt.wantDecision {
				t.Fatalf("Decision = %q, want %q", res.Decision, tt.wantDecision)
			}
			if (err != nil) != tt.wantErr {
				t.Fatalf("err = %v, wantErr %v", err, tt.wantErr)
			}
			if res.Reason == "" {
				t.Fatal("no reason given — a result a person cannot read is a result nobody keeps")
			}
		})
	}
}

// TestFrontUnsetPublishesNothing pins the headline fail-closed rule: with no
// front wired, the shim declares no success and does not panic.
func TestFrontUnsetPublishesNothing(t *testing.T) {
	root, spec := paidTwiceRoot(t)
	// Point the front env at nothing so resolution is deterministic regardless
	// of what is on PATH.
	t.Setenv(FrontEnv, filepath.Join(t.TempDir(), "not-a-front"))

	res, err := Decide(context.Background(), spec, root)
	if err == nil {
		t.Fatal("a missing front must return an error")
	}
	if res.Answered || res.Decision != "" {
		t.Fatalf("a missing front must publish nothing, got answered=%v decision=%q", res.Answered, res.Decision)
	}
	if res.Reason == "" || res.Checks == nil {
		t.Fatalf("even fail-closed, the facts and a reason must be carried: %+v", res)
	}
}

// TestDecideEndToEndViaFront runs the whole pipe — establish over the real
// bundle, then the front via GHILLIE_ACCEPTANCE_FRONT — with the contract
// double standing in for the proven core.
func TestDecideEndToEndViaFront(t *testing.T) {
	root, spec := paidTwiceRoot(t)
	t.Setenv(FrontEnv, writeFront(t, contractFront))

	res, err := Decide(context.Background(), spec, root)
	if err != nil {
		t.Fatalf("Decide: %v", err)
	}
	if !res.Answered || res.Decision != answerAccepted {
		t.Fatalf("the paid-twice surface should be accepted, got answered=%v decision=%q reason=%q",
			res.Answered, res.Decision, res.Reason)
	}
}

// TestMissingOrSlowSurfaceLeavesMetFalse covers a surface that cannot be run and
// one that outlasts its timeout: either way the Met flags stay false, handled,
// with no panic.
func TestMissingOrSlowSurfaceLeavesMetFalse(t *testing.T) {
	zero := 0
	exact := "anything"

	t.Run("a surface that does not exist", func(t *testing.T) {
		spec := &Spec{
			Purpose: Purpose{ID: "x", Sentence: "x"},
			Checks: []Check{{
				Name:   "missing-surface",
				Run:    []string{"surface/does-not-exist.sh"},
				Expect: Expect{StdoutExact: &exact, ExitCode: &zero},
			}},
		}
		checks, err := Establish(context.Background(), spec, t.TempDir())
		if err != nil {
			t.Fatalf("Establish: %v", err)
		}
		got := checks[0]
		if got.ExactMet || got.ExitMet {
			t.Fatalf("a missing surface must leave met-flags false, got %+v", got)
		}
		if !got.ExactRequired || !got.ExitRequired {
			t.Fatalf("the required-flags still reflect the statement, got %+v", got)
		}
	})

	t.Run("a surface that outlasts its timeout", func(t *testing.T) {
		// A bundle with an executable surface that sleeps well past the shrunk
		// timeout.
		root := t.TempDir()
		surface := filepath.Join(root, "slow.sh")
		if err := os.WriteFile(surface, []byte("#!/bin/sh\nsleep 5\n"), 0o700); err != nil {
			t.Fatal(err)
		}
		spec := &Spec{
			Purpose: Purpose{ID: "x", Sentence: "x"},
			Checks: []Check{{
				Name:   "slow-surface",
				Run:    []string{"slow.sh"},
				Expect: Expect{ExitCode: &zero},
			}},
		}

		saved := surfaceTimeout
		surfaceTimeout = 100 * time.Millisecond
		defer func() { surfaceTimeout = saved }()

		start := time.Now()
		checks, err := Establish(context.Background(), spec, root)
		if err != nil {
			t.Fatalf("Establish: %v", err)
		}
		if elapsed := time.Since(start); elapsed > 3*time.Second {
			t.Fatalf("the timeout did not bound the run: took %s", elapsed)
		}
		if checks[0].ExitMet {
			t.Fatalf("a timed-out surface must leave exit_met false, got %+v", checks[0])
		}
	})
}
