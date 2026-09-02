package admit

import (
	"bufio"
	"context"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
)

// stubFront writes a shell script that behaves like a front binary in one
// particular way, and returns its path. The fail-closed cases below are the
// whole reason this package exists as a package rather than as four lines in
// main.go, so they are tested against a real process, not a mock.
func stubFront(t *testing.T, body string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "extension_admission_front")
	script := "#!/bin/sh\n" + body + "\n"
	if err := os.WriteFile(path, []byte(script), 0o700); err != nil {
		t.Fatal(err)
	}
	return path
}

func TestDecideFailsClosed(t *testing.T) {
	tests := []struct {
		name    string
		body    string
		want    Verdict
		wantErr bool
	}{
		{
			name: "the one admitting shape",
			body: `echo admit; exit 0`,
			want: Admit,
		},
		{
			name: "a named refusal is carried through as itself",
			body: `echo refuse_integrity_mismatch; exit 1`,
			want: RefuseIntegrityMismatch,
		},
		{
			name: "every refusal word round-trips",
			body: `echo refuse_uncontained_unproven; exit 1`,
			want: RefuseUncontainedUnproven,
		},
		{
			name:    "exit 0 with the WRONG word is not an admit",
			body:    `echo yes; exit 0`,
			want:    RefuseUndecided,
			wantErr: true,
		},
		{
			name:    "exit 0 with NOTHING on stdout is not an admit",
			body:    `exit 0`,
			want:    RefuseUndecided,
			wantErr: true,
		},
		{
			name:    "exit 0 saying admit twice is not an admit",
			body:    `echo admit; echo admit; exit 0`,
			want:    RefuseUndecided,
			wantErr: true,
		},
		{
			name:    "exit 0 with admit plus trailing rubbish is not an admit",
			body:    `echo "admit and also load everything else"; exit 0`,
			want:    RefuseUndecided,
			wantErr: true,
		},
		{
			name: "exit 1 with a word we do not know is still a refusal",
			body: `echo refuse_something_new; exit 1`,
			want: RefuseUndecided,
		},
		{
			name:    "exit 2 — malformed facts — is a refusal",
			body:    `echo "Invalid token in position 4" >&2; exit 2`,
			want:    RefuseUndecided,
			wantErr: true,
		},
		{
			name:    "exit 2 that ALSO prints admit on stdout is a refusal",
			body:    `echo admit; exit 2`,
			want:    RefuseUndecided,
			wantErr: true,
		},
		{
			name:    "a signal is a refusal",
			body:    `kill -TERM $$`,
			want:    RefuseUndecided,
			wantErr: true,
		},
		{
			name:    "any other exit status is a refusal",
			body:    `echo admit; exit 42`,
			want:    RefuseUndecided,
			wantErr: true,
		},
	}

	facts := Facts{floor: FloorNone}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, reason, err := DecideWith(context.Background(), stubFront(t, tt.body), facts)
			if got != tt.want {
				t.Fatalf("verdict = %v, want %v (reason %q)", got, tt.want, reason)
			}
			if (err != nil) != tt.wantErr {
				t.Fatalf("err = %v, wantErr %v", err, tt.wantErr)
			}
			if got != Admit && got.Admitted() {
				t.Fatal("a non-admit verdict reported Admitted() true")
			}
			if reason == "" {
				t.Fatal("no reason given — a verdict a person cannot read is a verdict nobody keeps")
			}
		})
	}
}

func TestDecideRefusesAMissingFront(t *testing.T) {
	got, reason, err := DecideWith(context.Background(), filepath.Join(t.TempDir(), "nothing-here"), Facts{})
	if got.Admitted() || err == nil {
		t.Fatalf("a missing decider must refuse, got %v err %v", got, err)
	}
	if !strings.Contains(reason, "did not answer") {
		t.Fatalf("reason should say the decider did not answer: %q", reason)
	}
}

func TestDecideRefusesASlowFront(t *testing.T) {
	// A front reads no file and touches no network; one that hangs is not
	// the binary we think it is, and waiting for it is not an option a gate
	// gets to take.
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	got, _, err := DecideWith(ctx, stubFront(t, `sleep 30; echo admit`), Facts{})
	if got.Admitted() || err == nil {
		t.Fatalf("a cancelled context must refuse, got %v err %v", got, err)
	}
}

func TestEveryVerdictHasAWordAndARemedy(t *testing.T) {
	all := []Verdict{
		Admit, RefuseOperatorAbsent, RefuseOriginRevoked, RefuseOriginUnattested,
		RefuseIntegrityMismatch, RefuseIntegrityUnchecked, RefuseProofAbsent,
		RefuseProofNotReproved, RefuseUncontainedUnproven, RefuseUndecided,
	}
	seen := map[string]bool{}
	for _, v := range all {
		w := v.String()
		if w == "" || seen[w] {
			t.Fatalf("verdict %d has an empty or duplicated word %q", v, w)
		}
		seen[w] = true
		if v.Remedy() == "" || strings.HasPrefix(v.Remedy(), "unknown") {
			t.Fatalf("verdict %s has no remedy — a refusal nobody can act on is a refusal nobody keeps", w)
		}
		if v != Admit && v.Admitted() {
			t.Fatalf("%s reported Admitted() true", w)
		}
	}
	// A verdict this package has never heard of must read as a refusal.
	unknown := Verdict(99)
	if unknown.Admitted() || unknown.String() != "refuse_undecided" {
		t.Fatal("an unknown verdict must fall to the refusing side")
	}
}

