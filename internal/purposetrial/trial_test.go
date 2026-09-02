package purposetrial

import (
	"context"
	"encoding/json"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
	"time"
)

// stubModel is an injected Model that returns canned responses in order, or a
// fixed error on every call. It lets the whole loop run — sandbox and all —
// without a real ollama.
type stubModel struct {
	responses []string
	err       error
	calls     int
}

func (m *stubModel) Complete(_ context.Context, _ string) (string, error) {
	if m.err != nil {
		return "", m.err
	}
	r := ""
	if m.calls < len(m.responses) {
		r = m.responses[m.calls]
	}
	m.calls++
	return r, nil
}

// copyPaidTwice copies the real paid-twice bundle into a temp dir so a trial can
// write its transcript without dirtying the checked-in tree, and returns the
// copy's root. Executable bits are preserved so surface/run.sh still runs.
func copyPaidTwice(t *testing.T) (root string) {
	t.Helper()
	src := filepath.Join("..", "..", "bundles", "paid-twice")
	if _, err := os.Stat(src); err != nil {
		t.Fatalf("the paid-twice bundle is missing: %v", err)
	}
	root = t.TempDir()
	err := filepath.WalkDir(src, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(src, path)
		if err != nil {
			return err
		}
		dst := filepath.Join(root, rel)
		if d.IsDir() {
			return os.MkdirAll(dst, 0o755)
		}
		info, err := d.Info()
		if err != nil {
			return err
		}
		data, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		return os.WriteFile(dst, data, info.Mode().Perm())
	})
	if err != nil {
		t.Fatalf("copying the paid-twice bundle: %v", err)
	}
	return root
}

// fixClock pins nowFunc for the duration of a test so produced_at — and thus the
// content hash — is deterministic.
func fixClock(t *testing.T) {
	t.Helper()
	saved := nowFunc
	nowFunc = func() time.Time { return time.Date(2026, 9, 1, 12, 0, 0, 0, time.UTC) }
	t.Cleanup(func() { nowFunc = saved })
}

// A proposal that makes the paid-twice surface flag a duplicate, so the real
// stdout is meaningful and assertable.
const proposeDouble = `Here are three cases:
[
  {"input": "date,payee,amount\n2026-08-01,Roofer Ltd,450.00\n2026-08-03,Roofer Ltd,450.00\n", "args": []}
]`

const judgeServed = `{"served": true, "reasoning": "It flagged the duplicate payment, matching the stated purpose."}`

// TestRunTrialOnPaidTwice is the headline test: with a stub model, the loop
// runs, the sandbox executes the REAL paid-twice surface on the stub's proposed
// input, and a complete TrialRecord comes out carrying the stub's judgement, the
// real output, and a 64-hex content hash.
func TestRunTrialOnPaidTwice(t *testing.T) {
	fixClock(t)
	root := copyPaidTwice(t)
	model := &stubModel{responses: []string{proposeDouble, judgeServed}}

	record, path, err := RunTrial(context.Background(), model, root)
	if err != nil {
		t.Fatalf("RunTrial: %v", err)
	}

	if record.Status != StatusComplete {
		t.Fatalf("status = %q, want %q (note %q)", record.Status, StatusComplete, record.Note)
	}
	if record.ModelID != OllamaModelID {
		t.Errorf("model_id = %q, want %q", record.ModelID, OllamaModelID)
	}
	if record.Purpose.ID != "flag-duplicate-payments" {
		t.Errorf("purpose id = %q, want flag-duplicate-payments", record.Purpose.ID)
	}
	if len(record.Trials) != 1 {
		t.Fatalf("got %d trials, want 1", len(record.Trials))
	}

	tr := record.Trials[0]
	if tr.Exit != 0 {
		t.Errorf("trial exit = %d, want 0", tr.Exit)
	}
	if !strings.Contains(tr.Stdout, "possible double: Roofer Ltd") {
		t.Errorf("trial stdout did not carry the real surface output: %q", tr.Stdout)
	}

	// The model's judgement is transcribed verbatim, not decided here.
	if !record.Judgement.Served {
		t.Error("the model's served flag was not transcribed")
	}
	if record.Judgement.Raw != judgeServed {
		t.Errorf("judgement raw = %q, want the model's reply verbatim %q", record.Judgement.Raw, judgeServed)
	}
	if record.Judgement.Reasoning == "" {
		t.Error("judgement reasoning was not transcribed")
	}

	// The content hash is present and well-shaped.
	if !regexp.MustCompile(`^[0-9a-f]{64}$`).MatchString(record.ContentSHA256) {
		t.Errorf("content_sha256 = %q, want 64 hex chars", record.ContentSHA256)
	}
	if record.Signature != "" {
		t.Errorf("signature should be empty in this increment, got %q", record.Signature)
	}

	// The transcript was written to <bundleRoot>/trial/trial-record.json and
	// reloads as the same record.
	if want := filepath.Join(root, "trial", "trial-record.json"); path != want {
		t.Errorf("path = %q, want %q", path, want)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("reading back the transcript: %v", err)
	}
	var reloaded TrialRecord
	if err := json.Unmarshal(data, &reloaded); err != nil {
		t.Fatalf("the written transcript is not valid JSON: %v", err)
	}
	if reloaded.ContentSHA256 != record.ContentSHA256 {
		t.Errorf("reloaded hash %q != in-memory %q", reloaded.ContentSHA256, record.ContentSHA256)
	}
}

