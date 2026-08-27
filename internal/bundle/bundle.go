// Package bundle is the ability-bundle seam: what a downloadable skill IS,
// how it is verified, and how an OWNER installs one.
//
// A bundle is the five-part convention the estate already keeps (the tmux
// tab display and the GUI tabs both read it):
//
//	human.md        instructions for the human — becomes the ability's tab
//	ghillie.md      instructions for ghillie — how the assistant offers it
//	provenance.md   where this came from, and its HONEST proof status
//	core/           the proven core, or its attestation while unproven
//	surface/        the working surface (non-visual path required)
//
// ★ INSTALLING IS AN OWNER ACT, ALWAYS. Nothing installs a bundle but the
// owner running the install command by hand — not a poll, not a note, not a
// download. Download and install are two separate events with the owner
// between them, exactly as claiming and possessing are separated at the
// factory. The destination pipeline (abilities arriving factory-proved
// through quarantine and admission) supersedes this by ADDING gates, never
// by removing the owner.
//
// ★ HONESTY TRAVELS IN provenance.md. A bundle whose core is not yet
// factory-proved must say so there in plain words; the installer refuses a
// bundle that omits the file, and the ledger records the provenance hash so
// what was installed can always be answered exactly.
package bundle

import (
	"archive/tar"
	"compress/gzip"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

// required is the five-part contract. surface/ and core/ are directories;
// the three documents are files at the bundle root.
var requiredFiles = []string{"human.md", "ghillie.md", "provenance.md"}
var requiredDirs = []string{"core", "surface"}

// maxBundleBytes bounds an unpacked bundle — a skill is a skill, not a
// payload dump.
const maxBundleBytes = 64 << 20

// unpackFileMode is the mode every file written out of a bundle receives.
//
// Owner-readable and writable, and NEVER executable, whatever the archive
// asked for. This is the third link of the H3 chain removed: an unsigned index
// can still lie about what a bundle is, but it can no longer arrange for the
// bytes it delivered to be executed by something downstream.
const unpackFileMode os.FileMode = 0o600

// Verify checks a bundle DIRECTORY against the five-part contract and
// returns its content hash (sha256 over every file, path-sorted). It never
// executes anything.
func Verify(dir string) (hash string, err error) {
	for _, f := range requiredFiles {
		info, statErr := os.Stat(filepath.Join(dir, f))
		if statErr != nil || info.IsDir() {
			return "", fmt.Errorf("bundle: %s lacks %s — the five-part contract is the contract; an incomplete bundle installs nowhere", dir, f)
		}
		if info.Size() == 0 {
			return "", fmt.Errorf("bundle: %s is EMPTY in %s — a blank document is a lie of omission, not a document", f, dir)
		}
	}
	for _, d := range requiredDirs {
		info, statErr := os.Stat(filepath.Join(dir, d))
		if statErr != nil || !info.IsDir() {
			return "", fmt.Errorf("bundle: %s lacks %s/ — the five-part contract is the contract", dir, d)
		}
	}

	h := sha256.New()
	var files []string
	var total int64
	walkErr := filepath.Walk(dir, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		if info.Mode().IsRegular() {
			files = append(files, path)
			total += info.Size()
		}
		return nil
	})
	if walkErr != nil {
		return "", fmt.Errorf("bundle: walking %s: %w", dir, walkErr)
	}
	if total > maxBundleBytes {
		return "", fmt.Errorf("bundle: %s is %d bytes — over the %d bound; a skill is a skill, not a payload dump", dir, total, int64(maxBundleBytes))
	}
	sort.Strings(files)
	for _, f := range files {
		rel, _ := filepath.Rel(dir, f)
		raw, readErr := os.ReadFile(f)
		if readErr != nil {
			return "", fmt.Errorf("bundle: reading %s: %w", f, readErr)
		}
		fmt.Fprintf(h, "%s\x00", rel)
		h.Write(raw)
	}
	return hex.EncodeToString(h.Sum(nil)), nil
}

// LedgerEntry is one installed bundle's record: what, when, and exactly
// which bytes.
type LedgerEntry struct {
	Name        string `json:"name"`
	Hash        string `json:"hash"`
	InstalledAt string `json:"installed_at"`
	Source      string `json:"source"`
	// Proof records the rung this install used, in plain words: ProofReproved,
	// ProofCarried, or empty for a bundle that claims no proof. Never silently
	// upgraded — what the ledger says is what happened here.
	Proof string `json:"proof,omitempty"`
}

