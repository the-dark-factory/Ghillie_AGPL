//go:build darwin

package ears

// The ACOUSTIC LOOP: the honest live test, no human needed. A known sentence
// is rendered through the settled textplan pipeline, played through THIS
// machine's speakers with afplay while this package records from the REAL
// microphone, transcribed by the resident whisper, and compared — normalised
// word error rate, reported as measured. Run three times, because one lucky
// pass is an anecdote.
//
// Guarded twice over because it makes noise and needs a quiet room:
//
//	GHILLIE_EARS_LIVE=1 \
//	GHILLIE_EARS_LIVE_DIR=~/ObVault/ghillie-voice/wu-runs/<run>/ \
//	GHILLIE_VOICE_PIPELINE_DIR=<the checkout holding .venv-kokoro and demo/textplan.py> \
//	go test -run TestLiveAcousticLoop -v ./internal/ears
//
// If macOS refuses the microphone (TCC), the capture error NAMES the
// permission and where to grant it — that is the result of the run then,
// not something to paper over.

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/tonygair/ghillie/internal/voice"
)

// loopSentence is the known text. Plain declarative English, long enough
// that a lucky partial match cannot score well.
const loopSentence = "The greenhouse watering system must never run while the door is standing open."

// TestLiveAcousticLoop runs the loop three times and reports each WER.
func TestLiveAcousticLoop(t *testing.T) {
	if os.Getenv("GHILLIE_EARS_LIVE") != "1" {
		t.Skip("acoustic loop: set GHILLIE_EARS_LIVE=1 (and GHILLIE_EARS_LIVE_DIR) to run speakers-to-mic for real")
	}
	evidenceDir := os.Getenv("GHILLIE_EARS_LIVE_DIR")
	if evidenceDir == "" {
		t.Fatal("acoustic loop: GHILLIE_EARS_LIVE_DIR must name a durable evidence directory — never /tmp")
	}
	if err := os.MkdirAll(evidenceDir, 0o755); err != nil {
		t.Fatalf("evidence dir: %v", err)
	}
	pipelineDir := os.Getenv("GHILLIE_VOICE_PIPELINE_DIR")
	if pipelineDir == "" {
		t.Fatal("acoustic loop: GHILLIE_VOICE_PIPELINE_DIR must name the checkout holding .venv-kokoro and demo/textplan.py")
	}

	ctx := context.Background()

	// Render the known sentence through the settled pipeline once; all three
	// runs play the same WAV, so the loop measures the AIR PATH, not render
	// variance.
	renderer := &voice.TextPlan{
		PythonPath: filepath.Join(pipelineDir, ".venv-kokoro", "bin", "python"),
		ScriptPath: filepath.Join(pipelineDir, "demo", "textplan.py"),
		RenderDir:  evidenceDir,
	}
	rendering, err := renderer.Render(ctx, loopSentence)
	if err != nil {
		t.Fatalf("render through the settled pipeline: %v", err)
	}

	// The resident whisper: attach to one already warm, or start our own for
	// the duration of the test.
	resident, err := StartResident(ctx, ResidentConfig{
		URL:       "http://127.0.0.1:8932",
		ModelPath: filepath.Join(os.Getenv("HOME"), "models", "whisper", "ggml-base.en.bin"),
	})
	if err != nil {
		t.Fatalf("resident whisper: %v", err)
	}
	defer func() {
		if cerr := resident.Close(); cerr != nil {
			t.Logf("resident close: %v", cerr)
		}
	}()
	transcriber := &WhisperServer{URL: resident.URL}

	capture := &Capture{
		CaptureDir: evidenceDir,
		MaxTurn:    30 * time.Second, // the sentence is ~6 s; 120 s would only stretch a failure
	}

	var evidence strings.Builder
	fmt.Fprintf(&evidence, "ghillie ears slice — ACOUSTIC LOOP, %s\n", time.Now().UTC().Format(time.RFC3339))
	fmt.Fprintf(&evidence, "sentence: %q\n", loopSentence)
	fmt.Fprintf(&evidence, "rendered: %s (audio %s)\n", rendering.WavPath, rendering.Audio)
	fmt.Fprintf(&evidence, "path: afplay → the room's air → the default microphone → ffmpeg → whisper (resident, %s, external=%v)\n\n",
		resident.URL, resident.External)

	worst := 0.0
	for run := 1; run <= 3; run++ {
		type capResult struct {
			wav string
			err error
		}
		done := make(chan capResult, 1)
		go func() {
			wav, cerr := capture.CaptureUtterance(ctx)
			done <- capResult{wav: wav, err: cerr}
		}()
		// Let ffmpeg open the device before the speakers start; the leading
		// silence this adds is whisper's to ignore.
		time.Sleep(700 * time.Millisecond)
		if out, perr := exec.CommandContext(ctx, "/usr/bin/afplay", rendering.WavPath).CombinedOutput(); perr != nil {
			t.Fatalf("run %d: afplay: %v (%s)", run, perr, out)
		}
		res := <-done
		if res.err != nil {
			// The honest report — including, verbatim, the TCC guidance if
			// that is what this is.
			t.Fatalf("run %d: capture: %v", run, res.err)
		}
		tr, terr := transcriber.Transcribe(ctx, res.wav)
		if terr != nil {
			t.Fatalf("run %d: transcribe: %v", run, terr)
		}
		wer := wordErrorRate(loopSentence, tr.Text)
		if wer > worst {
			worst = wer
		}
		fmt.Fprintf(&evidence, "run %d: wav %s\n  heard: %q\n  transcribed in %s\n  WER %.1f%% (normalised words: ref %d)\n",
			run, res.wav, tr.Text, tr.Duration, wer*100, len(normalise(loopSentence)))
		t.Logf("run %d: WER %.1f%% — heard %q", run, wer*100, tr.Text)
	}
	fmt.Fprintf(&evidence, "\nworst of three: WER %.1f%%\n", worst*100)

	if err := os.WriteFile(filepath.Join(evidenceDir, "EVIDENCE-ACOUSTIC-LOOP.txt"), []byte(evidence.String()), 0o644); err != nil {
		t.Fatalf("write evidence: %v", err)
	}

	// The gate is deliberately loose: the loop crosses real air and a real
	// room. What it must prove is that the path WORKS — most words survive —
	// not that the room was silent.
	if worst > 0.5 {
		t.Fatalf("worst WER %.1f%% — more than half the words were lost; the loop does not honestly work", worst*100)
	}
}

// normalise lowercases and strips punctuation, returning words — so "open."
// and "Open" count as the same word and the WER measures hearing, not
// orthography.
func normalise(s string) []string {
	var b strings.Builder
	for _, r := range strings.ToLower(s) {
		switch {
		case r >= 'a' && r <= 'z', r >= '0' && r <= '9', r == ' ', r == '\'':
			b.WriteRune(r)
		default:
			b.WriteRune(' ')
		}
	}
	return strings.Fields(b.String())
}

// wordErrorRate is the standard Levenshtein word distance over the reference
// length.
func wordErrorRate(ref, hyp string) float64 {
	r, h := normalise(ref), normalise(hyp)
	if len(r) == 0 {
		return 0
	}
	prev := make([]int, len(h)+1)
	cur := make([]int, len(h)+1)
	for j := range prev {
		prev[j] = j
	}
	for i := 1; i <= len(r); i++ {
		cur[0] = i
		for j := 1; j <= len(h); j++ {
			sub := prev[j-1]
			if r[i-1] != h[j-1] {
				sub++
			}
			cur[j] = min(sub, min(prev[j]+1, cur[j-1]+1))
		}
		prev, cur = cur, prev
	}
	return float64(prev[len(h)]) / float64(len(r))
}
