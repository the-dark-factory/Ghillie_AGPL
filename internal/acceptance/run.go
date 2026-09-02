package acceptance

// run.go is the acceptance SHIM — the glue half of the extension publish gate.
//
// spec.go and parse.go answer "is this acceptance statement well-formed?". This
// file answers a different, later question: "does a bundle's own surface behave
// the way its statement says?". It does so the way internal/admit does — it
// ESTABLISHES facts and DELEGATES the decision to a proven core reached through
// a thin exec-front. It runs the surface, compares the run against the declared
// expectation, and from that establishes six booleans per check. It then hands
// every check's six booleans to the proven acceptance core's front and relays
// the core's answer word verbatim.
//
// ★ THE SHIM DECIDES NOTHING. It performs no fold over the booleans, applies no
// Check_Satisfied rule, and never concludes for itself that a surface is good.
// Those belong to the proven core (an Ada main over Acceptance_Verdict_Pkg,
// ledger core acceptance_verdict_pkg). The Go side establishes facts and asks;
// the core answers. When the core cannot be reached or answers oddly, the shim
// reports "cannot decide — nothing published", which is never an acceptance.
//
// Wire contract to the front is documented in docs/ACCEPTANCE_FRONT.md and,
// briefly, at payload below.

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"time"
)

// FrontName is the file name of the proven acceptance core's command-line
// front. It is ecosystem-neutral in exactly the way internal/admit's front is:
// the same binary answers for any host that can establish the six booleans per
// check, and nothing in it knows what ghillie is.
const FrontName = "acceptance_verdict_front"

// FrontEnv names the environment variable an owner can set to point at a
// particular front binary — the same shape as GHILLIE_ADMISSION_FRONT.
const FrontEnv = "GHILLIE_ACCEPTANCE_FRONT"

// decideTimeout bounds the front. It reads no file and touches no network, so
// it returns in microseconds; anything approaching this bound means the binary
// is not the binary we think it is, and a timeout is treated as no answer.
const decideTimeout = 5 * time.Second

// maxSurfaceOutput caps how much of a surface's stdout the shim keeps. An
// acceptance expectation is a short string, not a payload; a surface that emits
// more than this has its stdout marked truncated, which makes an exact-match
// fact impossible to establish (and so leaves it unmet).
const maxSurfaceOutput = 1 << 20

// surfaceTimeout bounds one surface run. It is a var, not a const, only so that
// tests can shrink it; nothing in the package changes it at runtime.
var surfaceTimeout = 30 * time.Second

// The two answer words the proven core prints. They are the whole of the core's
// output vocabulary in this contract: "accepted" on exit 0 when the surface
// satisfies every declared check, "refused" on exit 1 when the fold says it does
// not. Any other shape is not an answer this shim understands, and it publishes
// nothing on it.
const (
	answerAccepted = "accepted"
	answerRefused  = "refused"
)

// ErrNoFront is returned when the proven acceptance core's front cannot be
// found. It is deliberately an error and not a silent acceptance: a gate that
// quietly stops gating when its decider goes missing is worse than no gate.
var ErrNoFront = errors.New("acceptance: the proven acceptance core's front is not here")

// CheckFacts is the six-boolean fact tuple the shim establishes for one check.
// Every field is a fact established here by running the surface and comparing it
// to the declared expectation — none is a conclusion. "Required" says the
// statement asked for that arm; "Met" says the observed run satisfied it. A
// required arm that the run did not satisfy — and every arm when the run could
// not be observed at all — leaves the matching Met false.
type CheckFacts struct {
	// Name is the check's kebab identifier, carried through for rendering.
	Name string
	// ExactRequired is true when the statement declares a stdout_exact arm.
	ExactRequired bool
	// ExactMet is true only when ExactRequired and the observed stdout equals it.
	ExactMet bool
	// PatternRequired is true when the statement declares a stdout_matches arm.
	PatternRequired bool
	// PatternMet is true only when PatternRequired and the RE2 matched stdout.
	PatternMet bool
	// ExitRequired is true when the statement declares an exit_code arm.
	ExitRequired bool
	// ExitMet is true only when ExitRequired and the observed exit equalled it.
	ExitMet bool
}

