// Package protocol holds the JSON shapes of the ghillie⇄facade poll.
//
// DIRECTION. ghillie POLLS OUTWARD, ALWAYS. The customer's machine opens every
// connection; there is no listener, no open port and no inbound path on the
// customer side. That is the architectural half of the safety claim and it is
// true before any theorem is invoked — the facade physically cannot initiate
// contact. Nothing in this package should ever grow a server for the ghillie
// side.
//
// WHAT IS LOAD-BEARING AND WHAT IS NOT. The JSON here is ordinary unproven
// byte-shuffling and that is fine, because none of it is load-bearing: the only
// thing that carries authority is the 22-byte instruction frame inside
// Envelope, which is signed over verbatim and decoded by a codec cross-checked
// against ledger 119. A transport change (protobuf, byte-stream, carrier
// pigeon) changes nothing above that line.
package protocol

import (
	"encoding/binary"
	"strconv"
)

// PollPath returns the poll endpoint for a claw. The customer side calls it;
// the facade side serves it.
func PollPath(clawID string) string { return "/claws/" + clawID + "/poll" }

// EnrolPath returns the enrolment endpoint for a claw. ENROLMENT IS AN OWNER
// ACT (Claw_Enrolment_Pkg, ledger 113) — the endpoint records what the proven
// core gates; it does not itself confer authority.
func EnrolPath(clawID string) string { return "/claws/" + clawID + "/enrol" }

// AttestPath returns the attestation endpoint for a claw: challenge on an empty
// body, verify on {nonce, sig}.
func AttestPath(clawID string) string { return "/claws/" + clawID + "/attest" }

// BriefPath returns the outward GET for a brief BODY.
//
// ★ THIS FETCH CARRIES NO AUTHORITY. The 22-byte signed frame remains the only
// authority-bearing object on the wire: a Deliver_Artifact frame names the brief
// (ArtifactRef) and its revision (Version), the gate admits it, and only then
// does the terminal reach out for the body. The body is checked back against the
// signed frame's ref and version, so an unsigned GET cannot substitute a
// different brief for the one the signed frame announced.
func BriefPath(artifactRef uint64, version uint32) string {
	return "/briefs/" + strconv.FormatUint(artifactRef, 16) + "?version=" + strconv.FormatUint(uint64(version), 10)
}

// SpecsPath is where answers are submitted. ANSWERS LEAVE BY GHILLIE'S OWN
// INITIATIVE and by no other route: Facade_Command_Pkg proves
// Is_Local_Only (Request_Spec_Upload), so the facade can never pull them at any
// ceiling or consent level.
const SpecsPath = "/specs"

// CreditPath returns the outward GET that asks the FACADE whether this claw may
// perform an act. The local balance is a courtesy; this is the decision.
func CreditPath(clawID, act string) string {
	return "/claws/" + clawID + "/credit?act=" + act
}

// NotesPath returns the outward GET for the claw's correspondent notes — the
// progress channel (internal/notes). A NOTE CARRIES INFORMATION AND NO
// AUTHORITY: this fetch shares nothing with the instruction path. The facade
// signs every note it serves; the terminal decodes, checks, records, renders —
// and dispatches on none of it.
func NotesPath(clawID string) string { return "/claws/" + clawID + "/notes" }

// Envelope carries one instruction: the exact frame bytes and an Ed25519
// signature over those exact bytes and nothing else. Both are lowercase hex —
// hex rather than base64 so a frame can be eyeballed against the golden
// vectors, and "the exact bytes" rather than a JSON object because signing JSON
// requires canonicalisation, a well-known swamp this protocol declines to enter.
type Envelope struct {
	Frame     string `json:"frame"`     // 44 hex chars = 22 bytes
	Signature string `json:"signature"` // 128 hex chars = 64 bytes
}

// Outcome is the terminal's record of what it did with one instruction, sent
// back to the facade on the NEXT poll. A refused instruction is never silently
// dropped.
//
// ⚠ OPEN DESIGN DECISION 7, UNRESOLVED: reporting the reason class to the
// facade helps operations and honesty, but it also tells a compromised facade
// exactly which probe bounced off which ceiling. v0 reports the full reason
// because the refusal is the thing being demonstrated. If the decision lands
// the other way, the change is to send only "refused" here while keeping the
// full reason in the local log — the user-visible refusal is the asset and it
// stays either way.
type Outcome struct {
	Seq         uint64 `json:"seq"`
	Command     uint8  `json:"command"`
	CommandName string `json:"command_name"`
	Admitted    bool   `json:"admitted"`
	Reason      string `json:"reason,omitempty"`
	Note        string `json:"note,omitempty"`
	At          string `json:"at"`
}

