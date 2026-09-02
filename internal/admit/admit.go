// Package admit is the named decider ghillie did not have.
//
// Until now the decision to install an ability was the emergent behaviour of
// four separate places returning nil — Fetcher.Fetch checking an archive
// digest, bundle.Verify checking the five-part contract, VerifyManifest
// checking per-file digests, and bundle.Install refusing an unrecorded
// install. Nothing named the decision. `err == nil` WAS the decision.
//
// This package names it. Six normalised facts go in; one verdict comes out,
// decided by a proven core (KingKlaw's admission member — see
// ~/dev/kingklaw). The core reads no clock, opens no file and holds no
// state: it decides what FOLLOWS once the six facts are established, and
// establishing them is this package's job.
//
// ★ NO CALLER MAY ASSERT A FACT IT DID NOT ESTABLISH. Facts has unexported
// fields and no literal of it can be written outside this package. The only
// way to obtain one is through a constructor here, and every constructor
// does its own hashing, its own manifest check and its own walk. A caller
// that would like to claim Integrity_Verified must instead hand over the
// bytes and let this package look.
//
// ★ EVERY DOUBT IS A REFUSAL. A missing front binary, a signal, a timeout,
// an unreadable exit status, an empty stdout and a word this package does
// not recognise all resolve to the same side: not admitted. Only exit 0 with
// the literal word "admit" on stdout admits.
package admit

import (
	"fmt"
)

// Operator is fact 1: whether a human explicitly asked for THIS artefact,
// NOW. An auto-update path that installs without a human is a different
// state and must get a different verdict.
type Operator int

// The operator states.
const (
	// OperatorRequested means a fresh human act initiated this install.
	OperatorRequested Operator = iota
	// OperatorAbsent means something else did — a schedule, a poll, a peer.
	OperatorAbsent
)

// Origin is fact 2: whether an authority this host has pinned asserts the
// binding from this artefact's identity to this digest, and whether that
// assertion still stands.
type Origin int

// The origin states.
const (
	// OriginAttested means a pinned authority asserts the binding and has
	// not withdrawn it.
	OriginAttested Origin = iota
	// OriginUnattested covers BOTH no known authority and a known authority
	// silent about this artefact. It is the fail-closed default.
	OriginUnattested
	// OriginRevoked means the authority has withdrawn the artefact.
	OriginRevoked
)

// Integrity is fact 3: whether the bytes about to be installed hash to the
// digest the authority recorded.
type Integrity int

// The integrity states.
const (
	// IntegrityVerified means they were hashed here and match.
	IntegrityVerified Integrity = iota
	// IntegrityMismatch means they were hashed here and differ — the
	// delivery is altered. Re-fetch it; never repair it.
	IntegrityMismatch
	// IntegrityUnchecked means nobody hashed them at all.
	IntegrityUnchecked
)

// Proof is fact 4: the artefact's honest proof rung, as ghillie already
// records it in the ledger.
type Proof int

// The proof states.
const (
	// ProofReprovedHere means the proofs were re-derived on this machine.
	ProofReprovedHere Proof = iota
	// ProofCarried means the bundle ships proofs that were read but not
	// re-run here.
	ProofCarried
	// ProofAbsent means it carries none. This is a valid, honest answer.
	ProofAbsent
)

// Floor is fact 5: the HOST's declared minimum rung for this load site. It
// is ghillie's policy, not the artefact's property, and it is the field that
// lets one core serve hosts with entirely different proof ambitions.
type Floor int

// The floors.
const (
	// FloorReprovedHere demands proofs re-derived on this machine.
	FloorReprovedHere Floor = iota
	// FloorCarried demands at least carried proofs.
	FloorCarried
	// FloorNone demands no proof at all.
	FloorNone
)

// Containment is fact 6: whether the artefact will run inside a boundary the
// host controls, or at the host's own process trust. It is a property of the
// LOADER'S OWN PLAN, not of the artefact.
type Containment int

// The containment states.
const (
	// Contained means this install plan never grants the artefact's bytes
	// the execute bit and never runs them at ghillie's trust.
	Contained Containment = iota
	// Uncontained means something from this install runs at ghillie's own
	// process trust.
	Uncontained
)

// Verdict is the decision the proven core returned.
type Verdict int

// The nine verdicts, in the core's own order. Only Admit permits an install.
const (
	Admit Verdict = iota
	RefuseOperatorAbsent
	RefuseOriginRevoked
	RefuseOriginUnattested
	RefuseIntegrityMismatch
	RefuseIntegrityUnchecked
	RefuseProofAbsent
	RefuseProofNotReproved
	RefuseUncontainedUnproven
	// RefuseUndecided is not one of the core's verdicts. It is what this
	// package returns when it could not obtain one: no front binary, a
	// signal, a timeout, a word it does not recognise. It exists so that
	// "the decider did not answer" can never be mistaken for "the decider
	// said yes".
	RefuseUndecided
)

// verdictWords maps a verdict to the exact word the front prints. The front
// is the authority on these words; this table is checked against it by
// TestVerdictWordsMatchTheFront.
var verdictWords = map[Verdict]string{
	Admit:                     "admit",
	RefuseOperatorAbsent:      "refuse_operator_absent",
	RefuseOriginRevoked:       "refuse_origin_revoked",
	RefuseOriginUnattested:    "refuse_origin_unattested",
	RefuseIntegrityMismatch:   "refuse_integrity_mismatch",
	RefuseIntegrityUnchecked:  "refuse_integrity_unchecked",
	RefuseProofAbsent:         "refuse_proof_absent",
	RefuseProofNotReproved:    "refuse_proof_not_reproved",
	RefuseUncontainedUnproven: "refuse_uncontained_unproven",
	RefuseUndecided:           "refuse_undecided",
}