// Tokens renders the six facts as the six position-distinct words the front
// expects, in a fixed order. The words differ per position on purpose: a tuple
// with two arguments transposed names a word invalid in that slot, so the front
// rejects it (exit 2 → no answer) rather than silently deciding a different
// question. This mirrors the floor_ prefixing in internal/admit.
func (c CheckFacts) Tokens() []string {
	tok := func(b bool, yes, no string) string {
		if b {
			return yes
		}
		return no
	}
	return []string{
		tok(c.ExactRequired, "exact_required", "exact_absent"),
		tok(c.ExactMet, "exact_met", "exact_unmet"),
		tok(c.PatternRequired, "pattern_required", "pattern_absent"),
		tok(c.PatternMet, "pattern_met", "pattern_unmet"),
		tok(c.ExitRequired, "exit_required", "exit_absent"),
		tok(c.ExitMet, "exit_met", "exit_unmet"),
	}
}

// Line is the check's tuple as one wire line: six space-separated tokens.
func (c CheckFacts) Line() string { return strings.Join(c.Tokens(), " ") }

// Result is what the caller renders. It carries the per-check established facts
// and, verbatim, the core's answer word. The shim contributes no judgement of
// its own beyond whether it managed to obtain an answer at all.
type Result struct {
	// Checks are the per-check facts the shim established, in statement order.
	Checks []CheckFacts
	// Answered is true only when the proven core returned a recognised answer.
	// It is false — fail-closed — whenever the front was absent, unwired, timed
	// out, was signalled, or spoke a shape this shim does not understand.
	Answered bool
	// Decision is the core's answer word, verbatim ("accepted" or "refused"),
	// or empty when Answered is false. The shim never writes this field from its
	// own reading of the booleans; only the core's stdout sets it.
	Decision string
	// Reason is a human-readable line: the core's answer restated, or, when the
	// shim could not obtain one, why — always opening "cannot decide — nothing
	// published".
	Reason string
}

// FindFront locates the front binary: the owner's explicit choice first, then
// beside the ghillie executable, then the PATH. It is the same resolution
// internal/admit uses.
func FindFront() (path string, err error) {
	if p := os.Getenv(FrontEnv); p != "" {
		info, statErr := os.Stat(p)
		if statErr != nil {
			return "", fmt.Errorf("%w: %s points at %s but nothing is there", ErrNoFront, FrontEnv, p)
		}
		if info.IsDir() || info.Mode()&0o111 == 0 {
			return "", fmt.Errorf("%w: %s points at %s, which is not an executable file", ErrNoFront, FrontEnv, p)
		}
		return p, nil
	}
	if exe, exeErr := os.Executable(); exeErr == nil {
		beside := filepath.Join(filepath.Dir(exe), FrontName)
		if info, statErr := os.Stat(beside); statErr == nil && !info.IsDir() && info.Mode()&0o111 != 0 {
			return beside, nil
		}
	}
	if p, lookErr := exec.LookPath(FrontName); lookErr == nil {
		return p, nil
	}
	return "", fmt.Errorf("%w: no %s beside the ghillie binary, on PATH, or named by %s", ErrNoFront, FrontName, FrontEnv)
}

// Establish runs each check's surface against the bundle's own tree and returns
// the six-boolean fact tuple for every check, in statement order. It reaches no
// conclusion: a surface that errors, times out or is signalled simply leaves
// that check's Met flags false, and is never a panic and never an error here.
// The returned error is reserved for a misuse the caller can fix — a nil spec,
// an empty bundle root, or a cancelled context — not for a badly-behaved
// surface, which is a fact to establish rather than a failure to report.
func Establish(ctx context.Context, spec *Spec, bundleRoot string) (checks []CheckFacts, err error) {
	if spec == nil {
		return nil, errors.New("acceptance: no spec to establish facts from")
	}
	if bundleRoot == "" {
		return nil, errors.New("acceptance: no bundle root to run the surface in")
	}
	checks = make([]CheckFacts, 0, len(spec.Checks))
	for i := range spec.Checks {
		if ctxErr := ctx.Err(); ctxErr != nil {
			return nil, fmt.Errorf("acceptance: %w", ctxErr)
		}
		checks = append(checks, establishOne(ctx, &spec.Checks[i], bundleRoot))
	}
	return checks, nil
}

