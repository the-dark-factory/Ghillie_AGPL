// Package adversary is the ADVERSARIAL PASS: the deliberate mirror of the
// agent-purpose-trial (internal/purposetrial) on the extension publish path
// (doctrine: feedback_assume_someone_will_try_it_on — design for the bad-faith
// actor; every extension earns an adversarial pass before it can be published).
//
// The problem it answers. The purpose-trial tries to CONFIRM an extension serves
// its declared purpose. That is the friendly reading. But a purpose can be served
// on the flattering inputs and still fall apart on the inputs a bad actor would
// choose. So this pass does the opposite: it drives a HOSTILE model to generate
// inputs designed to make the surface MISS its stated purpose — edge cases,
// malformed-but-plausible data, boundary values — runs the bundle's OWN declared
// surface on each, and RECORDS what happened in a transcript the author never
// supplies and does not control. The surface is deterministic, so anyone can
// re-run the recorded inputs and confirm the transcript byte-for-byte.
//
// Independence by construction. The adversary is a DIFFERENT model from the one
// the purpose-trial uses (see ollama.go): a critic that tries to break the
// extension must not be the same voice that approved it. The model name is
// configurable (GHILLIE_ADVERSARY_MODEL), defaulting to the proven local
// adversarial tester, and it is reached only through the same LOCAL ollama the
// purpose-trial uses — sovereign, on the owner's own machine, no cloud.
//
// ★ THIS HARNESS IS GLUE. It runs a sandbox, calls a model, and records what
// happened. It reaches no conclusion of its own: it performs no fold over the
// attacks, it hand-codes no held/defeated rule, and it never concludes for itself
// that a surface withstood attack. The JUDGEMENT is the MODEL's, transcribed
// verbatim in the transcript. Whether a recorded pass ever bears on a publish
// decision is a SEPARATE, later increment — not this one. This mirrors how
// purposetrial and the acceptance shim stay glue: they establish facts and
// record; they do not decide.
//
// Fail-closed. If the model cannot be reached or errors, the transcript is still
// written, marked incomplete in its Status field, and is never presented as a
// completed pass. A harness that quietly wrote nothing — or worse, a clean-looking
// record — when its model went missing would be worse than no harness. A surface
// that itself crashes or times out is a valid OBSERVATION (recorded with its exit
// and whatever output it produced), not a harness failure.
package adversary

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/tonygair/ghillie/internal/acceptance"
	"github.com/tonygair/ghillie/internal/purposetrial"
)

// StatusComplete marks a transcript whose model was reached for both the
// attack-proposal and the judgement, and whose surface runs were all observed.
const StatusComplete = "complete"

// StatusIncomplete marks a transcript the harness wrote fail-closed: the model
// was unreachable or errored at some step, so the pass did not run to the end. A
// reader must never treat an incomplete transcript as a completed pass.
const StatusIncomplete = "incomplete"

// proposedAttacks is how many hostile input cases the harness asks the model to
// propose. A small fixed number: enough to probe the purpose's weak points from
// several angles, few enough to stay cheap and re-runnable.
const proposedAttacks = 4

// maxSurfaceOutput caps how much of a surface's stdout the transcript keeps, so a
// runaway surface cannot balloon the record. It mirrors the same cap in
// internal/purposetrial and internal/acceptance.
const maxSurfaceOutput = 1 << 20

// surfaceTimeout bounds one surface run. It is a var, not a const, only so tests
// can shrink it — exactly as purposetrial and the acceptance shim do; nothing in
// the package changes it at runtime.
var surfaceTimeout = 30 * time.Second

// nowFunc reads the wall clock for the transcript's produced_at stamp. It is a
// var so a test can fix it and get a byte-stable transcript (and so a stable
// content hash); production leaves it as time.Now.
var nowFunc = time.Now

// The situations the harness reports to its caller. Callers match with
// errors.Is. None of these mean the surface withstood or failed anything — they
// mean the harness could not complete a pass and wrote an incomplete transcript
// saying so.
var (
	// ErrNoModel means no Model was supplied to drive the pass.
	ErrNoModel = errors.New("adversary: no model to drive the pass")
	// ErrNoBundle means no bundle root was supplied.
	ErrNoBundle = errors.New("adversary: no bundle root to run the surface in")
	// ErrNoSurface means the bundle's statement declares no surface to invoke.
	ErrNoSurface = errors.New("adversary: the bundle declares no surface to invoke")
	// ErrModelUnreachable wraps whatever the model returned when it could not be
	// reached or could not answer. The transcript is written incomplete; this
	// error tells the caller why.
	ErrModelUnreachable = errors.New("adversary: the model could not be reached")
)

