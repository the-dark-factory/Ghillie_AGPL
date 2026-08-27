package interview

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/tonygair/ghillie/internal/brief"
	"github.com/tonygair/ghillie/internal/conduct"
	"github.com/tonygair/ghillie/internal/credit"
)

// scriptedSurface answers each question from a script, so an interview runs
// without a keyboard.
type scriptedSurface struct {
	replies []Reply
	next    int
	said    []string
	asked   []string
	err     error
}

func (s *scriptedSurface) Say(_ context.Context, line string) error {
	if s.err != nil {
		return s.err
	}
	s.said = append(s.said, line)
	return nil
}

func (s *scriptedSurface) Ask(_ context.Context, question string) (Reply, error) {
	if s.err != nil {
		return Reply{}, s.err
	}
	s.asked = append(s.asked, question)
	if s.next >= len(s.replies) {
		return Reply{Text: "(nothing more to say)"}, nil
	}
	r := s.replies[s.next]
	s.next++
	return r, nil
}

func (s *scriptedSurface) transcriptOfSaid() string { return strings.Join(s.said, "\n") }

// testBrief builds a three-item brief without going near JSON.
func testBrief(t *testing.T) *brief.Brief {
	t.Helper()
	body := `{"brief_id":"deadbeef","version":2,"items":[
	  {"id":1,"want":"What the software is actually for."},
	  {"id":2,"want":"Who uses it, and what they are in the middle of doing."},
	  {"id":3,"want":"What it has to talk to."}]}`
	b, err := brief.Decode(strings.NewReader(body))
	if err != nil {
		t.Fatalf("Decode: %v", err)
	}
	return b
}

// newSession is the common construction. There is no attempt-policy argument:
// the bound is compiled-in conduct, decided by the attempt-bound core.
func newSession(s Surface) *Session {
	return New(s, credit.Courtesy{Units: 42, Stale: true}, nil)
}

// TestInterruptedMidAskLandsInterrupted is the headline test.
//
// A client who cuts ghillie off every time it puts an item exhausts the
// attempt bound, and the item must land Interrupted, must NEVER read as
// Answered, and must never be robotically restarted: each put is a fresh ask,
// only the fragments that reached the client are on the record, and the whole
// sentence appears nowhere.
func TestInterruptedMidAskLandsInterrupted(t *testing.T) {
	b := testBrief(t)
	surface := &scriptedSurface{replies: []Reply{
		{Text: "ready when you are"}, // the person REQUESTS the interview — consumed by invite(), content discarded
		{Text: "It is for booking the vans, mostly."},
		{Text: "sorry — before that, it is three depots not one", Interrupted: true, Heard: "Who uses it, and what"},
		{Text: "and the drivers book their own slots", Interrupted: true, Heard: "Who uses"},
		{Text: "also weekends work differently", Interrupted: true, Heard: ""},
		{Text: "A spreadsheet the yard manager keeps."},
	}}
	session := newSession(surface)

	answers, err := session.Conduct(context.Background(), b)
	if err != nil {
		t.Fatalf("Conduct: %v", err)
	}
	if len(answers) != 3 {
		t.Fatalf("got %d answers, want 3", len(answers))
	}

	byItem := map[int]int{}
	for i, a := range answers {
		byItem[a.ItemID] = i
	}

	interrupted := answers[byItem[2]]
	if interrupted.State != conduct.Interrupted.String() {
		t.Errorf("item 2 state = %q, want %q", interrupted.State, conduct.Interrupted)
	}
	if interrupted.Answered {
		t.Error("★ item 2 was reported ANSWERED after being cut off — A-LET-LIE-ITEM-IS-NEVER-ANSWERED does not hold")
	}
	if !interrupted.CutShort {
		t.Error("item 2 does not record that ghillie was cut off putting it")
	}
	if interrupted.Text != "also weekends work differently" {
		t.Errorf("the client's last interjection was not kept verbatim: %q", interrupted.Text)
	}

	// The other two are ordinary answers.
	for _, id := range []int{1, 3} {
		a := answers[byItem[id]]
		if !a.Answered || a.State != conduct.Answered.String() {
			t.Errorf("item %d: state %q answered=%v, want Answered/true", id, a.State, a.Answered)
		}
	}

	// ★ NOT ROBOTICALLY RESTARTED, AND BOUNDED. Every put of the item was cut
	// off, so the full text of the question must appear NOWHERE in the
	// transcript — only the fragments that reached the client, each marked.
	// And it must have been PUT exactly 3 times: the compiled-in bound,
	// ATTEMPTS-ARE-BOUNDED made visible.
	full := b.Items[1].Want
	transcript := strings.Join(session.Transcript(), "\n")
	if strings.Contains(transcript, full) {
		t.Errorf("the whole interrupted question is in the transcript — the sentence could be resumed:\n%s", transcript)
	}
	if !strings.Contains(transcript, CutOffMarker) {
		t.Errorf("the transcript does not mark where ghillie was cut off:\n%s", transcript)
	}
	asked := 0
	for _, q := range surface.asked {
		if q == full {
			asked++
		}
	}
	if asked != 3 {
		t.Errorf("the interrupted question was put %d times, want exactly 3 — the attempt bound did not hold", asked)
	}
	for _, said := range surface.said {
		if strings.Contains(strings.ToLower(said), "as i was saying") {
			t.Errorf("ghillie resumed with %q", said)
		}
	}
	if interrupted.Attempts != 3 {
		t.Errorf("attempts on the interrupted item = %d, want 3", interrupted.Attempts)
	}

	// Letting it lie is never silent: the client hears both the fresh puts and
	// the letting-go.
	said := strings.ToLower(surface.transcriptOfSaid())
	if !strings.Contains(said, "again:") {
		t.Errorf("a re-put was not announced as a fresh put:\n%s", surface.transcriptOfSaid())
	}
	if !strings.Contains(said, "leaving that one") {
		t.Errorf("ghillie let the item lie silently:\n%s", surface.transcriptOfSaid())
	}
}

