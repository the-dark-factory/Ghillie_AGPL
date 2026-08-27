package interview

// voice.go is the v2 voice surface, in its smallest honest form: ghillie's
// lines are SPOKEN through the settled pipeline (internal/voice → the
// textplan.py harness → local playback), and the reply is still TYPED. Microphone,
// barge-in and streaming are deliberately not here — the /cut affordance
// remains the keyboard's honest stand-in for cutting ghillie off.
//
// ★ DESIGN DECISION (seat's, 2026-07-30, flagged for Tony's ear):
// SAY IS NON-OFFERING; ONLY ASK OFFERS THE FLOOR.
//
// Ghillie's opening turn emits several consecutive Say lines. If every
// utterance ended on an offered-floor breath, ghillie would audibly invite a
// turn at the end of each line and then immediately talk over the turn it just
// invited — accidental floor competition, which the standing principle forbids
// outright (ghillie NEVER competes for the floor; breath OFFERS the floor).
// So the offer is reserved for the one place ghillie genuinely wants the
// floor taken:
//
//   - Say renders and plays the line, full stop. No trailing breath. The
//     engine still breathes WITHIN the utterance where it must — that is
//     physiology, licensed breath by breath in the plan sidecar — but the
//     utterance does not end on an invitation.
//
//   - Ask renders and plays the question, then plays ONE audible offered-floor
//     in-breath (medium class, speaker 1089, −4.9 dB — the approved run's
//     discipline, see internal/voice/breath.go), and then IMMEDIATELY opens
//     the reply capture. The hand-over signal and the listening window are one
//     call, in one method, and cannot drift apart: there is no code path that
//     plays the offer without opening the capture, and none that opens the
//     capture without the offer.
//
// Known accepted deviation: lung state resets per utterance. Continuity across
// an utterance sequence is a recorded refinement, not tonight's work.
//
// ★ EARS (the second slice, same night): when a Listener is attached, the
// reply to Ask can arrive BY VOICE. The microphone opens ONLY inside the
// reply window the offer already created — after the question has played and
// after the offered-floor breath has played — so the mic NEVER listens while
// ghillie speaks and the standing principle is kept by construction, not by
// vigilance. The transcript is echoed to the console before it goes on the
// record; an empty or failed transcription is an HONEST TYPED REPROMPT,
// never a silent skip; /cut stays typed. Deliberately absent: barge-in,
// VAD-driven interruption, the floor metagame.

import (
	"context"
	"strings"

	"github.com/tonygair/ghillie/internal/voice"
)

// SpeechRenderer renders one utterance to disk through the settled pipeline.
// It is the consumer-side view of voice.TextPlan.
type SpeechRenderer interface {
	// Render turns text into a validated Rendering — WAV plus plan sidecar in
	// which every breath carries an engine verdict.
	Render(ctx context.Context, text string) (voice.Rendering, error)
}

// FloorBreath prepares the audible offered-floor in-breath for a question. It
// is the consumer-side view of voice.BreathBank.
type FloorBreath interface {
	// OfferedFloor returns the path of a level-matched breath WAV for the
	// question whose rendered speech is at speechWav.
	OfferedFloor(ctx context.Context, text, speechWav string) (string, error)
}

// Listener captures one spoken reply from the microphone and returns its
// transcript. It is the consumer-side view of ears.Ears. An empty string
// with a nil error means the mic worked and no speech was found.
type Listener interface {
	// Listen opens the microphone, waits through the person's answer, and
	// returns what was heard as text.
	Listen(ctx context.Context) (heard string, err error)
}

// VoiceSurface speaks ghillie's side of the interview aloud and takes the
// client's side typed. It implements Surface without interview.go changing —
// the seam doing exactly what it was documented to do.
//
// Reply capture is COMPOSITION, not reimplementation: an embedded
// ConsoleSurface does all the reading, so the /cut affordance and the
// no-deadline promise ("how long ghillie waits is conduct, compiled in")
// survive untouched and untested-twice.
type VoiceSurface struct {
	render  SpeechRenderer
	breath  FloorBreath
	play    voice.Player
	console *ConsoleSurface

	// ears, when non-nil, lets the interviewee ANSWER BY VOICE. The mic
	// opens only after the offered-floor breath has finished playing; typed
	// input remains the fallback and /cut stays typed. See WithEars.
	ears Listener

	// fallbackText permits carrying on in text when the voice pipeline fails.
	// It is OFF by default and set only by an explicit flag: a voiceless run
	// must be loud — a surface that silently stopped speaking would be lying
	// about what the client experienced.
	fallbackText bool

	log Logger
}

// NewVoiceSurface builds the voice surface over the pipeline seam, the local
// player, the breath bank, and the console that captures typed replies.
func NewVoiceSurface(render SpeechRenderer, play voice.Player, breath FloorBreath, console *ConsoleSurface, fallbackText bool, log Logger) *VoiceSurface {
	return &VoiceSurface{
		render:       render,
		breath:       breath,
		play:         play,
		console:      console,
		fallbackText: fallbackText,
		log:          log,
	}
}