// RecordedPurpose is the extension's declared purpose, copied into the transcript
// so the record stands alone: id and one-sentence intent. It mirrors
// purposetrial's RecordedPurpose rather than importing it, so this record type is
// self-contained.
type RecordedPurpose struct {
	// ID is the kebab-case purpose identifier from the bundle's statement.
	ID string `json:"id"`
	// Sentence is the one-line human statement of what the surface is for.
	Sentence string `json:"sentence"`
}

// Attack is one hostile exercise of the surface: the input the model proposed to
// try to break it, the exact argv the harness ran (argv[0] is always the bundle's
// own declared surface — never a model-proposed command), the surface's captured
// stdout, and its exit status. A reader can re-run the argv against the recorded
// input and reproduce the stdout exactly, because the surface is deterministic.
type Attack struct {
	// Input is the hostile data the model proposed to feed the surface. When the
	// surface consumes a file, this is written to a temp file whose path is
	// substituted into the argv; it is recorded here so anyone can recreate it.
	Input string `json:"input"`
	// Argv is the exact command line the harness executed, relative to the bundle
	// root. Argv[0] is the bundle's declared surface run target.
	Argv []string `json:"argv"`
	// Stdout is the surface's captured standard output (capped).
	Stdout string `json:"stdout"`
	// Exit is the surface's exit status, or -1 when the surface could not be
	// observed as a clean exit (never started, timed out, or was signalled). A
	// crash or a timeout is a legitimate observation of an attack landing, not a
	// harness error.
	Exit int `json:"exit"`
}

// Judgement is the model's answer, transcribed verbatim. The harness makes no
// decision of its own here: Held is the MODEL's own yes/no, copied from its
// response, and Raw is the model's whole reply byte-for-byte. Nothing in this
// package folds Held into a conclusion; that is a later increment's concern.
type Judgement struct {
	// Held is the model's own answer to whether the purpose HELD against every
	// attack (true) or was DEFEATED by at least one (false). It is transcribed
	// from the model's reply, not decided here, and is meaningful only when the
	// enclosing transcript is complete.
	Held bool `json:"held"`
	// WhichInput is the model's own naming of the input that defeated the purpose,
	// transcribed from its reply; empty when the model reports the purpose held.
	WhichInput string `json:"which_input"`
	// Reasoning is the model's short reasoning — how the input defeated the
	// purpose, or why nothing did — transcribed from its reply.
	Reasoning string `json:"reasoning"`
	// Raw is the model's full response, kept verbatim so the transcript carries
	// the judgement exactly as the model gave it, whatever its shape.
	Raw string `json:"raw"`
}

// AdversarialRecord is the whole transcript: the declared purpose, which model
// drove the pass, every hostile exercise of the surface with its real output, the
// model's verbatim judgement, when it was produced, and a content hash over the
// canonical body. It mirrors purposetrial's TrialRecord and is structured, hashed
// and sign-ready.
type AdversarialRecord struct {
	// Purpose is the extension's declared purpose, copied from its statement.
	Purpose RecordedPurpose `json:"purpose"`
	// ModelID names the model that drove the pass (e.g. "deepseek-r1:32b").
	ModelID string `json:"model_id"`
	// Attacks are the hostile surface exercises the harness ran, in the order the
	// model proposed them.
	Attacks []Attack `json:"attacks"`
	// Judgement is the model's verbatim answer over the purpose and the real
	// outputs. It is meaningful only when Status is complete.
	Judgement Judgement `json:"judgement"`
	// Status is StatusComplete or StatusIncomplete. An incomplete transcript was
	// written fail-closed and must never be read as a completed pass.
	Status string `json:"status"`
	// Note carries a human-readable reason when Status is incomplete; it is empty
	// on a complete transcript.
	Note string `json:"note,omitempty"`
	// ProducedAt is the RFC3339 UTC time the transcript was produced.
	ProducedAt string `json:"produced_at"`
	// ContentSHA256 is the hex SHA-256 over the canonical record body — every
	// field except ContentSHA256 and Signature. Anyone can recompute it from the
	// rest of the record.
	ContentSHA256 string `json:"content_sha256"`
	// Signature is empty in this increment. A later increment slots a gate-key
	// Ed25519 signature over ContentSHA256 in here — matching the purpose-trial's
	// and ratchet's planned Ed25519 receipts. No key mechanism is invented now;
	// the field only reserves the place the signature will occupy.
	Signature string `json:"signature"`
}

