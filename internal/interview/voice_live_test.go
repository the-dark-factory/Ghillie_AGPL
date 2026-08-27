//go:build darwin

package interview

// The LIVE smoke run: one Say line and one Ask question through the REAL
// settled pipeline — textplan.py rendering, afplay actually playing on this
// machine's speakers, the reply typed on stdin (a here-doc in a scripted run).
// It is guarded twice over because it makes noise and takes real seconds:
//
//	GHILLIE_VOICE_LIVE=1 \
//	GHILLIE_VOICE_LIVE_DIR=~/ObVault/ghillie-voice/wu-runs/<run>/ \
//	GHILLIE_VOICE_PIPELINE_DIR=<the checkout holding .venv-kokoro and demo/textplan.py> \
//	go test -run TestLiveVoiceSmoke -v ./internal/interview <<'EOF'
//	the typed reply
//	EOF
//
// Still no ear: what is verified is that the artifacts EXIST and are
// structurally honest — non-trivial WAVs whose duration is sane against the
// text length, sidecars in which every breath is the engine's, the offered
// floor prepared, wall clocks recorded without latency claims. Whether it
// sounds right stays a human's call.

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/tonygair/ghillie/internal/voice"
)

// recordingRenderer decorates the real pipeline so the smoke run can inspect
// each Rendering the surface consumed, without the surface changing.
type recordingRenderer struct {
	real *voice.TextPlan
	got  []voice.Rendering
}

// Render delegates to the real pipeline and keeps the result.
func (r *recordingRenderer) Render(ctx context.Context, text string) (voice.Rendering, error) {
	rendering, err := r.real.Render(ctx, text)
	if err == nil {
		r.got = append(r.got, rendering)
	}
	return rendering, err
}

// TestLiveVoiceSmoke runs the real chain once. Skipped unless explicitly
// summoned — see the header comment for the invocation.
func TestLiveVoiceSmoke(t *testing.T) {
	if os.Getenv("GHILLIE_VOICE_LIVE") != "1" {
		t.Skip("live smoke: set GHILLIE_VOICE_LIVE=1 (and GHILLIE_VOICE_LIVE_DIR) to run the real pipeline aloud")
	}
	evidenceDir := os.Getenv("GHILLIE_VOICE_LIVE_DIR")
	if evidenceDir == "" {
		t.Fatal("live smoke: GHILLIE_VOICE_LIVE_DIR must name a durable evidence directory — never /tmp")
	}
	if err := os.MkdirAll(evidenceDir, 0o755); err != nil {
		t.Fatalf("evidence dir: %v", err)
	}

	// The pipeline checkout comes from the environment, never a literal here:
	// this file sits inside the opsec gate's client-visible scan, and the
	// checkout's name stays out of it. The product default lives with the
	// -voice-pipeline-dir flag in cmd/ghillie.
	pipelineDir := os.Getenv("GHILLIE_VOICE_PIPELINE_DIR")
	if pipelineDir == "" {
		t.Fatal("live smoke: GHILLIE_VOICE_PIPELINE_DIR must name the checkout holding .venv-kokoro and demo/textplan.py")
	}
	renderer := &recordingRenderer{real: &voice.TextPlan{
		PythonPath: filepath.Join(pipelineDir, ".venv-kokoro", "bin", "python"),
		ScriptPath: filepath.Join(pipelineDir, "demo", "textplan.py"),
		RenderDir:  evidenceDir,
	}}
	if _, err := os.Stat(renderer.real.PythonPath); err != nil {
		t.Fatalf("live smoke asked for but the pipeline venv is missing: %v", err)
	}
	player, err := voice.NewAFPlay()
	if err != nil {
		t.Fatalf("player: %v", err)
	}
	surface := NewVoiceSurface(renderer, player, voice.NewBreathBank(),
		NewConsoleSurface(os.Stdin, os.Stdout), false, nil)

	sayLine := "I am ghillie. I sit with you while you describe the software you want built, and I get it down in your words."
	question := "What is the software you want built actually for?"

	ctx := context.Background()
	if err := surface.Say(ctx, sayLine); err != nil {
		t.Fatalf("Say through the real chain: %v", err)
	}
	reply, err := surface.Ask(ctx, question)
	if err != nil {
		t.Fatalf("Ask through the real chain: %v", err)
	}
	if strings.TrimSpace(reply.Text) == "" {
		t.Fatal("the typed reply never arrived — was stdin supplied?")
	}

	if len(renderer.got) != 2 {
		t.Fatalf("expected 2 renders (one Say, one Ask), recorded %d", len(renderer.got))
	}
	var evidence strings.Builder
	fmt.Fprintf(&evidence, "ghillie voice slice — LIVE smoke run, %s\n", time.Now().UTC().Format(time.RFC3339))
	fmt.Fprintf(&evidence, "pipeline: %s %s (settled path; harness breath-4-textplan-r2)\n\n", renderer.real.PythonPath, renderer.real.ScriptPath)
	texts := []string{sayLine, question}
	for i, r := range renderer.got {
		info, statErr := os.Stat(r.WavPath)
		if statErr != nil {
			t.Fatalf("render %d wav vanished: %v", i, statErr)
		}
		words := len(strings.Fields(texts[i]))
		perWord := r.Audio / time.Duration(words)
		if perWord < 100*time.Millisecond || perWord > 1500*time.Millisecond {
			t.Fatalf("render %d duration %s over %d words (%s/word) is not a sane utterance", i, r.Audio, words, perWord)
		}
		plan, planErr := voice.ReadPlan(r.PlanPath)
		if planErr != nil {
			t.Fatalf("render %d sidecar: %v", i, planErr)
		}
		if err := plan.Validate(); err != nil {
			t.Fatalf("render %d: %v", i, err)
		}
		fmt.Fprintf(&evidence, "render %d: %q\n  wav %s (%d bytes)\n  audio %s over %d words\n  RENDER WALL CLOCK %s — measured, no daemon, no latency claim\n  engine breaths %d (of %d engine events)\n",
			i+1, texts[i], r.WavPath, info.Size(), r.Audio, words, r.Wall, len(plan.Breaths), plan.EngineEventsTotal)
		for _, b := range plan.Breaths {
			fmt.Fprintf(&evidence, "    breath at %q — tick %d depth %d verdict %s sample %s\n",
				b.Words, b.Engine.Tick, b.Engine.Depth, b.Engine.Verdict, b.Sample)
		}
	}
	breathWav := strings.TrimSuffix(renderer.got[1].WavPath, ".wav") + ".breath.wav"
	binfo, err := os.Stat(breathWav)
	if err != nil {
		t.Fatalf("offered-floor breath was never prepared: %v", err)
	}
	fmt.Fprintf(&evidence, "\noffered-floor in-breath (Ask only, exactly one): %s (%d bytes)\n", breathWav, binfo.Size())
	fmt.Fprintf(&evidence, "typed reply captured after playback: %q\n", reply.Text)

	t.Log("\n" + evidence.String())
	if err := os.WriteFile(filepath.Join(evidenceDir, "EVIDENCE.txt"), []byte(evidence.String()), 0o644); err != nil {
		t.Fatalf("write evidence: %v", err)
	}
}
