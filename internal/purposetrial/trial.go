// Package purposetrial is the AGENT-PURPOSE-TRIAL harness: the anti-gaming
// heart of the extension publish path (design memory:
// project_extension_test_gate_and_catalogue).
//
// The problem it answers. An extension author declares a purpose and a set of
// acceptance checks, but those checks are cherry-pickable — an author can hand
// in exactly the inputs that flatter the surface. So this harness does not trust
// the author's record. Instead the harness ITSELF drives a model to exercise the
// bundle's own surface against its declared purpose, and RECORDS what happened in
// a transcript the author never supplies and does not control. The surface's
// outputs are deterministic, so anyone can re-run the recorded inputs and confirm
// the transcript byte-for-byte.
//
// ★ THIS HARNESS IS GLUE. It runs a sandbox, calls a model, and records what
// happened. It reaches no conclusion of its own: it performs no fold over the
// trials, it hand-codes no served/not rule, and it never concludes for itself
// that a surface is good. The JUDGEMENT is the MODEL's, recorded verbatim in the
// transcript. Whether a recorded trial ever bears on a publish decision is a
// SEPARATE, later increment — not this one. This package mirrors how the
// acceptance shim (internal/acceptance/run.go) stays glue: it establishes facts
// and records; it does not decide.
//
// The model. The model that drives a trial is the owner's LOCAL ollama serving
// muse-glimmer:latest — sovereign, on the owner's own machine, no cloud. That is a
// hard requirement of this harness (see ollama.go). The model is reached only
// through the small Model interface below, so a test injects a stub and needs no
// running ollama.
//
// Fail-closed. If the model cannot be reached or errors, the transcript is still
// written, marked incomplete in its Status field, and is never presented as a
// completed, successful trial. A harness that quietly wrote nothing — or worse,
// wrote a clean-looking record — when its model went missing would be worse than
// no harness.
package purposetrial

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
)

// StatusComplete marks a transcript whose model was reached for both the
// input-proposal and the judgement, and whose surface runs were all observed.
const StatusComplete = "complete"

// StatusIncomplete marks a transcript the harness wrote fail-closed: the model
// was unreachable or errored at some step, so the trial did not run to the end.
// A reader must never treat an incomplete transcript as a completed trial.
const StatusIncomplete = "incomplete"

// proposedInputs is how many concrete input cases the harness asks the model to
// propose. A small fixed number: enough to probe the purpose from more than one
// angle, few enough to stay cheap and re-runnable.
const proposedInputs = 3

// maxSurfaceOutput caps how much of a surface's stdout the transcript keeps, so
// a runaway surface cannot balloon the record. It mirrors the acceptance shim's
// cap of the same name.
const maxSurfaceOutput = 1 << 20

// surfaceTimeout bounds one surface run. It is a var, not a const, only so tests
// can shrink it — exactly as internal/acceptance/run.go does; nothing in the
// package changes it at runtime.
var surfaceTimeout = 30 * time.Second

// nowFunc reads the wall clock for the transcript's produced_at stamp. It is a
// var so a test can fix it and get a byte-stable transcript (and so a stable
// content hash); production leaves it as time.Now.
var nowFunc = time.Now

// The situations the harness reports to its caller. Callers match with
// errors.Is. None of these mean the surface is bad — they mean the harness could
// not complete a trial and wrote an incomplete transcript saying so.
var (
	// ErrNoModel means no Model was supplied to drive the trial.
	ErrNoModel = errors.New("purposetrial: no model to drive the trial")
	// ErrNoBundle means no bundle root was supplied.
	ErrNoBundle = errors.New("purposetrial: no bundle root to run the surface in")
	// ErrNoSurface means the bundle's statement declares no surface to invoke.
	ErrNoSurface = errors.New("purposetrial: the bundle declares no surface to invoke")
	// ErrModelUnreachable wraps whatever the model returned when it could not be
	// reached or could not answer. The transcript is written incomplete; this
	// error tells the caller why.
	ErrModelUnreachable = errors.New("purposetrial: the model could not be reached")
)

// Model is the seam the harness drives a trial through. Complete sends one
// prompt and returns the model's whole response as text. The real
// implementation (OllamaModel) speaks to a local ollama; a test injects a stub.
type Model interface {
	// Complete sends prompt to the model and returns its full text response, or
	// an error if the model could not be reached or could not answer. It honours
	// ctx for cancellation and deadlines.
	Complete(ctx context.Context, prompt string) (response string, err error)
}

