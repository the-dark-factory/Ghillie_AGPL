package bundle

// catalogue.go — THE CATALOGUE, client side: what abilities exist, fetching
// one, and taking one back off.
//
// ★ THE CATALOGUE'S PRIMARY CONSUMER IS GHILLIE, NOT A HUMAN (canon): the
// index is machine-legible so the assistant can know what exists, which one
// fits, and when to offer it. There is no shop window; the person talks to
// their ghillie.
//
// ★ FETCHING IS NOT INSTALLING. Fetch pulls bytes to a staging directory and
// verifies the publisher's digest; INSTALL is still the owner's separate act
// (Install, in bundle.go). A catalogue that could install would be a remote
// party changing what a ghillie can do, which is precisely what the whole
// design refuses.
//
// ★ THE INDEX CARRIES NO AUTHORITY. It is decoded strictly, bounded, and
// every entry's digest is checked against the bytes actually received. An
// index that says "trust me" gets nothing; the digest is the only claim that
// matters, and a mismatch refuses the download whole.

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// maxIndexBytes bounds a fetched index. It is a list of abilities, not a feed.
const maxIndexBytes = 1 << 20

// Entry is one ability as the catalogue describes it. Machine-legible first:
// ghillie reads Offer/Needs to decide whether an ability FITS a moment, and
// Proof to be honest about what its guarantee covers.
type Entry struct {
	Name    string `json:"name"`    // the install name; also its tab
	Summary string `json:"summary"` // one line, for a person
	Offer   string `json:"offer"`   // when ghillie should offer it, in his own terms
	Needs   string `json:"needs"`   // what it needs FROM the person (data, a file, consent)
	Proof   string `json:"proof"`   // HONEST proof status: "factory-proved" | "prototype" | …
	Cost    string `json:"cost"`    // what it costs, as a single figure or "free"
	Archive string `json:"archive"` // path or URL, relative to the index's base
	Digest  string `json:"digest"`  // sha256 of the archive bytes, hex
	// Delivery marks how the archive travels: "" (plain tar.gz) or
	// "encrypted" (the same tar.gz sealed to THIS claw's published
	// encryption key — the confidential tier; opens nowhere else).
	Delivery string `json:"delivery,omitempty"`
	Bytes    int64  `json:"bytes"`
}

// Index is a catalogue document.
type Index struct {
	Catalogue string  `json:"catalogue"` // a name for the source, shown to the person
	Published string  `json:"published"` // RFC3339
	Abilities []Entry `json:"abilities"`
}

// Fetcher reads a catalogue and pulls archives from it. Base may be an https
// URL or a local directory (the local case is how the estate's own catalogue
// is served today — the shape is identical, so nothing changes when a URL
// replaces it).
type Fetcher struct {
	Base   string
	Client *http.Client
}

// NewFetcher builds a fetcher with an honest timeout.
func NewFetcher(base string) *Fetcher {
	return &Fetcher{Base: strings.TrimRight(base, "/"),
		Client: &http.Client{Timeout: 60 * time.Second}}
}

// Index fetches and strictly decodes the catalogue index. Unknown fields are
// refused: a catalogue cannot smuggle a field this client has no place for.
func (f *Fetcher) Index() (Index, error) {
	r, closer, err := f.open("index.json")
	if err != nil {
		return Index{}, err
	}
	defer closer()
	dec := json.NewDecoder(io.LimitReader(r, maxIndexBytes+1))
	dec.DisallowUnknownFields()
	var idx Index
	if err := dec.Decode(&idx); err != nil {
		return Index{}, fmt.Errorf("catalogue: index does not decode: %w", err)
	}
	for i, e := range idx.Abilities {
		if e.Name == "" || e.Archive == "" || e.Digest == "" {
			return Index{}, fmt.Errorf("catalogue: entry %d lacks name, archive or digest — an entry without a digest is an unverifiable claim", i)
		}
		if strings.ContainsAny(e.Name, "/\\.") {
			return Index{}, fmt.Errorf("catalogue: entry name %q is not a plain name", e.Name)
		}
		if e.Proof == "" {
			return Index{}, fmt.Errorf("catalogue: %s states no proof status — honesty about the guarantee is not optional", e.Name)
		}
	}
	return idx, nil
}

// Fetch pulls one ability's archive into stagingDir and verifies its digest
// against the index. It returns the archive path; INSTALLING it remains the
// owner's separate act.
func (f *Fetcher) Fetch(e Entry, stagingDir string) (archivePath string, err error) {
	if err := os.MkdirAll(stagingDir, 0o755); err != nil {
		return "", fmt.Errorf("catalogue: %w", err)
	}
	r, closer, err := f.open(e.Archive)
	if err != nil {
		return "", err
	}
	defer closer()

	archivePath = filepath.Join(stagingDir, e.Name+".tar.gz")
	out, err := os.OpenFile(archivePath, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o600)
	if err != nil {
		return "", fmt.Errorf("catalogue: %w", err)
	}
	h := sha256.New()
	n, copyErr := io.Copy(io.MultiWriter(out, h), io.LimitReader(r, maxBundleBytes+1))
	closeErr := out.Close()
	if copyErr != nil {
		_ = os.Remove(archivePath)
		return "", fmt.Errorf("catalogue: fetching %s: %w", e.Name, copyErr)
	}
	if closeErr != nil {
		_ = os.Remove(archivePath)
		return "", fmt.Errorf("catalogue: %w", closeErr)
	}
	if n > maxBundleBytes {
		_ = os.Remove(archivePath)
		return "", fmt.Errorf("catalogue: %s exceeds the %d-byte bound", e.Name, int64(maxBundleBytes))
	}
	got := hex.EncodeToString(h.Sum(nil))
	if got != e.Digest {
		_ = os.Remove(archivePath)
		return "", fmt.Errorf("catalogue: %s DIGEST MISMATCH — index says %s, the bytes are %s; refused whole and deleted", e.Name, e.Digest, got)
	}
	if e.Bytes != 0 && n != e.Bytes {
		_ = os.Remove(archivePath)
		return "", fmt.Errorf("catalogue: %s is %d bytes, the index says %d — refused", e.Name, n, e.Bytes)
	}
	return archivePath, nil
}

