package gate

import (
	"bufio"
	"os"
	"strconv"
	"strings"
	"testing"
)

// TestGateExhaustiveGoldenLedger112 replays the COMPLETE decision table of the
// proven core Facade_Command_Pkg (ada-factory ledger 112) through the Go mirror
// and fails on any disagreement.
//
// This is the differential that the mirror comment in gate.go asserts but did
// not previously check. MayCommand claims to be "byte-for-byte the function
// that mirrors ledger 112"; before this test, nothing executed the Ada to find
// out. Gobra independently PROVES the Go satisfies the same postconditions
// (verification/gobra), which is a different guarantee: proof says each side
// meets the contract, this says the two artefacts actually agree case-for-case.
//
// The vectors are exhaustive, not sampled — 6 x 6 x 3 x 2 = 216 is the whole
// input domain of May_Command — so passing here means the mirror matches the
// core everywhere, not merely on remembered cases.
//
// Provenance and regeneration: testdata/README.md.
func TestGateExhaustiveGoldenLedger112(t *testing.T) {
	const path = "testdata/ledger112_vectors.txt"

	f, err := os.Open(path)
	if err != nil {
		t.Fatalf("open golden vectors: %v", err)
	}
	defer f.Close()

	var checked, allowed int
	sc := bufio.NewScanner(f)
	for line := 1; sc.Scan(); line++ {
		text := strings.TrimSpace(sc.Text())
		if text == "" {
			continue
		}
		fields := strings.Fields(text)
		if len(fields) != 5 {
			t.Fatalf("%s:%d: want 5 fields, got %d (%q)", path, line, len(fields), text)
		}
		nums := make([]int, 5)
		for i, raw := range fields {
			n, convErr := strconv.Atoi(raw)
			if convErr != nil {
				t.Fatalf("%s:%d: field %d not an integer: %v", path, line, i, convErr)
			}
			nums[i] = n
		}
		c, ceiling, consent := Command(nums[0]), Command(nums[1]), Consent(nums[2])
		authentic, want := nums[3] == 1, nums[4] == 1

		if got := MayCommand(c, ceiling, consent, authentic); got != want {
			t.Errorf("%s:%d: MayCommand(c=%d ceiling=%d k=%d authentic=%v) = %v, proven core says %v",
				path, line, nums[0], nums[1], nums[2], authentic, got, want)
		}
		checked++
		if want {
			allowed++
		}
	}
	if err := sc.Err(); err != nil {
		t.Fatalf("read golden vectors: %v", err)
	}

	// The table must be the WHOLE domain. A truncated file would pass every
	// comparison above while checking almost nothing, so assert the count.
	const wantCases = 216
	if checked != wantCases {
		t.Fatalf("golden table is not exhaustive: replayed %d cases, want %d", checked, wantCases)
	}

	// Guard against a vacuous table: if the core refused (or allowed)
	// everything, agreement would be meaningless. 48 allows was measured from
	// the proven core on 2026-08-25.
	const wantAllowed = 48
	if allowed != wantAllowed {
		t.Fatalf("golden table allow-count changed: %d, want %d — regenerate and review, do not edit", allowed, wantAllowed)
	}
}
