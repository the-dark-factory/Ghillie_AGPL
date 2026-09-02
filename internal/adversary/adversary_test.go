package adversary

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

	"github.com/tonygair/ghillie/internal/purposetrial"
)

// testModelID is the adversary model id the tests record and assert on. It is a
// fixed string, not the real ollama default, so the tests stay offline and pin
// that whatever id the caller supplies is transcribed verbatim into the record.
const testModelID = "deepseek-r1:32b"

// stubModel is an injected purposetrial.Model that returns canned responses in
// order, or a fixed error on every call. It lets the whole loop run — sandbox and
// all — without a real ollama.
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

var _ purposetrial.Model = (*stubModel)(nil)

// copyPaidTwice copies the real paid-twice bundle into a temp dir so a pass can
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

// A hostile proposal that feeds the paid-twice surface a genuine duplicate, so
// the real stdout is meaningful and assertable — the adversary is trying to see
// whether the surface still flags it.
const proposeHostile = `A hostile tester replies:
[
  {"input": "date,payee,amount\n2026-08-01,Roofer Ltd,450.00\n2026-08-03,Roofer Ltd,450.00\n", "args": []}
]`

// The model's judgement, transcribed verbatim: the purpose held against the
// attacks. held=true, no input defeated it.
const judgeHeld = `{"held": true, "which_input": "", "reasoning": "The surface still flagged the duplicate despite the crafted input, so the purpose held."}`

// The model's judgement that an input DEFEATED the purpose.
const judgeDefeated = `{"held": false, "which_input": "the near-duplicate with a trailing space", "reasoning": "A trailing space in the payee slipped past the match, so the duplicate went unflagged."}`