// TestReputAfterInterruptionGetsTheAnswer is the other half of the attempt
// bound: an interrupted item is STILL OUTSTANDING, so the core licenses a
// fresh put, and the answer that arrives on the second attempt is held
// honestly.
func TestReputAfterInterruptionGetsTheAnswer(t *testing.T) {
	b := testBrief(t)
	surface := &scriptedSurface{replies: []Reply{
		{Text: "ready when you are"}, // the person REQUESTS the interview — consumed by invite(), content discarded
		{Text: "It is for booking the vans, mostly."},
		{Text: "sorry — before that, it is three depots not one", Interrupted: true, Heard: "Who uses it, and what"},
		{Text: "The yard manager and the drivers."},
		{Text: "A spreadsheet the yard manager keeps."},
	}}
	session := newSession(surface)

	answers, err := session.Conduct(context.Background(), b)
	if err != nil {
		t.Fatalf("Conduct: %v", err)
	}
	byItem := map[int]int{}
	for i, a := range answers {
		byItem[a.ItemID] = i
	}
	got := answers[byItem[2]]
	if !got.Answered || got.State != conduct.Answered.String() {
		t.Errorf("item 2: state %q answered=%v, want Answered/true after a licensed re-put", got.State, got.Answered)
	}
	if got.Attempts != 2 {
		t.Errorf("attempts on the re-put item = %d, want 2", got.Attempts)
	}
	if got.Text != "The yard manager and the drivers." {
		t.Errorf("the answer to the fresh put was not kept: %q", got.Text)
	}

	// The cut-off instance is on the record as a marked fragment; the fresh
	// put is on the record whole; and the fresh put was announced, never
	// resumed.
	transcript := strings.Join(session.Transcript(), "\n")
	if !strings.Contains(transcript, "Who uses it, and what"+CutOffMarker) {
		t.Errorf("the cut-off fragment is not marked in the transcript:\n%s", transcript)
	}
	if !strings.Contains(transcript, b.Items[1].Want) {
		t.Errorf("the fresh put is not on the record:\n%s", transcript)
	}
	for _, said := range surface.said {
		if strings.Contains(strings.ToLower(said), "as i was saying") {
			t.Errorf("ghillie resumed with %q", said)
		}
	}
}

