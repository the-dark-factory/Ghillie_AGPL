// Package ears is ghillie's listening seam: the microphone in, text out. It is
// the SECOND consumer of the v2 voice path and deliberately the smallest honest
// one — the interviewee can ANSWER BY VOICE, and nothing more.
//
// ★ THE MIC NEVER LISTENS WHILE GHILLIE SPEAKS. Capture is opened by the voice
// surface AFTER a question's offered-floor breath has finished playing — inside
// the reply window that was always there — and it closes when the person stops
// talking. There is no barge-in here, no VAD-driven interruption, no floor
// metagame: the standing principle (ghillie NEVER competes for the floor) is
// kept by construction, because no code path in this package can run while
// playback is still happening. /cut remains the keyboard's affordance.
//
// The shape of everything here is mirrored from voice-relay (~/dev/voice-relay,
// READ-ONLY to this repo, deliberately not imported — its module is private to
// its own deployment): internal/audio's canonical 16 kHz mono s16 format and
// SpeechDetector hysteresis, internal/transcribe's Transcriber seam and
// exec-boundary discipline (fixed external command, exported Args, ctx first,
// explicit errors carrying stderr). Provenance comments mark each mirror.
//
// Deliberately out of this slice: barge-in, streaming, the relay, any
// deployment off this machine. Typed input remains available and is the
// honest fallback whenever an ear fails.
package ears

import (
	"errors"
	"time"
)

// Canonical capture parameters. whisper.cpp requires exactly 16 kHz mono
// 16-bit PCM; these mirror voice-relay/internal/audio's canonical constants.
const (
	// SampleRate is the canonical sample rate in Hz.
	SampleRate = 16000

	// Channels is the canonical channel count.
	Channels = 1

	// BytesPerSample is the canonical sample width in bytes (16-bit PCM).
	BytesPerSample = 2

	// FrameDuration is the analysis frame the end-of-turn detector observes.
	FrameDuration = 20 * time.Millisecond

	// FrameBytes is the canonical PCM byte count of one analysis frame:
	// 20 ms at 16 kHz mono s16 = 320 samples = 640 bytes.
	FrameBytes = int(FrameDuration*SampleRate/time.Second) * Channels * BytesPerSample
)

// End-of-turn constants. These are CONDUCT and they are compiled in, like every
// other conduct decision in this binary.
const (
	// DefaultSpeechThreshold is the RMS level above which a frame counts as
	// speech. The value is voice-relay's calibration: chosen against the
	// built-in MacBook Pro microphone in a quiet room; noisier environments
	// need it raised.
	DefaultSpeechThreshold = 0.02

	// DefaultOnsetFrames is how many consecutive loud frames mark the person
	// as speaking: 3 frames = 60 ms, so a keystroke or a door does not open a
	// turn (the hysteresis rationale, mirrored from voice-relay's detector).
	DefaultOnsetFrames = 3

	// DefaultEndOfTurnFrames is how many consecutive quiet frames END the
	// person's turn once they have spoken: 60 frames = 1.2 s. Generous on
	// purpose — a pause to think mid-answer is not the end of a turn, and
	// cutting one short would be ghillie competing for the floor by clock.
	DefaultEndOfTurnFrames = 60

	// DefaultMaxTurn is the safety stop on one captured answer. It is not a
	// reply deadline — the typed surface still waits forever — it is the
	// bound on how much audio one utterance file may hold before it is sent
	// for transcription as it stands.
	DefaultMaxTurn = 120 * time.Second
)

// Errors reported by this package.
var (
	// ErrNotConfigured reports a seam missing a required path.
	ErrNotConfigured = errors.New("ears: not configured")

	// ErrCaptureFailed reports that microphone capture could not start or
	// ended unexpectedly.
	ErrCaptureFailed = errors.New("ears: capture failed")

	// ErrTranscribeFailed reports that transcription failed to run or the
	// transcriber refused the audio.
	ErrTranscribeFailed = errors.New("ears: transcription failed")
)

// Logger is the sink for the ears' own narration, separate from what the
// client sees. A nil logger is a legitimate configuration.
type Logger interface {
	Printf(format string, v ...any)
}
