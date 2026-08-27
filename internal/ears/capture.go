package ears

// capture.go opens the microphone for ONE answer and closes it again. The
// exec boundary is the same discipline as everywhere else on this path
// (internal/voice/pipeline.go, mirroring voice-relay): ffmpeg is a fixed
// external dependency, the exact argument list is exported so a capture can
// be reproduced by hand, and every failure is an explicit error carrying
// stderr — including the macOS microphone-permission case, which is named
// rather than fought.

import (
	"context"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"time"
)

// tccGuidance is what a capture that produced no audio at all tells the
// operator. macOS gates the microphone per app (TCC): the grant belongs to
// the terminal application ghillie runs inside, and there is nothing this
// binary can or should do to take it — the honest move is to say exactly
// where the human grants it.
const tccGuidance = "if the microphone light never came on, macOS is refusing the mic (TCC): " +
	"grant access in System Settings → Privacy & Security → Microphone " +
	"for the terminal application ghillie is running in, then start ghillie again"

// Capture opens the microphone for one utterance at a time.
//
// The zero value works for everything except CaptureDir, which must be set —
// captured audio is a generated asset and generated assets NEVER go to /tmp
// (standing rule; /tmp took the original harness and the breath bank).
type Capture struct {
	// FFmpegPath is the ffmpeg binary. Empty means "ffmpeg" on PATH.
	FFmpegPath string

	// Device is the ffmpeg avfoundation input specifier. The default ":0"
	// selects the system default audio input with no video. Run
	//
	//	ffmpeg -f avfoundation -list_devices true -i ""
	//
	// to enumerate the indices. (Mirrored from voice-relay's CaptureConfig.)
	Device string

	// CaptureDir is where utterance WAVs land. Required; never /tmp.
	CaptureDir string

	// Threshold, OnsetFrames, EndOfTurnFrames and MaxTurn tune the
	// end-of-turn detector. Zero values take the package defaults, whose
	// rationale lives on the constants.
	Threshold       float64
	OnsetFrames     int
	EndOfTurnFrames int
	MaxTurn         time.Duration

	seq atomic.Uint64
}

// Args returns the ffmpeg argument list for microphone capture to canonical
// PCM on stdout. Exported so the exact command can be logged and reproduced
// by hand. (Mirrored from voice-relay/internal/audio captureArgs.)
func (c *Capture) Args() []string {
	device := c.Device
	if device == "" {
		device = ":0"
	}
	return []string{
		"-hide_banner",
		"-loglevel", "error",
		"-nostdin",
		// Keep ffmpeg's internal queues short; this is a live path where
		// latency costs more than the occasional dropped frame.
		"-fflags", "nobuffer",
		"-f", "avfoundation",
		"-i", device,
		"-ac", fmt.Sprint(Channels),
		"-ar", fmt.Sprint(SampleRate),
		"-f", "s16le",
		"-acodec", "pcm_s16le",
		"-flush_packets", "1",
		"pipe:1",
	}
}

// validate reports whether the capture has what it needs.
func (c *Capture) validate() error {
	if c.CaptureDir == "" {
		return fmt.Errorf("%w: capture dir is empty", ErrNotConfigured)
	}
	clean := filepath.Clean(c.CaptureDir)
	if strings.HasPrefix(clean, "/tmp/") || strings.HasPrefix(clean, "/private/tmp/") {
		return fmt.Errorf("%w: capture dir %s is under /tmp — generated assets never go there (standing rule)", ErrNotConfigured, c.CaptureDir)
	}
	return nil
}

// tuning returns the effective detector settings, defaults applied.
func (c *Capture) tuning() (threshold float64, onset, endOfTurn int, maxTurn time.Duration) {
	threshold, onset, endOfTurn, maxTurn = c.Threshold, c.OnsetFrames, c.EndOfTurnFrames, c.MaxTurn
	if threshold <= 0 {
		threshold = DefaultSpeechThreshold
	}
	if onset <= 0 {
		onset = DefaultOnsetFrames
	}
	if endOfTurn <= 0 {
		endOfTurn = DefaultEndOfTurnFrames
	}
	if maxTurn <= 0 {
		maxTurn = DefaultMaxTurn
	}
	return threshold, onset, endOfTurn, maxTurn
}