// Answer is one item of a conducted interview as ghillie found it.
//
// ★ IT REPORTS STATE, IT DOES NOT PASS JUDGEMENT. State is the ledger-123 state
// name verbatim, and Answered is Is_Got — so an INTERRUPTED item arrives at the
// factory carrying the client's interjection AND the fact that it is not an
// answer. What that is worth is the factory's to decide.
type Answer struct {
	ItemID   int    `json:"item_id"`
	State    string `json:"state"`               // Unasked | Asked | Answered | Interrupted (ledger 123)
	Answered bool   `json:"answered"`            // Is_Got — NEVER true for Interrupted
	Text     string `json:"text,omitempty"`      // what the client said, verbatim, uninterpreted
	CutShort bool   `json:"cut_short,omitempty"` // ghillie was cut off while putting this item
	Attempts int    `json:"attempts"`
	At       string `json:"at"`
}

// Submission is a batch of answers leaving the claw, signed with the DEVICE
// KEY over SigningBytes.
//
// The signature is the authority, not the route: the same signed object is
// posted to SpecsPath on ghillie's own initiative and, when that post has not
// yet succeeded, carried on the next outward poll so that nothing queued is ever
// lost. A facade that receives it twice dedupes on SubmissionID.
type Submission struct {
	SubmissionID string   `json:"submission_id"`
	ClawID       string   `json:"claw_id"`
	BriefID      string   `json:"brief_id"`
	Version      uint32   `json:"version"`
	Answers      []Answer `json:"answers"`
	At           string   `json:"at"`
	Signature    string   `json:"signature"` // hex Ed25519 over SigningBytes
}

// SigningBytes renders the deterministic byte string the device key signs.
//
// It is a LENGTH-PREFIXED CONCATENATION and deliberately not JSON. Signing JSON
// requires canonicalisation, which is a well-known swamp this protocol declines
// to enter — the same reasoning that made the 22-byte instruction frame a fixed
// layout signed verbatim. Every variable-length field is preceded by its length,
// so no two distinct submissions share a byte string.
func (s Submission) SigningBytes() []byte {
	var out []byte
	put := func(field string) {
		var n [8]byte
		binary.BigEndian.PutUint64(n[:], uint64(len(field)))
		out = append(out, n[:]...)
		out = append(out, field...)
	}
	put("ghillie-submission-v1")
	put(s.SubmissionID)
	put(s.ClawID)
	put(s.BriefID)
	var v [4]byte
	binary.BigEndian.PutUint32(v[:], s.Version)
	out = append(out, v[:]...)
	put(s.At)

	var count [8]byte
	binary.BigEndian.PutUint64(count[:], uint64(len(s.Answers)))
	out = append(out, count[:]...)
	for _, a := range s.Answers {
		var id [8]byte
		binary.BigEndian.PutUint64(id[:], uint64(a.ItemID))
		out = append(out, id[:]...)
		put(a.State)
		if a.Answered {
			out = append(out, 1)
		} else {
			out = append(out, 0)
		}
		if a.CutShort {
			out = append(out, 1)
		} else {
			out = append(out, 0)
		}
		put(a.Text)
		var att [8]byte
		binary.BigEndian.PutUint64(att[:], uint64(a.Attempts))
		out = append(out, att[:]...)
		put(a.At)
	}
	return out
}

// PollRequest is the outward poll. It carries the claw's declared ceiling (so a
// facade can avoid sending what will bounce, without that being a control — the
// gate refuses regardless of what the facade believes), any outcomes not yet
// reported, and any signed submissions whose direct post has not yet landed.
type PollRequest struct {
	ClawID  string       `json:"claw_id"`
	Ceiling uint8        `json:"ceiling"`
	Reports []Outcome    `json:"reports,omitempty"`
	Answers []Submission `json:"answers,omitempty"`
}

// PollResponse is the facade's answer: zero or more signed instruction frames.
//
// THE EMPTY RESPONSE IS THE NON-DISCLOSURE IDENTITY. "Nothing for you" and
// "instructions above your ceiling exist" must be indistinguishable on the
// wire, which is why the facade answers both with 204 No Content and no body at
// all. A facade that answered them differently would leak the shape of what it
// wanted to do.
// MaxPollInstructions bounds how many instructions one poll may carry.
//
// The facade is modelled as HOSTILE, so its reply is bounded like any other
// remote read. An Envelope is FIXED size — a 44-hex frame and a 128-hex
// signature — so this bound is exact rather than a guess: 64 envelopes is well
// past any honest batch and still under 20 kB on the wire. A poll carrying more
// is REFUSED whole, not truncated: silently dropping the tail would hide from
// the owner that the facade sent something the terminal chose not to see.
const MaxPollInstructions = 64

type PollResponse struct {
	Instructions []Envelope `json:"instructions"`
}