// canonicalBytes renders the record for hashing: the whole record with
// ContentSHA256 and Signature blanked, marshalled deterministically. Struct
// fields marshal in declaration order and the record holds no maps, so the bytes
// are stable for a given record.
func (r AdversarialRecord) canonicalBytes() (canonical []byte, err error) {
	body := r
	body.ContentSHA256 = ""
	body.Signature = ""
	canonical, err = json.Marshal(body)
	if err != nil {
		return nil, fmt.Errorf("adversary: rendering the canonical body: %w", err)
	}
	return canonical, nil
}

// hashInto computes the content hash over the canonical body and stores it in
// ContentSHA256.
func (r *AdversarialRecord) hashInto() (err error) {
	canonical, err := r.canonicalBytes()
	if err != nil {
		return err
	}
	sum := sha256.Sum256(canonical)
	r.ContentSHA256 = hex.EncodeToString(sum[:])
	return nil
}

// RunAdversarialPass drives one adversarial pass over the bundle at bundleRoot
// and writes the transcript under <bundleRoot>/adversary/adversary-record.json.
//
// It reads the bundle's acceptance statement to learn the declared purpose and
// how the surface is invoked, asks the model to propose a few HOSTILE inputs
// aimed at making the surface miss that purpose, runs the bundle's OWN declared
// surface on each in a sandbox, then asks the model to judge — over the purpose
// and the REAL outputs — whether any input DEFEATED the purpose, and records that
// judgement verbatim.
//
// modelID names the model behind model and is recorded verbatim as the record's
// model_id; production callers get both from NewAdversary. The harness never
// invokes anything but the bundle's declared surface: the model proposes input
// DATA and args, never commands. The returned record is also written to disk;
// path is where it landed. When the model cannot be reached the record is written
// with Status incomplete and err wraps ErrModelUnreachable — a written, honest,
// incomplete transcript, never a silent success.
func RunAdversarialPass(ctx context.Context, model purposetrial.Model, modelID, bundleRoot string) (record *AdversarialRecord, path string, err error) {
	if model == nil {
		return nil, "", ErrNoModel
	}
	if bundleRoot == "" {
		return nil, "", ErrNoBundle
	}

	spec, err := acceptance.Load(filepath.Join(bundleRoot, "acceptance", "acceptance.json"))
	if err != nil {
		return nil, "", fmt.Errorf("adversary: reading the bundle's acceptance statement: %w", err)
	}

	runTemplate, err := surfaceTemplate(spec)
	if err != nil {
		return nil, "", err
	}

	record = &AdversarialRecord{
		Purpose:    RecordedPurpose{ID: spec.Purpose.ID, Sentence: spec.Purpose.Sentence},
		ModelID:    modelID,
		ProducedAt: nowFunc().UTC().Format(time.RFC3339),
		Status:     StatusComplete,
	}

	// Step 1 — ask the model for hostile inputs. A model failure here is
	// fail-closed: write an incomplete transcript with no attacks and stop.
	attacks, err := proposeAttacks(ctx, model, spec, runTemplate)
	if err != nil {
		record.Status = StatusIncomplete
		record.Note = "the model could not be reached to propose attacks: " + err.Error()
		writtenPath, writeErr := writeRecord(record, bundleRoot)
		if writeErr != nil {
			return record, writtenPath, writeErr
		}
		return record, writtenPath, fmt.Errorf("%w: %v", ErrModelUnreachable, err)
	}

	// Step 2 — run the bundle's declared surface on each hostile input. This
	// touches no model and never fails the pass: a surface that crashes or times
	// out is an observation recorded with Exit -1, the attack landing.
	base, err := os.MkdirTemp("", "adversary-*")
	if err != nil {
		return nil, "", fmt.Errorf("adversary: making a temp dir for the proposed inputs: %w", err)
	}
	defer func() { _ = os.RemoveAll(base) }()

	record.Attacks = make([]Attack, 0, len(attacks))
	for i, a := range attacks {
		record.Attacks = append(record.Attacks, runOne(ctx, runTemplate, a, bundleRoot, base, i))
	}

	// Step 3 — ask the model to judge over the purpose and the REAL outputs. A
	// model failure here is fail-closed too: keep the attacks, mark incomplete.
	judgement, err := judge(ctx, model, spec, record.Attacks)
	if err != nil {
		record.Status = StatusIncomplete
		record.Note = "the model could not be reached to judge the outputs: " + err.Error()
		if hashErr := record.hashInto(); hashErr != nil {
			return record, "", hashErr
		}
		writtenPath, writeErr := writeRecord(record, bundleRoot)
		if writeErr != nil {
			return record, writtenPath, writeErr
		}
		return record, writtenPath, fmt.Errorf("%w: %v", ErrModelUnreachable, err)
	}
	record.Judgement = judgement

	if err = record.hashInto(); err != nil {
		return record, "", err
	}
	path, err = writeRecord(record, bundleRoot)
	if err != nil {
		return record, path, err
	}
	return record, path, nil
}

