package ears

// Tests without a microphone: the capture window's rules are driven with
// synthesised PCM frames, and the transcriber seams are held to their
// exported invocations. Nothing here pretends to judge sound.

import (
	"context"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// loudFrame returns one 20 ms frame of canonical PCM well above the default
// threshold (a square-ish tone at half scale).
func loudFrame() []byte {
	frame := make([]byte, FrameBytes)
	for i := 0; i+1 < len(frame); i += 2 {
		frame[i] = 0x00
		frame[i+1] = 0x40 // int16 0x4000 = 16384 ≈ 0.5 full scale
	}
	return frame
}

// quietFrame returns one 20 ms frame of digital silence.
func quietFrame() []byte { return make([]byte, FrameBytes) }

// frameSource yields a scripted sequence of frames, then endless silence —
// the shape of a real mic stream, which never reaches EOF on its own.
type frameSource struct {
	frames [][]byte
	next   int
	pos    int
}

func (s *frameSource) Read(p []byte) (int, error) {
	var cur []byte
	if s.next < len(s.frames) {
		cur = s.frames[s.next]
	} else {
		cur = quietFrame()
	}
	n := copy(p, cur[s.pos:])
	s.pos += n
	if s.pos >= len(cur) {
		s.pos = 0
		s.next++
	}
	return n, nil
}

// repeat returns n copies of a frame.
func repeat(frame []byte, n int) [][]byte {
	out := make([][]byte, n)
	for i := range out {
		out[i] = frame
	}
	return out
}

// TestCaptureWindowRules drives the capture loop's whole conduct table: the
// window closes after the end-of-turn hysteresis once speech has happened,
// waits through mid-answer pauses shorter than the hangover, and stops at
// the safety bound whether or not anyone spoke.
func TestCaptureWindowRules(t *testing.T) {
	cfg := windowConfig{
		threshold: DefaultSpeechThreshold,
		onset:     3,
		endOfTurn: 5,
		maxFrames: 100,
	}
	cases := []struct {
		name       string
		frames     [][]byte
		wantFrames int
	}{
		{
			// 10 quiet, 20 loud, then endless quiet: the window closes on
			// the frame that completes the 5-quiet-frame release run.
			name:       "silence hysteresis closes the window",
			frames:     append(repeat(quietFrame(), 10), repeat(loudFrame(), 20)...),
			wantFrames: 10 + 20 + cfg.endOfTurn,
		},
		{
			// A 3-frame pause mid-answer is SHORTER than the hangover and
			// must not close the window; the second burst extends the turn.
			name: "a thinking pause shorter than the hangover does not end the turn",
			frames: append(append(repeat(loudFrame(), 10), repeat(quietFrame(), 3)...),
				repeat(loudFrame(), 10)...),
			wantFrames: 10 + 3 + 10 + cfg.endOfTurn,
		},
		{
			// Nobody ever speaks: the window waits to the safety stop —
			// waiting is conduct — and returns what it heard (silence).
			name:       "no speech runs to the safety stop",
			frames:     nil,
			wantFrames: cfg.maxFrames,
		},
		{
			// Speech that never stops: the safety stop closes the window.
			name:       "unbroken speech stops at the safety stop",
			frames:     repeat(loudFrame(), 200),
			wantFrames: cfg.maxFrames,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			pcm, err := captureFrom(context.Background(), &frameSource{frames: tc.frames}, cfg)
			if err != nil {
				t.Fatalf("captureFrom: %v", err)
			}
			if got := len(pcm) / FrameBytes; got != tc.wantFrames {
				t.Fatalf("window held %d frames, want %d", got, tc.wantFrames)
			}
		})
	}
}

// TestCaptureStreamEndReturnsWhatWasHeard: a mic stream that dies mid-turn
// still hands back the audio it produced, so a transcription can be tried.
func TestCaptureStreamEndReturnsWhatWasHeard(t *testing.T) {
	r := io.LimitReader(&frameSource{frames: repeat(loudFrame(), 8)}, int64(8*FrameBytes))
	pcm, err := captureFrom(context.Background(), r,
		windowConfig{threshold: DefaultSpeechThreshold, onset: 3, endOfTurn: 5, maxFrames: 100})
	if err != nil {
		t.Fatalf("captureFrom on a dying stream: %v", err)
	}
	if got := len(pcm) / FrameBytes; got != 8 {
		t.Fatalf("kept %d frames, want the 8 the stream produced", got)
	}
}

// TestCaptureRefusesTmp: captured audio is a generated asset and generated
// assets never go to /tmp — the standing rule, enforced, not advised.
func TestCaptureRefusesTmp(t *testing.T) {
	for _, dir := range []string{"/tmp/ears", "/private/tmp/ears"} {
		c := &Capture{CaptureDir: dir}
		if _, err := c.CaptureUtterance(context.Background()); !errors.Is(err, ErrNotConfigured) {
			t.Fatalf("CaptureDir %s: err = %v, want ErrNotConfigured", dir, err)
		}
	}
	c := &Capture{}
	if _, err := c.CaptureUtterance(context.Background()); !errors.Is(err, ErrNotConfigured) {
		t.Fatalf("empty CaptureDir: err = %v, want ErrNotConfigured", err)
	}
}

