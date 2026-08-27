package guard

// surface.go carries the guard to wherever the person speaks: a
// GuardedSurface wraps any interview surface, and every reply that arrives
// passes through the guard on its way in. The reply itself is NEVER altered
// and never withheld from the record — the transcript keeps what was said;
// the guard acts on conduct. Ghillie's own lines pass through untouched: the
// guard watches how HE is addressed, not what he says.

import (
	"context"

	"github.com/tonygair/ghillie/internal/interview"
)

// GuardedSurface decorates an interview.Surface with the abuse guard.
type GuardedSurface struct {
	inner interview.Surface
	g     *Guard
	ctx   func() context.Context
}

// WrapSurface guards a surface. The returned surface is used exactly like the
// one it wraps; interview conduct cannot tell it is there.
func WrapSurface(inner interview.Surface, g *Guard) *GuardedSurface {
	return &GuardedSurface{inner: inner, g: g}
}

// Say passes straight through.
func (s *GuardedSurface) Say(ctx context.Context, line string) error {
	return s.inner.Say(ctx, line)
}

// Ask puts the question and passes the reply through the guard before
// returning it. When the guard warns or lectures, those lines go out through
// the SAME surface the person is on — then the reply is handed back to the
// interview exactly as spoken.
func (s *GuardedSurface) Ask(ctx context.Context, question string) (interview.Reply, error) {
	reply, err := s.inner.Ask(ctx, question)
	if err != nil {
		return reply, err
	}
	s.g.Utterance(reply.Text, func(line string) {
		// A guard line failing to send must not eat the person's reply; the
		// guard's own log carries the failure.
		if serr := s.inner.Say(ctx, line); serr != nil {
			s.g.log.Printf("guard: could not deliver a guard line: %v", serr)
		}
	})
	return reply, nil
}