// TestArgvIsAlwaysTheDeclaredSurface pins the core anti-abuse property: whatever
// the model proposes, the harness only ever invokes the bundle's own declared
// surface. Argv[0] must be the run target from the statement, and the recorded
// argv must not contain the random temp path used to run (it keeps {given}).
func TestArgvIsAlwaysTheDeclaredSurface(t *testing.T) {
	fixClock(t)
	root := copyPaidTwice(t)
	// A hostile proposal that tries to smuggle a command into args. It must have
	// no effect on argv[0].
	hostile := `[
	  {"input": "date,payee,amount\n2026-08-01,A,1.00\n", "args": ["; rm -rf /"]},
	  {"input": "", "args": ["--nonsense"]}
	]`
	model := &stubModel{responses: []string{hostile, judgeServed}}

	record, _, err := RunTrial(context.Background(), model, root)
	if err != nil {
		t.Fatalf("RunTrial: %v", err)
	}
	if len(record.Trials) != 2 {
		t.Fatalf("got %d trials, want 2", len(record.Trials))
	}
	for i, tr := range record.Trials {
		if len(tr.Argv) == 0 {
			t.Fatalf("trial %d has empty argv", i)
		}
		if tr.Argv[0] != "surface/run.sh" {
			t.Errorf("trial %d argv[0] = %q, want the declared surface surface/run.sh", i, tr.Argv[0])
		}
		for _, tok := range tr.Argv {
			if strings.HasPrefix(tok, os.TempDir()) || strings.Contains(tok, "purposetrial-") {
				t.Errorf("trial %d recorded argv leaked a temp path: %q", i, tok)
			}
		}
	}
}

// TestContentHashIsStable proves the transcript is reproducible: the same stub
// inputs and a fixed clock yield byte-identical content hashes across runs, so a
// later increment can sign the hash and anyone can recompute it.
func TestContentHashIsStable(t *testing.T) {
	run := func() *TrialRecord {
		fixClock(t)
		root := copyPaidTwice(t)
		model := &stubModel{responses: []string{proposeDouble, judgeServed}}
		record, _, err := RunTrial(context.Background(), model, root)
		if err != nil {
			t.Fatalf("RunTrial: %v", err)
		}
		return record
	}
	first, second := run(), run()
	if first.ContentSHA256 != second.ContentSHA256 {
		t.Errorf("content hash is not stable across runs: %q != %q", first.ContentSHA256, second.ContentSHA256)
	}
}

