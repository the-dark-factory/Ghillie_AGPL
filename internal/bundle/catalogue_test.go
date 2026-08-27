package bundle

import (
	"archive/tar"
	"compress/gzip"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// packBundle writes a verifiable bundle archive and returns its path + digest.
func packBundle(t *testing.T, root, name string) (string, string, int64) {
	t.Helper()
	dir := scaffold(t, root, name)
	archive := filepath.Join(root, name+".tar.gz")
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
		_ = tw.WriteHeader(&tar.Header{Name: rel, Mode: 0o644, Size: int64(len(raw)), Typeflag: tar.TypeReg})
		_, e := tw.Write(raw)
		return e
	})
	tw.Close()
	gz.Close()
	f.Close()
	raw, _ := os.ReadFile(archive)
	sum := sha256.Sum256(raw)
	return archive, hex.EncodeToString(sum[:]), int64(len(raw))
}

// serveCatalogue lays out a local catalogue directory and returns its base.
func serveCatalogue(t *testing.T, root string, entries []Entry) string {
	t.Helper()
	base := filepath.Join(root, "catalogue")
	if err := os.MkdirAll(base, 0o755); err != nil {
		t.Fatal(err)
	}
	idx := Index{Catalogue: "test", Published: "2026-08-06T00:00:00Z", Abilities: entries}
	raw, _ := json.MarshalIndent(idx, "", "  ")
	if err := os.WriteFile(filepath.Join(base, "index.json"), raw, 0o644); err != nil {
		t.Fatal(err)
	}
	return base
}

func TestFetchInstallListUninstall(t *testing.T) {
	root := t.TempDir()
	archive, digest, size := packBundle(t, root, "paid-twice")
	base := serveCatalogue(t, root, []Entry{{
		Name: "paid-twice", Summary: "flags lookalike doubles", Offer: "when they wonder about a payment",
		Needs: "a payments CSV", Proof: "prototype", Cost: "free",
		Archive: "paid-twice.tar.gz", Digest: digest, Bytes: size,
	}})
	// the archive must live under the catalogue base
	raw, _ := os.ReadFile(archive)
	if err := os.WriteFile(filepath.Join(base, "paid-twice.tar.gz"), raw, 0o644); err != nil {
		t.Fatal(err)
	}

	f := NewFetcher(base)
	idx, err := f.Index()
	if err != nil {
		t.Fatalf("Index: %v", err)
	}
	if len(idx.Abilities) != 1 || idx.Abilities[0].Name != "paid-twice" {
		t.Fatalf("index wrong: %+v", idx)
	}

	staging := filepath.Join(root, "staging")
	got, err := f.Fetch(idx.Abilities[0], staging)
	if err != nil {
		t.Fatalf("Fetch: %v", err)
	}

	abilities := filepath.Join(root, "abilities")
	if err := os.MkdirAll(abilities, 0o755); err != nil {
		t.Fatal(err)
	}
	dir, err := Unpack(got, staging)
	if err != nil {
		t.Fatalf("Unpack: %v", err)
	}
	if _, err := Install(dir, abilities, "catalogue:test", ""); err != nil {
		t.Fatalf("Install: %v", err)
	}

	list, err := Installed(abilities)
	if err != nil || len(list) != 1 || list[0].Name != "paid-twice" {
		t.Fatalf("Installed = %+v, err %v", list, err)
	}
	if list[0].Hash == "" {
		t.Fatal("installed entry carries no hash — provenance lost")
	}

	if err := Uninstall("paid-twice", abilities); err != nil {
		t.Fatalf("Uninstall: %v", err)
	}
	if list, _ := Installed(abilities); len(list) != 0 {
		t.Fatalf("still installed after uninstall: %+v", list)
	}
	ledger, _ := os.ReadFile(filepath.Join(abilities, "LEDGER.jsonl"))
	if !strings.Contains(string(ledger), "removed_at") {
		t.Fatal("the removal is not in the ledger — both halves of the story must be there")
	}
	// re-installable after removal: that IS the upgrade path
	if _, err := Install(dir, abilities, "catalogue:test", ""); err != nil {
		t.Fatalf("reinstall after uninstall must work: %v", err)
	}
}

func TestDigestMismatchRefusedAndDeleted(t *testing.T) {
	root := t.TempDir()
	archive, _, size := packBundle(t, root, "wrong")
	base := serveCatalogue(t, root, []Entry{{
		Name: "wrong", Proof: "prototype", Archive: "wrong.tar.gz",
		Digest: strings.Repeat("0", 64), Bytes: size,
	}})
	raw, _ := os.ReadFile(archive)
	_ = os.WriteFile(filepath.Join(base, "wrong.tar.gz"), raw, 0o644)

	f := NewFetcher(base)
	idx, err := f.Index()
	if err != nil {
		t.Fatal(err)
	}
	staging := filepath.Join(root, "staging")
	if _, err := f.Fetch(idx.Abilities[0], staging); err == nil {
		t.Fatal("a digest mismatch must refuse the download")
	}
	if _, err := os.Stat(filepath.Join(staging, "wrong.tar.gz")); !os.IsNotExist(err) {
		t.Fatal("the refused download must be deleted, not left lying about")
	}
}

func TestIndexRefusesEntryWithoutProofStatus(t *testing.T) {
	root := t.TempDir()
	base := serveCatalogue(t, root, []Entry{{
		Name: "quiet", Archive: "quiet.tar.gz", Digest: strings.Repeat("a", 64),
	}})
	if _, err := NewFetcher(base).Index(); err == nil {
		t.Fatal("an entry with no proof status must be refused — honesty is not optional")
	}
}

func TestUnrecordedInstallIsNamed(t *testing.T) {
	root := t.TempDir()
	abilities := filepath.Join(root, "abilities", "smuggled")
	if err := os.MkdirAll(abilities, 0o755); err != nil {
		t.Fatal(err)
	}
	list, err := Installed(filepath.Join(root, "abilities"))
	if err != nil || len(list) != 1 {
		t.Fatalf("Installed = %+v, err %v", list, err)
	}
	if !strings.Contains(list[0].Source, "unrecorded") {
		t.Fatalf("an ability with no ledger record must be NAMED as such, got %+v", list[0])
	}
}