// TestCutOffBeforeAWordGotOut covers the edge where nothing reached the client
// at all — on every attempt the bound allows. Nothing of the question may
// enter the transcript.
func TestCutOffBeforeAWordGotOut(t *testing.T) {
	b := testBrief(t)
	surface := &scriptedSurface{replies: []Reply{
		{Text: "ready when you are"}, // the person REQUESTS the interview — consumed by invite(), content discarded
		{Text: "actually let me start somewhere else", Interrupted: true, Heard: ""},
		{Text: "hold on, I want to begin at the depot", Interrupted: true, Heard: ""},
		{Text: "no — wait", Interrupted: true, Heard: ""},
	}}
	session := newSession(surface)

	answers, err := session.Conduct(context.Background(), b)
	if err != nil {
		t.Fatalf("Conduct: %v", err)
	}
	if answers[0].Answered || answers[0].State != conduct.Interrupted.String() {
		t.Errorf("item 1: state %q answered %v, want Interrupted/false", answers[0].State, answers[0].Answered)
	}
	transcript := strings.Join(session.Transcript(), "\n")
	if strings.Contains(transcript, b.Items[0].Want) {
		t.Errorf("a question nobody heard reached the transcript:\n%s", transcript)
	}
	if !strings.Contains(transcript, "cut off before a word got out") {
		t.Errorf("the transcript does not record that nothing was heard:\n%s", transcript)
	}
}

// TestAskingAboutMechanismIsDeclinedHonestly covers the disclosure rule:
// explain outcome, decline mechanism, and never claim a judgement ghillie does
// not hold. The client answers every put of item 1 with a question of their
// own, so the item exhausts the bound and stays Asked — put, never answered,
// reported exactly so.
func TestAskingAboutMechanismIsDeclinedHonestly(t *testing.T) {
	b := testBrief(t)
	surface := &scriptedSurface{replies: []Reply{
		{Text: "ready when you are"}, // the person REQUESTS the interview — consumed by invite(), content discarded
		{Text: "How do you judge my spec?"},
		{Text: "Is that enough for you to go on?"},
		{Text: "what happens to my answers"},
	}}
	session := newSession(surface)

	answers, err := session.Conduct(context.Background(), b)
	if err != nil {
		t.Fatalf("Conduct: %v", err)
	}

	// No disclosure question counts as an answer — the client did not answer,
	// and nobody was cut off, so the item stays Asked, at the bound.
	if answers[0].Answered {
		t.Errorf("item %d was recorded as answered when the client asked questions instead", answers[0].ItemID)
	}
	if answers[0].State != conduct.Asked.String() {
		t.Errorf("item %d state = %q, want Asked", answers[0].ItemID, answers[0].State)
	}
	if answers[0].Attempts != 3 {
		t.Errorf("item %d attempts = %d, want 3 — the bound, not a nag", answers[0].ItemID, answers[0].Attempts)
	}

	said := strings.ToLower(surface.transcriptOfSaid())
	if !strings.Contains(said, "could not tell you") && !strings.Contains(said, "do not see it") {
		t.Errorf("ghillie did not decline the mechanism question:\n%s", surface.transcriptOfSaid())
	}
	if !strings.Contains(said, "cannot tell you that") {
		t.Errorf("ghillie did not decline to say whether the spec is done:\n%s", surface.transcriptOfSaid())
	}
	// ★ It must never claim the judgement.
	for _, claim := range []string{"your spec is ready", "that is enough", "you are done", "looks complete"} {
		if strings.Contains(said, claim) {
			t.Errorf("ghillie claimed a judgement it cannot make: %q", claim)
		}
	}
}

