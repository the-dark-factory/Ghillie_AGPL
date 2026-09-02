//go:build darwin

package voice

// Platform returns whether the voice surface can run here, and if not, why.
//
// The voice pipeline shells out to a settled set of macOS tools — `say` and
// `afplay` for playback, ffmpeg's `avfoundation` input for capture, and the
// respire checkout for the breath pipeline. None of that is portable as it
// stands, so the capability is declared per platform rather than discovered at
// the moment somebody tries to speak.
//
// Declaring it up front matters: the alternative is a partner on Windows
// hitting "no such file or directory" on a missing pipeline path and concluding
// the product is broken, when in fact the product is fine and the surface is
// simply not built for their machine yet.
func Platform() (ok bool, why string) { return true, "" }
