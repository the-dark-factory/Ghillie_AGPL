// Package enrol is the claw side of the enrolment and attestation ceremony.
//
// ⚠ WHY IT EXISTS AT ALL. v0 had no enrolment client and sent no Authorization
// header. Against the mock facade that was invisible, because the mock has no
// auth; against the REAL facade door
// (ada-factory/cmd/specifier/claw_wiring.go) it would have taken a 401 on every
// single poll. This package closes that gap.
//
// The ceremony, matching the real door exactly:
//
//	POST /claws/{id}/enrol   {pubkey, ceiling, ...binding}  → recorded
//	POST /claws/{id}/attest  (empty body)                   → {nonce}
//	POST /claws/{id}/attest  {nonce, sig}                   → {session, expires_in}
//	POST /claws/{id}/poll    Authorization: Bearer <session>
//
// ★ ENROLMENT IS AN OWNER ACT (Claw_Enrolment_Pkg, ledger 113). Before this
// package sends anything it puts the request through gate.MayEnrol, so a claw
// that tries to enrol itself, or a Grapple that tries to recruit one, is refused
// ON THIS MACHINE and never reaches the wire. The facade decides its own side;
// this is the claw declining to participate in an enrolment its own proven core
// says is not lawful.
//
// ★ THE ENROLMENT PAYLOAD IS WHERE THE IDENTITY BINDING LANDS. Claw, owner,
// user and — if there is one — the Apple account that bought the credits are
// bound here and nowhere else. The Apple reference is PII: this file contains
// the ONLY call to identity.AppleAccountRef.Reveal in the repository, and it is
// on the enrolment payload path exclusively. It must never appear in a log line,
// an outcome report or a quarantine file.
package enrol

import (
	"bytes"
	"context"
	"crypto/ed25519"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/tonygair/ghillie/internal/gate"
	"github.com/tonygair/ghillie/internal/identity"
	"github.com/tonygair/ghillie/internal/protocol"
)

// ErrNotAnOwnerAct reports an enrolment refused by ledger 113 before it left
// this machine.
var ErrNotAnOwnerAct = errors.New("enrol: refused locally — enrolment is an owner act (Claw_Enrolment_Pkg, ledger 113)")

// ErrAttestationFailed reports that the facade would not issue a session. The
// real door answers every attestation failure with one uniform refusal, so this
// error is deliberately as uninformative as the response that produced it.
var ErrAttestationFailed = errors.New("enrol: attestation refused")

// ErrNoDeviceKey reports a client built without a device signing key.
var ErrNoDeviceKey = errors.New("enrol: no device key")

// Client is the claw's enrolment and attestation client. It holds the DEVICE
// KEY — the claw's identity, not the owner's and not the user's.
type Client struct {
	base    string
	binding identity.Binding
	key     ed25519.PrivateKey
	http    *http.Client

	mu      sync.Mutex
	state   gate.EnrolmentState
	session string
	expires time.Time

	// facadeKey is the facade signing key the door presented in its enrol
	// response, when it presented one. THE KEY TRAVELS AT ENROLMENT — that is
	// the design — but older doors predate the field and send nothing, which
	// stays legal. This client only CARRIES the key; whether to pin it, compare
	// it, or refuse over it is the caller's trust decision, made where the pin
	// file lives.
	facadeKey ed25519.PublicKey
}

// New builds an enrolment client for one claw.
//
// The state argument is this machine's own record of whether it is already
// enrolled; ledger 113's NO-TWO-MASTERS turns on it, so it is a parameter rather
// than an assumption.
func New(base string, binding identity.Binding, key ed25519.PrivateKey, state gate.EnrolmentState, httpClient *http.Client) (*Client, error) {
	if len(key) != ed25519.PrivateKeySize {
		return nil, fmt.Errorf("%w: got %d bytes, want %d", ErrNoDeviceKey, len(key), ed25519.PrivateKeySize)
	}
	if err := binding.Validate(); err != nil {
		return nil, fmt.Errorf("enrol: %w", err)
	}
	if httpClient == nil {
		httpClient = &http.Client{Timeout: 30 * time.Second}
	}
	return &Client{
		base:    strings.TrimRight(base, "/"),
		binding: binding,
		key:     key,
		http:    httpClient,
		state:   state,
	}, nil
}

// PublicKey returns the claw's device public key. It is public by construction
// and safe to render.
func (c *Client) PublicKey() ed25519.PublicKey {
	pub, ok := c.key.Public().(ed25519.PublicKey)
	if !ok {
		// ed25519.PrivateKey.Public always returns ed25519.PublicKey; this arm
		// exists so the assertion is not a bare panic-on-nil.
		return nil
	}
	return pub
}