// CaptureUtterance records one answer from the microphone: it opens the mic,
// waits through the person's speech, closes the window after EndOfTurnFrames
// of quiet (or at MaxTurn, the safety stop), and writes what it heard as a
// canonical 16 kHz mono s16 WAV under CaptureDir. It returns the WAV path.
//
// A capture that ends with NO audio at all is an error that names the macOS
// microphone permission, because that is what it almost always means.
func (c *Capture) CaptureUtterance(ctx context.Context) (wavPath string, err error) {
	if err := c.validate(); err != nil {
		return "", err
	}
	if err := os.MkdirAll(c.CaptureDir, 0o755); err != nil {
		return "", fmt.Errorf("%w: create capture dir: %w", ErrCaptureFailed, err)
	}

	bin := c.FFmpegPath
	if bin == "" {
		bin = "ffmpeg"
	}
	resolved, err := exec.LookPath(bin)
	if err != nil {
		return "", fmt.Errorf("%w: %s is not installed or not on PATH (needed for microphone capture; brew install ffmpeg): %w", ErrCaptureFailed, bin, err)
	}

	// The capture process gets its own cancellable context so ending the
	// window (end-of-turn or safety stop) stops ffmpeg promptly; the parent
	// ctx still cancels everything.
	capCtx, stop := context.WithCancel(ctx)
	defer stop()

	cmd := exec.CommandContext(capCtx, resolved, c.Args()...)
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return "", fmt.Errorf("%w: stdout pipe: %w", ErrCaptureFailed, err)
	}
	stderr := &lockedBuffer{}
	cmd.Stderr = stderr
	if err := cmd.Start(); err != nil {
		return "", fmt.Errorf("%w: starting %s: %w", ErrCaptureFailed, resolved, err)
	}

	pcm, readErr := captureFrom(ctx, stdout, c.tuningAsConfig())

	// However the window closed, the mic process is stopped and reaped. A
	// killed capture exits non-zero by design; that is not a failure.
	stop()
	_ = cmd.Wait()

	if readErr != nil && !errors.Is(readErr, context.Canceled) {
		return "", fmt.Errorf("%w: %s %s: %w (stderr: %s)",
			ErrCaptureFailed, resolved, strings.Join(c.Args(), " "), readErr, tail(stderr.String(), 400))
	}
	if err := ctx.Err(); err != nil {
		return "", fmt.Errorf("%w: %w", ErrCaptureFailed, err)
	}
	if len(pcm) < FrameBytes {
		return "", fmt.Errorf("%w: the microphone produced no audio — %s (ffmpeg stderr: %s)",
			ErrCaptureFailed, tccGuidance, tail(stderr.String(), 400))
	}

	out := filepath.Join(c.CaptureDir,
		fmt.Sprintf("ear-%s-%03d.wav", time.Now().UTC().Format("20060102-150405"), c.seq.Add(1)))
	if err := writePCM16WAV(out, pcm); err != nil {
		return "", fmt.Errorf("%w: %w", ErrCaptureFailed, err)
	}
	return out, nil
}

// windowConfig is the end-of-turn tuning handed to the capture loop.
type windowConfig struct {
	threshold float64
	onset     int
	endOfTurn int
	maxFrames int
}

// tuningAsConfig renders the capture's tuning for the loop, converting the
// safety stop to a frame count.
func (c *Capture) tuningAsConfig() windowConfig {
	threshold, onset, endOfTurn, maxTurn := c.tuning()
	return windowConfig{
		threshold: threshold,
		onset:     onset,
		endOfTurn: endOfTurn,
		maxFrames: int(maxTurn / FrameDuration),
	}
}

// captureFrom reads canonical PCM frames from r until the person's turn ends:
// once speech has been observed (cfg.onset consecutive loud frames), a run of
// cfg.endOfTurn consecutive quiet frames closes the window; cfg.maxFrames is
// the safety stop either way. It is split out from CaptureUtterance so the
// window rules can be tested without a microphone.
func captureFrom(ctx context.Context, r io.Reader, cfg windowConfig) (pcm []byte, err error) {
	detector := NewSpeechDetector(cfg.threshold, cfg.onset, cfg.endOfTurn)
	frame := make([]byte, FrameBytes)
	spoke := false

	for frames := 0; frames < cfg.maxFrames; frames++ {
		if err := ctx.Err(); err != nil {
			return pcm, err
		}
		n, readErr := io.ReadFull(r, frame)
		pcm = append(pcm, frame[:n]...)
		if readErr != nil {
			if errors.Is(readErr, io.EOF) || errors.Is(readErr, io.ErrUnexpectedEOF) {
				// The mic stream ended on its own — ffmpeg died or the ctx
				// was cancelled. What was heard so far is still returned.
				return pcm, ctx.Err()
			}
			return pcm, readErr
		}

		speaking, obsErr := detector.Observe(frame)
		if obsErr != nil {
			return pcm, obsErr
		}
		if speaking {
			spoke = true
		}
		// The window closes ONLY after the person has spoken and then gone
		// quiet for the full hangover — the detector's release edge IS the
		// end-of-turn decision. Before any speech, the window stays open to
		// the safety stop: waiting is conduct, and it is compiled in.
		if spoke && !speaking {
			return pcm, nil
		}
	}
	return pcm, nil
}

// writePCM16WAV writes canonical PCM as a 16 kHz mono 16-bit WAV. The header
// layout matches internal/voice's writer; this one takes raw s16le bytes
// because that is what the capture stream already is.
func writePCM16WAV(path string, pcm []byte) error {
	const headerSize = 44
	data := make([]byte, headerSize+len(pcm))
	copy(data[0:4], "RIFF")
	binary.LittleEndian.PutUint32(data[4:8], uint32(36+len(pcm)))
	copy(data[8:12], "WAVE")
	copy(data[12:16], "fmt ")
	binary.LittleEndian.PutUint32(data[16:20], 16)
	binary.LittleEndian.PutUint16(data[20:22], 1) // PCM
	binary.LittleEndian.PutUint16(data[22:24], Channels)
	binary.LittleEndian.PutUint32(data[24:28], SampleRate)
	binary.LittleEndian.PutUint32(data[28:32], SampleRate*Channels*BytesPerSample) // byte rate
	binary.LittleEndian.PutUint16(data[32:34], Channels*BytesPerSample)            // block align
	binary.LittleEndian.PutUint16(data[34:36], 8*BytesPerSample)                   // bits
	copy(data[36:40], "data")
	binary.LittleEndian.PutUint32(data[40:44], uint32(len(pcm)))
	copy(data[headerSize:], pcm)
	if err := os.WriteFile(path, data, 0o644); err != nil {
		return fmt.Errorf("write wav %s: %w", path, err)
	}
	return nil
}

// lockedBuffer is a strings builder safe for the concurrent writes exec
// performs from its own goroutine while the caller reads it on failure.
// (Mirrored from voice-relay/internal/audio.)
type lockedBuffer struct {
	mu  sync.Mutex
	buf strings.Builder
}

// Write implements io.Writer.
func (b *lockedBuffer) Write(p []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.Write(p)
}

// String returns the buffered text.
func (b *lockedBuffer) String() string {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.String()
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