// RecordedPurpose is the extension's declared purpose, copied into the
// transcript so the record stands alone: id and one-sentence intent.
type RecordedPurpose struct {
	// ID is the kebab-case purpose identifier from the bundle's statement.
	ID string `json:"id"`
	// Sentence is the one-line human statement of what the surface is for.
	Sentence string `json:"sentence"`
}

// Trial is one exercise of the surface: the input the model proposed, the exact
// argv the harness ran (argv[0] is always the bundle's own declared surface —
// never a model-proposed command), the surface's captured stdout, and its exit
// status. A reader can re-run the argv against the recorded input and reproduce
// the stdout exactly, because the surface is deterministic.
type Trial struct {
	// Input is the data the model proposed to feed the surface. When the surface
	// consumes a file, this is written to a temp file whose path is substituted
	// into the argv; it is recorded here so anyone can recreate that file.
	Input string `json:"input"`
	// Argv is the exact command line the harness executed, relative to the
	// bundle root. Argv[0] is the bundle's declared surface run target.
	Argv []string `json:"argv"`
	// Stdout is the surface's captured standard output (capped).
	Stdout string `json:"stdout"`
	// Exit is the surface's exit status, or -1 when the surface could not be
	// observed as a clean exit (never started, timed out, or was signalled).
	Exit int `json:"exit"`
}

// Judgement is the model's answer, transcribed verbatim. The harness makes no
// decision of its own here: Served is the MODEL's own yes/no, copied from its
// response, and Raw is the model's whole reply byte-for-byte. Nothing in this
// package folds Served into a conclusion; that is a later increment's concern.
type Judgement struct {
	// Served is the model's own answer to whether the surface genuinely serves
	// its stated purpose. It is transcribed from the model's reply, not decided
	// here. It is meaningful only when the enclosing transcript is complete.
	Served bool `json:"served"`
	// Reasoning is the model's short reasoning, transcribed from its reply.
	Reasoning string `json:"reasoning"`
	// Raw is the model's full response, kept verbatim so the transcript carries
	// the judgement exactly as the model gave it, whatever its shape.
	Raw string `json:"raw"`
}

// TrialRecord is the whole transcript: the declared purpose, which model drove
// the trial, every exercise of the surface with its real output, the model's
// verbatim judgement, when it was produced, and a content hash over the
// canonical body. It is structured, hashed and sign-ready.
type TrialRecord struct {
	// Purpose is the extension's declared purpose, copied from its statement.
	Purpose RecordedPurpose `json:"purpose"`
	// ModelID names the model that drove the trial (e.g. "qwen2.5-coder:32b").
	ModelID string `json:"model_id"`
	// Trials are the surface exercises the harness ran, in the order proposed.
	Trials []Trial `json:"trials"`
	// Judgement is the model's verbatim answer over the purpose and the real
	// outputs. It is meaningful only when Status is complete.
	Judgement Judgement `json:"judgement"`
	// Status is StatusComplete or StatusIncomplete. An incomplete transcript was
	// written fail-closed and must never be read as a completed trial.
	Status string `json:"status"`
	// Note carries a human-readable reason when Status is incomplete; it is
	// empty on a complete transcript.
	Note string `json:"note,omitempty"`
	// ProducedAt is the RFC3339 UTC time the transcript was produced.
	ProducedAt string `json:"produced_at"`
	// ContentSHA256 is the hex SHA-256 over the canonical record body — every
	// field except ContentSHA256 and Signature. Anyone can recompute it from the
	// rest of the record.
	ContentSHA256 string `json:"content_sha256"`
	// Signature is empty in this increment. A later increment slots a gate-key
	// Ed25519 signature over ContentSHA256 in here — matching ratchet's planned
	// Ed25519 receipts. No key mechanism is invented now; the field only reserves
	// the place the signature will occupy.
	Signature string `json:"signature"`
}

// canonicalBytes renders the record for hashing: the whole record with
// ContentSHA256 and Signature blanked, marshalled deterministically. Struct
// fields marshal in declaration order and the record holds no maps, so the bytes
// are stable for a given record.
func (r TrialRecord) canonicalBytes() (canonical []byte, err error) {
	body := r
	body.ContentSHA256 = ""
	body.Signature = ""
	canonical, err = json.Marshal(body)
	if err != nil {
		return nil, fmt.Errorf("purposetrial: rendering the canonical body: %w", err)
	}
	return canonical, nil
}

