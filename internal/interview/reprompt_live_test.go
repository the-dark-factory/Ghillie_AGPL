//go:build darwin

package interview

// The REPROMPT LIVE FIRE: the one honest path the ears slice shipped with
// unexercised air — a spoken answer that transcribes to nothing must become
// a LOUD typed reprompt, never a silent skip. Unit tests cover it with
// fakes; this test makes it happen acoustically on THIS machine: the real
// question is rendered and played aloud, the real microphone opens inside
// the reply window, and the "answer" is a WAV played by afplay at a volume
// chosen to land under the ears' speech gate (see the calibration in the
// evidence directory).
//
// The assertion is the INVARIANT, not one branch: either the reprompt line
// printed and the TYPED answer became the reply, or the heard-by-ear echo
// printed and the transcript became the reply. Anything else — a silent
// skip, an empty reply, an error — fails. WHICH branch fired is recorded
// verbatim in the evidence, because on this path the branch itself is the
// measurement: whisper hearing speech the RMS gate cannot is a finding,
// not a pass or a fail.
//
// Guarded twice over because it makes noise and needs a quiet room:
//
//	GHILLIE_REPROMPT_LIVE=1 \
//	GHILLIE_REPROMPT_LIVE_DIR=~/ObVault/ghillie-voice/wu-runs/<run>/ \
//	GHILLIE_REPROMPT_ANSWER_WAV=<the quiet answer WAV afplay will speak> \
//	GHILLIE_REPROMPT_ANSWER_VOLUME=<afplay -v value, e.g. 0.05> \
//	GHILLIE_VOICE_PIPELINE_DIR=<the checkout holding .venv-kokoro and demo/textplan.py> \
//	go test -run TestLiveRepromptFire -v ./internal/interview

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/tonygair/ghillie/internal/ears"
	"github.com/tonygair/ghillie/internal/voice"
)

// typedAnswer is what the keyboard supplies when (and only when) the
// reprompt opens the typed path.
const typedAnswer = "the greenhouse timer, typed after the ear failed"

// signallingListener wraps the real ears so the test knows the moment the
// reply window opens — that is when the quiet "answer" may start playing,
// and not before: the mic never listens while ghillie speaks, and the test
// must not cheat that by seeding the room early.
type signallingListener struct {
	real   *ears.Ears
	opened chan struct{}
}

// Listen signals the window and delegates to the real ears.
func (s *signallingListener) Listen(ctx context.Context) (heard string, err error) {
	close(s.opened)
	return s.real.Listen(ctx)
}

// captureLogger tees the operator narration into the evidence.
type captureLogger struct {
	t   *testing.T
	buf *strings.Builder
}

// Printf implements Logger (and ears.Logger) into both sinks.
func (l *captureLogger) Printf(format string, v ...any) {
	line := fmt.Sprintf(format, v...)
	l.t.Logf("%s", line)
	l.buf.WriteString(line + "\n")
}