// surfaceTemplate reads, from the validated statement, the argv template that
// invokes the bundle's surface. All checks in a bundle exercise the same surface,
// so the first check's Run is the declared invocation. Its argv[0] is the surface
// run target the harness will invoke, and only that. Mirrors purposetrial.
func surfaceTemplate(spec *acceptance.Spec) (template []string, err error) {
	if spec == nil || len(spec.Checks) == 0 || len(spec.Checks[0].Run) == 0 {
		return nil, ErrNoSurface
	}
	template = make([]string, len(spec.Checks[0].Run))
	copy(template, spec.Checks[0].Run)
	return template, nil
}

// takesFile reports whether the surface consumes a given file — i.e. its argv
// template carries the "{given}" placeholder.
func takesFile(template []string) bool {
	for _, tok := range template {
		if tok == "{given}" {
			return true
		}
	}
	return false
}

// attack is one hostile input case the model proposed: the data to feed the
// surface and any extra args to append after it. The harness treats input as DATA
// and args as flags for the declared surface — never as a command.
type attack struct {
	Input string   `json:"input"`
	Args  []string `json:"args"`
}

// proposeAttacks asks the model, given the purpose and how the surface is
// invoked, for a few hostile input cases aimed at breaking the purpose, and
// parses them leniently.
func proposeAttacks(ctx context.Context, model purposetrial.Model, spec *acceptance.Spec, template []string) (attacks []attack, err error) {
	resp, err := model.Complete(ctx, proposePrompt(spec, template))
	if err != nil {
		return nil, err
	}
	attacks = parseAttacks(resp)
	if len(attacks) > proposedAttacks {
		attacks = attacks[:proposedAttacks]
	}
	// An empty or unparseable proposal is still a completed model turn — the model
	// answered. Record a single empty case so the surface is at least exercised on
	// the model's behalf rather than not at all.
	if len(attacks) == 0 {
		attacks = []attack{{}}
	}
	return attacks, nil
}

// proposePrompt builds the attack-proposal prompt: the purpose sentence, how the
// surface is invoked, and a request for a small JSON array of hostile input cases.
func proposePrompt(spec *acceptance.Spec, template []string) string {
	var b strings.Builder
	b.WriteString("You are a HOSTILE tester trying to BREAK a small command-line extension — to make it MISS its stated purpose.\n\n")
	b.WriteString("Purpose: ")
	b.WriteString(spec.Purpose.Sentence)
	b.WriteString("\n\nThe extension is invoked as: ")
	b.WriteString(strings.Join(template, " "))
	b.WriteString("\n")
	if takesFile(template) {
		b.WriteString("The token {given} is replaced by the path to an input file whose CONTENTS you provide.\n")
	} else {
		b.WriteString("The extension takes no input file; you provide command-line arguments only.\n")
	}
	fmt.Fprintf(&b, "\nPropose %d hostile input cases designed to make the extension fail its purpose — ", proposedAttacks)
	b.WriteString("edge cases, malformed-but-plausible data, boundary values, the inputs a bad actor would use to make it miss, mis-report, or crash. ")
	b.WriteString("Return ONLY a JSON array; each element is an object ")
	b.WriteString(`{"input": "<file contents, or empty>", "args": ["<extra arg>", ...]}. `)
	b.WriteString("Do not propose commands to run — only the hostile input DATA and arguments for the extension above.")
	return b.String()
}