// Install verifies a bundle directory and copies it into
// <abilitiesDir>/<name>, appending the provenance record to
// <abilitiesDir>/LEDGER.jsonl. It refuses to overwrite an installed ability
// — an upgrade is a REMOVE (visible) followed by an install, never a silent
// replacement.
func Install(bundleDir, abilitiesDir, source, proofNote string) (entry LedgerEntry, err error) {
	name := filepath.Base(filepath.Clean(bundleDir))
	if name == "" || name == "." || name == "/" || strings.HasPrefix(name, ".") {
		return LedgerEntry{}, fmt.Errorf("bundle: %q is not an installable name", name)
	}
	hash, err := Verify(bundleDir)
	if err != nil {
		return LedgerEntry{}, err
	}
	dest := filepath.Join(abilitiesDir, name)
	if _, statErr := os.Stat(dest); statErr == nil {
		return LedgerEntry{}, fmt.Errorf("bundle: %s is already installed — remove it first; an upgrade is a visible remove-then-install, never a silent replacement", name)
	}
	if err := copyTree(bundleDir, dest); err != nil {
		_ = os.RemoveAll(dest) // half an ability is worse than none
		return LedgerEntry{}, fmt.Errorf("bundle: installing %s: %w", name, err)
	}

	entry = LedgerEntry{
		Name: name, Hash: hash,
		InstalledAt: time.Now().UTC().Format(time.RFC3339),
		Source:      source,
		Proof:       proofNote,
	}
	raw, _ := json.Marshal(entry)
	ledger, err := os.OpenFile(filepath.Join(abilitiesDir, "LEDGER.jsonl"),
		os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o600)
	if err != nil {
		_ = os.RemoveAll(dest) // no record, no ability — the ledger IS the install
		return LedgerEntry{}, fmt.Errorf("bundle: the ledger would not open — refusing to install unrecorded: %w", err)
	}
	defer ledger.Close()
	if _, err := fmt.Fprintf(ledger, "%s\n", raw); err != nil {
		_ = os.RemoveAll(dest)
		return LedgerEntry{}, fmt.Errorf("bundle: the ledger would not take the record — refusing to install unrecorded: %w", err)
	}
	return entry, nil
}

// Unpack extracts a .tar.gz bundle DOWNLOAD into destDir and returns the
// bundle directory within it. Paths are confined; links are refused —
// a downloaded archive is untrusted bytes until Verify passes.
func Unpack(archivePath, destDir string) (bundleDir string, err error) {
	f, err := os.Open(archivePath)
	if err != nil {
		return "", fmt.Errorf("bundle: %w", err)
	}
	defer f.Close()
	gz, err := gzip.NewReader(f)
	if err != nil {
		return "", fmt.Errorf("bundle: %s is not a gzip archive: %w", archivePath, err)
	}
	defer gz.Close()
	tr := tar.NewReader(gz)
	var root string
	var total int64
	for {
		hdr, nextErr := tr.Next()
		if nextErr == io.EOF {
			break
		}
		if nextErr != nil {
			return "", fmt.Errorf("bundle: reading archive: %w", nextErr)
		}
		clean := filepath.Clean(hdr.Name)
		if strings.HasPrefix(clean, "..") || filepath.IsAbs(clean) {
			return "", fmt.Errorf("bundle: archive path %q escapes — refused whole", hdr.Name)
		}
		parts := strings.SplitN(clean, string(filepath.Separator), 2)
		if root == "" {
			root = parts[0]
		} else if parts[0] != root {
			return "", fmt.Errorf("bundle: archive holds more than one root (%s, %s) — one bundle per download", root, parts[0])
		}
		target := filepath.Join(destDir, clean)
		switch hdr.Typeflag {
		case tar.TypeDir:
			if err := os.MkdirAll(target, 0o755); err != nil {
				return "", err
			}
		case tar.TypeReg:
			total += hdr.Size
			if total > maxBundleBytes {
				return "", fmt.Errorf("bundle: archive exceeds the %d-byte bound", int64(maxBundleBytes))
			}
			if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
				return "", err
			}
			// H3 (security review 2026-08-07): the archive's own mode was
			// honoured, so a tar header — attacker-authored, and at this point
			// attested by nothing but the index that pointed at it — could mark
			// any file EXECUTABLE. Combined with an out-of-repo executor (the
			// tmux display runs tab.sh), that completed a chain from "unsigned
			// index" to "code runs". The unpacked bytes are DATA here: written
			// non-executable, always. A surface that must run is made runnable
			// by the thing that runs it, deliberately, not by a bit an archive
			// asked for.
			w, createErr := os.OpenFile(target, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, unpackFileMode)
			if createErr != nil {
				return "", createErr
			}
			if _, err := io.Copy(w, io.LimitReader(tr, maxBundleBytes)); err != nil {
				w.Close()
				return "", err
			}
			w.Close()
		default:
			return "", fmt.Errorf("bundle: archive entry %q is a %c (link/device) — refused whole; a bundle is files and directories only", hdr.Name, hdr.Typeflag)
		}
	}
	if root == "" {
		return "", fmt.Errorf("bundle: archive is empty")
	}
	return filepath.Join(destDir, root), nil
}

// copyTree copies files and directories, nothing else.
func copyTree(src, dest string) error {
	return filepath.Walk(src, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		rel, _ := filepath.Rel(src, path)
		target := filepath.Join(dest, rel)
		if info.IsDir() {
			return os.MkdirAll(target, 0o755)
		}
		if !info.Mode().IsRegular() {
			return fmt.Errorf("%s is not a regular file", path)
		}
		raw, readErr := os.ReadFile(path)
		if readErr != nil {
			return readErr
		}
		// Same rule on the copy path as on the unpack path: never carry an
		// executable bit across a trust boundary (see unpackFileMode).
		return os.WriteFile(target, raw, unpackFileMode)
	})
}