// establishOne establishes the six facts for one check. The Required flags come
// straight from the declared expectation; the Met flags come only from an
// observed run. When the run could not be observed as a clean exit, every Met
// flag stays false — the fail-closed default.
func establishOne(ctx context.Context, chk *Check, bundleRoot string) CheckFacts {
	f := CheckFacts{
		Name:            chk.Name,
		ExactRequired:   chk.Expect.StdoutExact != nil,
		PatternRequired: chk.Expect.StdoutMatches != "",
		ExitRequired:    chk.Expect.ExitCode != nil,
	}
	r := runSurface(ctx, chk, bundleRoot)
	if !r.ran {
		return f
	}
	if f.ExactRequired {
		f.ExactMet = !r.truncated && r.stdout == *chk.Expect.StdoutExact
	}
	if f.PatternRequired {
		re, compileErr := regexp.Compile(chk.Expect.StdoutMatches)
		if compileErr == nil {
			f.PatternMet = re.MatchString(r.stdout)
		}
	}
	if f.ExitRequired {
		f.ExitMet = r.exit == *chk.Expect.ExitCode
	}
	return f
}

// surfaceRun is one observation of a surface: its captured stdout, its exit
// status, whether the capture was truncated, and whether it ran to a clean exit
// at all. ran is false for a start failure, a timeout or a signal — none of
// which is a clean observation of behaviour.
type surfaceRun struct {
	stdout    string
	exit      int
	truncated bool
	ran       bool
}

// runSurface expands the check's argv and runs it inside the bundle, under a
// scrubbed environment, a per-run timeout and a capped stdout. It never returns
// an error: an un-runnable surface is an observation (ran=false), not a failure.
func runSurface(ctx context.Context, chk *Check, bundleRoot string) surfaceRun {
	argv := expandArgv(chk)
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
		// Timed out or cancelled: not a clean observation.
		return surfaceRun{stdout: cap.String(), truncated: cap.truncated}
	case cmd.ProcessState == nil:
		// Never started — e.g. the surface path does not exist. runErr says why.
		return surfaceRun{stdout: cap.String(), truncated: cap.truncated}
	case !cmd.ProcessState.Exited():
		// Terminated by a signal.
		return surfaceRun{stdout: cap.String(), truncated: cap.truncated}
	}

	// A non-zero exit is a legitimate observation, so an *exec.ExitError here is
	// expected and not a failure of the shim. Any other exec error is not, and
	// is treated fail-closed.
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

// expandArgv builds the argv for a check: each "{given}" token becomes
// Given.File, then Given.Args are appended. Argv[0] is the program, run
// relative to the bundle root. Path safety of the tokens is parse.go's job.
func expandArgv(chk *Check) []string {
	argv := make([]string, 0, len(chk.Run)+len(chk.Given.Args))
	for _, tok := range chk.Run {
		if tok == "{given}" {
			argv = append(argv, chk.Given.File)
			continue
		}
		argv = append(argv, tok)
	}
	return append(argv, chk.Given.Args...)
}

// scrubbedEnv is the environment a surface runs under: PATH, LC_ALL=C and
// LANG=C, and nothing else. Inherited credentials, tokens and proxy settings
// are dropped so a surface cannot reach the operator's secrets or the network's
// egress config. PATH's value is carried through so the surface's own
// interpreters (a shell, awk) are still found; PATH is not a credential.
func scrubbedEnv() []string {
	env := []string{"LC_ALL=C", "LANG=C"}
	if p := os.Getenv("PATH"); p != "" {
		env = append(env, "PATH="+p)
	} else {
		env = append(env, "PATH=/usr/bin:/bin:/usr/sbin:/sbin")
	}
	return env
}

// capWriter keeps at most limit bytes and records whether it had to drop any.
// It always reports the whole write as accepted so the child keeps writing and
// is never surprised by a short write or a broken pipe.
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