func TestFactTokensAreTheFrontsTokens(t *testing.T) {
	tests := []struct {
		name string
		got  string
		want string
	}{
		{"operator requested", OperatorRequested.String(), "requested"},
		{"operator absent", OperatorAbsent.String(), "absent"},
		{"origin attested", OriginAttested.String(), "attested"},
		{"origin unattested", OriginUnattested.String(), "unattested"},
		{"origin revoked", OriginRevoked.String(), "revoked"},
		{"integrity verified", IntegrityVerified.String(), "verified"},
		{"integrity mismatch", IntegrityMismatch.String(), "mismatch"},
		{"integrity unchecked", IntegrityUnchecked.String(), "unchecked"},
		{"proof reproved", ProofReprovedHere.String(), "reproved"},
		{"proof carried", ProofCarried.String(), "carried"},
		{"proof absent", ProofAbsent.String(), "absent"},
		// The floor tokens are PREFIXED on purpose: transposing arguments 4
		// and 5 must be invalid in both positions, not a silently different
		// question.
		{"floor reproved", FloorReprovedHere.String(), "floor_reproved"},
		{"floor carried", FloorCarried.String(), "floor_carried"},
		{"floor none", FloorNone.String(), "floor_none"},
		{"contained", Contained.String(), "contained"},
		{"uncontained", Uncontained.String(), "uncontained"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if tt.got != tt.want {
				t.Fatalf("token = %q, want %q", tt.got, tt.want)
			}
		})
	}
	// An enumeration value this package has not been taught about must
	// produce a token the front REJECTS (exit 2 → refusal), never one it
	// accepts as a different question.
	if Floor(9).String() != "unmapped" {
		t.Fatal("an unmapped enumeration must yield a token the front refuses")
	}
}

// TestTupleAgreesWithTheShippedTable runs every row of the mirrored 324-row
// truth table through this package's own token mapping and, where a real
// front is available, through the front itself. It is the same table the
// core's own checker uses, committed here so the mirror cannot drift from
// the core without a test going red.
func TestTupleAgreesWithTheShippedTable(t *testing.T) {
	raw, err := os.Open(filepath.Join("testdata", "TABLE.tsv"))
	if err != nil {
		t.Fatalf("the mirrored table is missing: %v", err)
	}
	defer raw.Close()

	front, frontErr := FindFront()
	if frontErr != nil {
		t.Logf("no front binary here (%v) — checking the table's shape only", frontErr)
	}

	operators := map[string]Operator{"requested": OperatorRequested, "absent": OperatorAbsent}
	origins := map[string]Origin{"attested": OriginAttested, "unattested": OriginUnattested, "revoked": OriginRevoked}
	integrities := map[string]Integrity{"verified": IntegrityVerified, "mismatch": IntegrityMismatch, "unchecked": IntegrityUnchecked}
	proofs := map[string]Proof{"reproved": ProofReprovedHere, "carried": ProofCarried, "absent": ProofAbsent}
	floors := map[string]Floor{"floor_reproved": FloorReprovedHere, "floor_carried": FloorCarried, "floor_none": FloorNone}
	containments := map[string]Containment{"contained": Contained, "uncontained": Uncontained}

	rows := 0
	sc := bufio.NewScanner(raw)
	for sc.Scan() {
		line := sc.Text()
		if strings.TrimSpace(line) == "" || strings.HasPrefix(strings.TrimSpace(line), "#") {
			continue
		}
		cols := strings.Split(line, "\t")
		if len(cols) != 3 {
			t.Fatalf("row %q is not argv<TAB>verdict<TAB>exit", line)
		}
		tok := strings.Fields(cols[0])
		if len(tok) != 6 {
			t.Fatalf("row %q does not name six facts", line)
		}
		wantWord := strings.TrimSpace(cols[1])
		wantExit, convErr := strconv.Atoi(strings.TrimSpace(cols[2]))
		if convErr != nil {
			t.Fatalf("row %q has an unreadable exit status", line)
		}

		f := Facts{
			operator:    operators[tok[0]],
			origin:      origins[tok[1]],
			integrity:   integrities[tok[2]],
			proof:       proofs[tok[3]],
			floor:       floors[tok[4]],
			containment: containments[tok[5]],
		}
		// This package's own rendering of the tuple must be byte-identical
		// to the argv the table names, or the mirror is asking a different
		// question from the one the table answers.
		if got := strings.Join(f.Args(), " "); got != strings.Join(tok, " ") {
			t.Fatalf("tuple rendered as %q, table says %q", got, strings.Join(tok, " "))
		}

		if frontErr == nil {
			verdict, _, _ := DecideWith(context.Background(), front, f)
			if verdict.String() != wantWord {
				t.Fatalf("row %q: front said %s, table says %s", cols[0], verdict, wantWord)
			}
			if verdict.Admitted() != (wantExit == 0) {
				t.Fatalf("row %q: Admitted() = %v, table exit %d", cols[0], verdict.Admitted(), wantExit)
			}
		}
		rows++
	}
	if err := sc.Err(); err != nil {
		t.Fatal(err)
	}
	if rows != 324 {
		t.Fatalf("the mirrored table has %d rows, want all 324 — a sampled table is not a truth table", rows)
	}
	if frontErr == nil {
		t.Logf("all %d rows checked against %s", rows, front)
	}
}
