package interview

// console.go is the terminal-window Surface: ghillie asks on stdout, the person
// types on stdin. It is the whole of v1's client-facing surface.
//
// ★ HOW LONG IT WAITS IS NOT A SETTING. There is no read deadline here and no
// timeout to configure, because "ghillie will wait for you" is a promise about
// conduct and conduct is compiled in. A person who needs a minute to think gets
// a minute. Silence is not a failure state.

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"io"
	"strings"
)

// CutPrefix lets a person exercise the interrupted-question path from a
// keyboard, where there is no microphone to cut in on.
//
// It is a TEST AFFORDANCE and it is honest about that: typing "/cut ..." means
// "pretend you were cut off mid-sentence and I said this instead". In v2 the
// same Reply comes from the voice pipeline noticing someone start speaking. The
// state machine on the other side of it does not know or care which.
const CutPrefix = "/cut"

// ConsoleSurface drives an interview through an ordinary terminal.
type ConsoleSurface struct {
	in  *bufio.Scanner
	out io.Writer
}

// NewConsoleSurface builds a surface over a reader and a writer.
func NewConsoleSurface(in io.Reader, out io.Writer) *ConsoleSurface {
	scanner := bufio.NewScanner(in)
	scanner.Buffer(make([]byte, 0, 64*1024), 1<<20)
	return &ConsoleSurface{in: scanner, out: out}
}

// Say writes one line as ghillie.
func (c *ConsoleSurface) Say(ctx context.Context, line string) error {
	if err := ctx.Err(); err != nil {
		return fmt.Errorf("say: %w", err)
	}
	if _, err := fmt.Fprintf(c.out, "\n  ghillie ▸ %s\n", line); err != nil {
		return fmt.Errorf("write line: %w", err)
	}
	return nil
}

// Ask puts a question and waits for a reply, with no deadline of its own.
//
// A reply beginning with CutPrefix is reported as an interruption: the client is
// saying they cut ghillie off part-way through the question. Heard is set to
// roughly the first half of the question, which is the honest thing to record —
// the rest of it never reached them, so the rest of it must not enter the
// transcript.
func (c *ConsoleSurface) Ask(ctx context.Context, question string) (Reply, error) {
	if err := ctx.Err(); err != nil {
		return Reply{}, fmt.Errorf("ask: %w", err)
	}
	if _, err := fmt.Fprintf(c.out, "\n  ghillie ▸ %s\n  you     ▸ ", question); err != nil {
		return Reply{}, fmt.Errorf("write question: %w", err)
	}
	return c.readReply(question)
}

// showListening echoes a question that was put ALOUD, so the interview stays
// readable while the microphone window is open. It reads nothing.
func (c *ConsoleSurface) showListening(ctx context.Context, question string) error {
	if err := ctx.Err(); err != nil {
		return fmt.Errorf("show listening: %w", err)
	}
	if _, err := fmt.Fprintf(c.out, "\n  ghillie ▸ %s\n  you     ▸ (listening — speak your answer, or pause and it will close on its own)\n", question); err != nil {
		return fmt.Errorf("write listening note: %w", err)
	}
	return nil
}

// showHeard echoes what the ears transcribed, so the interviewee SEES what
// was heard before it goes on the record.
func (c *ConsoleSurface) showHeard(ctx context.Context, heard string) error {
	if err := ctx.Err(); err != nil {
		return fmt.Errorf("show heard: %w", err)
	}
	if _, err := fmt.Fprintf(c.out, "  you     ▸ %s   (heard by ear)\n", heard); err != nil {
		return fmt.Errorf("write heard echo: %w", err)
	}
	return nil
}

// readReply reads one typed line and interprets the /cut affordance. It is
// split out of Ask so the voiced reprompt path shares exactly this reader —
// /cut STAYS TYPED, in every mode.
func (c *ConsoleSurface) readReply(question string) (Reply, error) {
	if !c.in.Scan() {
		if err := c.in.Err(); err != nil {
			return Reply{}, fmt.Errorf("read reply: %w", err)
		}
		return Reply{}, errors.New("read reply: the client's input ended")
	}
	line := strings.TrimSpace(c.in.Text())

	if rest, cut := strings.CutPrefix(line, CutPrefix); cut {
		return Reply{
			Text:        strings.TrimSpace(rest),
			Interrupted: true,
			Heard:       halfOf(question),
		}, nil
	}
	return Reply{Text: line}, nil
}

// halfOf returns roughly the first half of a question, cut at a word boundary:
// what a person would have heard before cutting in. It is an approximation and
// nothing depends on its precision — what matters is that the REST IS NOT
// RETURNED, so it cannot reach the transcript and cannot be resumed.
func halfOf(question string) string {
	words := strings.Fields(question)
	if len(words) <= 1 {
		return question
	}
	return strings.Join(words[:len(words)/2], " ")
}
