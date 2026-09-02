package ears

// detector.go is the energy-gate speech detector with hysteresis, MIRRORED
// from voice-relay/internal/audio (SpeechDetector + RMS) — that repo is
// read-only to this one and deliberately not imported; what is worth keeping
// is the pattern: a single loud frame is usually a keystroke or a door, a
// single quiet frame is usually a pause between words, so both edges require
// a run of frames before the state flips.
//
// Here the detector serves a smaller job than voice-relay's: there is no
// barge-in to raise (the mic never listens while ghillie speaks), only the
// end of the interviewee's turn to notice.

import (
	"fmt"
	"math"
)

// RMS returns the root-mean-square amplitude of canonical PCM, normalised so
// that 0 is silence and 1 is full scale.
func RMS(pcm []byte) (level float64, err error) {
	if len(pcm)%BytesPerSample != 0 {
		return 0, fmt.Errorf("%w: %d bytes is not a whole number of samples", ErrCaptureFailed, len(pcm))
	}
	if len(pcm) == 0 {
		return 0, nil
	}
	var sumSquares float64
	for i := 0; i+1 < len(pcm); i += 2 {
		// Little-endian signed 16-bit.
		sample := float64(int16(uint16(pcm[i]) | uint16(pcm[i+1])<<8))
		sumSquares += sample * sample
	}
	n := float64(len(pcm) / BytesPerSample)
	return math.Sqrt(sumSquares/n) / float64(math.MaxInt16), nil
}

// SpeechDetector is an energy-gate voice activity detector with hysteresis:
// speech is reported once onsetFrames consecutive frames exceed the threshold,
// and silence again once releaseFrames consecutive frames fall below it.
type SpeechDetector struct {
	threshold     float64
	onsetFrames   int
	releaseFrames int

	loudRun  int
	quietRun int
	speaking bool
}

// NewSpeechDetector returns a detector over the given threshold and runs.
// Non-positive run lengths are clamped to 1.
func NewSpeechDetector(threshold float64, onsetFrames, releaseFrames int) *SpeechDetector {
	if onsetFrames < 1 {
		onsetFrames = 1
	}
	if releaseFrames < 1 {
		releaseFrames = 1
	}
	return &SpeechDetector{
		threshold:     threshold,
		onsetFrames:   onsetFrames,
		releaseFrames: releaseFrames,
	}
}

// Observe feeds one frame of canonical PCM to the detector and returns the
// current speaking state.
func (d *SpeechDetector) Observe(pcm []byte) (speaking bool, err error) {
	level, err := RMS(pcm)
	if err != nil {
		return d.speaking, err
	}
	return d.observeLevel(level), nil
}

// observeLevel advances the hysteresis state machine for one frame's level. It
// is split out from Observe so tests can drive levels directly.
func (d *SpeechDetector) observeLevel(level float64) bool {
	if level > d.threshold {
		d.loudRun++
		d.quietRun = 0
		if !d.speaking && d.loudRun >= d.onsetFrames {
			d.speaking = true
		}
		return d.speaking
	}
	d.quietRun++
	d.loudRun = 0
	if d.speaking && d.quietRun >= d.releaseFrames {
		d.speaking = false
	}
	return d.speaking
}

// Speaking reports the detector's current state.
func (d *SpeechDetector) Speaking() bool { return d.speaking }
