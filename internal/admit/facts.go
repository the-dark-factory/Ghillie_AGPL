package admit

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/tonygair/ghillie/internal/bundle"
)

// Candidate describes one install that is about to be recorded, as the
// install path already knows it. Every field here is EVIDENCE TO BE CHECKED,
// never a fact to be believed: the constructors below do their own hashing,
// their own manifest check and their own walk, and a field that turns out not
// to hold produces a weaker fact, never a stronger one.
type Candidate struct {
	// SourceDir is the staging tree — what the catalogue unpacked, or the
	// directory the operator named.
	SourceDir string
	// DestDir is the destination copy, already written. THE FACTS ARE
	// ESTABLISHED OVER THIS TREE, not over SourceDir, because SourceDir is
	// still writable by whoever produced it. That ordering is the whole of
	// the TOCTOU defence and it is not optional.
	DestDir string
	// ArchivePath is the delivered archive, when one was delivered. Empty on
	// the operator-directory path.
	ArchivePath string
	// WantDigest is the digest the catalogue's index named for that archive.
	// Empty means the index named none, which is itself a fact.
	WantDigest string
	// Revoked records that the catalogue withdrew this entry.
	Revoked bool
	// OperatorRequested records that a human ran this install by hand. Every
	// install verb ghillie has today is an owner act by doctrine, so this is
	// true on both paths; anything that ever installs without a human in the
	// room must pass false, and will get a different verdict for it.
	OperatorRequested bool
	// ProverRanClean records that the prover re-derived this bundle's proof
	// on THIS machine and the built front agreed with its shipped table.
	//
	// This is the one input the package cannot re-establish for itself: only
	// the code that ran the prover knows that it ran. What the package CAN do,
	// and does, is refuse to be talked upwards — a bundle that ships no proof
	// project is ProofAbsent whatever this field says. The field can lower
	// the rung; it can never raise it past what the bundle supports.
	ProverRanClean bool
	// RunsAtHostTrust records that this install runs something at ghillie's
	// own process trust — which today means only the locally-built front that
	// -reprove compiles from source it has just re-proved.
	RunsAtHostTrust bool
	// Floor is the host's declared minimum proof rung for this install site.
	Floor Floor
}

// FromCatalogue establishes the six facts for an ability fetched from a
// catalogue. The archive is re-hashed HERE against the digest the index
// named — the fetcher's earlier check is not taken on trust, because a
// constructor that trusts an earlier check is a constructor that can be
// lied to. The destination copy is then hashed against the staging tree and,
// where the bundle ships one, checked file by file against its manifest.
//
// Origin is attested by the catalogue index, and the authority string says
// plainly that the index is not signed: until it is, Origin_Attested here
// rests on trust-on-first-use and the ledger must not pretend otherwise.
func FromCatalogue(ctx context.Context, c Candidate, catalogueBase string) (facts Facts, err error) {
	if err := ctx.Err(); err != nil {
		return Facts{}, fmt.Errorf("admit: %w", err)
	}
	facts.floor = c.Floor
	facts.operator = operatorOf(c)
	facts.containment = containmentOf(c)
	facts.proof, err = proofOf(c)
	if err != nil {
		return Facts{}, err
	}

	// FALSE on this path, always: a catalogue install's integrity rests on
	// the index's digest and on nothing else. Bytes that arrived over a wire
	// with nothing said about them are UNCHECKED, and must never be allowed
	// to borrow the operator-hand rule that belongs to the other path.
	digest, integrity, err := integrityOf(ctx, c, false)
	if err != nil {
		return Facts{}, err
	}
	facts.integrity = integrity
	facts.digest = digest

	switch {
	case c.Revoked:
		facts.origin = OriginRevoked
		facts.authority = "the catalogue index at " + catalogueBase + " — WITHDRAWN"
	case c.WantDigest == "":
		// An index entry with no digest is an unverifiable claim, and the
		// catalogue reader already refuses those. If one ever reaches here,
		// it vouches for nothing.
		facts.origin = OriginUnattested
		facts.authority = "the catalogue index at " + catalogueBase + " named no digest — it vouches for nothing"
	case integrity != IntegrityVerified:
		// The authority named a digest and the bytes are not it. The binding
		// the authority asserted does not hold for these bytes, so nothing
		// is attested about them.
		facts.origin = OriginUnattested
		facts.authority = "the catalogue index at " + catalogueBase + " named a digest these bytes do not match"
	default:
		facts.origin = OriginAttested
		facts.authority = "the catalogue index at " + catalogueBase +
			" (trust-on-first-use: the index is not signed, so this attestation is only as strong as the pinned base)"
	}
	return facts, nil
}