// hashInto computes the content hash over the canonical body and stores it in
// ContentSHA256.
func (r *TrialRecord) hashInto() (err error) {
	canonical, err := r.canonicalBytes()
	if err != nil {
		return err
	}
	sum := sha256.Sum256(canonical)
	r.ContentSHA256 = hex.EncodeToString(sum[:])
	return nil
}

// RunTrial drives one agent-purpose-trial over the bundle at bundleRoot and
// writes the transcript under <bundleRoot>/trial/trial-record.json.
//
// It reads the bundle's acceptance statement to learn the declared purpose and
// how the surface is invoked, asks the model to propose a few concrete inputs
// aimed at that purpose, runs the bundle's OWN declared surface on each in a
// sandbox, then asks the model to judge — over the purpose and the REAL outputs —
// whether the surface genuinely serves its purpose, and records that judgement
// verbatim.
//
// The harness never invokes anything but the bundle's declared surface: the
// model proposes input DATA and args, never commands. The returned record is
// also written to disk; path is where it landed. When the model cannot be
// reached the record is written with Status incomplete and err wraps
// ErrModelUnreachable — a written, honest, incomplete transcript, never a silent
// success.
func RunTrial(ctx context.Context, model Model, bundleRoot string) (record *TrialRecord, path string, err error) {
	if model == nil {
		return nil, "", ErrNoModel
	}
	if bundleRoot == "" {
		return nil, "", ErrNoBundle
	}

	spec, err := acceptance.Load(filepath.Join(bundleRoot, "acceptance", "acceptance.json"))
	if err != nil {
		return nil, "", fmt.Errorf("purposetrial: reading the bundle's acceptance statement: %w", err)
	}

	runTemplate, err := surfaceTemplate(spec)
	if err != nil {
		return nil, "", err
	}

	record = &TrialRecord{
		Purpose:    RecordedPurpose{ID: spec.Purpose.ID, Sentence: spec.Purpose.Sentence},
		ModelID:    OllamaModelID,
		ProducedAt: nowFunc().UTC().Format(time.RFC3339),
		Status:     StatusComplete,
	}

	// Step 1 — ask the model for concrete inputs. A model failure here is
	// fail-closed: write an incomplete transcript with no trials and stop.
	inputs, err := proposeInputs(ctx, model, spec, runTemplate)
	if err != nil {
		record.Status = StatusIncomplete
		record.Note = "the model could not be reached to propose inputs: " + err.Error()
		path, writeErr := writeRecord(record, bundleRoot)
		if writeErr != nil {
			return record, path, writeErr
		}
		return record, path, fmt.Errorf("%w: %v", ErrModelUnreachable, err)
	}

	// Step 2 — run the bundle's declared surface on each proposed input. This
	// touches no model and never fails the trial: an un-runnable surface is an
	// observation recorded with Exit -1.
	base, err := os.MkdirTemp("", "purposetrial-*")
	if err != nil {
		return nil, "", fmt.Errorf("purposetrial: making a temp dir for the proposed inputs: %w", err)
	}
	defer func() { _ = os.RemoveAll(base) }()

	record.Trials = make([]Trial, 0, len(inputs))
	for i, in := range inputs {
		trial := runOne(ctx, runTemplate, in, bundleRoot, base, i)
		record.Trials = append(record.Trials, trial)
	}

	// Step 3 — ask the model to judge over the purpose and the REAL outputs. A
	// model failure here is fail-closed too: keep the trials, mark incomplete.
	judgement, err := judge(ctx, model, spec, record.Trials)
	if err != nil {
		record.Status = StatusIncomplete
		record.Note = "the model could not be reached to judge the outputs: " + err.Error()
		if hashErr := record.hashInto(); hashErr != nil {
			return record, "", hashErr
		}
		path, writeErr := writeRecord(record, bundleRoot)
		if writeErr != nil {
			return record, path, writeErr
		}
		return record, path, fmt.Errorf("%w: %v", ErrModelUnreachable, err)
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
// invokes the bundle's surface. All checks in a bundle exercise the same
// surface, so the first check's Run is the declared invocation. Its argv[0] is
// the surface run target the harness will invoke, and only that.
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

// proposal is one input case the model proposed: the data to feed the surface
// and any extra args to append after it. The harness treats input as DATA and
// args as flags for the declared surface — never as a command.
type proposal struct {
	Input string   `json:"input"`
	Args  []string `json:"args"`
}

// proposeInputs asks the model, given the purpose and how the surface is
// invoked, for a few concrete input cases, and parses them leniently.
func proposeInputs(ctx context.Context, model Model, spec *acceptance.Spec, template []string) (inputs []proposal, err error) {
	prompt := proposePrompt(spec, template)
	resp, err := model.Complete(ctx, prompt)
	if err != nil {
		return nil, err
	}
	inputs = parseProposals(resp)
	if len(inputs) > proposedInputs {
		inputs = inputs[:proposedInputs]
	}
	// An empty or unparseable proposal is still a completed model turn — the
	// model answered. Record a single empty case so the surface is at least
	// exercised on the model's behalf rather than not at all.
	if len(inputs) == 0 {
		inputs = []proposal{{}}
	}
	return inputs, nil
}

// proposePrompt builds the input-proposal prompt: the purpose sentence, how the
// surface is invoked, and a request for a small JSON array of input cases.
func proposePrompt(spec *acceptance.Spec, template []string) string {
	var b strings.Builder
	b.WriteString("You are testing a small command-line extension against its stated purpose.\n\n")
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
	fmt.Fprintf(&b, "\nPropose %d concrete input cases that would exercise this purpose. ", proposedInputs)
	b.WriteString("Return ONLY a JSON array; each element is an object ")
	b.WriteString(`{"input": "<file contents, or empty>", "args": ["<extra arg>", ...]}. `)
	b.WriteString("Do not propose commands to run — only the input DATA and arguments for the extension above.")
	return b.String()
}

// parseProposals extracts the model's proposed input cases from its reply. It is
// lenient: it finds the first JSON array in the text and decodes it, so a model
// that wraps the array in prose still parses.
func parseProposals(resp string) []proposal {
	raw := firstJSONArray(resp)
	if raw == "" {
		return nil
	}
	var proposals []proposal
	if err := json.Unmarshal([]byte(raw), &proposals); err != nil {
		return nil
	}
	return proposals
}

// firstJSONArray returns the first bracket-balanced JSON array substring in s,
// or "" if there is none. It is a tolerant extractor for a model reply that may
// carry prose around the array.
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

// runOne runs the bundle's declared surface once on a proposed input, inside the
// sandbox, and records the exercise. index disambiguates the temp file per
// trial. It never returns an error: an un-runnable surface is an observation
// (Exit -1), not a failure.
//
// The recorded argv keeps the "{given}" placeholder rather than the random temp
// path the run used, so the transcript is reproducible: a re-runner writes Input
// to a file, substitutes it for {given}, and gets the same stdout. The exec argv
// (with the real temp path) is used only to run and never recorded.
func runOne(ctx context.Context, template []string, in proposal, bundleRoot, base string, index int) Trial {
	recorded := recordedArgv(template, in)
	toRun, err := execArgv(template, in, base, index)
	if err != nil {
		return Trial{Input: in.Input, Argv: recorded, Stdout: "", Exit: -1}
	}
	r := runSurface(ctx, toRun, bundleRoot)
	exit := -1
	if r.ran {
		exit = r.exit
	}
	return Trial{Input: in.Input, Argv: recorded, Stdout: r.stdout, Exit: exit}
}

// recordedArgv is the reproducible argv stored in the transcript: the declared
// template with in.Args appended and the "{given}" placeholder left intact.
// Argv[0] is the surface run target, straight from the template — never the
// model's.
func recordedArgv(template []string, in proposal) []string {
	argv := make([]string, 0, len(template)+len(in.Args))
	argv = append(argv, template...)
	return append(argv, in.Args...)
}

// execArgv expands the surface template for one proposed input for ACTUAL
// execution: each "{given}" becomes the path of a temp file holding in.Input,
// then in.Args are appended. Argv[0] — the surface run target — is copied
// straight from the template and is never taken from the model.
func execArgv(template []string, in proposal, base string, index int) (argv []string, err error) {
	var givenPath string
	if takesFile(template) {
		givenPath = filepath.Join(base, fmt.Sprintf("input-%d", index))
		if err = os.WriteFile(givenPath, []byte(in.Input), 0o600); err != nil {
			return nil, fmt.Errorf("purposetrial: writing a proposed input file: %w", err)
		}
	}
	argv = make([]string, 0, len(template)+len(in.Args))
	for _, tok := range template {
		if tok == "{given}" {
			argv = append(argv, givenPath)
			continue
		}
		argv = append(argv, tok)
	}
	return append(argv, in.Args...), nil
}

// judge asks the model, over the purpose and the REAL outputs the surface
// produced, whether the surface genuinely serves its purpose, and transcribes
// the answer verbatim. The harness parses the model's own served/reasoning for
// structure but keeps the whole reply in Raw; it decides nothing itself.
func judge(ctx context.Context, model Model, spec *acceptance.Spec, trials []Trial) (judgement Judgement, err error) {
	resp, err := model.Complete(ctx, judgePrompt(spec, trials))
	if err != nil {
		return Judgement{}, err
	}
	served, reasoning := parseJudgement(resp)
	return Judgement{Served: served, Reasoning: reasoning, Raw: resp}, nil
}

// judgePrompt builds the judgement prompt: the purpose, and for each trial the
// input, the argv and the surface's real stdout and exit.
func judgePrompt(spec *acceptance.Spec, trials []Trial) string {
	var b strings.Builder
	b.WriteString("You proposed inputs for a command-line extension and here are the REAL results.\n\n")
	b.WriteString("Purpose: ")
	b.WriteString(spec.Purpose.Sentence)
	b.WriteString("\n\n")
	for i, t := range trials {
		fmt.Fprintf(&b, "Case %d:\n", i+1)
		fmt.Fprintf(&b, "  argv: %s\n", strings.Join(t.Argv, " "))
		fmt.Fprintf(&b, "  input: %q\n", t.Input)
		fmt.Fprintf(&b, "  stdout: %q\n", t.Stdout)
		fmt.Fprintf(&b, "  exit: %d\n", t.Exit)
	}
	b.WriteString("\nDoes this extension genuinely do what its purpose says? ")
	b.WriteString(`Answer with ONLY a JSON object {"served": true|false, "reasoning": "<one or two sentences>"}.`)
	return b.String()
}

// parseJudgement extracts the model's own served flag and reasoning from its
// reply, leniently. A reply the harness cannot parse yields served=false and an
// empty reasoning — the full reply is kept verbatim in Raw regardless, so no
// judgement is lost.
func parseJudgement(resp string) (served bool, reasoning string) {
	raw := firstJSONObject(resp)
	if raw == "" {
		return false, ""
	}
	var parsed struct {
		Served    bool   `json:"served"`
		Reasoning string `json:"reasoning"`
	}
	if err := json.Unmarshal([]byte(raw), &parsed); err != nil {
		return false, ""
	}
	return parsed.Served, parsed.Reasoning
}

// firstJSONObject returns the first brace-balanced JSON object substring in s,
// or "" if there is none.
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
// <bundleRoot>/trial/trial-record.json, creating the trial dir. It returns the
// path it wrote to.
func writeRecord(record *TrialRecord, bundleRoot string) (path string, err error) {
	dir := filepath.Join(bundleRoot, "trial")
	if err = os.MkdirAll(dir, 0o755); err != nil {
		return "", fmt.Errorf("purposetrial: making the trial dir: %w", err)
	}
	path = filepath.Join(dir, "trial-record.json")
	data, err := json.MarshalIndent(record, "", "  ")
	if err != nil {
		return path, fmt.Errorf("purposetrial: marshalling the transcript: %w", err)
	}
	if err = os.WriteFile(path, append(data, '\n'), 0o644); err != nil {
		return path, fmt.Errorf("purposetrial: writing the transcript: %w", err)
	}
	return path, nil
}

// --- sandbox -----------------------------------------------------------------
//
// The sandbox mirrors internal/acceptance/run.go. That shim's surface runner is
// unexported (runSurface/scrubbedEnv/capWriter are package-private), so it
// cannot be imported; the logic below is a faithful copy of the same
// scrubbed-env, per-run-timeout, capped-output approach. If that runner is ever
// exported, this block should call it instead of duplicating it.

// surfaceRun is one observation of the surface: its captured stdout, exit
// status, whether the capture was truncated, and whether it ran to a clean exit.
type surfaceRun struct {
	stdout    string
	exit      int
	truncated bool
	ran       bool
}

// runSurface runs the already-expanded argv inside the bundle, under a scrubbed
// environment, a per-run timeout and a capped stdout. Argv[0] is the bundle's
// declared surface run target, relative to bundleRoot. It never returns an
// error: an un-runnable surface is an observation (ran=false).
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

// scrubbedEnv is the environment a surface runs under: PATH, LC_ALL=C and
// LANG=C, and nothing else. Inherited credentials, tokens and proxy settings are
// dropped so a surface cannot reach the operator's secrets or the network. PATH
// is carried so the surface's own interpreters (a shell, awk) still resolve;
// PATH is not a credential. Copied from internal/acceptance/run.go.
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
// never surprised by a short write. Copied from internal/acceptance/run.go.
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