// Say renders and plays one line, NON-OFFERING — no trailing breath, see the
// design decision at the top of this file. The line is echoed to the console
// after it plays, so the interview also remains readable.
func (v *VoiceSurface) Say(ctx context.Context, line string) error {
	if err := v.speakAloud(ctx, line); err != nil {
		if ferr := v.fallback(err); ferr != nil {
			return ferr
		}
	}
	return v.console.Say(ctx, line)
}

// Ask puts a question aloud, offers the floor with one audible in-breath, and
// opens the typed reply capture — the offer and the listening window as one
// call. The wait itself is the embedded console's: no deadline, ever.
func (v *VoiceSurface) Ask(ctx context.Context, question string) (Reply, error) {
	r, err := v.render.Render(ctx, question)
	if err != nil {
		return v.fallbackAsk(ctx, question, err)
	}
	if err := v.play.Play(ctx, r.WavPath); err != nil {
		return v.fallbackAsk(ctx, question, err)
	}

	// ★ THE OFFER. Exactly one, exactly here, and the capture opens in the
	// same breath of control flow — the hand-over signal and the listening
	// window cannot drift apart.
	breathWav, err := v.breath.OfferedFloor(ctx, question, r.WavPath)
	if err != nil {
		return v.fallbackAsk(ctx, question, err)
	}
	if err := v.play.Play(ctx, breathWav); err != nil {
		return v.fallbackAsk(ctx, question, err)
	}

	// The reply window. With ears attached, the MICROPHONE opens here — and
	// only here, strictly after both plays above have returned — otherwise
	// the reply is typed, exactly as before.
	if v.ears != nil {
		return v.askByEar(ctx, question)
	}
	return v.console.Ask(ctx, question)
}

// WithEars attaches a Listener so the reply to Ask can arrive by voice. It
// returns the surface for chaining. A nil listener leaves the surface typed.
func (v *VoiceSurface) WithEars(ears Listener) *VoiceSurface {
	v.ears = ears
	return v
}

// askByEar captures the reply from the microphone and feeds the TRANSCRIPT
// through the same reply path a typed answer takes — the conduct and attempt
// machinery upstream cannot tell the difference and must not.
//
// Failure handling is the honest kind, in both directions: a mic or
// transcription failure is said out loud (operator log AND the client's
// console) and the question falls back to the TYPED path — never a silent
// skip, never a fatal stop for something the keyboard can still answer.
func (v *VoiceSurface) askByEar(ctx context.Context, question string) (Reply, error) {
	if err := v.console.showListening(ctx, question); err != nil {
		return Reply{}, err
	}
	heard, err := v.ears.Listen(ctx)
	if err != nil {
		v.logf("⚠ EARS FAILED — reprompting for a TYPED answer: %v", err)
		return v.repromptTyped(ctx, question)
	}
	if strings.TrimSpace(heard) == "" {
		v.logf("ears heard no speech — reprompting for a TYPED answer")
		return v.repromptTyped(ctx, question)
	}
	if err := v.console.showHeard(ctx, heard); err != nil {
		return Reply{}, err
	}
	return Reply{Text: heard}, nil
}

// repromptTyped is the honest fallback when an ear produced nothing usable:
// the client is told, on the console, and the question is put again on the
// TYPED path — where the /cut affordance still lives.
func (v *VoiceSurface) repromptTyped(ctx context.Context, question string) (Reply, error) {
	if err := v.console.Say(ctx, "I did not catch that by ear. Please type your answer instead."); err != nil {
		return Reply{}, err
	}
	return v.console.Ask(ctx, question)
}

// speakAloud renders and plays one utterance with no floor offer.
func (v *VoiceSurface) speakAloud(ctx context.Context, line string) error {
	r, err := v.render.Render(ctx, line)
	if err != nil {
		return err
	}
	v.logf("spoke %q: audio %s, render wall %s, %d engine breath(s)", head(line), r.Audio, r.Wall, r.Breaths)
	return v.play.Play(ctx, r.WavPath)
}

// fallback decides what a pipeline failure means: an honest error by default,
// or — only when the operator explicitly asked for it — a LOUD note and
// permission to carry on in text.
func (v *VoiceSurface) fallback(err error) error {
	if !v.fallbackText {
		return err
	}
	v.logf("⚠ VOICE PIPELINE FAILED — carrying on in TEXT because -voice-fallback-text is set: %v", err)
	return nil
}

// fallbackAsk applies the fallback rule to a question: on permission, the
// question is put on the console — which prints it and opens the capture, so
// the floor is still offered, in text.
func (v *VoiceSurface) fallbackAsk(ctx context.Context, question string, cause error) (Reply, error) {
	if ferr := v.fallback(cause); ferr != nil {
		return Reply{}, ferr
	}
	return v.console.Ask(ctx, question)
}

// logf narrates for the operator; a nil logger is a legitimate configuration.
func (v *VoiceSurface) logf(format string, args ...any) {
	if v.log == nil {
		return
	}
	v.log.Printf("  voice │ "+format, args...)
}

// head shortens an utterance for a log line.
func head(s string) string {
	const n = 32
	if len(s) <= n {
		return s
	}
	return s[:n] + "…"
}
