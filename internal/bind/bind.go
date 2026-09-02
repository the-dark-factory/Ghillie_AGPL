// Package bind is the claw side of BINDING THIS MACHINE TO A MEMBERSHIP.
//
// ⚠ THIS IS NOT internal/enrol, AND THE TWO MUST NOT BE CONFLATED. That
// package is the claw⇄facade ceremony (Claw_Enrolment_Pkg, ledger 113): who
// may submit this machine to a factory, and how it attests for a session
// afterwards. THIS package is a different joint entirely — it ties the device
// key to a CUSTOMER MEMBERSHIP at the portal, so the factory knows whose
// ghillie this is. A machine can be enrolled with a facade and bound to no
// membership, or the other way round. They are separate promises, kept
// separately, and the flags say so: -enrol against the facade, -enrol-code
// against the portal.
//
// # The ceremony, one round trip
//
//	the member, signed in at the portal, presses a button and is shown a
//	pairing code ONCE. They read it to this machine.
//
//	POST {portal}/enrol  {code, pubkey, sig}  → {verdict, admitted, ...}
//
// The signature is over the CANONICAL code (see Canonical), made with the
// device key. Nothing secret travels: the portal learns this machine's PUBLIC
// key and nothing else, and this machine sends no password, because there is
// none to send.
//
// # THE PORTAL DECIDES, AND THE WORD IT SENDS IS THE PROVEN CORE'S OWN
//
// The verdict comes from Enrolment_Admission_Pkg (ada-factory ledger 226)
// through the portal's thin front. This package renders it and NEVER
// second-guesses it: there is no local check here that anticipates a refusal,
// no retry that hopes for a different answer, and no path that treats a
// failure to reach the portal as any kind of yes. A refusal is shown with the
// core's own word alongside the plain-English sentence, so a member can quote
// the exact term when asking why.
package bind