// parseAttacks extracts the model's proposed hostile cases from its reply. It is
// lenient: it finds the first JSON array in the text and decodes it, so a model
// that wraps the array in prose still parses.
func parseAttacks(resp string) []attack {
	raw := firstJSONArray(resp)
	if raw == "" {
		return nil
	}
	var attacks []attack
	if err := json.Unmarshal([]byte(raw), &attacks); err != nil {
		return nil
	}
	return attacks
}

// firstJSONArray returns the first bracket-balanced JSON array substring in s, or
// "" if there is none. It is a tolerant extractor for a model reply that may carry
// prose around the array. Mirrors purposetrial.
func firstJSONArray(s string) string {
	start := strings.IndexByte(s, '[')
	if start < 0 {
		return ""
	}
	depth := 0
	inStr := false
	escaped := false
	for i := start; i < len(s); i++ {
		c := s[i]
		switch {
		case escaped:
			escaped = false
		case c == '\\' && inStr:
			escaped = true
		case c == '"':
			inStr = !inStr
		case inStr:
			// within a string literal: ignore brackets
		case c == '[':
			depth++
		case c == ']':
			depth--
			if depth == 0 {
				return s[start : i+1]
			}
		}
	}
	return ""
}

// runOne runs the bundle's declared surface once on a hostile input, inside the
// sandbox, and records the exercise. index disambiguates the temp file per
// attack. It never returns an error: a surface that will not run is an observation
// (Exit -1), the attack landing.
//
// The recorded argv keeps the "{given}" placeholder rather than the random temp
// path the run used, so the transcript is reproducible: a re-runner writes Input
// to a file, substitutes it for {given}, and gets the same stdout. The exec argv
// (with the real temp path) is used only to run and never recorded.
func runOne(ctx context.Context, template []string, a attack, bundleRoot, base string, index int) Attack {
	recorded := recordedArgv(template, a)
	toRun, err := execArgv(template, a, base, index)
	if err != nil {
		return Attack{Input: a.Input, Argv: recorded, Stdout: "", Exit: -1}
	}
	r := runSurface(ctx, toRun, bundleRoot)
	exit := -1
	if r.ran {
		exit = r.exit
	}
	return Attack{Input: a.Input, Argv: recorded, Stdout: r.stdout, Exit: exit}
}

// recordedArgv is the reproducible argv stored in the transcript: the declared
// template with a.Args appended and the "{given}" placeholder left intact.
// Argv[0] is the surface run target, straight from the template — never the
// model's.
func recordedArgv(template []string, a attack) []string {
	argv := make([]string, 0, len(template)+len(a.Args))
	argv = append(argv, template...)
	return append(argv, a.Args...)
}

// execArgv expands the surface template for one hostile input for ACTUAL
// execution: each "{given}" becomes the path of a temp file holding a.Input, then
// a.Args are appended. Argv[0] — the surface run target — is copied straight from
// the template and is never taken from the model.
func execArgv(template []string, a attack, base string, index int) (argv []string, err error) {
	var givenPath string
	if takesFile(template) {
		givenPath = filepath.Join(base, fmt.Sprintf("input-%d", index))
		if err = os.WriteFile(givenPath, []byte(a.Input), 0o600); err != nil {
			return nil, fmt.Errorf("adversary: writing a proposed input file: %w", err)
		}
	}
	argv = make([]string, 0, len(template)+len(a.Args))
	for _, tok := range template {
		if tok == "{given}" {
			argv = append(argv, givenPath)
			continue
		}
		argv = append(argv, tok)
	}
	return append(argv, a.Args...), nil
}

