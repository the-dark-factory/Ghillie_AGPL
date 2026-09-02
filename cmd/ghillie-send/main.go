// Command ghillie-send is the LOUD EXCEPTION to the claw's no-egress posture:
// the one tool that seals a customer's own material (a COBOL program for
// confirmation, and nothing the owner did not name) to the factory's
// published encryption key, for the owner to transport.
//
// Three promises, enforced here rather than in prose:
//
//  1. CONSENT IS EXPLICIT. Without -i-authorise-this-to-leave the tool
//     refuses. There is no environment variable, no config default, no quiet
//     path. Sending source out is a decision, made per invocation.
//  2. THE MANIFEST COMES FIRST. Before one byte is sealed, an append-only
//     local journal records what is leaving (name, size, digest), sealed to
//     whom, when, at whose command. If the journal cannot be written, nothing
//     is sealed. The customer's own auditors read this file.
//  3. THE RECIPIENT IS VERIFIED. The factory's recipient string must carry a
//     signature that verifies against the signing key this claw already
//     trusts (the facade key pinned at enrolment). An unsigned or missigned
//     recipient is refused — nobody is tricked into sealing source to an
//     impostor.
//
// The reply comes back sealed to THIS claw: -open decrypts it with the
// encryption identity kept beside the device key.
package main

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"crypto/ed25519"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/tonygair/ghillie/internal/keys"
)

func main() {
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, "ghillie-send:", err)
		os.Exit(1)
	}
}

type manifestEntry struct {
	At       string         `json:"at"`
	SealedTo string         `json:"sealed_to"`
	Files    []manifestFile `json:"files"`
	Out      string         `json:"out"`
	Note     string         `json:"note"`
}

type manifestFile struct {
	Name   string `json:"name"`
	Bytes  int64  `json:"bytes"`
	SHA256 string `json:"sha256"`
}

func run() error {
	var (
		authorise = flag.Bool("i-authorise-this-to-leave", false, "the owner's explicit, per-invocation consent for the named files to leave this machine — nothing is sealed without it")
		recipient = flag.String("factory-recipient", "", "the factory's published age recipient")
		recSig    = flag.String("recipient-sig", "", "hex Ed25519 signature over the recipient string, made by the signing key this claw trusts")
		signerPub = flag.String("signer-pub", "", "hex Ed25519 public key the signature must verify against (the facade key pinned at enrolment)")
		manifest  = flag.String("manifest", "", "the append-only egress journal (default: ghillie-egress.jsonl beside -encrypt-key-file)")
		encKey    = flag.String("encrypt-key-file", "ghillie-encrypt.key", "this claw's encryption identity, for -open")
		out       = flag.String("out", "", "where the sealed submission (or opened reply) is written")
		note      = flag.String("note", "", "why this is leaving, in the owner's words — journalled with the manifest")
		open      = flag.String("open", "", "instead of sending: open a reply sealed to this claw")
	)
	flag.Parse()

	if *open != "" {
		if *out == "" {
			return fmt.Errorf("-open needs -out")
		}
		id, _, err := keys.LoadOrCreate(*encKey)
		if err != nil {
			return err
		}
		sealed, err := os.ReadFile(*open)
		if err != nil {
			return err
		}
		plain, err := keys.Open(sealed, id)
		if err != nil {
			return err
		}
		if err := os.WriteFile(*out, plain, 0o600); err != nil {
			return err
		}
		fmt.Fprintf(os.Stderr, "opened %s -> %s (%d bytes)\n", *open, *out, len(plain))
		return nil
	}

	files := flag.Args()
	if len(files) == 0 || *recipient == "" || *recSig == "" || *signerPub == "" || *out == "" {
		return fmt.Errorf("sending needs files plus -factory-recipient, -recipient-sig, -signer-pub and -out (and your explicit -i-authorise-this-to-leave)")
	}
	if !*authorise {
		return fmt.Errorf("REFUSED: sending source off this machine requires -i-authorise-this-to-leave, given by you, per invocation — there is no default that sends")
	}

	pub, err := hex.DecodeString(*signerPub)
	if err != nil || len(pub) != ed25519.PublicKeySize {
		return fmt.Errorf("-signer-pub must be %d hex-encoded bytes", ed25519.PublicKeySize)
	}
	sig, err := hex.DecodeString(*recSig)
	if err != nil {
		return fmt.Errorf("-recipient-sig: %w", err)
	}
	to, err := keys.VerifiedRecipient(keys.Binding{Recipient: *recipient, Signature: sig}, ed25519.PublicKey(pub))
	if err != nil {
		return fmt.Errorf("REFUSED to seal: %w", err)
	}

	// The manifest is written — and flushed — before anything is sealed.
	entry := manifestEntry{
		At:       time.Now().UTC().Format(time.RFC3339),
		SealedTo: *recipient,
		Out:      *out,
		Note:     *note,
	}
	var tarBuf bytes.Buffer
	gz := gzip.NewWriter(&tarBuf)
	tw := tar.NewWriter(gz)
	for _, name := range files {
		raw, err := os.ReadFile(name)
		if err != nil {
			return err
		}
		sum := sha256.Sum256(raw)
		entry.Files = append(entry.Files, manifestFile{
			Name: filepath.Base(name), Bytes: int64(len(raw)), SHA256: hex.EncodeToString(sum[:]),
		})
		if err := tw.WriteHeader(&tar.Header{Name: filepath.Base(name), Mode: 0o600, Size: int64(len(raw)), Typeflag: tar.TypeReg}); err != nil {
			return err
		}
		if _, err := tw.Write(raw); err != nil {
			return err
		}
	}
	if err := tw.Close(); err != nil {
		return err
	}
	if err := gz.Close(); err != nil {
		return err
	}
	mpath := *manifest
	if mpath == "" {
		mpath = filepath.Join(filepath.Dir(*encKey), "ghillie-egress.jsonl")
	}
	line, err := json.Marshal(entry)
	if err != nil {
		return err
	}
	mf, err := os.OpenFile(mpath, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o600)
	if err != nil {
		return fmt.Errorf("cannot journal the egress — so nothing leaves: %w", err)
	}
	if _, err := mf.Write(append(line, '\n')); err != nil {
		mf.Close()
		return fmt.Errorf("cannot journal the egress — so nothing leaves: %w", err)
	}
	if err := mf.Close(); err != nil {
		return fmt.Errorf("cannot journal the egress — so nothing leaves: %w", err)
	}

	sealed, err := keys.Seal(tarBuf.Bytes(), to)
	if err != nil {
		return err
	}
	if err := os.WriteFile(*out, sealed, 0o600); err != nil {
		return err
	}
	fmt.Fprintf(os.Stderr, "journalled to %s, then sealed %d file(s) -> %s (%d bytes) for %s\n",
		mpath, len(entry.Files), *out, len(sealed), *recipient)
	return nil
}
