// The notes fetch: the terminal's outward GET for correspondent notes, and the
// only consumer of internal/notes. It is deliberately not part of the poll —
// notes are not instructions, and keeping the fetch separate keeps the proven
// instruction path (frame → gate → act) untouched by the progress channel.
//
// The structural property holds here as everywhere: a note is decoded, checked,
// recorded and RENDERED. There is no branch below that turns one into an act.
package terminal

import (
	"context"
	"net/http"

	"github.com/tonygair/ghillie/internal/notes"
	"github.com/tonygair/ghillie/internal/protocol"
)

// maxSeenNotes bounds the dedupe set. Like the answers queue it is in-memory
// only: a restart may re-render an old note, which is noise, not authority —
// the acceptable failure direction for a channel that can never make anything
// happen.
const maxSeenNotes = 512

// fetchNotes makes one outward GET for this claw's notes, renders each new one
// through the terminal's human-facing log, and retires terminal correspondents
// (notes.State.Terminal: a finished job can never be resurrected by a
// straggler — the drop happens HERE, the client end, exactly as the package
// doc promises).
//
// Every failure is logged and swallowed: the progress channel must never be
// able to stall or kill the instruction loop.
func (t *Terminal) fetchNotes(ctx context.Context) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, t.cfg.FacadeURL+protocol.NotesPath(t.cfg.ClawID), nil)
	if err != nil {
		t.log.Printf("notes: %v", err)
		return
	}
	if err := t.authorize(ctx, req); err != nil {
		t.log.Printf("notes: %v", err)
		return
	}
	resp, err := t.client.Do(req)
	if err != nil {
		t.log.Printf("notes: %v", err)
		return
	}
	defer resp.Body.Close()
	if resp.StatusCode == http.StatusNotFound || resp.StatusCode == http.StatusNoContent {
		return // a facade without the channel is an old facade, not a fault
	}
	if resp.StatusCode != http.StatusOK {
		t.log.Printf("notes: HTTP %d", resp.StatusCode)
		return
	}

	good, refused, err := notes.Decode(resp.Body, t.facadeKey)
	if err != nil {
		t.log.Printf("notes: %v", err)
		return
	}
	// Refusals are logged LOUDLY: a bad signature or an unknown state word is
	// either a newer facade than this binary or someone probing the channel,
	// and both are worth a person's eyes.
	for _, rerr := range refused {
		t.log.Printf("note refused: %v", rerr)
	}

	for _, n := range good {
		if t.seenNotes[n.Signature] {
			continue
		}
		if t.retired[n.Correspondent] {
			t.log.Printf("note dropped: straggler for a retired correspondence (%s)", n.From)
			continue
		}
		t.rememberNote(n.Signature)
		if t.cfg.Notes != nil {
			// A ledger surface takes the note as DATA (the GUI's job list);
			// it inherits the same obligation: record and render, never
			// dispatch.
			t.cfg.Notes.Note(n)
		} else {
			// The "post │ " prefix is the render contract with the line
			// surfaces: a correspondent's sentence is POST, distinguishable
			// from the engine's own narration.
			t.log.Printf("post │ %s", n.Sentence())
		}
		if n.State.Terminal() {
			t.retired[n.Correspondent] = true
		}
	}
}

// rememberNote records a rendered note's signature, evicting the oldest
// remembered signature once the bound is reached — FIFO via the ring below.
func (t *Terminal) rememberNote(sig string) {
	if t.seenNotes == nil {
		return
	}
	if len(t.seenRing) >= maxSeenNotes {
		oldest := t.seenRing[0]
		t.seenRing = t.seenRing[1:]
		delete(t.seenNotes, oldest)
	}
	t.seenNotes[sig] = true
	t.seenRing = append(t.seenRing, sig)
}

// initNotes prepares the in-memory note state. Called from Run so a Terminal
// built by New and run twice starts each run with a clean slate, mirroring how
// lastSeq is loaded there.
func (t *Terminal) initNotes() {
	t.seenNotes = make(map[string]bool)
	t.retired = make(map[string]bool)
	t.seenRing = nil
}