import (
	"bytes"
	"context"
	"crypto/ed25519"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

// DefaultPortal is the Dark Factory's customer area. A member who runs
// nothing but `ghillie -enrol-code <code>` reaches the right door.
const DefaultPortal = "https://customerarea.thedarkfactory.co.uk"

// codeAlphabet MUST match the portal's binding package exactly: the
// signature is over the canonical form, so a disagreement about which
// characters count would make every signature fail to verify, and the
// member would be told their key was refused when the two sides simply
// could not agree on what they were signing.
const codeAlphabet = "23456789ABCDEFGHJKMNPQRSTVWXYZ"

// maxBody bounds the portal's reply. The lawful reply is a few hundred
// bytes.
const maxBody = 64 << 10

// Named errors. None of them is ever a yes.
var (
	// ErrNoCode reports an empty or unreadable pairing code.
	ErrNoCode = errors.New("bind: that is not a pairing code")
	// ErrNoKey reports a missing or malformed device key.
	ErrNoKey = errors.New("bind: no device key")
	// ErrPortal reports that the portal could not be reached or would
	// not answer in a shape this client understands. A door that did
	// not answer has not admitted anything.
	ErrPortal = errors.New("bind: the portal did not answer")
	// ErrRefused reports a REFUSAL the proven core actually made — as
	// distinct from ErrPortal, which is the absence of an answer.
	ErrRefused = errors.New("bind: refused")
)

// Canonical reduces a pairing code as the member typed it to the form
// that is signed: upper case, with every character outside the alphabet
// dropped. It is the twin of the portal's binding.Canonical, and the
// shared-contract table in bind_test.go is duplicated there.
func Canonical(code string) string {
	var b strings.Builder
	for _, r := range strings.ToUpper(code) {
		if strings.ContainsRune(codeAlphabet, r) {
			b.WriteRune(r)
		}
	}
	return b.String()
}

// Fingerprint renders a public key as the short string the member sees on
// their account page: the first 16 bytes of SHA-256 over the raw key,
// hex, in four-character groups.
//
// ★ THE PORTAL COMPUTES THE SAME STRING (internal/binding). The two live
// in separate repositories and cannot share a test, so each holds the
// SAME GOLDEN VECTOR: drift on either side fails a test rather than
// showing a member two different names for one key.
func Fingerprint(pub ed25519.PublicKey) string {
	sum := sha256.Sum256(pub)
	encoded := hex.EncodeToString(sum[:8])
	var b strings.Builder
	for i := 0; i < len(encoded); i += 4 {
		if i > 0 {
			b.WriteByte('-')
		}
		b.WriteString(encoded[i : i+4])
	}
	return b.String()
}

// Sign signs the canonical form of the pairing code with the device key.
func Sign(key ed25519.PrivateKey, code string) ([]byte, error) {
	if len(key) != ed25519.PrivateKeySize {
		return nil, fmt.Errorf("%w: got %d bytes, want %d", ErrNoKey, len(key), ed25519.PrivateKeySize)
	}
	canonical := Canonical(code)
	if canonical == "" {
		return nil, fmt.Errorf("%w: %q reduces to nothing", ErrNoCode, code)
	}
	return ed25519.Sign(key, []byte(canonical)), nil
}

// Verify checks a signature the way the portal will. It exists so the
// round trip can be proved on this side of the wire, without a portal.
func Verify(pub ed25519.PublicKey, code string, sig []byte) bool {
	if len(pub) != ed25519.PublicKeySize {
		return false
	}
	return ed25519.Verify(pub, []byte(Canonical(code)), sig)
}

// Result is the portal's answer, carried whole.
type Result struct {
	// Verdict is the PROVEN CORE'S OWN WORD — "admit" or one of the six
	// refusals — passed through unedited.
	Verdict string `json:"verdict"`
	// Admitted is true only when the core admitted.
	Admitted bool `json:"admitted"`
	// Fingerprint names the now-bound key; present only on admission.
	Fingerprint string `json:"fingerprint,omitempty"`
	// AccountID names the membership; present only on admission.
	AccountID string `json:"account_id,omitempty"`
	// Error is the portal's plain-words reason when NO VERDICT was
	// obtained — a different thing from a refusal, and it says so.
	Error string `json:"error,omitempty"`
}

// offer is the wire shape sent to the portal.
type offer struct {
	Code   string `json:"code"`
	PubKey string `json:"pubkey"`
	Sig    string `json:"sig"`
}

// Client offers this machine's key to one portal.
type Client struct {
	// Base is the portal's origin, e.g. DefaultPortal.
	Base string
	// HTTP is the transport; nil takes a 30-second default.
	HTTP *http.Client
}

// Offer signs the pairing code and presents it. A nil error means the
// portal ANSWERED — which may still be a refusal, carried in the Result.
// A non-nil error means no verdict was obtained at all, and the caller
// must say so rather than implying anything about the key.
func (c Client) Offer(ctx context.Context, code string, key ed25519.PrivateKey) (Result, error) {
	sig, err := Sign(key, code)
	if err != nil {
		return Result{}, err
	}
	pub, ok := key.Public().(ed25519.PublicKey)
	if !ok {
		return Result{}, ErrNoKey
	}
	body, err := json.Marshal(offer{
		Code:   Canonical(code),
		PubKey: hex.EncodeToString(pub),
		Sig:    hex.EncodeToString(sig),
	})
	if err != nil {
		return Result{}, fmt.Errorf("bind: encode offer: %w", err)
	}

	url := strings.TrimRight(c.Base, "/") + "/enrol"
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(body))
	if err != nil {
		return Result{}, fmt.Errorf("bind: build request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json")

	client := c.HTTP
	if client == nil {
		client = &http.Client{Timeout: 30 * time.Second}
	}
	resp, err := client.Do(req)
	if err != nil {
		return Result{}, fmt.Errorf("%w: %s: %w", ErrPortal, url, err)
	}
	defer func() {
		if cerr := resp.Body.Close(); cerr != nil {
			// Nothing useful remains to do; the verdict is already read.
			_ = cerr
		}
	}()

	raw, err := io.ReadAll(io.LimitReader(resp.Body, maxBody))
	if err != nil {
		return Result{}, fmt.Errorf("%w: reading the reply failed: %w", ErrPortal, err)
	}
	var result Result
	if jerr := json.Unmarshal(raw, &result); jerr != nil {
		return Result{}, fmt.Errorf("%w: HTTP %d, and the reply is not the shape this door speaks: %s",
			ErrPortal, resp.StatusCode, strings.TrimSpace(string(raw)))
	}
	// A reply carrying neither a verdict nor a reason is not an answer,
	// whatever its status code.
	if result.Verdict == "" && result.Error == "" {
		return Result{}, fmt.Errorf("%w: HTTP %d with neither a verdict nor a reason", ErrPortal, resp.StatusCode)
	}
	// No verdict, but the portal said why: carry the reason as the error,
	// because nothing was decided about this key.
	if result.Verdict == "" {
		return Result{}, fmt.Errorf("%w: %s", ErrPortal, result.Error)
	}
	return result, nil
}

// explanations turns the proven core's verdict word into a sentence for
// somebody standing at a terminal. The WORD is always shown as well: it
// is the theorem's name, and a member quoting it is quoting the core.
var explanations = map[string]string{
	"refuse_member_unknown": "that code does not match one we issued — check for a typo, or press the button again on your account page for a fresh one.",
	"refuse_signature_failed": "the signature over the code did not check out against this machine's key. " +
		"That usually means the key file changed under you; a machine that lost its device key is a new machine as far as the factory is concerned.",
	"refuse_code_spent":   "that code has already been used once. Codes are one-shot on purpose — press the button again for a fresh one.",
	"refuse_code_expired": "that code ran out. They last ten minutes; press the button again and come straight here.",
	"refuse_code_unknown": "no such code was ever issued.",
	"refuse_key_bound": "this machine's key is already bound. Binding means a NEW binding, so a second one refuses rather than " +
		"quietly moving it — if you meant to move it, unbind on the account page first.",
}

// Explain returns the plain-English sentence for a verdict word, or a
// truthful admission of ignorance for a word this build does not know.
// An unknown word is never smoothed over: a newer portal saying something
// this binary has never heard of is exactly when a member needs to see
// the raw term.
func Explain(verdict string) string {
	if sentence, ok := explanations[verdict]; ok {
		return sentence
	}
	return "this build does not have a sentence for that verdict — the word above is the factory's own, and it is the thing to quote when you ask why."
}
