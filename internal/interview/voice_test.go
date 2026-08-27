package interview

// Tests without an ear, surface side: what is asserted is the CONDUCT of the
// voice surface — Say never offers the floor, Ask offers it exactly once and
// only after the question has finished playing, a pipeline failure is loud,
// and the /cut affordance rides through composition untouched. No test here
// pretends to judge sound.

import (
	"bytes"
	"context"
	"errors"
	"io"
	"strings"
	"testing"

	"github.com/tonygair/ghillie/internal/voice"
)

// events is a shared ordered record of everything the fakes saw happen.
type events struct{ log []string }

func (e *events) add(s string) { e.log = append(e.log, s) }

// index returns the position of the first event with the given prefix, or -1.
func (e *events) index(prefix string) int {
	for i, s := range e.log {
		if strings.HasPrefix(s, prefix) {
			return i
		}
	}
	return -1
}

// count returns how many events carry the given prefix.
func (e *events) count(prefix string) (n int) {
	for _, s := range e.log {
		if strings.HasPrefix(s, prefix) {
			n++
		}
	}
	return n
}

// fakeRenderer stands in for the pipeline seam.
type fakeRenderer struct {
	ev  *events
	err error
}

func (f *fakeRenderer) Render(_ context.Context, text string) (voice.Rendering, error) {
	if f.err != nil {
		return voice.Rendering{}, f.err
	}
	f.ev.add("render:" + text)
	return voice.Rendering{WavPath: "speech.wav", PlanPath: "speech.wav.json", Breaths: 1}, nil
}

// fakePlayer stands in for local playback.
type fakePlayer struct {
	ev  *events
	err error
}

func (f *fakePlayer) Play(_ context.Context, wavPath string) error {
	if f.err != nil {
		return f.err
	}
	f.ev.add("play:" + wavPath)
	return nil
}

// fakeBreath stands in for the breath bank.
type fakeBreath struct {
	ev  *events
	err error
}

func (f *fakeBreath) OfferedFloor(_ context.Context, _, _ string) (string, error) {
	if f.err != nil {
		return "", f.err
	}
	f.ev.add("breath-prepared")
	return "offered-floor.breath.wav", nil
}

// captureReader wraps typed input and records the moment the reply capture
// first actually reads — the opening of the listening window.
type captureReader struct {
	ev *events
	r  io.Reader
}

func (c *captureReader) Read(p []byte) (int, error) {
	if c.ev.index("capture-open") < 0 {
		c.ev.add("capture-open")
	}
	return c.r.Read(p)
}

// voiceHarness wires a VoiceSurface over fakes and a typed reply.
func voiceHarness(ev *events, typed string, renderErr, playErr, breathErr error, fallback bool) (*VoiceSurface, *bytes.Buffer) {
	var out bytes.Buffer
	console := NewConsoleSurface(&captureReader{ev: ev, r: strings.NewReader(typed)}, &out)
	s := NewVoiceSurface(
		&fakeRenderer{ev: ev, err: renderErr},
		&fakePlayer{ev: ev, err: playErr},
		&fakeBreath{ev: ev, err: breathErr},
		console, fallback, nil)
	return s, &out
}

// TestSayIsNonOffering: several consecutive Say lines — as the opening turn
// emits — produce NO offered-floor breath and open no capture. The floor is
// never invited where ghillie would immediately talk over it.
func TestSayIsNonOffering(t *testing.T) {
	ev := &events{}
	s, out := voiceHarness(ev, "", nil, nil, nil, false)
	lines := []string{"I am ghillie.", "I do not build it and I do not judge it.", "Cut me off whenever you like."}
	for _, l := range lines {
		if err := s.Say(context.Background(), l); err != nil {
			t.Fatalf("Say(%q): %v", l, err)
		}
	}
	if got := ev.count("breath-prepared"); got != 0 {
		t.Fatalf("Say prepared %d offered-floor breath(s); Say is non-offering", got)
	}
	if got, want := ev.count("play:"), len(lines); got != want {
		t.Fatalf("plays = %d, want %d (one per line, no trailing breath)", got, want)
	}
	if ev.index("capture-open") >= 0 {
		t.Fatal("Say opened the reply capture; only Ask does that")
	}
	for _, l := range lines {
		if !strings.Contains(out.String(), l) {
			t.Fatalf("console echo missing %q — the interview must stay readable", l)
		}
	}
}