// FromDirectory establishes the six facts for an operator-supplied
// directory, where THE OPERATOR'S OWN ACT IS THE ATTESTATION and the digest
// is computed at this instant.
//
// This is the honest resolution of a path that until now verified no digest
// at all. It is not a flag and not an exception: a person who reaches into
// their own filesystem and names a path has vouched for it more directly
// than any index can, and the ledger says exactly that. What changes is that
// the install becomes reconstructible — the digest of what was installed is
// recorded, so the same question can be asked again later.
func FromDirectory(ctx context.Context, c Candidate) (facts Facts, err error) {
	if err := ctx.Err(); err != nil {
		return Facts{}, fmt.Errorf("admit: %w", err)
	}
	facts.floor = c.Floor
	facts.operator = operatorOf(c)
	facts.containment = containmentOf(c)
	facts.proof, err = proofOf(c)
	if err != nil {
		return Facts{}, err
	}

	// THE OPERATOR'S HAND IS THE DIGEST — but only when there was a hand. An
	// unattended directory install has nobody vouching for the bytes, so it
	// gets the same UNCHECKED answer any other unhashed delivery gets.
	digest, integrity, err := integrityOf(ctx, c, facts.operator == OperatorRequested)
	if err != nil {
		return Facts{}, err
	}
	facts.integrity = integrity
	facts.digest = digest

	if facts.operator == OperatorRequested {
		facts.origin = OriginAttested
		facts.authority = "the operator's own hand — a person named this directory at the command line; no index vouches for it"
	} else {
		// Nobody asked for it, so nobody vouched for it either. An
		// unattended directory install has no attesting authority at all.
		facts.origin = OriginUnattested
		facts.authority = "nobody — an unattended directory install has no attesting authority"
	}
	return facts, nil
}

// operatorOf reads fact 1. It is a single boolean because ghillie's install
// verbs are standalone owner acts by doctrine; the value of naming it is
// that the day something installs unattended, the tuple changes and the
// verdict changes with it.
func operatorOf(c Candidate) Operator {
	if c.OperatorRequested {
		return OperatorRequested
	}
	return OperatorAbsent
}

// containmentOf reads fact 6 — a property of the LOAD PLAN, not of the
// artefact.
//
// Ghillie's plan for delivered bytes is: never grant them the execute bit
// (copyTree writes 0o600 whatever the archive asked for) and never run them.
// That is a boundary ghillie controls, so it is Contained. The one thing
// that runs at ghillie's own trust is the front that -reprove BUILDS here
// from source it has just re-proved — and when that happens the honest
// answer is Uncontained, which is exactly why the proof rung has to carry
// the weight in that case.
func containmentOf(c Candidate) Containment {
	if c.RunsAtHostTrust {
		return Uncontained
	}
	return Contained
}

// proofOf reads fact 4 from what is on disk, and refuses to be talked
// upwards. A bundle that ships no proof project carries no proof, whatever
// the caller believes it ran.
func proofOf(c Candidate) (Proof, error) {
	if c.DestDir == "" {
		return ProofAbsent, fmt.Errorf("admit: no destination copy to establish facts over — the tuple must be built over the bytes that were installed")
	}
	if !bundle.ClaimsProof(c.DestDir) {
		return ProofAbsent, nil
	}
	if c.ProverRanClean {
		return ProofReprovedHere, nil
	}
	return ProofCarried, nil
}

