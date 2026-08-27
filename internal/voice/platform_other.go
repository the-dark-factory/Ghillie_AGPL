//go:build !darwin

package voice

import "runtime"

// Platform reports that the voice surface is not available on this OS, and says
// exactly why and what still works.
//
// ★ IT REFUSES LOUDLY RATHER THAN DOWNGRADING SILENTLY. The same rule the
// demo scripts already hold: a run that quietly delivers less than it advertised
// is a dishonest run. So a request for voice on a platform that cannot provide
// it is an error with an explanation, never a shrug into text.
//
// What is missing is the SURFACE, not the product. The interview, the conduct
// wall, enrolment and attestation, signed frames, TOFU pinning, the facade
// protocol and briefs are all pure Go and work identically here. Only speaking
// and listening are macOS-bound today:
//
//   - playback shells out to `afplay` / `say`;
//   - the breath pipeline lives in the respire checkout;
//   - capture uses ffmpeg's `avfoundation` input, which is macOS-only —
//     Windows would need `dshow` and Linux `alsa`/`pulse`.
//
// None of that is hard, but none of it is done, and saying so plainly is better
// than letting someone discover it as a path error.
func Platform() (ok bool, why string) {
	return false, "the voice surface is not built for " + runtime.GOOS + " yet — " +
		"playback (afplay/say), the breath pipeline (respire) and microphone capture " +
		"(ffmpeg avfoundation) are macOS-bound today. Everything else works: run without " +
		"-voice for the full text interview."
}