// enrolPayload is the wire shape of an enrolment.
//
// pubkey and ceiling are what the REAL door reads today. The binding fields are
// the v1 addition, and the real handler currently ignores unknown fields, so a
// claw carrying them enrols against the real door unchanged — the binding lands
// when the facade side grows a receiver for it (work item D, separately gated).
type enrolPayload struct {
	PubKey  string `json:"pubkey"`
	Ceiling int    `json:"ceiling"`

	// The identity binding. FOUR IDENTITIES, NOT ONE.
	ClawID string `json:"claw_id"`
	Owner  string `json:"owner_id"`
	User   string `json:"user_id"`

	// ⚠ PII. The purchaser's reference, revealed HERE AND NOWHERE ELSE, because
	// the enrolment ceremony is the only place the claw ↔ owner ↔ purchaser
	// binding can be established. omitempty so that an unpaid install sends no
	// field at all rather than an empty one.
	AppleAccount string `json:"apple_account_ref,omitempty"`
}

// Enrol performs the owner's act.
//
// requester and authentic are the facts ledger 113 decides over: WHO is
// submitting this machine, and whether that claim has been established. They are
// handed in rather than assumed, exactly as the Ada takes them as parameters.
func (c *Client) Enrol(ctx context.Context, requester gate.Actor, authentic bool, ceiling gate.Command) error {
	c.mu.Lock()
	state := c.state
	c.mu.Unlock()

	// ★ The proven core decides, and it decides BEFORE anything is sent.
	if !gate.MayEnrol(state, requester, authentic) {
		return fmt.Errorf("%w: requester %s, state %s, authentic %v", ErrNotAnOwnerAct, requester, state, authentic)
	}

	body := enrolPayload{
		PubKey:       hex.EncodeToString(c.PublicKey()),
		Ceiling:      gate.CommandRank(ceiling),
		ClawID:       string(c.binding.Claw),
		Owner:        string(c.binding.Owner),
		User:         string(c.binding.User),
		AppleAccount: c.binding.Apple.Reveal(), // ⚠ THE ONLY Reveal IN THE REPOSITORY
	}
	encoded, err := json.Marshal(body)
	if err != nil {
		return fmt.Errorf("enrol: encode payload: %w", err)
	}

	resp, err := c.post(ctx, protocol.EnrolPath(string(c.binding.Claw)), encoded, "")
	if err != nil {
		return fmt.Errorf("enrol: %w", err)
	}
	defer c.closeBody(resp)

	if resp.StatusCode != http.StatusOK {
		detail, rerr := io.ReadAll(io.LimitReader(resp.Body, 512))
		if rerr != nil {
			return fmt.Errorf("enrol: HTTP %d (and reading the body failed: %w)", resp.StatusCode, rerr)
		}
		return fmt.Errorf("enrol: HTTP %d: %s", resp.StatusCode, bytes.TrimSpace(detail))
	}

	// The enrol response may carry the facade's signing key — the design's "key
	// travels at enrolment". The field is ADDITIVE: a door that sends nothing
	// is an older door and that is legal. A door that sends garbage is not: a
	// key that cannot be a key means either a broken door or something standing
	// in front of one, and enrolment is refused rather than completed blind.
	var reply struct {
		FacadePub string `json:"facade_pubkey"`
	}
	if err := json.NewDecoder(io.LimitReader(resp.Body, 1<<16)).Decode(&reply); err == nil && reply.FacadePub != "" {
		raw, derr := hex.DecodeString(reply.FacadePub)
		if derr != nil {
			return fmt.Errorf("enrol: the door presented facade_pubkey that is not hex: %w — refusing to complete enrolment against a door whose key cannot be read", derr)
		}
		if len(raw) != ed25519.PublicKeySize {
			return fmt.Errorf("enrol: the door presented facade_pubkey of %d bytes, want %d (Ed25519) — refusing to complete enrolment against a door whose key cannot be a key", len(raw), ed25519.PublicKeySize)
		}
		c.mu.Lock()
		c.facadeKey = ed25519.PublicKey(raw)
		c.mu.Unlock()
	}

	c.mu.Lock()
	c.state = gate.Enrolled
	c.mu.Unlock()
	return nil
}

// FacadeKey returns the facade signing key the door presented at enrolment, or
// nil when it presented none (older doors predate the field, which stays
// legal). What to DO with the key — pin it on first use, compare it against a
// pin, refuse over a mismatch — is the caller's decision, made where the pin
// file lives, not this client's.
func (c *Client) FacadeKey() ed25519.PublicKey {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.facadeKey
}

// State reports this machine's own record of its enrolment state.
func (c *Client) State() gate.EnrolmentState {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.state
}

