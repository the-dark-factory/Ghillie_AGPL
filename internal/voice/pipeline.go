package voice

// pipeline.go is the one exec boundary to the settled breath pipeline. The
// pattern is voice-relay's WhisperCLI (internal/transcribe): the external
// program is a fixed dependency, nothing here builds or vendors it, the exact
// argument list is exported so a failed render can be reproduced by hand, and
// a failure is an explicit error carrying stderr — never a silent fallback.

import (
	"bytes"
	"context"
	"crypto/sha1"
	"encoding/hex"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync/atomic"
	"time"
)

// Rendering is one utterance rendered to disk: the WAV, its plan sidecar, and
// the honest wall-clock cost of producing it.
type Rendering struct {
	// WavPath is the rendered speech, engine-scheduled breaths spliced in.
	WavPath string

	// PlanPath is the JSON sidecar recording what the engine decided and what
	// the harness did about it. It has been validated: every breath in it
	// carries an engine verdict.
	PlanPath string

	// Audio is the duration of the rendered audio itself.
	Audio time.Duration

	// Wall is how long the render took, measured, start to finish. Kokoro is
	// not a daemon yet: expect seconds, and claim nothing.
	Wall time.Duration

	// Breaths is how many engine-scheduled breaths the utterance carries.
	Breaths int
}

// TextPlan renders speech by invoking respire's textplan.py — the
// reconstructed harness of the approved run (harness_rev breath-4-textplan-r2,
// commit 34c227c in respire).
//
// The zero value is not usable; at minimum PythonPath, ScriptPath and
// RenderDir must be set.
type TextPlan struct {
	// PythonPath is the interpreter of the pipeline's own venv
	// (respire/.venv-kokoro/bin/python) — the harness's dependencies live
	// there and nowhere else.
	PythonPath string

	// ScriptPath is textplan.py itself.
	ScriptPath string

	// KokoroRoot is where the Kokoro weights live. Empty uses the harness's
	// own default (a durable models dir under the home — never /tmp).
	KokoroRoot string

	// RenderDir is where WAVs and sidecars land. It must live under the
	// ghillie state dir (or another durable, backed-up place) — NEVER /tmp:
	// a /tmp evaporation is what destroyed the original harness and the
	// original breath bank.
	RenderDir string

	// Env holds additional environment variables, each "KEY=VALUE", applied
	// on top of the parent environment — the harness's TP_* knobs
	// (TP_VOICE, TP_EMO, …) belong here when the defaults are wrong.
	Env []string

	// PiperPath + PiperModel, when BOTH set, put a bespoke Piper voice in
	// front of the harness: piper renders the speech, and textplan.py plans
	// and splices breath OVER it via its own TP_SRC_WAV door — the full
	// trimmings on a fine-tuned voice, no harness change. This is how
	// ghillie speaks SCOTS (the scottish.onnx fine-tune, named by
	// -scots-model or found under the ghillie home) instead of the
	// stock-RP placeholder
	// (feedback_ghillie_voice_must_be_scottish).
	//
	// ⚠ THE DEVIL-VOICE GUARD: the model's SIBLING CONFIG scottish.la.json
	// is the Summoner demon (Vulgate-Latin espeak), NOT ghillie. Only the
	// model path is ever passed — piper defaults to <model>.json — and a
	// PiperModel naming a .la.json (or any explicit config) is refused.
	PiperPath  string
	PiperModel string

	seq atomic.Uint64
}

// Args returns the textplan.py argument list for the given output path. It is
// exported so the exact command can be logged and reproduced by hand; the text
// itself travels in the TP_TEXT environment variable (the harness's documented
// idiom), so reproduction is:
//
//	TP_TEXT='<text>' KOKORO_ROOT=<root> <python> <script> <out.wav>
func (t *TextPlan) Args(outWav string) []string {
	return []string{t.ScriptPath, outWav}
}

// validate reports whether the renderer has the paths it needs.
func (t *TextPlan) validate() error {
	if t.PythonPath == "" {
		return fmt.Errorf("%w: python path is empty", ErrNotConfigured)
	}
	if t.ScriptPath == "" {
		return fmt.Errorf("%w: textplan.py path is empty", ErrNotConfigured)
	}
	if t.RenderDir == "" {
		return fmt.Errorf("%w: render dir is empty", ErrNotConfigured)
	}
	if strings.HasPrefix(filepath.Clean(t.RenderDir), "/tmp/") ||
		strings.HasPrefix(filepath.Clean(t.RenderDir), "/private/tmp/") {
		return fmt.Errorf("%w: render dir %s is under /tmp — generated assets never go there (standing rule; /tmp took the original harness and the breath bank)", ErrNotConfigured, t.RenderDir)
	}
	return nil
}

