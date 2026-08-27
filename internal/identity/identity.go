// Package identity holds the FOUR identities this system must keep apart.
// Conflating any two of them is the classic authorisation bug, so they are four
// distinct Go types and not four strings.
//
//	┌───────────────────────┬──────────────────────────────┬────────────────────┐
//	│ identity              │ what it is                   │ governed by        │
//	├───────────────────────┼──────────────────────────────┼────────────────────┤
//	│ the CLAW              │ this installation; holds an  │ Claw_Enrolment_Pkg │
//	│                       │ Ed25519 device key, has a    │ (ledger 113)       │
//	│                       │ ceiling                      │                    │
//	│ the OWNER             │ sets the ceiling, holds      │ Claw_Enrolment_Pkg │
//	│                       │ enrolment and revocation     │ (ledger 113)       │
//	│ the USER at the       │ supplies Fresh_Explicit      │ User_Access_Pkg    │
//	│ keyboard              │ consent; revocable in        │ (ledger 115)       │
//	│                       │ absentia                     │                    │
//	│ ★ the APPLE ACCOUNT   │ who actually BOUGHT the      │ ⛔ nothing yet —    │
//	│                       │ credits                      │ this is the gap    │
//	└───────────────────────┴──────────────────────────────┴────────────────────┘
//
// ⚠ THE APPLE IDENTITY IS WHERE THE MONEY IS, AND IT IS NEITHER THE CLAW NOR
// THE USER. One person may own several claws; a claw may be used by someone who
// is not the purchaser. It is also PII, so it falls under the confinement
// doctrine: it must appear in NO log line, NO outcome report and NO quarantine
// file. That is enforced here rather than by care at every call site — the type
// redacts itself through fmt and through encoding/json, and the only way to the
// underlying string is Reveal, which is named so that an audit grep finds every
// use of it in one command.
//
// Nothing in this package makes a decision. The decisions live in internal/gate
// (ledgers 113 and 115); this is the vocabulary those decisions are about.
package identity

import (
	"encoding/json"
	"errors"
	"fmt"
	"strings"
)

// ClawID names THIS INSTALLATION — the device, not the person using it and not
// the person who bought the credits. It is the subject of an enrolment record
// and the holder of the device signing key.
type ClawID string

// OwnerID names the ENROLLING OWNER: the party that submitted this machine, set
// its ceiling, and alone may revoke it. Distinct from the user at the keyboard
// by ledger 113's REVOCATION-BELONGS-TO-THE-OWNER.
type OwnerID string

// UserID names the PERSON AT THE KEYBOARD. They supply Fresh_Explicit consent
// (ledger 112) and their standing may be revoked in absentia (ledger 115). They
// are not the owner and they do not command the machine.
type UserID string

// String renders a claw id. Claw ids are not secret and not personal.
func (c ClawID) String() string { return string(c) }

// String renders an owner id.
func (o OwnerID) String() string { return string(o) }

// String renders a user id.
func (u UserID) String() string { return string(u) }

// RedactedAppleAccount is what an Apple account reference renders as ANYWHERE
// it is printed, formatted or serialised by the ordinary routes.
const RedactedAppleAccount = "«apple-account redacted»"

// AppleAccountRef is a reference to the Apple Account that bought the credits.
//
// ★ IT IS PII AND IT IS TREATED AS PII BY CONSTRUCTION. The reference is held
// in an unexported field, so:
//
//   - fmt cannot reach it — the type implements fmt.Formatter, so even %#v and
//     %+v render the redaction rather than the struct;
//   - encoding/json cannot reach it — an unexported field is invisible to the
//     encoder anyway, and MarshalJSON makes the redaction explicit rather than
//     emitting an empty object;
//   - the only accessor is Reveal, whose one legitimate caller is the enrolment
//     payload builder.
//
// This is the confinement doctrine applied at the smallest possible scale: the
// value is confined to the places that must have it, and leaking it takes a
// deliberate, greppable act rather than an absent-minded %v.
type AppleAccountRef struct {
	ref string
}

