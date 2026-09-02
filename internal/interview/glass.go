package interview

// glass.go is the browser Surface: ghillie asks through the OpenClaw chat
// page attached over internal/glass, and the person answers in the composer.
// It is the third Surface (console, voice, glass) and, like the others, it
// changes nothing about conduct — interview.go cannot tell which one it holds.
//
// ★ THE GLASS DOES NOT GET A VOTE. The chat client can offer queue modes,
// fast modes and abort buttons; none of them reach here. A question is put,
// an answer comes back, and how long ghillie waits stays compiled in.

import (
	"context"
	"fmt"
	"strings"
)

// GlassChat is the slice of internal/glass this surface stands on. It is an
// interface so this package does not import glass — the dependency points
// from the wire to the interview, never back — and so tests can stand a fake
// page behind the surface.
type GlassChat interface {
	// SayLine shows one ghillie utterance to every attached client.
	SayLine(ctx context.Context, line string) error
	// AwaitReply blocks until a client utterance is available.
	AwaitReply(ctx context.Context) (reply string, err error)
}

// GlassSurface drives an interview through an attached chat page.
type GlassSurface struct {
	chat GlassChat
}

// NewGlassSurface builds a surface over an attached chat.
func NewGlassSurface(chat GlassChat) *GlassSurface {
	return &GlassSurface{chat: chat}
}

// Say shows one line as ghillie.
func (g *GlassSurface) Say(ctx context.Context, line string) error {
	if err := g.chat.SayLine(ctx, line); err != nil {
		return fmt.Errorf("glass surface: say: %w", err)
	}
	return nil
}

// Ask puts a question and waits for the composer — WITHOUT A DEADLINE OF ITS
// OWN, same promise as every other surface.
//
// The CutPrefix affordance works here exactly as on the console: a chat
// composer has no microphone to cut in on either, so "/cut ..." is how a
// person exercises the interrupted-question path from a keyboard.
func (g *GlassSurface) Ask(ctx context.Context, question string) (Reply, error) {
	if err := g.chat.SayLine(ctx, question); err != nil {
		return Reply{}, fmt.Errorf("glass surface: ask: %w", err)
	}
	text, err := g.chat.AwaitReply(ctx)
	if err != nil {
		return Reply{}, fmt.Errorf("glass surface: await: %w", err)
	}
	if cut, ok := strings.CutPrefix(text, CutPrefix); ok {
		return Reply{
			Text:        strings.TrimSpace(cut),
			Interrupted: true,
			Heard:       question, // the whole question rendered before the cut
		}, nil
	}
	return Reply{Text: text}, nil
}
