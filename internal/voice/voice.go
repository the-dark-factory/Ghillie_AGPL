// Package voice is ghillie's speech seam: text in, rendered audio out. It is
// the FIRST consumer of the v2 voice path, and deliberately the smallest honest
// one — offline, local playback on this machine, replies still typed.
//
// The pipeline it drives is the SETTLED one (~/ObVault/ghillie-voice/
// VOICE_PIPELINE.md, Tony approved P4_planned_quiet.wav 2026-07-30): text-planned
// breath opportunities, the Ada engine (Respire.Lungs) as the SOLE authority on
// whether and how many breaths, 20 ms constant-power splice, single speaker,
// no WORLD. This package does not reimplement any of that — it shells out to
// the reconstructed harness (respire demo/textplan.py, harness_rev
// breath-4-textplan-r2) exactly as voice-relay shells out to whisper-cli: a
// fixed external command, exported Args so the exact invocation can be logged
// and reproduced by hand, explicit errors, context first.
//
// ★ NO BREATH IS INVENTED HERE. Every breath inside an utterance comes from
// the engine schedule and is recorded in the plan sidecar; Render refuses any
// sidecar whose breaths do not each carry an engine verdict. The one breath
// this seam adds OUTSIDE an utterance — the offered-floor in-breath after a
// question — is not speech and not in any sidecar; it is a hand-over signal,
// and internal/interview/voice.go documents why it exists and why only there.
//
// Deliberately out of this slice: microphone, relay, barge-in, whisper,
// streaming, daemons. Latency is recorded, never claimed. Known accepted
// deviation: lung state resets per utterance — continuity across an utterance
// sequence is a recorded refinement, not tonight's work.
package voice

import "errors"

// Errors reported by this package.
var (
	// ErrNotConfigured reports a seam missing a required path.
	ErrNotConfigured = errors.New("voice: not configured")

	// ErrRenderFailed reports that the render process failed to run, exited
	// non-zero, or produced no usable output.
	ErrRenderFailed = errors.New("voice: render failed")

	// ErrPlaybackFailed reports that local playback failed.
	ErrPlaybackFailed = errors.New("voice: playback failed")

	// ErrPlanViolation reports a plan sidecar carrying a breath the engine did
	// not schedule. It should be unreachable; seeing it means the harness has
	// drifted from the settled pipeline and the render must not be trusted.
	ErrPlanViolation = errors.New("voice: PLAN VIOLATION")
)