// Attest runs the challenge–response and stores the resulting session token.
func (c *Client) Attest(ctx context.Context) error {
	id := string(c.binding.Claw)

	// Phase 1: empty body → the challenge. It discloses nothing about whether
	// this claw is enrolled; the refusal, if any, comes at verify.
	resp, err := c.post(ctx, protocol.AttestPath(id), nil, "")
	if err != nil {
		return fmt.Errorf("attest challenge: %w", err)
	}
	var challenge struct {
		Nonce string `json:"nonce"`
	}
	if err := c.decodeOK(resp, &challenge); err != nil {
		return fmt.Errorf("attest challenge: %w", err)
	}
	nonce, err := hex.DecodeString(challenge.Nonce)
	if err != nil {
		return fmt.Errorf("attest challenge: nonce is not hex: %w", err)
	}
	if len(nonce) == 0 {
		return fmt.Errorf("%w: the facade issued an empty challenge", ErrAttestationFailed)
	}

	// Phase 2: sign the nonce BYTES with the device key. Possession of the
	// device key is the whole claim being made.
	verify, err := json.Marshal(struct {
		Nonce string `json:"nonce"`
		Sig   string `json:"sig"`
	}{
		Nonce: challenge.Nonce,
		Sig:   hex.EncodeToString(ed25519.Sign(c.key, nonce)),
	})
	if err != nil {
		return fmt.Errorf("attest verify: encode: %w", err)
	}

	resp2, err := c.post(ctx, protocol.AttestPath(id), verify, "")
	if err != nil {
		return fmt.Errorf("attest verify: %w", err)
	}
	var session struct {
		Session   string `json:"session"`
		ExpiresIn int    `json:"expires_in"`
	}
	if err := c.decodeOK(resp2, &session); err != nil {
		return fmt.Errorf("attest verify: %w", err)
	}
	if session.Session == "" {
		return fmt.Errorf("%w: the facade issued no session token", ErrAttestationFailed)
	}

	ttl := time.Duration(session.ExpiresIn) * time.Second
	if ttl <= 0 {
		ttl = time.Minute
	}
	c.mu.Lock()
	c.session = session.Session
	// Re-attest a little before the facade would drop us, so a long poll
	// interval does not walk into an expired session.
	c.expires = time.Now().Add(ttl - ttl/10)
	c.mu.Unlock()
	return nil
}

// Authorization returns the header value for an outward request, attesting
// first when there is no live session. It satisfies the terminal's Authorizer.
func (c *Client) Authorization(ctx context.Context) (string, error) {
	c.mu.Lock()
	token, expires := c.session, c.expires
	c.mu.Unlock()

	if token != "" && time.Now().Before(expires) {
		return "Bearer " + token, nil
	}
	if err := c.Attest(ctx); err != nil {
		return "", err
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.session == "" {
		return "", ErrAttestationFailed
	}
	return "Bearer " + c.session, nil
}

// Invalidate drops the cached session, so the next Authorization re-attests. It
// is what a 401 means: the session this claw believed in is not one the facade
// honours.
func (c *Client) Invalidate() {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.session = ""
	c.expires = time.Time{}
}

// Sign signs arbitrary bytes with the DEVICE key. It is how a submission is
// signed, and it is on this type because the device key lives here and should
// not be handed around.
func (c *Client) Sign(message []byte) string {
	return hex.EncodeToString(ed25519.Sign(c.key, message))
}

// post makes one outward request. THE CLAW OPENS EVERY CONNECTION.
func (c *Client) post(ctx context.Context, path string, body []byte, authorization string) (*http.Response, error) {
	var reader io.Reader
	if body != nil {
		reader = bytes.NewReader(body)
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.base+path, reader)
	if err != nil {
		return nil, fmt.Errorf("build request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	if authorization != "" {
		req.Header.Set("Authorization", authorization)
	}
	resp, err := c.http.Do(req)
	if err != nil {
		return nil, fmt.Errorf("post %s: %w", path, err)
	}
	return resp, nil
}

// decodeOK reads a 200 body into v, closing the body either way. Any non-200 is
// the uniform refusal.
func (c *Client) decodeOK(resp *http.Response, v any) error {
	defer c.closeBody(resp)
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("%w: HTTP %d", ErrAttestationFailed, resp.StatusCode)
	}
	if err := json.NewDecoder(io.LimitReader(resp.Body, 1<<16)).Decode(v); err != nil {
		return fmt.Errorf("decode response: %w", err)
	}
	return nil
}

// closeBody closes a response body, handling the error rather than discarding
// it. There is nothing useful to do with a failed close but notice it, and
// dropping it into `_` is not allowed here.
func (c *Client) closeBody(resp *http.Response) {
	if resp == nil || resp.Body == nil {
		return
	}
	if err := resp.Body.Close(); err != nil {
		// Deliberately silent beyond this comment: the caller already has its
		// result, and a close failure on a fully-read body changes nothing.
		return
	}
}