// Decide establishes the facts for a bundle and puts them to the proven core's
// front, returning the core's answer. It is the whole pipe: Establish, then find
// the front, then DecideWith. When the front cannot be found the result is
// fail-closed — Answered false, Decision empty — and the error is returned so a
// caller can say why nothing was published.
func Decide(ctx context.Context, spec *Spec, bundleRoot string) (result Result, err error) {
	checks, err := Establish(ctx, spec, bundleRoot)
	if err != nil {
		return Result{Reason: "cannot decide — nothing published: " + err.Error()}, err
	}
	front, err := FindFront()
	if err != nil {
		return Result{
			Checks: checks,
			Reason: "cannot decide — nothing published: " + err.Error(),
		}, err
	}
	return DecideWith(ctx, front, checks)
}

// DecideWith puts already-established facts to a named front and relays its
// answer. It exists so tests can drive a test-double front, and so an operator
// can point at a core they built and truth-tabled themselves.
//
// ★ EVERYTHING THAT IS NOT A CLEAN ANSWER PUBLISHES NOTHING. Only exit 0 with
// the single word "accepted" is an acceptance; only exit 1 with the single word
// "refused" is a decided refusal. A missing front, a timeout, a signal, exit 2
// (the core rejected the tuples), an unrecognised word and any other status all
// resolve to Answered false with a "cannot decide — nothing published" reason.
func DecideWith(ctx context.Context, front string, checks []CheckFacts) (result Result, err error) {
	result = Result{Checks: checks}
	if len(checks) == 0 {
		result.Reason = "cannot decide — nothing published: no established facts to put to the core"
		return result, errors.New("acceptance: no checks to decide over")
	}

	runCtx, cancel := context.WithTimeout(ctx, decideTimeout)
	defer cancel()

	cmd := exec.CommandContext(runCtx, front)
	cmd.Stdin = strings.NewReader(payload(checks))
	var stderr strings.Builder
	cmd.Stderr = &stderr
	out, runErr := cmd.Output()
	word := strings.TrimSpace(string(out))

	if runCtx.Err() != nil {
		result.Reason = fmt.Sprintf("cannot decide — nothing published: the core did not answer within %s", decideTimeout)
		return result, fmt.Errorf("acceptance: the core timed out after %s", decideTimeout)
	}

	code := -1
	if cmd.ProcessState != nil {
		code = cmd.ProcessState.ExitCode()
	}

	switch {
	case code == 0 && word == answerAccepted:
		result.Answered = true
		result.Decision = answerAccepted
		result.Reason = "the core's answer: the surface satisfies its acceptance statement"
		return result, nil
	case code == 1 && word == answerRefused:
		result.Answered = true
		result.Decision = answerRefused
		result.Reason = "the core's answer: the surface does not satisfy its acceptance statement"
		return result, nil
	case code == 0:
		result.Reason = fmt.Sprintf("cannot decide — nothing published: the core exited 0 but said %q, not %q", word, answerAccepted)
		return result, fmt.Errorf("acceptance: the core exited 0 with unrecognised output %q", word)
	case code == 2:
		result.Reason = "cannot decide — nothing published: the core rejected the tuples as malformed: " + strings.TrimSpace(stderr.String())
		return result, fmt.Errorf("acceptance: the core rejected the tuples as malformed (exit 2): %s", strings.TrimSpace(stderr.String()))
	default:
		detail := strings.TrimSpace(stderr.String())
		if runErr != nil && detail == "" {
			detail = runErr.Error()
		}
		result.Reason = fmt.Sprintf("cannot decide — nothing published: the core did not answer (exit %d): %s", code, detail)
		return result, fmt.Errorf("acceptance: the core did not answer (exit %d): %s", code, detail)
	}
}

// payload renders the wire the front reads on stdin: one line per check, in
// statement order, each line the six space-separated tokens of CheckFacts. The
// count of checks is implicit in the count of lines; the front must not reorder
// them. See docs/ACCEPTANCE_FRONT.md for the full contract.
func payload(checks []CheckFacts) string {
	var b strings.Builder
	for _, c := range checks {
		b.WriteString(c.Line())
		b.WriteByte('\n')
	}
	return b.String()
}
