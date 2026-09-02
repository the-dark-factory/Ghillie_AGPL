package admit

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
)

// FrontName is the file name of the proven decider's command-line front.
// It is deliberately ecosystem-neutral: the same binary decides for any host
// that can establish the six facts, and nothing in it knows what ghillie is.
const FrontName = "extension_admission_front"

// FrontEnv names the environment variable an owner can set to point at a
// particular front binary — the same shape as GHILLIE_PROVER.
const FrontEnv = "GHILLIE_ADMISSION_FRONT"

// decideTimeout bounds the front. It reads no file and touches no network,
// so it returns in microseconds; anything approaching this bound means the
// binary is not the binary we think it is, and a timeout is a refusal.
const decideTimeout = 5 * time.Second

// ErrNoFront is returned when the proven decider cannot be found. It is
// deliberately an error and not a silent pass: a gate that quietly stops
// gating when its decider goes missing is worse than no gate, because the
// ledger goes on looking the same.
var ErrNoFront = errors.New("admit: the proven decider is not here")

// FindFront locates the front binary: the owner's explicit choice first,
// then beside the ghillie executable, then the PATH.
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

// Decide runs the proven front over the established facts and returns its
// verdict.
//
// ★ EVERYTHING THAT IS NOT AN ADMIT IS A REFUSAL. Only exit status 0 with
// the literal word "admit" alone on stdout admits. Exit 1, exit 2, a signal,
// a timeout, an unreadable binary, an empty stdout, extra output and a word
// this package does not recognise all return a refusal. This is the single
// most likely place for a well-meaning integrator to open the door, which is
// why it is a named test rather than a code comment.
//
// The returned reason is always populated: for a real verdict it is the
// front's own word, and for an undecided outcome it says what went wrong.
func Decide(ctx context.Context, f Facts) (verdict Verdict, reason string, err error) {
	front, err := FindFront()
	if err != nil {
		return RefuseUndecided, err.Error(), err
	}
	return DecideWith(ctx, front, f)
}

// DecideWith is Decide against a named front binary. It exists so that tests
// can drive a stub, and so that an operator can point at a front they built
// and truth-tabled themselves.
func DecideWith(ctx context.Context, front string, f Facts) (verdict Verdict, reason string, err error) {
	runCtx, cancel := context.WithTimeout(ctx, decideTimeout)
	defer cancel()

	cmd := exec.CommandContext(runCtx, front, f.Args()...)
	var stderr strings.Builder
	cmd.Stderr = &stderr
	out, runErr := cmd.Output()
	word := strings.TrimSpace(string(out))

	// The timeout first: a front that did not return did not decide.
	if runCtx.Err() != nil {
		return RefuseUndecided, fmt.Sprintf("the decider did not return within %s — refusing", decideTimeout),
			fmt.Errorf("admit: decider timed out after %s", decideTimeout)
	}

	code := -1
	if cmd.ProcessState != nil {
		code = cmd.ProcessState.ExitCode()
	}

	switch {
	case code == 0 && word == "admit":
		return Admit, "admit", nil
	case code == 0:
		// Exit 0 with anything other than the one word is NOT an admit. A
		// front that succeeds while saying something else is a front we do
		// not understand.
		return RefuseUndecided,
			fmt.Sprintf("the decider exited 0 but said %q, not \"admit\" — refusing", word),
			fmt.Errorf("admit: decider exited 0 with unrecognised output %q", word)
	case code == 1:
		if v, ok := verdictOfWord(word); ok && v != Admit {
			return v, word, nil
		}
		return RefuseUndecided,
			fmt.Sprintf("the decider refused with an unrecognised word %q — refusing anyway", word),
			nil
	case code == 2:
		return RefuseUndecided,
			fmt.Sprintf("the decider rejected the facts as malformed: %s", strings.TrimSpace(stderr.String())),
			fmt.Errorf("admit: the decider rejected the tuple as malformed (exit 2): %s", strings.TrimSpace(stderr.String()))
	default:
		// A signal, an exec failure, an unreadable status. All the same side.
		detail := strings.TrimSpace(stderr.String())
		if runErr != nil && detail == "" {
			detail = runErr.Error()
		}
		return RefuseUndecided,
			fmt.Sprintf("the decider did not answer (exit %d): %s", code, detail),
			fmt.Errorf("admit: the decider did not answer (exit %d): %s", code, detail)
	}
}

// verdictOfWord maps the front's word back to a verdict. An unknown word is
// not a verdict, and the caller refuses on it.
func verdictOfWord(word string) (Verdict, bool) {
	for v, w := range verdictWords {
		if w == word && v != RefuseUndecided {
			return v, true
		}
	}
	return RefuseUndecided, false
}