// TestClassifyRoutesTheConversation covers the disclosure matcher directly.
func TestClassifyRoutesTheConversation(t *testing.T) {
	tests := []struct {
		reply string
		want  Topic
	}{
		{"How do you judge my spec?", TopicHowItJudges},
		{"and how does the factory decide what to build", TopicHowItJudges},
		{"is that enough?", TopicAmIDone},
		{"am I done here", TopicAmIDone},
		{"what happens to my answers", TopicWhatIsThisFor},
		{"how much is this going to be", TopicCost},
		{"It is for booking the vans.", NoTopic},
		{"", NoTopic},
		{"Three depots, forty vans, one yard manager.", NoTopic},
	}
	for _, tt := range tests {
		t.Run(tt.reply, func(t *testing.T) {
			if got := Classify(tt.reply); got != tt.want {
				t.Errorf("Classify(%q) = %v, want %v", tt.reply, got, tt.want)
			}
			if tt.want != NoTopic && tt.want.Answer() == "" {
				t.Errorf("topic %v has nothing to say", tt.want)
			}
		})
	}
}

// TestNoOpeningSpeech pins the style rule that SUPERSEDED the old
// open-about-cost-unprompted behaviour (Tony 2026-08-26: "short sharp
// questions, no explanation" — feedback_interview_style_short_sharp_assess_
// first). The introduction, the cost sermon and the courtesy figure no longer
// open every sitting; that disclosure lives in the documentation and the
// owner's setup. What survives in the sitting is the record, not the speech.
func TestNoOpeningSpeech(t *testing.T) {
	b := testBrief(t)
	surface := &scriptedSurface{}
	session := New(surface, credit.Courtesy{Units: 42, Stale: true}, nil)

	if _, err := session.Conduct(context.Background(), b); err != nil {
		t.Fatalf("Conduct: %v", err)
	}
	said := strings.ToLower(surface.transcriptOfSaid())
	for _, speech := range []string{"i am ghillie", "cut me off", "costs", "courtesy"} {
		if strings.Contains(said, speech) {
			t.Errorf("the opening speech is back (%q):\n%s", speech, surface.transcriptOfSaid())
		}
	}
	if len(surface.asked) == 0 || !strings.Contains(surface.asked[0], "question(s) from the factory") {
		t.Fatalf("the terse invitation must be the only line before the questions, got %q", surface.asked)
	}
}

// TestGhillieNeverClaimsTheSpecIsDone checks the closing turn, which is where
// the temptation is greatest: every item answered, and ghillie still declines
// to say it is enough.
func TestGhillieNeverClaimsTheSpecIsDone(t *testing.T) {
	b := testBrief(t)
	surface := &scriptedSurface{replies: []Reply{
		{Text: "ready when you are"}, // the person REQUESTS the interview — consumed by invite(), content discarded
		{Text: "Booking vans."},
		{Text: "The yard manager."},
		{Text: "The stock system."},
	}}
	session := newSession(surface)

	answers, err := session.Conduct(context.Background(), b)
	if err != nil {
		t.Fatalf("Conduct: %v", err)
	}
	for _, a := range answers {
		if !a.Answered {
			t.Fatalf("item %d not answered in the all-answered case", a.ItemID)
		}
	}
	said := strings.ToLower(surface.transcriptOfSaid())
	if !strings.Contains(said, "away to the factory") {
		t.Errorf("ghillie did not hand the answers to the factory:\n%s", surface.transcriptOfSaid())
	}
	for _, claim := range []string{"that is everything", "your spec is complete", "we have enough", "that is enough"} {
		if strings.Contains(said, claim) {
			t.Errorf("ghillie claimed completeness: %q", claim)
		}
	}
}

