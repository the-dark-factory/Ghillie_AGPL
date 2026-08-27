package ears

// transcribe.go turns a captured utterance into text, locally. The seam is
// MIRRORED from voice-relay/internal/transcribe (read-only, not imported):
// whisper is a fixed external dependency, nothing here builds or links it,
// the exact invocation is exported so it can be reproduced by hand, and a
// failure is an explicit error carrying the tool's own diagnostics.
//
// Two implementations, in order of preference:
//
//   - WhisperServer speaks HTTP to a RESIDENT whisper-server (whisper.cpp
//     ships one). The model loads once and stays warm; measured on Bill,
//     ~30 ms per short utterance against ~660 ms for a cold whisper-cli
//     spawn — the estate's documented lesson that spawn cost dominates.
//
//   - WhisperCLI spawns whisper-cli per utterance. It is the fallback when
//     no server can be had, and its latency cost is stated where it is
//     chosen, not hidden.

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"os"
	"os/exec"
	"strings"
	"time"
)

// Transcriber turns one captured utterance into text.
type Transcriber interface {
	// Transcribe reads the canonical 16 kHz mono s16 WAV at wavPath and
	// returns its transcript.
	Transcribe(ctx context.Context, wavPath string) (Result, error)
}

// Result is the outcome of transcribing one utterance.
type Result struct {
	// Text is the transcript, cleaned: whitespace normalised and whisper's
	// bracketed non-speech tags dropped, so "no speech" is an EMPTY string
	// rather than literal text the interview would record as an answer.
	Text string

	// Duration is how long the transcription itself took.
	Duration time.Duration
}

// WhisperServer transcribes against a resident whisper-server over HTTP.
//
// The zero value is not usable; URL must be set.
type WhisperServer struct {
	// URL is the server base, e.g. http://127.0.0.1:8932. The inference
	// endpoint is URL + "/inference" (whisper-server's default path).
	URL string

	// HTTPClient overrides the default client. Nil uses a client with a
	// 60 s timeout — generous, because a MaxTurn-length utterance through
	// a small model still finishes well inside it.
	HTTPClient *http.Client
}

// InferenceURL returns the exact endpoint posted to, exported for the same
// reason WhisperCLI.Args is: so a failed transcription can be reproduced by
// hand —
//
//	curl -s <InferenceURL> -F file=@<wav> -F response_format=json
func (w *WhisperServer) InferenceURL() string {
	return strings.TrimRight(w.URL, "/") + "/inference"
}

// Transcribe posts the WAV to the resident server and returns the cleaned
// transcript.
func (w *WhisperServer) Transcribe(ctx context.Context, wavPath string) (Result, error) {
	if w.URL == "" {
		return Result{}, fmt.Errorf("%w: whisper-server URL is empty", ErrNotConfigured)
	}

	body := &bytes.Buffer{}
	form := multipart.NewWriter(body)
	part, err := form.CreateFormFile("file", "utterance.wav")
	if err != nil {
		return Result{}, fmt.Errorf("%w: build form: %w", ErrTranscribeFailed, err)
	}
	f, err := os.Open(wavPath)
	if err != nil {
		return Result{}, fmt.Errorf("%w: open %s: %w", ErrTranscribeFailed, wavPath, err)
	}
	_, copyErr := io.Copy(part, f)
	if cerr := f.Close(); cerr != nil && copyErr == nil {
		copyErr = cerr
	}
	if copyErr != nil {
		return Result{}, fmt.Errorf("%w: read %s: %w", ErrTranscribeFailed, wavPath, copyErr)
	}
	if err := form.WriteField("response_format", "json"); err != nil {
		return Result{}, fmt.Errorf("%w: build form: %w", ErrTranscribeFailed, err)
	}
	if err := form.Close(); err != nil {
		return Result{}, fmt.Errorf("%w: build form: %w", ErrTranscribeFailed, err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, w.InferenceURL(), body)
	if err != nil {
		return Result{}, fmt.Errorf("%w: %w", ErrTranscribeFailed, err)
	}
	req.Header.Set("Content-Type", form.FormDataContentType())

	client := w.HTTPClient
	if client == nil {
		client = &http.Client{Timeout: 60 * time.Second}
	}
	start := time.Now()
	resp, err := client.Do(req)
	if err != nil {
		return Result{}, fmt.Errorf("%w: POST %s: %w", ErrTranscribeFailed, w.InferenceURL(), err)
	}
	defer func() {
		if cerr := resp.Body.Close(); cerr != nil && err == nil {
			err = cerr
		}
	}()
	raw, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		return Result{}, fmt.Errorf("%w: read response from %s: %w", ErrTranscribeFailed, w.InferenceURL(), err)
	}
	if resp.StatusCode != http.StatusOK {
		return Result{}, fmt.Errorf("%w: %s returned %s: %s", ErrTranscribeFailed, w.InferenceURL(), resp.Status, tail(string(raw), 400))
	}
	var parsed struct {
		Text string `json:"text"`
	}
	if err := json.Unmarshal(raw, &parsed); err != nil {
		return Result{}, fmt.Errorf("%w: %s answered non-JSON: %w (%s)", ErrTranscribeFailed, w.InferenceURL(), err, tail(string(raw), 400))
	}
	return Result{Text: CleanTranscript(parsed.Text), Duration: time.Since(start)}, nil
}