// NewAppleAccountRef wraps an Apple account reference. An empty string yields a
// ref that reports Present as false — "no purchaser bound" is a lawful state,
// because binding the purchaser happens at enrolment and the ceremony may not
// have run.
func NewAppleAccountRef(ref string) AppleAccountRef {
	return AppleAccountRef{ref: strings.TrimSpace(ref)}
}

// Present reports whether a purchaser has been bound at all. It discloses
// nothing about who.
func (a AppleAccountRef) Present() bool { return a.ref != "" }

// Reveal returns the underlying reference.
//
// ⚠ AUDIT POINT. This is the ONLY way out of the type, and it exists for
// exactly one caller: the enrolment payload, where the claw ↔ owner ↔ purchaser
// binding is established. `grep -rn 'Reveal()' .` must return a list short
// enough to read, and every entry on it must be an enrolment payload. It must
// never appear on a logging, reporting or quarantine path.
func (a AppleAccountRef) Reveal() string { return a.ref }

// String renders the redaction. This is what a %s or a bare print gets.
func (a AppleAccountRef) String() string { return RedactedAppleAccount }

// Format renders the redaction for EVERY verb, including %v, %+v, %#v and %q.
//
// String alone is not enough: %#v ignores Stringer and prints the struct with
// its unexported field, which would put the reference straight into a log line.
// Implementing fmt.Formatter closes that route.
func (a AppleAccountRef) Format(f fmt.State, verb rune) {
	switch verb {
	case 'q':
		fmt.Fprintf(f, "%q", RedactedAppleAccount)
	default:
		if _, err := f.Write([]byte(RedactedAppleAccount)); err != nil {
			// A formatting sink that cannot be written to is the caller's
			// problem, not a reason to fall back to printing the reference.
			return
		}
	}
}

// MarshalJSON emits the redaction. An outcome report or a quarantine notice
// that happens to embed this type therefore carries the redaction, visibly,
// rather than silently carrying nothing (or worse, the reference).
func (a AppleAccountRef) MarshalJSON() ([]byte, error) {
	body, err := json.Marshal(RedactedAppleAccount)
	if err != nil {
		return nil, fmt.Errorf("marshal redacted apple account: %w", err)
	}
	return body, nil
}

// ErrIncompleteBinding reports a binding that is missing an identity the
// ceremony requires.
var ErrIncompleteBinding = errors.New("identity: incomplete binding")

// Binding is the identity binding established at the ENROLMENT CEREMONY: which
// claw, submitted by which owner, used by which person, paid for from which
// Apple account.
//
// ⚠ THIS BINDING DOES NOT EXIST IN THE PROVEN LAYER YET. Ledger 113 governs
// claw and owner, ledger 115 governs the user, and NOTHING governs the
// purchaser. The struct exists so that v1's shape is right and the gap is
// visible in code rather than only in prose.
type Binding struct {
	Claw  ClawID
	Owner OwnerID
	User  UserID
	Apple AppleAccountRef
}

// Validate reports a binding that cannot be enrolled. The Apple account is
// OPTIONAL — a claw with no purchaser bound is an ordinary unpaid install, and
// refusing to enrol it would be wrong.
func (b Binding) Validate() error {
	switch {
	case b.Claw == "":
		return fmt.Errorf("%w: no claw", ErrIncompleteBinding)
	case b.Owner == "":
		return fmt.Errorf("%w: no enrolling owner — enrolment is an owner act (ledger 113)", ErrIncompleteBinding)
	case b.User == "":
		return fmt.Errorf("%w: no user at the keyboard", ErrIncompleteBinding)
	default:
		return nil
	}
}

// String renders a binding for a human, with the purchaser redacted. This is
// the form that is safe to log, and it is the only rendering of a Binding that
// exists.
func (b Binding) String() string {
	return fmt.Sprintf("claw=%s owner=%s user=%s apple=%s", b.Claw, b.Owner, b.User, RedactedAppleAccount)
}