// TestBoundedAttemptsDoNotNag checks the attempt-bound core's observable
// conduct: an item that stays outstanding is put exactly MaxAttempts times —
// whatever mixture of interruptions and counter-questions it meets — then let
// lie, out loud, and the close names the bound.
func TestBoundedAttemptsDoNotNag(t *testing.T) {
	b := testBrief(t)
	surface := &scriptedSurface{replies: []Reply{
		{Text: "ready when you are"}, // the person REQUESTS the interview — consumed by invite(), content discarded
		{Text: "cut", Interrupted: true, Heard: "What the software"},
		{Text: "How do you score this?"},
		{Text: "is that enough?"},
	}}
	session := newSession(surface)

	answers, err := session.Conduct(context.Background(), b)
	if err != nil {
		t.Fatalf("Conduct: %v", err)
	}

	// Item 1 was put 3 times and never got; items 2 and 3 answered first time.
	first := 0
	for _, q := range surface.asked {
		if q == b.Items[0].Want {
			first++
		}
	}
	if first != 3 {
		t.Errorf("item 1 was put %d times, want exactly 3 — the compiled-in bound", first)
	}
	if len(surface.asked) != 6 {
		t.Errorf("%d questions put in all, want 6 (the invitation + 3 + 1 + 1)", len(surface.asked))
	}
	if answers[0].Answered || answers[0].Attempts != 3 {
		t.Errorf("item 1: answered=%v attempts=%d, want false/3 — let lie, never claimed", answers[0].Answered, answers[0].Attempts)
	}
	for _, i := range []int{1, 2} {
		if !answers[i].Answered || answers[i].Attempts != 1 {
			t.Errorf("item %d: answered=%v attempts=%d, want true/1", answers[i].ItemID, answers[i].Answered, answers[i].Attempts)
		}
	}

	said := strings.ToLower(surface.transcriptOfSaid())
	if !strings.Contains(said, "leaving that one") {
		t.Errorf("letting the item lie was silent:\n%s", surface.transcriptOfSaid())
	}
	// Terse close, honesty intact: the not-got count is still said aloud and
	// the judgement is still handed to the factory (style superseded the old
	// nag-explanation lines — feedback_interview_style_short_sharp_assess_first).
	if !strings.Contains(said, "not got") {
		t.Errorf("the close does not say what was not got:\n%s", surface.transcriptOfSaid())
	}
}

// TestSurfaceFailureIsReported checks that a broken surface produces an error
// rather than a silently empty interview.
func TestSurfaceFailureIsReported(t *testing.T) {
	b := testBrief(t)
	surface := &scriptedSurface{err: errors.New("the terminal went away")}
	session := newSession(surface)

	if _, err := session.Conduct(context.Background(), b); !errors.Is(err, ErrSurface) {
		t.Fatalf("Conduct returned %v, want an ErrSurface", err)
	}
}

// TestCancellationStopsCleanly checks that a cancelled context ends the
// interview without inventing answers for the items never put.
func TestCancellationStopsCleanly(t *testing.T) {
	b := testBrief(t)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	surface := &scriptedSurface{}
	session := newSession(surface)
	if _, err := session.Conduct(ctx, b); err == nil {
		t.Fatal("a cancelled interview reported success")
	}
}

// TestNothingIsConductedUntilThePersonAsks pins the person's gate: the first
// thing to cross the surface is the single quiet notice, the reply's content
// is discarded (never judged, never recorded), and only then does the
// interview open. A delivered brief is the factory's ask, not the person's —
// feedback_interview_starts_only_on_the_persons_request.
func TestNothingIsConductedUntilThePersonAsks(t *testing.T) {
	surface := &scriptedSurface{replies: []Reply{
		{Text: "eh? oh — go on then"}, // content deliberately irrelevant
		{Text: "a1"}, {Text: "a2"}, {Text: "a3"},
	}}
	s := newSession(surface)
	if _, err := s.Conduct(context.Background(), testBrief(t)); err != nil {
		t.Fatalf("Conduct: %v", err)
	}
	if len(surface.asked) == 0 || !strings.Contains(surface.asked[0], "question(s) from the factory") {
		t.Fatalf("the FIRST thing over the surface must be the invitation notice, got %q", surface.asked)
	}
	if len(surface.asked) < 2 || surface.asked[1] == surface.asked[0] {
		t.Fatalf("the questions must come only after the person asked, got %q", surface.asked)
	}
	tr := s.Transcript()
	if len(tr) == 0 || !strings.Contains(tr[0], "question(s) from the factory") {
		t.Fatalf("transcript[0] should be the notice, got %v", tr[:1])
	}
	for _, line := range tr {
		if strings.Contains(line, "go on then") {
			t.Fatalf("the consent reply's content must not be recorded: %q", line)
		}
	}
}