// TestDetectorHysteresis holds the detector to its two edges: onset needs
// the full loud run, release needs the full quiet run, and single-frame
// blips in either direction change nothing.
func TestDetectorHysteresis(t *testing.T) {
	cases := []struct {
		name   string
		levels []float64
		want   bool
	}{
		{"silence stays silent", []float64{0, 0, 0, 0}, false},
		{"one loud frame is a keystroke, not speech", []float64{0, 0.5, 0, 0}, false},
		{"a full onset run flips to speaking", []float64{0.5, 0.5, 0.5}, true},
		{"one quiet frame is a pause, not the end", []float64{0.5, 0.5, 0.5, 0, 0.5}, true},
		{"a full release run flips back to quiet", []float64{0.5, 0.5, 0.5, 0, 0, 0, 0, 0}, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			d := NewSpeechDetector(DefaultSpeechThreshold, 3, 5)
			for _, level := range tc.levels {
				d.observeLevel(level)
			}
			if got := d.Speaking(); got != tc.want {
				t.Fatalf("Speaking() = %v, want %v after %v", got, tc.want, tc.levels)
			}
		})
	}
}

// TestCleanTranscript holds the cleaner to its one promise: no speech comes
// back as an EMPTY string, never as bracketed tag text an interview would
// record as an answer.
func TestCleanTranscript(t *testing.T) {
	cases := []struct {
		name, raw, want string
	}{
		{"plain line", "  hello there\n", "hello there"},
		{"segments joined", " it schedules\n the watering\n", "it schedules the watering"},
		{"blank audio tag dropped", "[BLANK_AUDIO]\n", ""},
		{"silence tag dropped around speech", "[SILENCE]\n yes\n[BLANK_AUDIO]\n", "yes"},
		{"two tags on one line are kept", "[on] and [off]", "[on] and [off]"},
		{"empty", "", ""},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := CleanTranscript(tc.raw); got != tc.want {
				t.Fatalf("CleanTranscript(%q) = %q, want %q", tc.raw, got, tc.want)
			}
		})
	}
}

// TestWhisperCLIArgs pins the exported invocation — the reproduce-by-hand
// promise is only real if the argument list is what the docs say it is.
func TestWhisperCLIArgs(t *testing.T) {
	w := &WhisperCLI{BinPath: "whisper-cli", ModelPath: "m.bin", Threads: 4, Language: "en"}
	got := strings.Join(w.Args("u.wav"), " ")
	want := "-m m.bin -f u.wav -nt -np -t 4 -l en"
	if got != want {
		t.Fatalf("Args = %q, want %q", got, want)
	}
}

// TestWhisperServerInferenceURL pins the endpoint the reproduce-by-hand curl
// in its doc comment names.
func TestWhisperServerInferenceURL(t *testing.T) {
	w := &WhisperServer{URL: "http://127.0.0.1:8932/"}
	if got, want := w.InferenceURL(), "http://127.0.0.1:8932/inference"; got != want {
		t.Fatalf("InferenceURL = %q, want %q", got, want)
	}
}

// TestWrittenWAVIsCanonical: the WAV the capture writes must be exactly what
// whisper requires — 16 kHz mono s16 — and what internal/voice can read.
func TestWrittenWAVIsCanonical(t *testing.T) {
	pcm := append(loudFrame(), quietFrame()...)
	path := filepath.Join(t.TempDir(), "utt.wav")
	if err := writePCM16WAV(path, pcm); err != nil {
		t.Fatalf("writePCM16WAV: %v", err)
	}
	// t.TempDir is not /tmp-rule territory: nothing generated in a unit test
	// is an asset, and the file is checked, not kept.
	body := readAll(t, path)
	if string(body[0:4]) != "RIFF" || string(body[8:12]) != "WAVE" {
		t.Fatal("not a RIFF/WAVE file")
	}
	if got := int(body[24]) | int(body[25])<<8 | int(body[26])<<16 | int(body[27])<<24; got != SampleRate {
		t.Fatalf("sample rate %d, want %d", got, SampleRate)
	}
	if got := int(body[22]) | int(body[23])<<8; got != Channels {
		t.Fatalf("channels %d, want %d", got, Channels)
	}
	if got := len(body) - 44; got != len(pcm) {
		t.Fatalf("data bytes %d, want %d", got, len(pcm))
	}
}

// TestEarsListenComposesLoudly: a capture or transcription failure comes out
// of Listen as an explicit error, and a nil seam refuses to pretend.
func TestEarsListenComposesLoudly(t *testing.T) {
	e := &Ears{}
	if _, err := e.Listen(context.Background()); !errors.Is(err, ErrNotConfigured) {
		t.Fatalf("unconfigured Listen: err = %v, want ErrNotConfigured", err)
	}
}

// readAll is a test helper for reading a small file whole.
func readAll(t *testing.T, path string) []byte {
	t.Helper()
	body, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	return body
}
