package bundle

import (
	"archive/tar"
	"compress/gzip"
	"crypto/sha256"
	"encoding/hex"
	"filippo.io/age"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// scaffold builds a minimal five-part bundle.
func scaffold(t *testing.T, root, name string) string {
	t.Helper()
	dir := filepath.Join(root, name)
	for _, d := range []string{"core", "surface"} {
		if err := os.MkdirAll(filepath.Join(dir, d), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	for f, body := range map[string]string{
		"human.md":            "# For the human\nIt helps.",
		"ghillie.md":          "# For ghillie\nOffer when asked.",
		"provenance.md":       "Prototype. NOT factory-proved. Says so plainly.",
		"core/ATTESTATION.md": "unproven, honest",
		"surface/run.sh":      "#!/bin/sh\necho ok\n",
	} {
		if err := os.WriteFile(filepath.Join(dir, f), []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	return dir
}

func TestVerifyAndInstall(t *testing.T) {
	root := t.TempDir()
	dir := scaffold(t, root, "paid-twice")
	abilities := filepath.Join(root, "abilities")
	if err := os.MkdirAll(abilities, 0o755); err != nil {
		t.Fatal(err)
	}

	hash1, err := Verify(dir)
	if err != nil {
		t.Fatalf("Verify: %v", err)
	}
	entry, err := Install(dir, abilities, "test", "")
	if err != nil {
		t.Fatalf("Install: %v", err)
	}
	if entry.Hash != hash1 {
		t.Fatal("ledger hash differs from verified hash")
	}
	if _, err := os.Stat(filepath.Join(abilities, "paid-twice", "human.md")); err != nil {
		t.Fatal("installed bundle lacks human.md")
	}
	ledger, _ := os.ReadFile(filepath.Join(abilities, "LEDGER.jsonl"))
	if !strings.Contains(string(ledger), hash1) {
		t.Fatal("the ledger does not record the hash — the ledger IS the install")
	}

	// Reinstall refused — an upgrade is remove-then-install, visible.
	if _, err := Install(dir, abilities, "test", ""); err == nil {
		t.Fatal("silent overwrite must be refused")
	}
}

func TestIncompleteBundleRefused(t *testing.T) {
	root := t.TempDir()
	dir := scaffold(t, root, "half")
	if err := os.Remove(filepath.Join(dir, "provenance.md")); err != nil {
		t.Fatal(err)
	}
	if _, err := Verify(dir); err == nil {
		t.Fatal("a bundle without provenance must not verify")
	}
}

func TestUnpackConfinesPaths(t *testing.T) {
	root := t.TempDir()
	archive := filepath.Join(root, "evil.tar.gz")
	f, _ := os.Create(archive)
	gz := gzip.NewWriter(f)
	tw := tar.NewWriter(gz)
	_ = tw.WriteHeader(&tar.Header{Name: "../escape.txt", Mode: 0o644, Size: 4, Typeflag: tar.TypeReg})
	_, _ = tw.Write([]byte("boom"))
	tw.Close()
	gz.Close()
	f.Close()

	if _, err := Unpack(archive, filepath.Join(root, "out")); err == nil {
		t.Fatal("a path-escaping archive must be refused whole")
	}
}

func TestUnpackRoundTrip(t *testing.T) {
	root := t.TempDir()
	dir := scaffold(t, root, "old-words")
	archive := filepath.Join(root, "old-words.tar.gz")

	f, _ := os.Create(archive)
	gz := gzip.NewWriter(f)
	tw := tar.NewWriter(gz)
	_ = filepath.Walk(dir, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		rel, _ := filepath.Rel(root, path)
		if info.IsDir() {
			return tw.WriteHeader(&tar.Header{Name: rel + "/", Mode: 0o755, Typeflag: tar.TypeDir})
		}
		raw, _ := os.ReadFile(path)
		_ = tw.WriteHeader(&tar.Header{Name: rel, Mode: int64(info.Mode().Perm()), Size: int64(len(raw)), Typeflag: tar.TypeReg})
		_, e := tw.Write(raw)
		return e
	})
	tw.Close()
	gz.Close()
	f.Close()

	got, err := Unpack(archive, filepath.Join(root, "unpacked"))
	if err != nil {
		t.Fatalf("Unpack: %v", err)
	}
	if _, err := Verify(got); err != nil {
		t.Fatalf("round-tripped bundle does not verify: %v", err)
	}
}

// TestVerifyManifest pins both polarities: a good manifest passes with the
// count of what it checked, an altered file is a refusal naming the file,
// and no manifest at all is the legacy pass (checked 0).
func TestVerifyManifest(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "a.txt"), []byte("alpha\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	sum := sha256.Sum256([]byte("alpha\n"))
	good := hex.EncodeToString(sum[:]) + "  ./a.txt\n"

	if n, err := VerifyManifest(dir); err != nil || n != 0 {
		t.Fatalf("no manifest: want (0, nil), got (%d, %v)", n, err)
	}
	if err := os.WriteFile(filepath.Join(dir, "MANIFEST.sha256"), []byte(good), 0o600); err != nil {
		t.Fatal(err)
	}
	if n, err := VerifyManifest(dir); err != nil || n != 1 {
		t.Fatalf("good manifest: want (1, nil), got (%d, %v)", n, err)
	}
	if err := os.WriteFile(filepath.Join(dir, "a.txt"), []byte("tampered\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := VerifyManifest(dir); err == nil || !strings.Contains(err.Error(), "a.txt") {
		t.Fatalf("tampered file: want a refusal naming a.txt, got %v", err)
	}
	escape := "0000000000000000000000000000000000000000000000000000000000000000  ../escape\n"
	if err := os.WriteFile(filepath.Join(dir, "MANIFEST.sha256"), []byte(escape), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := VerifyManifest(dir); err == nil {
		t.Fatal("path escape in manifest: want a refusal, got none")
	}
}

// TestClaimsProof: the claim is the presence of the proof project, nothing
// subtler.
func TestClaimsProof(t *testing.T) {
	dir := t.TempDir()
	if ClaimsProof(dir) {
		t.Fatal("empty dir claims a proof")
	}
	if err := os.MkdirAll(filepath.Join(dir, "core", "src"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "core", "src", "proof.gpr"), []byte("project P is end P;"), 0o600); err != nil {
		t.Fatal(err)
	}
	if !ClaimsProof(dir) {
		t.Fatal("proof.gpr present but no claim seen")
	}
}

// TestEncryptedDelivery pins the confidential tier's bundle mechanics:
// a sealed archive opens only with the right identity, streams through the
// same unpack (so every path/size/mode guard applies), and Dispose leaves
// no readable source behind.
func TestEncryptedDelivery(t *testing.T) {
	root := t.TempDir()
	dir := scaffold(t, root, "secret-ability")
	plainArchive := filepath.Join(root, "secret.tar.gz")

	f, _ := os.Create(plainArchive)
	gz := gzip.NewWriter(f)
	tw := tar.NewWriter(gz)
	_ = filepath.Walk(dir, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		rel, _ := filepath.Rel(root, path)
		if info.IsDir() {
			return tw.WriteHeader(&tar.Header{Name: rel + "/", Mode: 0o755, Typeflag: tar.TypeDir})
		}
		raw, _ := os.ReadFile(path)
		_ = tw.WriteHeader(&tar.Header{Name: rel, Mode: int64(info.Mode().Perm()), Size: int64(len(raw)), Typeflag: tar.TypeReg})
		_, e := tw.Write(raw)
		return e
	})
	tw.Close()
	gz.Close()
	f.Close()

	id, err := age.GenerateX25519Identity()
	if err != nil {
		t.Fatal(err)
	}
	otherID, err := age.GenerateX25519Identity()
	if err != nil {
		t.Fatal(err)
	}
	raw, err := os.ReadFile(plainArchive)
	if err != nil {
		t.Fatal(err)
	}
	sealed := filepath.Join(root, "secret.tar.gz.age")
	sf, err := os.Create(sealed)
	if err != nil {
		t.Fatal(err)
	}
	w, err := age.Encrypt(sf, id.Recipient())
	if err != nil {
		t.Fatal(err)
	}
	if _, err := w.Write(raw); err != nil {
		t.Fatal(err)
	}
	if err := w.Close(); err != nil {
		t.Fatal(err)
	}
	sf.Close()

	t.Run("right key opens and verifies", func(t *testing.T) {
		got, err := UnpackEncrypted(sealed, filepath.Join(root, "unpacked"), id)
		if err != nil {
			t.Fatalf("UnpackEncrypted: %v", err)
		}
		if _, err := Verify(got); err != nil {
			t.Fatalf("sealed round-trip does not verify: %v", err)
		}
	})
	t.Run("wrong key refuses whole", func(t *testing.T) {
		if _, err := UnpackEncrypted(sealed, filepath.Join(root, "unpacked-wrong"), otherID); err == nil {
			t.Fatal("a delivery sealed to another claw opened")
		}
	})
	t.Run("tampered ciphertext refuses before the prover", func(t *testing.T) {
		bad, err := os.ReadFile(sealed)
		if err != nil {
			t.Fatal(err)
		}
		bad[len(bad)-1] ^= 0x01
		tampered := filepath.Join(root, "tampered.age")
		if err := os.WriteFile(tampered, bad, 0o600); err != nil {
			t.Fatal(err)
		}
		if _, err := UnpackEncrypted(tampered, filepath.Join(root, "unpacked-tampered"), id); err == nil {
			t.Fatal("tampered ciphertext opened")
		}
	})
	t.Run("dispose leaves no source behind", func(t *testing.T) {
		staging := filepath.Join(root, "staging")
		if _, err := UnpackEncrypted(sealed, staging, id); err != nil {
			t.Fatal(err)
		}
		if err := Dispose(staging); err != nil {
			t.Fatalf("Dispose: %v", err)
		}
		if _, err := os.Stat(staging); !os.IsNotExist(err) {
			t.Errorf("staging survives disposal: %v", err)
		}
	})
}
