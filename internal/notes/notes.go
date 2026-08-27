// Package notes is the CORRESPONDENT CHANNEL: how a remote party — the forge
// queue, a lane, a supermarket, an accountant's system, and one day another
// ghillie — tells a person's ghillie something.
//
// ═══════════════════════════════════════════════════════════════════════════
//
//	★  A NOTE CARRIES INFORMATION AND NO AUTHORITY.
//
// ═══════════════════════════════════════════════════════════════════════════
//
// The instruction vocabulary (Facade_Command_Pkg, ledger 112) is closed and
// proven and its position IS the wire byte. A progress update is not a command
// and must never become one, so notes are a SEPARATE type with a structural
// property: nothing in ghillie dispatches on a note. It is decoded, checked,
// recorded and RENDERED. There is no branch anywhere that turns a note into an
// act — which is what makes it safe to let arbitrary registered correspondents
// send them.
//
// ★ THE STATE WORD IS A CLOSED SET, versioned with the binary. A correspondent
// needing a word we lack does not get a free-text escape hatch into machine
// meaning; it gets a reviewed commit, or it uses Text — which renders as text
// and means NOTHING to the machine. This is the brief wall's lesson applied to
// the second channel: extensibility is how conduct gets delivered.
//
// ★ SIGNED, THOUGH IT CANNOT ACT. A note causes nothing, but a spoofed "your
// job failed — call this number" is a phishing vector wearing ghillie's voice.
// So the facade signs; an unsigned or badly-signed note is refused.
package notes

import (
	"crypto/ed25519"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"strings"
	"time"
)

// MaxNotesBytes bounds a fetched batch. Notes are short; a megabyte of them is
// a fault or an attack and is refused before it is parsed.
const MaxNotesBytes = 1 << 20

// State is a note's state word: the CLOSED set. Anything else refuses.
type State string

const (
	Accepted   State = "ACCEPTED"    // the work was taken on
	Started    State = "STARTED"     // it has begun
	Progress   State = "PROGRESS"    // in flight; Fraction may be set
	SpecReady  State = "SPEC_READY"  // a specification arrived (the queue's own milestone)
	Delayed    State = "DELAYED"     // later than expected, still alive
	Blocked    State = "BLOCKED"     // stopped, needs something
	WorkerDown State = "WORKER_DOWN" // the worker itself is unavailable
	Finished   State = "FINISHED"    // done; Ref may name the artifact/ledger record
	Failed     State = "FAILED"      // done, unsuccessfully
	Withdrawn  State = "WITHDRAWN"   // called back; no longer expected
)

// states is the admissible set. A note whose state is not here is refused
// WHOLE — never coerced to a nearby meaning, never rendered as unknown.
var states = map[State]bool{
	Accepted: true, Started: true, Progress: true, SpecReady: true,
	Delayed: true, Blocked: true, WorkerDown: true,
	Finished: true, Failed: true, Withdrawn: true,
}

// Terminal reports whether a state ends a correspondence. A terminal note
// retires its token: a later note against it is dropped, so a finished job can
// never be resurrected by a straggler.
func (s State) Terminal() bool {
	return s == Finished || s == Failed || s == Withdrawn
}

// Note is one thing a correspondent has to say.
//
// Correspondent is the OPAQUE PER-JOB TOKEN, never a ghillie id and never a
// customer identity: the facade alone maps token to ghillie, so a worker can
// address its updates without learning who it is working for or correlating
// two jobs to one person.
type Note struct {
	Correspondent string  `json:"correspondent"`
	From          string  `json:"from"` // the correspondent's registered name, for the person to see
	State         State   `json:"state"`
	Fraction      float64 `json:"fraction,omitempty"` // 0..1, ONLY where honest (a real stage boundary)
	Text          string  `json:"text,omitempty"`     // free text — rendered, never interpreted
	Ref           string  `json:"ref,omitempty"`      // artifact/ledger reference on FINISHED
	At            string  `json:"at"`                 // RFC3339, the correspondent's clock
	Signature     string  `json:"signature"`          // facade signature over SigningBytes
}

// ErrMalformed reports a note that does not parse or does not validate.
var ErrMalformed = errors.New("notes: malformed")

// ErrUnknownState reports a state word outside the closed set — reported
// distinctly because it is interesting: either a newer facade than this
// binary, or someone probing the vocabulary.
var ErrUnknownState = errors.New("notes: state word outside the closed set")

// ErrBadSignature reports a note the facade did not sign.
var ErrBadSignature = errors.New("notes: signature does not verify")

// SigningBytes is the exact byte string the facade signs. Field order is
// fixed here and must never be "tidied": the wire format is a contract.
func (n Note) SigningBytes() []byte {
	return []byte(fmt.Sprintf("note\x00%s\x00%s\x00%s\x00%.6f\x00%s\x00%s\x00%s",
		n.Correspondent, n.From, n.State, n.Fraction, n.Text, n.Ref, n.At))
}

