package terminal

import (
	"context"
	"crypto/ed25519"
	"encoding/hex"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/tonygair/ghillie/internal/notes"
	"github.com/tonygair/ghillie/internal/protocol"
)

// signedNote builds a facade-signed note for the tests. It signs with the SAME
// key the terminal pins, which is the whole trust model: ghillie's only check
// is "the facade signed it".
func signedNote(key ed25519.PrivateKey, correspondent, from string, state notes.State, text string) notes.Note {
	n := notes.Note{
		Correspondent: correspondent,
		From:          from,
		State:         state,
		Text:          text,
		At:            "2026-08-05T23:00:00Z",
	}
	n.Signature = hex.EncodeToString(ed25519.Sign(key, n.SigningBytes()))
	return n
}

// notesFacade is a poll-204 facade that serves a scripted sequence of note
// batches, one per GET.
type notesFacade struct {
	mu      sync.Mutex
	batches [][]notes.Note
	next    int
}

func (f *notesFacade) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if r.Method == http.MethodGet && strings.HasSuffix(r.URL.Path, "/notes") {
		f.mu.Lock()
		var batch []notes.Note
		if f.next < len(f.batches) {
			batch = f.batches[f.next]
			f.next++
		} else if len(f.batches) > 0 {
			batch = f.batches[len(f.batches)-1] // keep serving the last batch
		}
		f.mu.Unlock()
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string][]notes.Note{"notes": batch})
		return
	}
	w.WriteHeader(http.StatusNoContent) // empty poll
}

// runNotesTerminal drives a terminal for a fixed number of polls against a
// notes-serving facade and returns what the user was shown.
func runNotesTerminal(t *testing.T, f *notesFacade, polls int, key ed25519.PrivateKey) *testLogger {
	t.Helper()
	srv := httptest.NewServer(f)
	t.Cleanup(srv.Close)

	dir := t.TempDir()
	logger := &testLogger{}
	term, err := New(Config{
		FacadeURL:    srv.URL,
		ClawID:       "test-claw",
		StateFile:    filepath.Join(dir, "state.json"),
		PollInterval: time.Millisecond,
		MaxPolls:     polls,
	}, key.Public().(ed25519.PublicKey), logger)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err := term.Run(ctx); err != nil {
		t.Fatalf("Run: %v", err)
	}
	return logger
}

// TestNoteRendersOnceAcrossPolls: the same batch served on every fetch renders
// exactly once — the dedupe is what makes a progress channel bearable.
func TestNoteRendersOnceAcrossPolls(t *testing.T) {
	key := signer("facade")
	n := signedNote(key, "corr-1", "the forge queue", notes.Started, "")
	logger := runNotesTerminal(t, &notesFacade{batches: [][]notes.Note{{n}}}, 4, key)

	if got := strings.Count(logger.text(), n.Sentence()); got != 1 {
		t.Fatalf("note rendered %d times, want exactly 1\n%s", got, logger.text())
	}
}

// TestTerminalNoteRetiresCorrespondent: after FINISHED, a straggler PROGRESS
// note against the same correspondent is dropped — a finished job can never be
// resurrected.
func TestTerminalNoteRetiresCorrespondent(t *testing.T) {
	key := signer("facade")
	done := signedNote(key, "corr-1", "the forge queue", notes.Finished, "")
	straggler := signedNote(key, "corr-1", "the forge queue", notes.Progress, "still going")

	logger := runNotesTerminal(t, &notesFacade{batches: [][]notes.Note{
		{done},
		{straggler},
	}}, 4, key)

	text := logger.text()
	if !strings.Contains(text, done.Sentence()) {
		t.Fatalf("terminal note was not rendered:\n%s", text)
	}
	if strings.Contains(text, straggler.Sentence()) {
		t.Fatalf("straggler rendered after a terminal note:\n%s", text)
	}
	if !strings.Contains(text, "straggler") {
		t.Fatalf("straggler drop was silent — it must be visible:\n%s", text)
	}
}

// TestForgedNoteIsRefusedLoudly: a note signed by the wrong key never renders,
// and the refusal is shown to the person.
func TestForgedNoteIsRefusedLoudly(t *testing.T) {
	facadeKey := signer("facade")
	wrongKey := signer("not-the-facade")
	forged := signedNote(wrongKey, "corr-9", "impostor", notes.Finished, "trust me")

	logger := runNotesTerminal(t, &notesFacade{batches: [][]notes.Note{{forged}}}, 2, facadeKey)

	text := logger.text()
	if strings.Contains(text, forged.Sentence()) {
		t.Fatalf("forged note was rendered:\n%s", text)
	}
	if !strings.Contains(text, "note refused") {
		t.Fatalf("forged note refused silently — refusals are logged loudly:\n%s", text)
	}
}

// TestOldFacadeWithoutNotesIsQuiet: a 404 on the notes path is an old facade,
// not a fault — nothing is logged about it.
func TestOldFacadeWithoutNotesIsQuiet(t *testing.T) {
	key := signer("facade")
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodGet {
			http.NotFound(w, r)
			return
		}
		w.WriteHeader(http.StatusNoContent)
	}))
	t.Cleanup(srv.Close)

	dir := t.TempDir()
	logger := &testLogger{}
	term, err := New(Config{
		FacadeURL:    srv.URL,
		ClawID:       "test-claw",
		StateFile:    filepath.Join(dir, "state.json"),
		PollInterval: time.Millisecond,
		MaxPolls:     2,
	}, key.Public().(ed25519.PublicKey), logger)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	if err := term.Run(context.Background()); err != nil {
		t.Fatalf("Run: %v", err)
	}
	if strings.Contains(logger.text(), "notes:") {
		t.Fatalf("404 notes path logged a fault:\n%s", logger.text())
	}
	_ = protocol.NotesPath("x") // keep the import honest about what is under test
}
