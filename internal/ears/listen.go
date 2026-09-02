package ears

// listen.go composes the two seams into the one verb the voice surface
// needs: Listen captures one answer from the microphone and returns its
// transcript. The surface decides what an empty or failed transcript means
// (an honest typed reprompt — never a silent skip); this file only reports
// what was heard.

import (
	"context"
	"fmt"
)

// Ears is the composed listening path: one capture, one transcription.
type Ears struct {
	// Capture opens and closes the microphone window.
	Capture *Capture

	// Transcriber turns the captured WAV into text.
	Transcriber Transcriber

	// Log narrates for the operator. Nil is a legitimate configuration.
	Log Logger
}

// Listen records one answer from the microphone and returns the cleaned
// transcript. An empty string with a nil error means the mic worked and the
// model found no speech — the caller's honest-reprompt case.
func (e *Ears) Listen(ctx context.Context) (heard string, err error) {
	if e.Capture == nil || e.Transcriber == nil {
		return "", fmt.Errorf("%w: ears need both a capture and a transcriber", ErrNotConfigured)
	}
	wavPath, err := e.Capture.CaptureUtterance(ctx)
	if err != nil {
		return "", err
	}
	result, err := e.Transcriber.Transcribe(ctx, wavPath)
	if err != nil {
		return "", err
	}
	e.logf("heard %s → %q (transcribed in %s)", wavPath, result.Text, result.Duration)
	return result.Text, nil
}

// logf narrates for the operator; a nil logger is a legitimate configuration.
func (e *Ears) logf(format string, args ...any) {
	if e.Log == nil {
		return
	}
	e.Log.Printf("  ears │ "+format, args...)
}