// TestModelUnreachableIsIncomplete pins the fail-closed rule at both model
// steps: an unreachable model yields a transcript marked incomplete, an error
// wrapping ErrModelUnreachable, and never a silent success — with no panic.
func TestModelUnreachableIsIncomplete(t *testing.T) {
	tests := []struct {
		name      string
		responses []string // fed before the error stub takes over
		failFirst bool     // fail at the propose step, else at the judge step
		wantTrial bool     // whether the incomplete record should carry trials
	}{
		{"model unreachable at the propose step", nil, true, false},
		{"model unreachable at the judge step", []string{proposeDouble}, false, true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			fixClock(t)
			root := copyPaidTwice(t)

			var model Model
			if tt.failFirst {
				model = &stubModel{err: errors.New("connection refused")}
			} else {
				model = &failAfter{ok: tt.responses}
			}

			record, path, err := RunTrial(context.Background(), model, root)
			if !errors.Is(err, ErrModelUnreachable) {
				t.Fatalf("err = %v, want it to wrap ErrModelUnreachable", err)
			}
			if record == nil {
				t.Fatal("an incomplete trial must still return its record")
			}
			if record.Status != StatusIncomplete {
				t.Errorf("status = %q, want %q", record.Status, StatusIncomplete)
			}
			if record.Note == "" {
				t.Error("an incomplete transcript must carry a human-readable note")
			}
			if record.Judgement.Served {
				t.Error("an incomplete transcript must not present a served judgement")
			}
			if tt.wantTrial && len(record.Trials) == 0 {
				t.Error("a judge-step failure should still have recorded the surface trials")
			}
			// Even fail-closed, the transcript is written to disk.
			if _, statErr := os.Stat(path); statErr != nil {
				t.Errorf("the incomplete transcript was not written: %v", statErr)
			}
		})
	}
}

// failAfter answers with its canned ok responses, then errors on the next call.
// It drives the judge-step failure: propose succeeds, judge cannot be reached.
type failAfter struct {
	ok    []string
	calls int
}

func (m *failAfter) Complete(_ context.Context, _ string) (string, error) {
	if m.calls < len(m.ok) {
		r := m.ok[m.calls]
		m.calls++
		return r, nil
	}
	m.calls++
	return "", errors.New("connection refused")
}

// TestGuardsOnBadInput covers the caller-fixable misuses, which are errors
// rather than incomplete transcripts.
func TestGuardsOnBadInput(t *testing.T) {
	if _, _, err := RunTrial(context.Background(), nil, t.TempDir()); !errors.Is(err, ErrNoModel) {
		t.Errorf("nil model: err = %v, want ErrNoModel", err)
	}
	model := &stubModel{responses: []string{proposeDouble, judgeServed}}
	if _, _, err := RunTrial(context.Background(), model, ""); !errors.Is(err, ErrNoBundle) {
		t.Errorf("empty root: err = %v, want ErrNoBundle", err)
	}
}

// TestLenientProposalParsing checks the JSON extractors tolerate a model that
// wraps its answer in prose, and that an unparseable proposal still exercises
// the surface once rather than not at all.
func TestLenientProposalParsing(t *testing.T) {
	if got := firstJSONArray(`prose [ {"a":1} ] trailing`); got != `[ {"a":1} ]` {
		t.Errorf("firstJSONArray = %q", got)
	}
	if got := firstJSONObject(`the answer is {"served": true} ok`); got != `{"served": true}` {
		t.Errorf("firstJSONObject = %q", got)
	}
	// Bracket inside a string literal must not fool the balancer.
	if got := firstJSONArray(`[{"input": "a]b"}]`); got != `[{"input": "a]b"}]` {
		t.Errorf("firstJSONArray with bracket-in-string = %q", got)
	}

	fixClock(t)
	root := copyPaidTwice(t)
	model := &stubModel{responses: []string{"no json here at all", judgeServed}}
	record, _, err := RunTrial(context.Background(), model, root)
	if err != nil {
		t.Fatalf("RunTrial: %v", err)
	}
	if len(record.Trials) != 1 {
		t.Fatalf("an unparseable proposal should still exercise the surface once, got %d trials", len(record.Trials))
	}
}