// judge asks the model, over the purpose and the REAL outputs the surface
// produced, whether any hostile input DEFEATED the purpose, and transcribes the
// answer verbatim. The harness parses the model's own held/which/reasoning for
// structure but keeps the whole reply in Raw; it decides nothing itself.
func judge(ctx context.Context, model purposetrial.Model, spec *acceptance.Spec, attacks []Attack) (judgement Judgement, err error) {
	resp, err := model.Complete(ctx, judgePrompt(spec, attacks))
	if err != nil {
		return Judgement{}, err
	}
	held, which, reasoning := parseJudgement(resp)
	return Judgement{Held: held, WhichInput: which, Reasoning: reasoning, Raw: resp}, nil
}

// judgePrompt builds the judgement prompt: the purpose, and for each attack the
// input, the argv and the surface's real stdout and exit.
func judgePrompt(spec *acceptance.Spec, attacks []Attack) string {
	var b strings.Builder
	b.WriteString("You tried to break a command-line extension and here are the REAL results.\n\n")
	b.WriteString("Purpose: ")
	b.WriteString(spec.Purpose.Sentence)
	b.WriteString("\n\n")
	for i, a := range attacks {
		fmt.Fprintf(&b, "Case %d:\n", i+1)
		fmt.Fprintf(&b, "  argv: %s\n", strings.Join(a.Argv, " "))
		fmt.Fprintf(&b, "  input: %q\n", a.Input)
		fmt.Fprintf(&b, "  stdout: %q\n", a.Stdout)
		fmt.Fprintf(&b, "  exit: %d\n", a.Exit)
	}
	b.WriteString("\nDid ANY of these inputs DEFEAT the purpose — break it, make it report the wrong thing, or crash it? ")
	b.WriteString("Answer with ONLY a JSON object ")
	b.WriteString(`{"held": true|false, "which_input": "<the input that defeated it, or empty>", "reasoning": "<one or two sentences>"}. `)
	b.WriteString("held is true only if the purpose survived every case; held is false if even one input defeated it.")
	return b.String()
}

// parseJudgement extracts the model's own held flag, the input it named, and its
// reasoning from its reply, leniently. A reply the harness cannot parse yields
// held=false (the unfavourable, adversarial-safe reading), an empty which_input
// and empty reasoning — the full reply is kept verbatim in Raw regardless, so no
// judgement is lost. This is a parse fallback for an unreadable structured field,
// not a hand-coded conclusion: the model's actual words survive in Raw.
func parseJudgement(resp string) (held bool, whichInput, reasoning string) {
	raw := firstJSONObject(resp)
	if raw == "" {
		return false, "", ""
	}
	var parsed struct {
		Held       bool   `json:"held"`
		WhichInput string `json:"which_input"`
		Reasoning  string `json:"reasoning"`
	}
	if err := json.Unmarshal([]byte(raw), &parsed); err != nil {
		return false, "", ""
	}
	return parsed.Held, parsed.WhichInput, parsed.Reasoning
}

// firstJSONObject returns the first brace-balanced JSON object substring in s, or
// "" if there is none. Mirrors purposetrial.
func firstJSONObject(s string) string {
	start := strings.IndexByte(s, '{')
	if start < 0 {
		return ""
	}
	depth := 0
	inStr := false
	escaped := false
	for i := start; i < len(s); i++ {
		c := s[i]
		switch {
		case escaped:
			escaped = false
		case c == '\\' && inStr:
			escaped = true
		case c == '"':
			inStr = !inStr
		case inStr:
			// within a string literal: ignore braces
		case c == '{':
			depth++
		case c == '}':
			depth--
			if depth == 0 {
				return s[start : i+1]
			}
		}
	}
	return ""
}

// writeRecord marshals the transcript and writes it under
// <bundleRoot>/adversary/adversary-record.json, creating the adversary dir. It
// returns the path it wrote to.
func writeRecord(record *AdversarialRecord, bundleRoot string) (path string, err error) {
	dir := filepath.Join(bundleRoot, "adversary")
	if err = os.MkdirAll(dir, 0o755); err != nil {
		return "", fmt.Errorf("adversary: making the adversary dir: %w", err)
	}
	path = filepath.Join(dir, "adversary-record.json")
	data, err := json.MarshalIndent(record, "", "  ")
	if err != nil {
		return path, fmt.Errorf("adversary: marshalling the transcript: %w", err)
	}
	if err = os.WriteFile(path, append(data, '\n'), 0o644); err != nil {
		return path, fmt.Errorf("adversary: writing the transcript: %w", err)
	}
	return path, nil
}