// open resolves a catalogue-relative path for reading, over https or from a
// local directory. A path that tries to escape the base is refused.
func (f *Fetcher) open(rel string) (io.Reader, func(), error) {
	if strings.Contains(rel, "..") {
		return nil, nil, fmt.Errorf("catalogue: path %q escapes the catalogue", rel)
	}
	// H3 (security review 2026-08-07): plain http was accepted, so the index —
	// which carries the digest, the cost, and the word "proved" that the owner
	// reads before consenting — could be rewritten in flight by anyone on the
	// path. The digest check below only proves the archive matches the index;
	// it cannot notice that the INDEX itself was replaced. TLS is the minimum
	// that makes the digest mean anything at all.
	if strings.HasPrefix(f.Base, "http://") {
		return nil, nil, fmt.Errorf("catalogue: %q is plain http — refused. "+
			"The index carries the digest and the proof claim the owner consents to; "+
			"over http both can be rewritten in flight. Use https, or a local directory", f.Base)
	}
	if strings.HasPrefix(f.Base, "https://") {
		u, err := url.JoinPath(f.Base, rel)
		if err != nil {
			return nil, nil, fmt.Errorf("catalogue: %w", err)
		}
		resp, err := f.Client.Get(u)
		if err != nil {
			return nil, nil, fmt.Errorf("catalogue: not reachable")
		}
		if resp.StatusCode != http.StatusOK {
			resp.Body.Close()
			return nil, nil, fmt.Errorf("catalogue: %s answered HTTP %d", rel, resp.StatusCode)
		}
		return resp.Body, func() { resp.Body.Close() }, nil
	}
	fh, err := os.Open(filepath.Join(f.Base, rel))
	if err != nil {
		return nil, nil, fmt.Errorf("catalogue: %w", err)
	}
	return fh, func() { fh.Close() }, nil
}

// Installed lists the abilities installed in abilitiesDir, newest ledger
// record first where one exists. The DIRECTORIES are the truth (a display
// reads them); the ledger adds provenance.
func Installed(abilitiesDir string) ([]LedgerEntry, error) {
	entries, err := os.ReadDir(abilitiesDir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}
	recorded := map[string]LedgerEntry{}
	if raw, rerr := os.ReadFile(filepath.Join(abilitiesDir, "LEDGER.jsonl")); rerr == nil {
		for _, line := range strings.Split(string(raw), "\n") {
			if strings.TrimSpace(line) == "" {
				continue
			}
			var le LedgerEntry
			if json.Unmarshal([]byte(line), &le) == nil {
				recorded[le.Name] = le // last record wins: the current install
			}
		}
	}
	var out []LedgerEntry
	for _, e := range entries {
		if !e.IsDir() || strings.HasPrefix(e.Name(), ".") {
			continue
		}
		if le, ok := recorded[e.Name()]; ok {
			out = append(out, le)
			continue
		}
		// On disk but not in the ledger: SAY SO rather than hide it.
		out = append(out, LedgerEntry{Name: e.Name(), Source: "unrecorded — installed outside the ledger"})
	}
	return out, nil
}

// Uninstall removes an installed ability and records the removal. It is the
// OWNER'S act, like installing; and it is how an upgrade happens — remove
// visibly, then install, so the ledger holds both halves of the story.
func Uninstall(name, abilitiesDir string) error {
	if name == "" || strings.ContainsAny(name, "/\\") || strings.HasPrefix(name, ".") {
		return fmt.Errorf("bundle: %q is not an installed name", name)
	}
	dest := filepath.Join(abilitiesDir, name)
	info, err := os.Stat(dest)
	if err != nil || !info.IsDir() {
		return fmt.Errorf("bundle: %s is not installed", name)
	}
	if err := os.RemoveAll(dest); err != nil {
		return fmt.Errorf("bundle: removing %s: %w", name, err)
	}
	raw, _ := json.Marshal(map[string]string{
		"name": name, "removed_at": time.Now().UTC().Format(time.RFC3339),
	})
	ledger, err := os.OpenFile(filepath.Join(abilitiesDir, "LEDGER.jsonl"),
		os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o600)
	if err != nil {
		// The ability IS gone; the record failing is worth saying, not hiding.
		return fmt.Errorf("bundle: %s removed but the ledger would not open — record it by hand: %w", name, err)
	}
	defer ledger.Close()
	_, err = fmt.Fprintf(ledger, "%s\n", raw)
	return err
}