// TestAskOffersTheFloorExactlyOnce: one question, one offered-floor breath,
// and the typed reply comes back through the embedded console.
func TestAskOffersTheFloorExactlyOnce(t *testing.T) {
	ev := &events{}
	s, _ := voiceHarness(ev, "it schedules the greenhouse watering\n", nil, nil, nil, false)
	reply, err := s.Ask(context.Background(), "What is the software for?")
	if err != nil {
		t.Fatalf("Ask: %v", err)
	}
	if reply.Text != "it schedules the greenhouse watering" || reply.Interrupted {
		t.Fatalf("reply = %+v", reply)
	}
	if got := ev.count("breath-prepared"); got != 1 {
		t.Fatalf("offered-floor breaths = %d, want exactly 1", got)
	}
	if got := ev.count("play:"); got != 2 {
		t.Fatalf("plays = %d, want 2 (question, then the one offered-floor breath)", got)
	}
}

// TestAskOpensCaptureOnlyAfterPlayback pins the order that makes the offer and
// the listening window one act: question plays, breath plays, THEN the capture
// opens. Nothing listens while ghillie is still speaking.
func TestAskOpensCaptureOnlyAfterPlayback(t *testing.T) {
	ev := &events{}
	s, _ := voiceHarness(ev, "typed answer\n", nil, nil, nil, false)
	if _, err := s.Ask(context.Background(), "Who uses it?"); err != nil {
		t.Fatalf("Ask: %v", err)
	}
	question := ev.index("play:speech.wav")
	breath := ev.index("play:offered-floor.breath.wav")
	capture := ev.index("capture-open")
	if question < 0 || breath < 0 || capture < 0 {
		t.Fatalf("missing events: %v", ev.log)
	}
	if !(question < breath && breath < capture) {
		t.Fatalf("order violated: question@%d breath@%d capture@%d (%v)", question, breath, capture, ev.log)
	}
}

// TestCutAffordanceSurvivesComposition: /cut typed at a voiced question still
// reports an interruption — the console's affordance, untouched by the wrap.
func TestCutAffordanceSurvivesComposition(t *testing.T) {
	ev := &events{}
	s, _ := voiceHarness(ev, "/cut just make it stop beeping\n", nil, nil, nil, false)
	reply, err := s.Ask(context.Background(), "What it has to talk to?")
	if err != nil {
		t.Fatalf("Ask: %v", err)
	}
	if !reply.Interrupted || reply.Text != "just make it stop beeping" {
		t.Fatalf("reply = %+v, want the interrupted path", reply)
	}
	if reply.Heard == "" || len(reply.Heard) >= len("What it has to talk to?") {
		t.Fatalf("Heard = %q — the unheard rest must not be returned", reply.Heard)
	}
}

// TestPipelineFailureIsLoudByDefault: without -voice-fallback-text, any
// pipeline failure surfaces as an error — a voiceless run never limps on
// silently pretending it spoke.
func TestPipelineFailureIsLoudByDefault(t *testing.T) {
	boom := errors.New("kokoro fell over")
	cases := []struct {
		name                          string
		renderErr, playErr, breathErr error
	}{
		{"render fails", boom, nil, nil},
		{"playback fails", nil, boom, nil},
		{"breath preparation fails", nil, nil, boom},
	}
	for _, tc := range cases {
		t.Run(tc.name+" on Ask", func(t *testing.T) {
			ev := &events{}
			s, _ := voiceHarness(ev, "never read\n", tc.renderErr, tc.playErr, tc.breathErr, false)
			if _, err := s.Ask(context.Background(), "Q?"); !errors.Is(err, boom) {
				t.Fatalf("Ask = %v, want the pipeline failure", err)
			}
			if ev.index("capture-open") >= 0 {
				t.Fatal("capture opened despite the failure being fatal")
			}
		})
	}
	t.Run("render fails on Say", func(t *testing.T) {
		ev := &events{}
		s, _ := voiceHarness(ev, "", boom, nil, nil, false)
		if err := s.Say(context.Background(), "line"); !errors.Is(err, boom) {
			t.Fatalf("Say = %v, want the pipeline failure", err)
		}
	})
}