// --- sandbox -----------------------------------------------------------------
//
// The sandbox mirrors internal/purposetrial (which mirrors internal/acceptance).
// purposetrial's surface runner is unexported (runSurface/scrubbedEnv/capWriter
// are package-private), so it cannot be imported; the logic below is a faithful
// copy of the same scrubbed-env, per-run-timeout, capped-output approach. If that
// runner is ever exported, this block should call it instead of duplicating it.
// Only the ollama CLIENT is shared (via purposetrial.NewOllamaModelNamed), because
// duplicating an HTTP client is a worse cost than mirroring this small sandbox.

// surfaceRun is one observation of the surface: its captured stdout, exit status,
// whether the capture was truncated, and whether it ran to a clean exit.
type surfaceRun struct {
	stdout    string
	exit      int
	truncated bool
	ran       bool
}

// runSurface runs the already-expanded argv inside the bundle, under a scrubbed
// environment, a per-run timeout and a capped stdout. Argv[0] is the bundle's
// declared surface run target, relative to bundleRoot. It never returns an error:
// an un-runnable surface is an observation (ran=false). Mirrors purposetrial.
func runSurface(ctx context.Context, argv []string, bundleRoot string) surfaceRun {
	if len(argv) == 0 {
		return surfaceRun{}
	}
	runCtx, cancel := context.WithTimeout(ctx, surfaceTimeout)
	defer cancel()

	cmd := exec.CommandContext(runCtx, argv[0], argv[1:]...)
	cmd.Dir = bundleRoot
	cmd.Env = scrubbedEnv()
	cap := &capWriter{limit: maxSurfaceOutput}
	cmd.Stdout = cap
	cmd.Stderr = io.Discard

	runErr := cmd.Run()

	switch {
	case runCtx.Err() != nil:
		return surfaceRun{stdout: cap.String(), truncated: cap.truncated}
	case cmd.ProcessState == nil:
		return surfaceRun{stdout: cap.String(), truncated: cap.truncated}
	case !cmd.ProcessState.Exited():
		return surfaceRun{stdout: cap.String(), truncated: cap.truncated}
	}

	var exitErr *exec.ExitError
	if runErr != nil && !errors.As(runErr, &exitErr) {
		return surfaceRun{stdout: cap.String(), truncated: cap.truncated}
	}
	return surfaceRun{
		stdout:    cap.String(),
		exit:      cmd.ProcessState.ExitCode(),
		truncated: cap.truncated,
		ran:       true,
	}
}

// scrubbedEnv is the environment a surface runs under: PATH, LC_ALL=C and LANG=C,
// and nothing else. Inherited credentials, tokens and proxy settings are dropped
// so a surface cannot reach the operator's secrets or the network. PATH is carried
// so the surface's own interpreters (a shell, awk) still resolve; PATH is not a
// credential. Mirrors purposetrial / internal/acceptance.
func scrubbedEnv() []string {
	env := []string{"LC_ALL=C", "LANG=C"}
	if p := os.Getenv("PATH"); p != "" {
		env = append(env, "PATH="+p)
	} else {
		env = append(env, "PATH=/usr/bin:/bin:/usr/sbin:/sbin")
	}
	return env
}

// capWriter keeps at most limit bytes and records whether it had to drop any. It
// always reports the whole write as accepted so the child keeps writing and is
// never surprised by a short write. Mirrors purposetrial / internal/acceptance.
type capWriter struct {
	buf       bytes.Buffer
	limit     int
	truncated bool
}

// Write keeps up to limit bytes and flags truncation past it.
func (w *capWriter) Write(p []byte) (n int, err error) {
	if w.truncated {
		return len(p), nil
	}
	room := w.limit - w.buf.Len()
	if room <= 0 {
		w.truncated = true
		return len(p), nil
	}
	if len(p) > room {
		w.buf.Write(p[:room])
		w.truncated = true
		return len(p), nil
	}
	return w.buf.Write(p)
}

// String returns the bytes kept so far.
func (w *capWriter) String() string { return w.buf.String() }