// TestLiveRepromptFire drives one Ask turn against the real air. Skipped
// unless explicitly summoned — see the header comment for the invocation.
func TestLiveRepromptFire(t *testing.T) {
	if os.Getenv("GHILLIE_REPROMPT_LIVE") != "1" {
		t.Skip("reprompt live fire: set GHILLIE_REPROMPT_LIVE=1 (and its _DIR, _ANSWER_WAV, _ANSWER_VOLUME) to run against the real air")
	}
	evidenceDir := os.Getenv("GHILLIE_REPROMPT_LIVE_DIR")
	if evidenceDir == "" {
		t.Fatal("reprompt live fire: GHILLIE_REPROMPT_LIVE_DIR must name a durable evidence directory — never /tmp")
	}
	if err := os.MkdirAll(evidenceDir, 0o755); err != nil {
		t.Fatalf("evidence dir: %v", err)
	}
	answerWav := os.Getenv("GHILLIE_REPROMPT_ANSWER_WAV")
	if answerWav == "" {
		t.Fatal("reprompt live fire: GHILLIE_REPROMPT_ANSWER_WAV must name the quiet WAV afplay speaks as the answer")
	}
	volume := os.Getenv("GHILLIE_REPROMPT_ANSWER_VOLUME")
	if volume == "" {
		t.Fatal("reprompt live fire: GHILLIE_REPROMPT_ANSWER_VOLUME must give the afplay -v value the calibration chose")
	}
	pipelineDir := os.Getenv("GHILLIE_VOICE_PIPELINE_DIR")
	if pipelineDir == "" {
		t.Fatal("reprompt live fire: GHILLIE_VOICE_PIPELINE_DIR must name the checkout holding .venv-kokoro and demo/textplan.py")
	}

	ctx := context.Background()

	renderer := &voice.TextPlan{
		PythonPath: filepath.Join(pipelineDir, ".venv-kokoro", "bin", "python"),
		ScriptPath: filepath.Join(pipelineDir, "demo", "textplan.py"),
		RenderDir:  evidenceDir,
	}
	player, err := voice.NewAFPlay()
	if err != nil {
		t.Fatalf("player: %v", err)
	}

	// The resident whisper: attach to one already warm, or start our own.
	resident, err := ears.StartResident(ctx, ears.ResidentConfig{
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

	operatorLog := &captureLogger{t: t, buf: &strings.Builder{}}
	listener := &signallingListener{
		real: &ears.Ears{
			Capture: &ears.Capture{
				CaptureDir: evidenceDir,
				// The product default MaxTurn is 120 s; a sub-gate answer
				// never fires onset, so the window can only close at the
				// safety stop — 8 s keeps the live fire honest AND finite.
				MaxTurn: 8 * time.Second,
			},
			Transcriber: &ears.WhisperServer{URL: resident.URL},
			Log:         operatorLog,
		},
		opened: make(chan struct{}),
	}

	// The client's console: the typed answer is queued on stdin, and it must
	// be consumed ONLY via the reprompt path; the transcript of everything
	// the client saw is kept for the evidence.
	var clientSaw strings.Builder
	console := NewConsoleSurface(strings.NewReader(typedAnswer+"\n"), &clientSaw)

	surface := NewVoiceSurface(renderer, player, voice.NewBreathBank(),
		console, false, operatorLog).WithEars(listener)

	// The quiet "answer": played into the open reply window, at the
	// calibrated volume, from a goroutine that waits for the window.
	playDone := make(chan error, 1)
	go func() {
		<-listener.opened
		// Let ffmpeg open the device before the "person" starts answering;
		// the leading silence is whisper's to ignore (same discipline as
		// the acoustic loop test).
		time.Sleep(700 * time.Millisecond)
		out, perr := exec.CommandContext(ctx, "/usr/bin/afplay", "-v", volume, answerWav).CombinedOutput()
		if perr != nil {
			playDone <- fmt.Errorf("afplay -v %s %s: %w (%s)", volume, answerWav, perr, out)
			return
		}
		playDone <- nil
	}()

	question := "Which bed should the greenhouse controller water first thing in the morning?"
	reply, err := surface.Ask(ctx, question)
	if err != nil {
		t.Fatalf("Ask through the real chain: %v", err)
	}
	if perr := <-playDone; perr != nil {
		t.Fatalf("the quiet answer never played: %v", perr)
	}

	saw := clientSaw.String()
	reprompted := strings.Contains(saw, "I did not catch that by ear.")
	heardByEar := strings.Contains(saw, "(heard by ear)")

	var evidence strings.Builder
	fmt.Fprintf(&evidence, "ghillie ears slice — REPROMPT LIVE FIRE, %s\n", time.Now().UTC().Format(time.RFC3339))
	fmt.Fprintf(&evidence, "question: %q\n", question)
	fmt.Fprintf(&evidence, "quiet answer: afplay -v %s %s\n", volume, answerWav)
	fmt.Fprintf(&evidence, "path: question aloud → offered-floor breath → mic (ffmpeg) → whisper (resident, %s, external=%v)\n",
		resident.URL, resident.External)
	fmt.Fprintf(&evidence, "capture tuning: threshold %g, onset %d frames, end-of-turn %d frames, MaxTurn %s (product default %s)\n\n",
		ears.DefaultSpeechThreshold, ears.DefaultOnsetFrames, ears.DefaultEndOfTurnFrames,
		listener.real.Capture.MaxTurn, ears.DefaultMaxTurn)
	fmt.Fprintf(&evidence, "REPROMPT FIRED: %v\nHEARD BY EAR: %v\nreply on the record: %q\n\n", reprompted, heardByEar, reply.Text)
	fmt.Fprintf(&evidence, "── what the client saw ──\n%s\n── operator log ──\n%s", saw, operatorLog.buf.String())

	t.Log("\n" + evidence.String())
	name := fmt.Sprintf("CONSOLE-vol%s.txt", volume)
	if err := os.WriteFile(filepath.Join(evidenceDir, name), []byte(evidence.String()), 0o644); err != nil {
		t.Fatalf("write evidence: %v", err)
	}

	// ★ THE INVARIANT: never a silent skip. One branch, cleanly, with the
	// matching reply — or the surface lied about what happened on the air.
	switch {
	case reprompted && !heardByEar:
		if reply.Text != typedAnswer {
			t.Fatalf("reprompt fired but the typed answer did not become the reply: got %q", reply.Text)
		}
	case heardByEar && !reprompted:
		if strings.TrimSpace(reply.Text) == "" {
			t.Fatal("heard-by-ear echoed an empty transcript — that is the silent skip the reprompt exists to prevent")
		}
		t.Logf("FINDING, not a failure: the ear accepted %q from audio the RMS gate never called speech — see evidence", reply.Text)
	default:
		t.Fatalf("neither branch fired cleanly (reprompted=%v heardByEar=%v) — the surface skipped silently.\nclient saw:\n%s",
			reprompted, heardByEar, saw)
	}
}