// Render runs the pipeline over one utterance and returns the validated
// result. A failure of any kind — process, missing output, a sidecar the
// engine does not vouch for — is an explicit error; the caller decides,
// loudly, what a voiceless run does next.
func (t *TextPlan) Render(ctx context.Context, text string) (r Rendering, err error) {
	if err := t.validate(); err != nil {
		return Rendering{}, err
	}
	if strings.TrimSpace(text) == "" {
		return Rendering{}, fmt.Errorf("%w: empty utterance", ErrRenderFailed)
	}
	if err := os.MkdirAll(t.RenderDir, 0o755); err != nil {
		return Rendering{}, fmt.Errorf("%w: create render dir: %w", ErrRenderFailed, err)
	}

	// One file per utterance WITHIN A RUN: a monotonic sequence number plus a
	// content hash, so re-putting the same question in one interview renders
	// afresh under a new name. Honest limit, stated rather than hidden: the
	// sequence restarts with the process, so a NEW run that speaks an
	// identical utterance re-renders over the old file. Cross-run retention
	// is the evidence directory's job, not this counter's.
	sum := sha1.Sum([]byte(text))
	out := filepath.Join(t.RenderDir,
		fmt.Sprintf("utt-%03d-%s.wav", t.seq.Add(1), hex.EncodeToString(sum[:])[:12]))

	// The bespoke-voice pre-step: piper renders the speech; the harness then
	// dresses it (breath, loudnorm, sidecar) via TP_SRC_WAV.
	var srcWav string
	if t.PiperPath != "" && t.PiperModel != "" {
		if strings.HasSuffix(t.PiperModel, ".json") {
			return Rendering{}, fmt.Errorf("%w: PiperModel %s is a CONFIG, not a model — configs are never passed (the demon lives in one)", ErrNotConfigured, t.PiperModel)
		}
		srcWav = strings.TrimSuffix(out, ".wav") + ".speech.wav"
		pcmd := exec.CommandContext(ctx, t.PiperPath,
			"--model", t.PiperModel, "--output_file", srcWav)
		pcmd.Stdin = strings.NewReader(text)
		var perr bytes.Buffer
		pcmd.Stderr = &perr
		if err := pcmd.Run(); err != nil {
			return Rendering{}, fmt.Errorf("%w: piper %s --model %s: %w (stderr: %s)",
				ErrRenderFailed, t.PiperPath, t.PiperModel, err, tail(perr.String(), 500))
		}
		if info, err := os.Stat(srcWav); err != nil || info.Size() <= wavHeaderSize {
			return Rendering{}, fmt.Errorf("%w: piper exited clean but wrote no speech at %s", ErrRenderFailed, srcWav)
		}
	}

	args := t.Args(out)
	cmd := exec.CommandContext(ctx, t.PythonPath, args...)
	cmd.Env = append(os.Environ(), "TP_TEXT="+text)
	if t.KokoroRoot != "" {
		cmd.Env = append(cmd.Env, "KOKORO_ROOT="+t.KokoroRoot)
	}
	if srcWav != "" {
		cmd.Env = append(cmd.Env, "TP_SRC_WAV="+srcWav)
	}
	cmd.Env = append(cmd.Env, t.Env...)

	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	start := time.Now()
	runErr := cmd.Run()
	wall := time.Since(start)
	if runErr != nil {
		return Rendering{}, fmt.Errorf("%w: TP_TEXT=%q %s %s: %w (stderr: %s)",
			ErrRenderFailed, text, t.PythonPath, strings.Join(args, " "),
			runErr, tail(stderr.String(), 500))
	}

	info, statErr := os.Stat(out)
	if statErr != nil {
		return Rendering{}, fmt.Errorf("%w: render exited clean but wrote no WAV at %s: %w", ErrRenderFailed, out, statErr)
	}
	if info.Size() <= wavHeaderSize {
		return Rendering{}, fmt.Errorf("%w: rendered WAV %s is %d bytes — a header with no audio", ErrRenderFailed, out, info.Size())
	}

	planPath := out + ".json"
	plan, err := ReadPlan(planPath)
	if err != nil {
		return Rendering{}, err
	}
	// ★ The engine vouches for every breath, or the render is refused whole.
	if err := plan.Validate(); err != nil {
		return Rendering{}, err
	}

	return Rendering{
		WavPath:  out,
		PlanPath: planPath,
		Audio:    time.Duration(plan.DurationMS) * time.Millisecond,
		Wall:     wall,
		Breaths:  len(plan.Breaths),
	}, nil
}

// tail returns at most n trailing bytes of s, for error messages that carry
// evidence without carrying a transcript.
func tail(s string, n int) string {
	s = strings.TrimSpace(s)
	if len(s) <= n {
		return s
	}
	return "…" + s[len(s)-n:]
}