// TestExplicitFallbackCarriesOnInText: with the flag set, the same failures
// fall back to the console — the question is still put and the reply still
// captured, in text, and Say still reaches the screen.
func TestExplicitFallbackCarriesOnInText(t *testing.T) {
	boom := errors.New("kokoro fell over")
	cases := []struct {
		name                          string
		renderErr, playErr, breathErr error
	}{
		{"render fails", boom, nil, nil},
		{"playback fails", nil, boom, nil},
		{"breath preparation fails", nil, nil, boom},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			ev := &events{}
			s, out := voiceHarness(ev, "a typed answer\n", tc.renderErr, tc.playErr, tc.breathErr, true)
			reply, err := s.Ask(context.Background(), "Q?")
			if err != nil {
				t.Fatalf("Ask with fallback: %v", err)
			}
			if reply.Text != "a typed answer" {
				t.Fatalf("reply = %+v", reply)
			}
			if err := s.Say(context.Background(), "still here"); err != nil {
				t.Fatalf("Say with fallback: %v", err)
			}
			if !strings.Contains(out.String(), "still here") {
				t.Fatal("fallback Say never reached the console")
			}
		})
	}
}

// TestVoiceSurfaceIsASurface pins the seam: the voice surface satisfies the
// documented interface without interview.go having changed.
func TestVoiceSurfaceIsASurface(t *testing.T) {
	var _ Surface = (*VoiceSurface)(nil)
}

// ---------------------------------------------------------------- ears tests

// fakeListener stands in for the microphone-and-whisper path.
type fakeListener struct {
	ev    *events
	heard string
	err   error
}

func (f *fakeListener) Listen(_ context.Context) (string, error) {
	f.ev.add("mic-open")
	if f.err != nil {
		return "", f.err
	}
	return f.heard, nil
}

// earHarness wires a VoiceSurface with ears attached over fakes, typed input
// standing by as the fallback.
func earHarness(ev *events, typed, heard string, listenErr error) (*VoiceSurface, *bytes.Buffer) {
	var out bytes.Buffer
	console := NewConsoleSurface(&captureReader{ev: ev, r: strings.NewReader(typed)}, &out)
	s := NewVoiceSurface(
		&fakeRenderer{ev: ev},
		&fakePlayer{ev: ev},
		&fakeBreath{ev: ev},
		console, false, nil).
		WithEars(&fakeListener{ev: ev, heard: heard, err: listenErr})
	return s, &out
}

// TestEarsMicOpensOnlyAfterPlayback pins THE invariant of this slice: the
// microphone opens strictly after the question has played and after the
// offered-floor breath has played. The mic never listens while ghillie
// speaks — by ORDER OF EVENTS, not by promise.
func TestEarsMicOpensOnlyAfterPlayback(t *testing.T) {
	ev := &events{}
	s, _ := earHarness(ev, "", "it waters the greenhouse", nil)
	reply, err := s.Ask(context.Background(), "What is it for?")
	if err != nil {
		t.Fatalf("Ask: %v", err)
	}
	if reply.Text != "it waters the greenhouse" || reply.Interrupted {
		t.Fatalf("reply = %+v", reply)
	}
	question := ev.index("play:speech.wav")
	breath := ev.index("play:offered-floor.breath.wav")
	mic := ev.index("mic-open")
	if question < 0 || breath < 0 || mic < 0 {
		t.Fatalf("missing events: %v", ev.log)
	}
	if !(question < breath && breath < mic) {
		t.Fatalf("order violated: question@%d breath@%d mic@%d (%v)", question, breath, mic, ev.log)
	}
}

// TestEarsNeverOpenForSay: Say lines offer no floor and open no microphone —
// the mic is an Ask-only, post-offer affair.
func TestEarsNeverOpenForSay(t *testing.T) {
	ev := &events{}
	s, _ := earHarness(ev, "", "should never be heard", nil)
	if err := s.Say(context.Background(), "I am ghillie."); err != nil {
		t.Fatalf("Say: %v", err)
	}
	if ev.index("mic-open") >= 0 {
		t.Fatal("Say opened the microphone; only Ask's reply window may")
	}
}