// WhisperCLI transcribes by spawning whisper.cpp's whisper-cli per utterance.
// (Mirrored from voice-relay/internal/transcribe WhisperCLI, minus the
// ROCm-host GPU pinning that has no meaning on this Mac.)
//
// The zero value is not usable; BinPath and ModelPath must be set.
type WhisperCLI struct {
	// BinPath is the whisper-cli executable.
	BinPath string

	// ModelPath is the ggml model file, e.g. ggml-base.en.bin.
	ModelPath string

	// Threads is the CPU thread count. Zero uses the binary's own default.
	Threads int

	// Language is the spoken language code, e.g. "en". Empty uses the
	// binary's own default.
	Language string
}

// Args returns the whisper-cli argument list for the given input file,
// exported so the exact command can be logged and reproduced by hand.
func (w *WhisperCLI) Args(wavPath string) []string {
	args := []string{
		"-m", w.ModelPath,
		"-f", wavPath,
		"-nt", // no timestamps: we want a bare transcript
		"-np", // no prints: keep stdout to the results only
	}
	if w.Threads > 0 {
		args = append(args, "-t", fmt.Sprint(w.Threads))
	}
	if w.Language != "" {
		args = append(args, "-l", w.Language)
	}
	return args
}

// Transcribe runs whisper-cli over wavPath and returns the cleaned
// transcript.
func (w *WhisperCLI) Transcribe(ctx context.Context, wavPath string) (Result, error) {
	if w.BinPath == "" {
		return Result{}, fmt.Errorf("%w: whisper-cli path is empty", ErrNotConfigured)
	}
	if w.ModelPath == "" {
		return Result{}, fmt.Errorf("%w: model path is empty", ErrNotConfigured)
	}

	args := w.Args(wavPath)
	cmd := exec.CommandContext(ctx, w.BinPath, args...)
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	start := time.Now()
	if err := cmd.Run(); err != nil {
		return Result{}, fmt.Errorf("%w: %s %s: %w (stderr: %s)",
			ErrTranscribeFailed, w.BinPath, strings.Join(args, " "), err, tail(stderr.String(), 400))
	}
	return Result{Text: CleanTranscript(stdout.String()), Duration: time.Since(start)}, nil
}

// CleanTranscript normalises whisper output into a single-line transcript.
// (Mirrored from voice-relay/internal/transcribe.)
//
// whisper emits one line per segment, often with leading spaces, and marks
// non-speech with bracketed tags such as [BLANK_AUDIO] or [SILENCE]. Those
// tags are dropped so that "no speech" is reported as an empty transcript
// rather than as literal text the interview would record as an answer.
func CleanTranscript(raw string) string {
	var parts []string
	for _, line := range strings.Split(raw, "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		if isNonSpeechTag(line) {
			continue
		}
		parts = append(parts, line)
	}
	return strings.TrimSpace(strings.Join(parts, " "))
}

// isNonSpeechTag reports whether a whole line is a single bracketed
// non-speech marker such as "[BLANK_AUDIO]".
func isNonSpeechTag(line string) bool {
	if !strings.HasPrefix(line, "[") || !strings.HasSuffix(line, "]") {
		return false
	}
	// Reject lines that merely start and end with brackets but contain real
	// text between two separate tags.
	return !strings.Contains(strings.TrimSuffix(strings.TrimPrefix(line, "["), "]"), "[")
}