// String returns the verdict word, which is what the ledger records and what
// a person reads.
func (v Verdict) String() string {
	if w, ok := verdictWords[v]; ok {
		return w
	}
	return "refuse_undecided"
}

// Admitted reports whether this verdict permits the install. It is the only
// question a caller should ask, and it is deliberately the narrow one: every
// value that is not exactly Admit answers false, including verdicts added
// later and including RefuseUndecided.
func (v Verdict) Admitted() bool { return v == Admit }

// Remedy returns what the owner can actually do about a refusal, in plain
// words. A verdict nobody can act on is a verdict nobody will keep.
func (v Verdict) Remedy() string {
	switch v {
	case Admit:
		return "nothing to do — admitted"
	case RefuseOperatorAbsent:
		return "installing is an owner act: run the install command yourself"
	case RefuseOriginRevoked:
		return "the catalogue has withdrawn this ability — do not install it, and do not work around this"
	case RefuseOriginUnattested:
		return "nothing here vouches for this ability's identity — install it from the catalogue, or hand over the directory yourself so your own act is the attestation"
	case RefuseIntegrityMismatch:
		return "the delivery is altered — RE-FETCH it; never repair it in place"
	case RefuseIntegrityUnchecked:
		return "nobody hashed these bytes — fetch through the catalogue so there is a digest to check against"
	case RefuseProofAbsent:
		return "this install site demands a proof rung and the ability carries none — lower the floor deliberately, or get an ability that carries proofs"
	case RefuseProofNotReproved:
		return "the ability carries proofs but no prover ran here — install with -reprove, or lower the floor deliberately"
	case RefuseUncontainedUnproven:
		return "unproven code would run at ghillie's own trust — install with -reprove, or do not install it"
	case RefuseUndecided:
		return "the decider did not answer: check that extension_admission_front is beside the ghillie binary, on PATH, or named by GHILLIE_ADMISSION_FRONT"
	}
	return "unknown verdict — treat as a refusal"
}

// tokens are the exact argument words the front expects, per position.
var (
	operatorTokens    = map[Operator]string{OperatorRequested: "requested", OperatorAbsent: "absent"}
	originTokens      = map[Origin]string{OriginAttested: "attested", OriginUnattested: "unattested", OriginRevoked: "revoked"}
	integrityTokens   = map[Integrity]string{IntegrityVerified: "verified", IntegrityMismatch: "mismatch", IntegrityUnchecked: "unchecked"}
	proofTokens       = map[Proof]string{ProofReprovedHere: "reproved", ProofCarried: "carried", ProofAbsent: "absent"}
	floorTokens       = map[Floor]string{FloorReprovedHere: "floor_reproved", FloorCarried: "floor_carried", FloorNone: "floor_none"}
	containmentTokens = map[Containment]string{Contained: "contained", Uncontained: "uncontained"}
)

// String returns the front's token for this operator state.
func (o Operator) String() string { return tokenOr(operatorTokens[o]) }

// String returns the front's token for this origin state.
func (o Origin) String() string { return tokenOr(originTokens[o]) }

// String returns the front's token for this integrity state.
func (i Integrity) String() string { return tokenOr(integrityTokens[i]) }

// String returns the front's token for this proof state.
func (p Proof) String() string { return tokenOr(proofTokens[p]) }

// String returns the front's token for this floor.
func (f Floor) String() string { return tokenOr(floorTokens[f]) }

// String returns the front's token for this containment state.
func (c Containment) String() string { return tokenOr(containmentTokens[c]) }

// tokenOr turns an unmapped enumeration into a word the front will REJECT,
// so a value this package has not been taught about produces exit 2 —
// a refusal — rather than a silently different question.
func tokenOr(s string) string {
	if s == "" {
		return "unmapped"
	}
	return s
}

// Facts is the neutral admission tuple, established by this package and by
// nothing else. Its fields are unexported: no caller can assert a fact it
// did not establish, because no caller can write a Facts at all.
//
// The two provenance fields are not facts and are not handed to the core.
// They are what the ledger records so that an install can always be
// re-decided from its own record.
type Facts struct {
	operator    Operator
	origin      Origin
	integrity   Integrity
	proof       Proof
	floor       Floor
	containment Containment

	// authority names, in plain words, who attested the identity→digest
	// binding — "the catalogue index at <base>" or "the operator's own hand".
	authority string
	// digest is the content hash the integrity fact rests on, over the
	// bytes that were actually installed.
	digest string
}

// Args returns the six tokens in the order the front expects them:
// OPERATOR ORIGIN INTEGRITY PROOF FLOOR CONTAINMENT.
func (f Facts) Args() []string {
	return []string{
		f.operator.String(), f.origin.String(), f.integrity.String(),
		f.proof.String(), f.floor.String(), f.containment.String(),
	}
}

// Authority reports, in plain words, who attested this artefact's identity.
func (f Facts) Authority() string { return f.authority }

// Digest reports the content hash the integrity fact rests on.
func (f Facts) Digest() string { return f.digest }

// Floor reports the floor that was in force for this decision, so the ledger
// can record it and a later audit can re-decide every past install at a
// higher one.
func (f Facts) Floor() Floor { return f.floor }

// String renders the tuple for a ledger line or a log.
func (f Facts) String() string {
	a := f.Args()
	return fmt.Sprintf("%s %s %s %s %s %s", a[0], a[1], a[2], a[3], a[4], a[5])
}
