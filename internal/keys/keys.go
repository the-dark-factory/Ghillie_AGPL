// Package keys holds the claw's encryption identity — the X25519 keypair that
// confidential material is sealed to, kept separate from the Ed25519 device
// key on purpose: a signing key cannot be encrypted to, and conflating the two
// jobs in one key is how both get weakened.
//
// The private half lives beside the device key (0600, generated once). The
// public half — the age recipient string — is published, but never bare: it is
// SIGNED by the device key, so a factory encrypting a confidential delivery to
// this claw can prove the encryption key belongs to the enrolled device and
// not to an impostor who slipped their own recipient into the channel.
//
// The same machinery carries material the other way: the claw seals a
// customer's source (a COBOL program for confirmation, and nothing else, ever,
// without the owner's explicit say-so) to the FACTORY'S published recipient.
// Sending anything out is a separate, loud, owner-authorised act recorded
// before it happens; this package only supplies the sealing.
package keys

import (
	"bytes"
	"crypto/ed25519"
	"errors"
	"fmt"
	"io"
	"os"
	"strings"

	"filippo.io/age"
)

// ErrNoIdentity reports an unreadable or malformed encryption key file.
var ErrNoIdentity = errors.New("keys: no encryption identity")

// ErrBadBinding reports a recipient whose signature does not verify against
// the signing key the caller trusts (a claw's device key, or the factory's
// published signer) — either way, nothing is ever sealed to it.
var ErrBadBinding = errors.New("keys: recipient not signed by the trusted signing key")

// LoadOrCreate returns the claw's encryption identity, generating and keeping
// it (0600) on first use. created reports whether this call generated it, so
// the caller can log the one-time event the way the device key does.
func LoadOrCreate(path string) (id *age.X25519Identity, created bool, err error) {
	raw, err := os.ReadFile(path)
	switch {
	case err == nil:
		id, perr := age.ParseX25519Identity(strings.TrimSpace(string(raw)))
		if perr != nil {
			return nil, false, fmt.Errorf("%w: %s: %v", ErrNoIdentity, path, perr)
		}
		return id, false, nil
	case os.IsNotExist(err):
		id, gerr := age.GenerateX25519Identity()
		if gerr != nil {
			return nil, false, fmt.Errorf("keys: generate: %w", gerr)
		}
		if werr := os.WriteFile(path, []byte(id.String()+"\n"), 0o600); werr != nil {
			return nil, false, fmt.Errorf("keys: keep %s: %w", path, werr)
		}
		return id, true, nil
	default:
		return nil, false, fmt.Errorf("keys: read %s: %w", path, err)
	}
}

// Binding is a published encryption public key, bound to a device identity:
// the recipient string plus the device key's signature over its exact bytes.
type Binding struct {
	Recipient string `json:"encrypt_pubkey"`
	Signature []byte `json:"encrypt_pubkey_sig"`
}

// Bind signs the identity's recipient string with the device key, producing
// the publishable form.
func Bind(id *age.X25519Identity, deviceKey ed25519.PrivateKey) Binding {
	r := id.Recipient().String()
	return Binding{Recipient: r, Signature: ed25519.Sign(deviceKey, []byte(r))}
}

// VerifiedRecipient checks the binding against a device public key and, only
// on success, parses the recipient for sealing. Every seal-to-a-claw and every
// seal-to-the-factory goes through this — an unverified recipient string is
// never sealed to.
func VerifiedRecipient(b Binding, devicePub ed25519.PublicKey) (*age.X25519Recipient, error) {
	if !ed25519.Verify(devicePub, []byte(b.Recipient), b.Signature) {
		return nil, ErrBadBinding
	}
	r, err := age.ParseX25519Recipient(b.Recipient)
	if err != nil {
		return nil, fmt.Errorf("keys: recipient: %w", err)
	}
	return r, nil
}

// Seal encrypts plaintext to the recipient. The ciphertext opens only with the
// matching identity; the sealer needs no shared secret.
func Seal(plaintext []byte, to *age.X25519Recipient) ([]byte, error) {
	var buf bytes.Buffer
	w, err := age.Encrypt(&buf, to)
	if err != nil {
		return nil, fmt.Errorf("keys: seal: %w", err)
	}
	if _, err := w.Write(plaintext); err != nil {
		return nil, fmt.Errorf("keys: seal write: %w", err)
	}
	if err := w.Close(); err != nil {
		return nil, fmt.Errorf("keys: seal close: %w", err)
	}
	return buf.Bytes(), nil
}

// Open decrypts a sealed message with this claw's identity. A ciphertext
// sealed to a different key, or tampered with, refuses.
func Open(ciphertext []byte, id *age.X25519Identity) ([]byte, error) {
	r, err := age.Decrypt(bytes.NewReader(ciphertext), id)
	if err != nil {
		return nil, fmt.Errorf("keys: open: %w", err)
	}
	plain, err := io.ReadAll(r)
	if err != nil {
		return nil, fmt.Errorf("keys: open read: %w", err)
	}
	return plain, nil
}
