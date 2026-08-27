package interview

import (
	"context"
	"errors"
	"testing"
)

// fakeChat is a chat page in miniature: it records what ghillie said and
// serves scripted replies.
type fakeChat struct {
	said    []string
	replies []string
	err     error
}

func (f *fakeChat) SayLine(ctx context.Context, line string) error {
	if f.err != nil {
		return f.err
	}
	f.said = append(f.said, line)
	return nil
}

func (f *fakeChat) AwaitReply(ctx context.Context) (string, error) {
	if f.err != nil {
		return "", f.err
	}
	if len(f.replies) == 0 {
		return "", errors.New("fakeChat: no scripted reply")
	}
	var reply string
	reply, f.replies = f.replies[0], f.replies[1:]
	return reply, nil
}

func TestGlassSurfaceAsk(t *testing.T) {
	cases := []struct {
		name            string
		reply           string
		wantText        string
		wantInterrupted bool
		wantHeard       string
	}{
		{
			name:     "a plain answer",
			reply:    "it makes pressure valves",
			wantText: "it makes pressure valves",
		},
		{
			name:            "the /cut affordance exercises the interrupted path",
			reply:           "/cut actually, hold on",
			wantText:        "actually, hold on",
			wantInterrupted: true,
			// Unlike the console's simulated half, the chat page rendered the
			// WHOLE question before the cut — the transcript records what
			// actually reached the client, so Heard is all of it.
			wantHeard: "What does the machine make?",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			chat := &fakeChat{replies: []string{tc.reply}}
			s := NewGlassSurface(chat)

			reply, err := s.Ask(context.Background(), "What does the machine make?")
			if err != nil {
				t.Fatalf("Ask: %v", err)
			}
			if len(chat.said) != 1 || chat.said[0] != "What does the machine make?" {
				t.Fatalf("the question must be shown before the wait; said = %v", chat.said)
			}
			if reply.Text != tc.wantText {
				t.Fatalf("Text = %q, want %q", reply.Text, tc.wantText)
			}
			if reply.Interrupted != tc.wantInterrupted {
				t.Fatalf("Interrupted = %v, want %v", reply.Interrupted, tc.wantInterrupted)
			}
			if reply.Heard != tc.wantHeard {
				t.Fatalf("Heard = %q, want %q", reply.Heard, tc.wantHeard)
			}
		})
	}
}

func TestGlassSurfaceSay(t *testing.T) {
	chat := &fakeChat{}
	s := NewGlassSurface(chat)
	if err := s.Say(context.Background(), "Recorded. Next."); err != nil {
		t.Fatalf("Say: %v", err)
	}
	if len(chat.said) != 1 || chat.said[0] != "Recorded. Next." {
		t.Fatalf("said = %v", chat.said)
	}
}

func TestGlassSurfaceCarriesTheWireError(t *testing.T) {
	chat := &fakeChat{err: errors.New("socket gone")}
	s := NewGlassSurface(chat)
	if err := s.Say(context.Background(), "x"); err == nil {
		t.Fatalf("Say must surface the wire failure")
	}
	if _, err := s.Ask(context.Background(), "x"); err == nil {
		t.Fatalf("Ask must surface the wire failure")
	}
}