// Decode reads a batch of notes strictly and verifies each one.
//
// The three refusals, in order, and all three matter:
//  1. bounded body (MaxNotesBytes);
//  2. DisallowUnknownFields — a correspondent cannot smuggle a field this
//     type has no place for, which is the same wall the brief path uses;
//  3. signature + validation per note.
//
// A batch is not all-or-nothing: a bad note is DROPPED AND REPORTED while its
// well-formed siblings are delivered, because one malformed message must not
// silence a person's whole correspondence. The refusals are returned so the
// caller can log them loudly.
func Decode(r io.Reader, facadeKey ed25519.PublicKey) (good []Note, refused []error, err error) {
	dec := json.NewDecoder(io.LimitReader(r, MaxNotesBytes+1))
	dec.DisallowUnknownFields()

	var batch struct {
		Notes []Note `json:"notes"`
	}
	if err := dec.Decode(&batch); err != nil {
		return nil, nil, fmt.Errorf("%w: %w", ErrMalformed, err)
	}
	if _, err := dec.Token(); !errors.Is(err, io.EOF) {
		return nil, nil, fmt.Errorf("%w: trailing content after the batch", ErrMalformed)
	}

	for _, n := range batch.Notes {
		if verr := n.validate(); verr != nil {
			refused = append(refused, verr)
			continue
		}
		if serr := n.verify(facadeKey); serr != nil {
			refused = append(refused, serr)
			continue
		}
		good = append(good, n)
	}
	return good, refused, nil
}

// validate checks the shape a note must have to be shown to a person at all.
func (n Note) validate() error {
	if strings.TrimSpace(n.Correspondent) == "" {
		return fmt.Errorf("%w: no correspondent token", ErrMalformed)
	}
	if !states[n.State] {
		return fmt.Errorf("%w: %q (from %q)", ErrUnknownState, n.State, n.From)
	}
	if n.Fraction < 0 || n.Fraction > 1 {
		return fmt.Errorf("%w: fraction %v is outside 0..1", ErrMalformed, n.Fraction)
	}
	// ★ A FRACTION IS A CLAIM ABOUT PROGRESS AND ONLY PROGRESS. Allowing one
	// on FINISHED or FAILED invites "99% failed", which is theatre; allowing
	// one on WORKER_DOWN invites a dead worker claiming ground.
	if n.Fraction != 0 && n.State != Progress {
		return fmt.Errorf("%w: a fraction belongs to PROGRESS, not %s", ErrMalformed, n.State)
	}
	if _, terr := time.Parse(time.RFC3339, n.At); terr != nil {
		return fmt.Errorf("%w: at %q is not RFC3339", ErrMalformed, n.At)
	}
	if len(n.Text) > 500 {
		// Bounded because it is rendered to a person. A correspondent with a
		// story to tell can send several notes; nobody gets to fill the window.
		return fmt.Errorf("%w: text is %d bytes (max 500)", ErrMalformed, len(n.Text))
	}
	return nil
}

// verify checks the facade's signature. A nil key means no facade key is
// pinned, which must FAIL CLOSED: unverifiable notes are refused, never shown
// with a shrug.
func (n Note) verify(facadeKey ed25519.PublicKey) error {
	if len(facadeKey) != ed25519.PublicKeySize {
		return fmt.Errorf("%w: no facade key pinned to verify against", ErrBadSignature)
	}
	sig, err := hex.DecodeString(n.Signature)
	if err != nil || len(sig) != ed25519.SignatureSize {
		return fmt.Errorf("%w: signature is not %d hex bytes", ErrBadSignature, ed25519.SignatureSize)
	}
	if !ed25519.Verify(facadeKey, n.SigningBytes(), sig) {
		return fmt.Errorf("%w: from %q, state %s", ErrBadSignature, n.From, n.State)
	}
	return nil
}

// Sentence renders a note as one plain line for the person.
//
// It is PRESENTATION ONLY and deliberately dull: no exclamation, no urgency,
// no instruction to do anything. The correspondent's own Text is quoted as
// theirs rather than spoken in ghillie's voice — a correspondent never gets to
// borrow the assistant's mouth.
func (n Note) Sentence() string {
	who := n.From
	if strings.TrimSpace(who) == "" {
		who = "a correspondent"
	}
	var head string
	switch n.State {
	case Accepted:
		head = fmt.Sprintf("%s has taken the work on.", who)
	case Started:
		head = fmt.Sprintf("%s has started.", who)
	case Progress:
		if n.Fraction > 0 {
			head = fmt.Sprintf("%s is %d%% of the way through.", who, int(n.Fraction*100+0.5))
		} else {
			head = fmt.Sprintf("%s is still working.", who)
		}
	case SpecReady:
		head = fmt.Sprintf("%s has your specification ready — your words are now a specification.", who)
	case Delayed:
		head = fmt.Sprintf("%s says this is running late.", who)
	case Blocked:
		head = fmt.Sprintf("%s is stuck and cannot go on for now.", who)
	case WorkerDown:
		head = fmt.Sprintf("%s is down. Nothing is lost; it will resume.", who)
	case Finished:
		head = fmt.Sprintf("%s has finished.", who)
	case Failed:
		head = fmt.Sprintf("%s could not finish this one.", who)
	case Withdrawn:
		head = fmt.Sprintf("%s has withdrawn it.", who)
	}
	if t := strings.TrimSpace(n.Text); t != "" {
		head += fmt.Sprintf(" Their words: %q", t)
	}
	return head
}