// integrityOf reads fact 3, over the DESTINATION COPY.
//
// Three checks, and it takes all of them:
//   - the delivered archive hashes to the digest the index named;
//   - the destination copy hashes to the same content hash as the staging
//     tree the digest covered, so nothing changed between the check and the
//     install;
//   - every file the bundle's own MANIFEST.sha256 lists still matches, in
//     the destination copy.
//
// A computed digest that disagrees is IntegrityMismatch — the delivery is
// altered and the remedy is to re-fetch, never to repair. Nothing to compare
// against at all is IntegrityUnchecked, which is a different state with a
// different remedy, and must never be allowed to read as verified.
// operatorHandIsTheDigest says whether this path may treat a hash computed
// at this instant, over bytes a person named by hand, as an integrity
// answer. Only FromDirectory may pass true, and only when a person really
// did name them; passing it from anywhere else would turn "nobody looked"
// into "verified", which is the one collapse the three-valued state exists
// to prevent.
func integrityOf(ctx context.Context, c Candidate, operatorHandIsTheDigest bool) (digest string, state Integrity, err error) {
	if err := ctx.Err(); err != nil {
		return "", IntegrityUnchecked, fmt.Errorf("admit: %w", err)
	}
	if c.DestDir == "" || c.SourceDir == "" {
		return "", IntegrityUnchecked, fmt.Errorf("admit: both the staging tree and the destination copy are needed to establish integrity")
	}

	destHash, err := bundle.Verify(c.DestDir)
	if err != nil {
		return "", IntegrityUnchecked, fmt.Errorf("admit: the destination copy does not verify: %w", err)
	}
	digest = destHash

	srcHash, err := bundle.Verify(c.SourceDir)
	if err != nil {
		return digest, IntegrityUnchecked, fmt.Errorf("admit: the staging tree does not verify: %w", err)
	}
	if srcHash != destHash {
		// The copy is not the thing that was checked. This is the window the
		// ordering exists to close, and finding it open is a refusal.
		return digest, IntegrityMismatch, nil
	}

	// The bundle's own per-file digests, re-checked over the destination.
	if _, mErr := bundle.VerifyManifest(c.DestDir); mErr != nil {
		return digest, IntegrityMismatch, nil
	}

	if c.ArchivePath == "" {
		// No delivered archive. On the operator-directory path that is fine:
		// the operator's own act named these bytes and the destination is
		// provably the tree they named, so the hash computed at this instant
		// over what was installed IS the integrity answer. Anywhere else,
		// nothing was hashed against anything and the honest answer is that
		// nobody looked.
		if operatorHandIsTheDigest && c.WantDigest == "" {
			return digest, IntegrityVerified, nil
		}
		return digest, IntegrityUnchecked, nil
	}

	if c.WantDigest == "" {
		// Bytes arrived over a wire and nothing said what they should be.
		return digest, IntegrityUnchecked, nil
	}
	archiveHash, hErr := hashFile(ctx, c.ArchivePath)
	if hErr != nil {
		return digest, IntegrityUnchecked, nil
	}
	if !strings.EqualFold(archiveHash, c.WantDigest) {
		return digest, IntegrityMismatch, nil
	}
	return digest, IntegrityVerified, nil
}

// hashFile is the sha256 of one file's bytes, hex-encoded lower case.
func hashFile(ctx context.Context, path string) (sum string, err error) {
	if err := ctx.Err(); err != nil {
		return "", err
	}
	f, err := os.Open(filepath.Clean(path))
	if err != nil {
		return "", fmt.Errorf("admit: %w", err)
	}
	defer f.Close()
	h := sha256.New()
	if _, err := io.Copy(h, f); err != nil {
		return "", fmt.Errorf("admit: hashing %s: %w", path, err)
	}
	return hex.EncodeToString(h.Sum(nil)), nil
}