// TestRunAdversarialPassOnPaidTwice is the headline test: with a stub model, the
// loop runs, the sandbox executes the REAL paid-twice surface on the stub's
// hostile input, and a complete AdversarialRecord comes out carrying the stub's
// judgement, the real output, the supplied model id, and a 64-hex content hash.
func TestRunAdversarialPassOnPaidTwice(t *testing.T) {
	fixClock(t)
	root := copyPaidTwice(t)
	model := &stubModel{responses: []string{proposeHostile, judgeHeld}}

	record, path, err := RunAdversarialPass(context.Background(), model, testModelID, root)
	if err != nil {
		t.Fatalf("RunAdversarialPass: %v", err)
	}

	if record.Status != StatusComplete {
		t.Fatalf("status = %q, want %q (note %q)", record.Status, StatusComplete, record.Note)
	}
	if record.ModelID != testModelID {
		t.Errorf("model_id = %q, want the supplied id %q transcribed verbatim", record.ModelID, testModelID)
	}
	if record.Purpose.ID != "flag-duplicate-payments" {
		t.Errorf("purpose id = %q, want flag-duplicate-payments", record.Purpose.ID)
	}
	if len(record.Attacks) != 1 {
		t.Fatalf("got %d attacks, want 1", len(record.Attacks))
	}

	a := record.Attacks[0]
	if a.Exit != 0 {
		t.Errorf("attack exit = %d, want 0", a.Exit)
	}
	if !strings.Contains(a.Stdout, "possible double: Roofer Ltd") {
		t.Errorf("attack stdout did not carry the real surface output: %q", a.Stdout)
	}

	// The model's judgement is transcribed verbatim, not decided here.
	if !record.Judgement.Held {
		t.Error("the model's held flag was not transcribed")
	}
	if record.Judgement.Raw != judgeHeld {
		t.Errorf("judgement raw = %q, want the model's reply verbatim %q", record.Judgement.Raw, judgeHeld)
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

	// The transcript was written to <bundleRoot>/adversary/adversary-record.json
	// and reloads as the same record.
	if want := filepath.Join(root, "adversary", "adversary-record.json"); path != want {
		t.Errorf("path = %q, want %q", path, want)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("reading back the transcript: %v", err)
	}
	var reloaded AdversarialRecord
	if err := json.Unmarshal(data, &reloaded); err != nil {
		t.Fatalf("the written transcript is not valid JSON: %v", err)
	}
	if reloaded.ContentSHA256 != record.ContentSHA256 {
		t.Errorf("reloaded hash %q != in-memory %q", reloaded.ContentSHA256, record.ContentSHA256)
	}
}

// TestDefeatedJudgementTranscribed proves the harness transcribes a "defeated"
// judgement exactly as the model gives it, naming the input — the harness itself
// reaches no held/defeated conclusion.
func TestDefeatedJudgementTranscribed(t *testing.T) {
	fixClock(t)
	root := copyPaidTwice(t)
	model := &stubModel{responses: []string{proposeHostile, judgeDefeated}}

	record, _, err := RunAdversarialPass(context.Background(), model, testModelID, root)
	if err != nil {
		t.Fatalf("RunAdversarialPass: %v", err)
	}
	if record.Status != StatusComplete {
		t.Fatalf("status = %q, want %q", record.Status, StatusComplete)
	}
	if record.Judgement.Held {
		t.Error("the model reported the purpose defeated; held must be transcribed as false")
	}
	if record.Judgement.WhichInput == "" {
		t.Error("the model named the defeating input; which_input must be transcribed")
	}
	if record.Judgement.Raw != judgeDefeated {
		t.Errorf("judgement raw = %q, want the model's reply verbatim", record.Judgement.Raw)
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
	model := &stubModel{responses: []string{hostile, judgeHeld}}

	record, _, err := RunAdversarialPass(context.Background(), model, testModelID, root)
	if err != nil {
		t.Fatalf("RunAdversarialPass: %v", err)
	}
	if len(record.Attacks) != 2 {
		t.Fatalf("got %d attacks, want 2", len(record.Attacks))
	}
	for i, a := range record.Attacks {
		if len(a.Argv) == 0 {
			t.Fatalf("attack %d has empty argv", i)
		}
		if a.Argv[0] != "surface/run.sh" {
			t.Errorf("attack %d argv[0] = %q, want the declared surface surface/run.sh", i, a.Argv[0])
		}
		for _, tok := range a.Argv {
			if strings.HasPrefix(tok, os.TempDir()) || strings.Contains(tok, "adversary-") {
				t.Errorf("attack %d recorded argv leaked a temp path: %q", i, tok)
			}
		}
	}
}

// TestContentHashIsStable proves the transcript is reproducible: the same stub
// inputs and a fixed clock yield byte-identical content hashes across runs, so a
// later increment can sign the hash and anyone can recompute it.
func TestContentHashIsStable(t *testing.T) {
	run := func() *AdversarialRecord {
		fixClock(t)
		root := copyPaidTwice(t)
		model := &stubModel{responses: []string{proposeHostile, judgeHeld}}
		record, _, err := RunAdversarialPass(context.Background(), model, testModelID, root)
		if err != nil {
			t.Fatalf("RunAdversarialPass: %v", err)
		}
		return record
	}
	first, second := run(), run()
	if first.ContentSHA256 != second.ContentSHA256 {
		t.Errorf("content hash is not stable across runs: %q != %q", first.ContentSHA256, second.ContentSHA256)
	}
}

// TestModelUnreachableIsIncomplete pins the fail-closed rule at both model steps:
// an unreachable model yields a transcript marked incomplete, an error wrapping
// ErrModelUnreachable, and never a silent success — with no panic and no held
// judgement presented.
func TestModelUnreachableIsIncomplete(t *testing.T) {
	tests := []struct {
		name       string
		responses  []string // fed before the error stub takes over
		failFirst  bool     // fail at the propose step, else at the judge step
		wantAttack bool     // whether the incomplete record should carry attacks
	}{
		{"model unreachable at the propose step", nil, true, false},
		{"model unreachable at the judge step", []string{proposeHostile}, false, true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			fixClock(t)
			root := copyPaidTwice(t)

			var model purposetrial.Model
			if tt.failFirst {
				model = &stubModel{err: errors.New("connection refused")}
			} else {
				model = &failAfter{ok: tt.responses}
			}

			record, path, err := RunAdversarialPass(context.Background(), model, testModelID, root)
			if !errors.Is(err, ErrModelUnreachable) {
				t.Fatalf("err = %v, want it to wrap ErrModelUnreachable", err)
			}
			if record == nil {
				t.Fatal("an incomplete pass must still return its record")
			}
			if record.Status != StatusIncomplete {
				t.Errorf("status = %q, want %q", record.Status, StatusIncomplete)
			}
			if record.Note == "" {
				t.Error("an incomplete transcript must carry a human-readable note")
			}
			if record.Judgement.Held {
				t.Error("an incomplete transcript must not present a held judgement")
			}
			if tt.wantAttack && len(record.Attacks) == 0 {
				t.Error("a judge-step failure should still have recorded the surface attacks")
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

// TestGuardsOnBadInput covers the caller-fixable misuses, which are errors rather
// than incomplete transcripts.
func TestGuardsOnBadInput(t *testing.T) {
	if _, _, err := RunAdversarialPass(context.Background(), nil, testModelID, t.TempDir()); !errors.Is(err, ErrNoModel) {
		t.Errorf("nil model: err = %v, want ErrNoModel", err)
	}
	model := &stubModel{responses: []string{proposeHostile, judgeHeld}}
	if _, _, err := RunAdversarialPass(context.Background(), model, testModelID, ""); !errors.Is(err, ErrNoBundle) {
		t.Errorf("empty root: err = %v, want ErrNoBundle", err)
	}
}

// TestLenientProposalParsing checks the JSON extractors tolerate a model that
// wraps its answer in prose, and that an unparseable proposal still exercises the
// surface once rather than not at all.
func TestLenientProposalParsing(t *testing.T) {
	if got := firstJSONArray(`prose [ {"a":1} ] trailing`); got != `[ {"a":1} ]` {
		t.Errorf("firstJSONArray = %q", got)
	}
	if got := firstJSONObject(`the answer is {"held": true} ok`); got != `{"held": true}` {
		t.Errorf("firstJSONObject = %q", got)
	}
	// Bracket inside a string literal must not fool the balancer.
	if got := firstJSONArray(`[{"input": "a]b"}]`); got != `[{"input": "a]b"}]` {
		t.Errorf("firstJSONArray with bracket-in-string = %q", got)
	}

	fixClock(t)
	root := copyPaidTwice(t)
	model := &stubModel{responses: []string{"no json here at all", judgeHeld}}
	record, _, err := RunAdversarialPass(context.Background(), model, testModelID, root)
	if err != nil {
		t.Fatalf("RunAdversarialPass: %v", err)
	}
	if len(record.Attacks) != 1 {
		t.Fatalf("an unparseable proposal should still exercise the surface once, got %d attacks", len(record.Attacks))
	}
}

// TestUnparseableJudgementIsSafeDefault pins the parse fallback: an unreadable
// judgement reply yields held=false (the adversarial-safe reading) while the full
// reply survives verbatim in Raw, so nothing the model said is lost.
func TestUnparseableJudgementIsSafeDefault(t *testing.T) {
	fixClock(t)
	root := copyPaidTwice(t)
	const garbled = "I could not decide, sorry — no JSON for you."
	model := &stubModel{responses: []string{proposeHostile, garbled}}

	record, _, err := RunAdversarialPass(context.Background(), model, testModelID, root)
	if err != nil {
		t.Fatalf("RunAdversarialPass: %v", err)
	}
	if record.Judgement.Held {
		t.Error("an unparseable judgement must default held=false, not true")
	}
	if record.Judgement.Raw != garbled {
		t.Errorf("judgement raw = %q, want the model's reply verbatim %q", record.Judgement.Raw, garbled)
	}
}

// TestAdversaryModelID checks the model selection: the env override wins, else
// the default deepseek-r1:32b, deliberately different from the purpose-trial's
// model so the two passes stay independent.
func TestAdversaryModelID(t *testing.T) {
	t.Setenv(AdversaryModelEnv, "")
	if got := AdversaryModelID(); got != DefaultAdversaryModel {
		t.Errorf("with env unset, AdversaryModelID() = %q, want %q", got, DefaultAdversaryModel)
	}
	if DefaultAdversaryModel == purposetrial.OllamaModelID {
		t.Errorf("the adversary model %q must differ from the purpose-trial model %q", DefaultAdversaryModel, purposetrial.OllamaModelID)
	}
	t.Setenv(AdversaryModelEnv, "llama3.1:70b")
	if got := AdversaryModelID(); got != "llama3.1:70b" {
		t.Errorf("with env set, AdversaryModelID() = %q, want the override llama3.1:70b", got)
	}
}