// TestEarsEchoTheTranscript: what was heard is shown to the interviewee on
// the console before it goes on the record.
func TestEarsEchoTheTranscript(t *testing.T) {
	ev := &events{}
	s, out := earHarness(ev, "", "keep the beds damp overnight", nil)
	if _, err := s.Ask(context.Background(), "What must it do at night?"); err != nil {
		t.Fatalf("Ask: %v", err)
	}
	if !strings.Contains(out.String(), "keep the beds damp overnight") {
		t.Fatalf("the transcript was never echoed; console showed:\n%s", out.String())
	}
	if !strings.Contains(out.String(), "heard by ear") {
		t.Fatalf("the echo does not say it came by ear; console showed:\n%s", out.String())
	}
}

// TestEarsFailureRepromptsTyped: a broken mic or transcriber is said out
// loud on the console and the question falls back to the TYPED path — the
// reply still arrives, from the keyboard, never silently skipped.
func TestEarsFailureRepromptsTyped(t *testing.T) {
	cases := []struct {
		name      string
		heard     string
		listenErr error
	}{
		{"the ear fails outright", "", errors.New("ffmpeg fell over")},
		{"the ear hears nothing", "", nil},
		{"the ear hears only whitespace", "   ", nil},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			ev := &events{}
			s, out := earHarness(ev, "a typed answer instead\n", tc.heard, tc.listenErr)
			reply, err := s.Ask(context.Background(), "Q?")
			if err != nil {
				t.Fatalf("Ask: %v", err)
			}
			if reply.Text != "a typed answer instead" {
				t.Fatalf("reply = %+v, want the typed fallback", reply)
			}
			if ev.index("mic-open") < 0 {
				t.Fatal("the mic never opened at all")
			}
			if capture := ev.index("capture-open"); capture < 0 || capture < ev.index("mic-open") {
				t.Fatalf("typed capture did not follow the failed ear: %v", ev.log)
			}
			if !strings.Contains(out.String(), "did not catch that by ear") {
				t.Fatalf("the fallback was silent toward the client; console showed:\n%s", out.String())
			}
		})
	}
}

// TestEarsCutStaysTyped: /cut is the KEYBOARD's affordance and it survives
// the ears — typed on the reprompt path, it still reports an interruption
// with the unheard rest withheld.
func TestEarsCutStaysTyped(t *testing.T) {
	ev := &events{}
	s, _ := earHarness(ev, "/cut just make it stop beeping\n", "", nil) // ear hears nothing → typed reprompt
	reply, err := s.Ask(context.Background(), "What it has to talk to?")
	if err != nil {
		t.Fatalf("Ask: %v", err)
	}
	if !reply.Interrupted || reply.Text != "just make it stop beeping" {
		t.Fatalf("reply = %+v, want the interrupted path", reply)
	}
	if reply.Heard == "" || len(reply.Heard) >= len("What it has to talk to?") {
		t.Fatalf("Heard = %q — the unheard rest must not be returned", reply.Heard)
	}
}

// TestEarsPipelineFailureStillFatalBeforeTheOffer: with ears attached, a
// render/play/breath failure is still loud and fatal by default, and the mic
// never opens — an offer that never happened licenses no listening.
func TestEarsPipelineFailureStillFatalBeforeTheOffer(t *testing.T) {
	boom := errors.New("kokoro fell over")
	ev := &events{}
	var out bytes.Buffer
	console := NewConsoleSurface(&captureReader{ev: ev, r: strings.NewReader("")}, &out)
	s := NewVoiceSurface(&fakeRenderer{ev: ev, err: boom}, &fakePlayer{ev: ev}, &fakeBreath{ev: ev},
		console, false, nil).WithEars(&fakeListener{ev: ev, heard: "never"})
	if _, err := s.Ask(context.Background(), "Q?"); !errors.Is(err, boom) {
		t.Fatalf("Ask = %v, want the pipeline failure", err)
	}
	if ev.index("mic-open") >= 0 {
		t.Fatal("the mic opened although the offer never played")
	}
}
