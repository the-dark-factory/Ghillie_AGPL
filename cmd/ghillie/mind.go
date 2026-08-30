package main

// mind.go is the OWNER'S END of the inference socket: the three flags that say
// where this ghillie thinks, and the one surface that uses it.
//
// ★ THE RULING (Tony, 2026-08-28): a downloaded ghillie does NOT draw the
// collective's inference — "they need to set the inference from their machine".
// So there is no endpoint in this file that anyone but the owner chose, no
// provider, no key, and no path by which a failure here reaches for somebody
// else's compute. The default is the ollama an owner installs on their own
// laptop, and it is loopback-only by construction.
//
// SHAPE COPIED FROM THE EARS, DELIBERATELY. -mind-url is to -ears-whisper-url
// what -mind-adopt-external is to -ears-adopt-external: a loopback-required
// endpoint plus an eyes-open opt-in that says, in the owner's own words, what
// the far end will be shown. The wording here is STERNER, because the stakes
// are: the ears hear an answer, the mind is told everything.

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"io"
	"log"
	"os"
	"strings"

	"github.com/tonygair/ghillie/internal/interview"
	"github.com/tonygair/ghillie/internal/mind"
)

// defaultMindURLValue is the ollama an owner installs on their own machine.
// It is loopback and it is the whole default: nothing else is contacted.
const defaultMindURLValue = "http://127.0.0.1:11434"

// defaultMindURL honours the owner's environment first (GHILLIE_MIND_URL),
// then the loopback ollama default — the same shape as GHILLIE_EARS_MODEL and
// GHILLIE_VOICE_PIPELINE.
func defaultMindURL() string {
	if u := os.Getenv("GHILLIE_MIND_URL"); u != "" {
		return u
	}
	return defaultMindURLValue
}

// defaultMindModel honours GHILLIE_MIND_MODEL, else stays EMPTY — and empty is
// a real answer, not a missing one: it means "ask the server what it serves and
// take the first", which the surface then says out loud.
func defaultMindModel() string { return os.Getenv("GHILLIE_MIND_MODEL") }

// mindSystemPrompt is who is speaking. It is short on purpose: the persona is
// the ghillie's, the words are the owner's model's, and padding a local model's
// context with a character sheet buys nothing an owner can see.
//
// It says what he IS NOT allowed to do, because a model asked to be helpful
// will otherwise volunteer to make the very judgements this product proves.
const mindSystemPrompt = `You are ghillie: a quiet, plain-spoken Scots assistant who works for the person
at this keyboard and for nobody else. Speak briefly. Do not pad, do not flatter,
do not offer to do things you cannot do.

You do not decide anything. You do not judge answers, grant or refuse
permissions, admit or reject work, or pronounce on whether a proof holds — those
are settled elsewhere, deterministically, and you must say so if asked rather
than guessing. You are here for conversation.

You run on the owner's own machine. Nothing said to you leaves it.`

// buildMind opens the owner's mind, or reports honestly that there is none.
//
// The bool is the honest half of the contract: (nil, false, nil) means NO MIND
// AND NO FAULT. Nothing broke, nothing is missing, and the caller says so.
func buildMind(ctx context.Context, o options) (client *mind.Client, ok bool, err error) {
	client, err = mind.Open(ctx, mind.Config{
		URL:           o.mindURL,
		Model:         o.mindModel,
		AdoptExternal: o.mindAdoptExternal,
	})
	if errors.Is(err, mind.ErrNoMind) {
		return nil, false, nil
	}
	if err != nil {
		return nil, false, fmt.Errorf("-mind-url: %w", err)
	}
	return client, true, nil
}

// runTalk is the free-conversation surface: the one place a mind is used.
//
// ★ MINIMAL BY DESIGN, AND THE SCOPE IS THE POINT. This is a standalone owner
// verb, like -enrol, -install-ability and -allow-fill: it does its one thing
// and exits. It does not poll, it does not touch the gate, it never reaches the
// conduct wall, and no brief, refusal, proof or admission passes through it.
// The isolation is asserted, not merely intended — see
// TestMindIsNotImportedByTheDecisionPaths in mind_isolation_test.go.
func runTalk(ctx context.Context, o options, in io.Reader, out io.Writer) error {
	client, ok, err := buildMind(ctx, o)
	if err != nil {
		return err
	}
	if !ok {
		// NO MIND, NO PRETENCE. He does not answer anyway from a script, and
		// he does not go looking for someone else's model.
		log.Printf("%s", mind.NoMindNotice(o.mindURL))
		return nil
	}

	// ★ NAME WHO IS SPEAKING, ALWAYS — the v0.1.3 locale-pack rule applied to
	// inference: the owner never has to wonder whose words these are.
	log.Printf("%s", client.Speaking())
	log.Printf("free conversation. %s ends it, and so does an empty line or Ctrl-D. "+
		"He decides nothing here — proofs, refusals and admissions are settled elsewhere",
		interview.CutPrefix)

	// The surface is used for its VOICE only — "  ghillie ▸ " is how he speaks
	// everywhere else and it must not become a second thing here. Reading is
	// done by the scanner below; giving the surface an empty reader keeps two
	// buffered readers off one stream, which is how input goes missing.
	surface := interview.NewConsoleSurface(strings.NewReader(""), out)
	history := []mind.Message{{Role: "system", Content: mindSystemPrompt}}
	reader := bufio.NewScanner(in)
	reader.Buffer(make([]byte, 0, 64*1024), 1<<20)

	if err := surface.Say(ctx, "Aye. What's on your mind?"); err != nil {
		return fmt.Errorf("talk: %w", err)
	}
	for {
		if err := ctx.Err(); err != nil {
			return fmt.Errorf("talk: %w", err)
		}
		if _, err := fmt.Fprintf(out, "\n  you     ▸ "); err != nil {
			return fmt.Errorf("talk: write prompt: %w", err)
		}
		if !reader.Scan() {
			if err := reader.Err(); err != nil {
				return fmt.Errorf("talk: read: %w", err)
			}
			break // Ctrl-D
		}
		said := strings.TrimSpace(reader.Text())
		if said == "" || strings.HasPrefix(said, interview.CutPrefix) {
			break
		}

		history = append(history, mind.Message{Role: "user", Content: said})
		answer, err := client.Chat(ctx, history)
		if err != nil {
			// A mind that fails mid-conversation is REPORTED, not papered
			// over: the alternative is an owner who cannot tell the model's
			// words from ours.
			return fmt.Errorf("talk: %w", err)
		}
		history = append(history, mind.Message{Role: "assistant", Content: answer})
		if err := surface.Say(ctx, strings.TrimSpace(answer)); err != nil {
			return fmt.Errorf("talk: %w", err)
		}
	}
	if _, err := fmt.Fprintf(out, "\n"); err != nil {
		return fmt.Errorf("talk: %w", err)
	}
	log.Printf("that's us. %s said nothing to anyone but you", client.Model())
	return nil
}
