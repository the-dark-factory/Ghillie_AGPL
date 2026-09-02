package enrol

import (
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	"github.com/tonygair/ghillie/internal/gate"
	"github.com/tonygair/ghillie/internal/identity"
)

const appleSecret = "apple-acct-999888777-PURCHASER"

// deviceKey is a deterministic test device key.
func deviceKey(seed string) ed25519.PrivateKey {
	sum := sha256.Sum256([]byte(seed))
	return ed25519.NewKeyFromSeed(sum[:])
}

func testBinding() identity.Binding {
	return identity.Binding{
		Claw:  identity.ClawID("claw-1"),
		Owner: identity.OwnerID("acme-it"),
		User:  identity.UserID("tony"),
		Apple: identity.NewAppleAccountRef(appleSecret),
	}
}

// doorDouble is a minimal stand-in for the facade's enrol/attest door, shaped
// exactly like the real one in ada-factory/cmd/specifier/claw_wiring.go.
type doorDouble struct {
	mu         sync.Mutex
	enrolBody  []byte
	pubKey     ed25519.PublicKey
	challenge  []byte
	sessions   int
	refuseAll  bool
	enrolCalls int

	// facadePub, when non-empty, is included in the enrol response as
	// facade_pubkey — the door presenting its signing key at enrolment. Empty
	// means an OLDER door that predates the field, which must stay legal.
	facadePub string
}

func (d *doorDouble) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	switch {
	case strings.HasSuffix(r.URL.Path, "/enrol"):
		d.mu.Lock()
		d.enrolCalls++
		d.mu.Unlock()
		body := make([]byte, r.ContentLength)
		if _, err := r.Body.Read(body); err != nil && len(body) == 0 {
			http.Error(w, "bad body", http.StatusBadRequest)
			return
		}
		var payload struct {
			PubKey string `json:"pubkey"`
		}
		if err := json.Unmarshal(body, &payload); err != nil {
			http.Error(w, "bad body", http.StatusBadRequest)
			return
		}
		raw, err := hex.DecodeString(payload.PubKey)
		if err != nil || len(raw) != ed25519.PublicKeySize {
			http.Error(w, "bad pubkey", http.StatusBadRequest)
			return
		}
		d.mu.Lock()
		d.enrolBody = body
		d.pubKey = ed25519.PublicKey(raw)
		facadePub := d.facadePub
		d.mu.Unlock()
		reply := map[string]any{"id": "claw-1", "ceiling": 2, "ceiling_name": "Deliver_Artifact"}
		if facadePub != "" {
			reply["facade_pubkey"] = facadePub
		}
		writeJSON(w, reply)

	case strings.HasSuffix(r.URL.Path, "/attest"):
		if d.refuseAll {
			http.Error(w, "attestation required", http.StatusUnauthorized)
			return
		}
		var req struct {
			Nonce string `json:"nonce"`
			Sig   string `json:"sig"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			// An empty body is the challenge phase, and decoding it fails with
			// EOF — which is not an error here.
			req = struct {
				Nonce string `json:"nonce"`
				Sig   string `json:"sig"`
			}{}
		}
		if req.Sig == "" {
			var n [32]byte
			if _, err := rand.Read(n[:]); err != nil {
				http.Error(w, "unavailable", http.StatusServiceUnavailable)
				return
			}
			d.mu.Lock()
			d.challenge = n[:]
			d.mu.Unlock()
			writeJSON(w, map[string]any{"nonce": hex.EncodeToString(n[:])})
			return
		}
		d.mu.Lock()
		stored, pub := d.challenge, d.pubKey
		d.mu.Unlock()
		sig, err := hex.DecodeString(req.Sig)
		if err != nil || pub == nil || !ed25519.Verify(pub, stored, sig) {
			http.Error(w, "attestation required", http.StatusUnauthorized)
			return
		}
		d.mu.Lock()
		d.sessions++
		token := "session-" + hex.EncodeToString(stored[:4])
		d.mu.Unlock()
		writeJSON(w, map[string]any{"session": token, "expires_in": 1800})

	default:
		http.NotFound(w, r)
	}
}

func writeJSON(w http.ResponseWriter, body any) {
	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(body); err != nil {
		http.Error(w, "encode", http.StatusInternalServerError)
	}
}

// TestEnrolmentIsAnOwnerAct is the ledger-113 wiring test: an enrolment that
// the proven core does not license never reaches the wire.
func TestEnrolmentIsAnOwnerAct(t *testing.T) {
	tests := []struct {
		name      string
		state     gate.EnrolmentState
		requester gate.Actor
		authentic bool
		wantSent  bool
	}{
		{"the owner enrols an unenrolled claw", gate.Unenrolled, gate.ActorEnrollingOwner, true, true},
		{"the local user tries to enrol", gate.Unenrolled, gate.ActorLocalUser, true, false},
		{"a Grapple tries to recruit the claw", gate.Unenrolled, gate.ActorCommandingGrapple, true, false},
		{"nobody tries to enrol", gate.Unenrolled, gate.ActorNobody, true, false},
		{"the owner, but the claim is not authentic", gate.Unenrolled, gate.ActorEnrollingOwner, false, false},
		{"NO-TWO-MASTERS: already enrolled", gate.Enrolled, gate.ActorEnrollingOwner, true, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			door := &doorDouble{}
			srv := httptest.NewServer(door)
			t.Cleanup(srv.Close)

			c, err := New(srv.URL, testBinding(), deviceKey("enrol-test"), tt.state, nil)
			if err != nil {
				t.Fatalf("New: %v", err)
			}
			err = c.Enrol(context.Background(), tt.requester, tt.authentic, gate.DeliverArtifact)

			door.mu.Lock()
			calls := door.enrolCalls
			door.mu.Unlock()

			if tt.wantSent {
				if err != nil {
					t.Fatalf("a lawful enrolment failed: %v", err)
				}
				if calls != 1 {
					t.Errorf("the facade saw %d enrolment(s), want 1", calls)
				}
				if c.State() != gate.Enrolled {
					t.Errorf("state after enrolling = %v, want Enrolled", c.State())
				}
				return
			}
			if err == nil {
				t.Fatal("an unlawful enrolment was allowed")
			}
			if !errors.Is(err, ErrNotAnOwnerAct) {
				t.Errorf("refused for the wrong reason: %v", err)
			}
			if calls != 0 {
				t.Errorf("★ the refused enrolment REACHED THE WIRE (%d call(s)) — it must be refused on this machine, before anything is sent", calls)
			}
		})
	}
}

// TestTheEnrolmentPayloadCarriesTheBinding checks that all four identities are
// bound at enrolment, and that this is the one place the purchaser's reference
// travels.
func TestTheEnrolmentPayloadCarriesTheBinding(t *testing.T) {
	door := &doorDouble{}
	srv := httptest.NewServer(door)
	t.Cleanup(srv.Close)

	c, err := New(srv.URL, testBinding(), deviceKey("enrol-test"), gate.Unenrolled, nil)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	if err := c.Enrol(context.Background(), gate.ActorEnrollingOwner, true, gate.DeliverArtifact); err != nil {
		t.Fatalf("Enrol: %v", err)
	}

	door.mu.Lock()
	body := string(door.enrolBody)
	door.mu.Unlock()

	for _, want := range []string{"claw-1", "acme-it", "tony", appleSecret} {
		if !strings.Contains(body, want) {
			t.Errorf("the enrolment payload does not carry %q — the identity binding is incomplete:\n%s", want, body)
		}
	}
	if !strings.Contains(body, `"ceiling":2`) {
		t.Errorf("the enrolment payload does not carry the ceiling:\n%s", body)
	}
}

// TestEnrolCarriesTheFacadeKey covers the additive facade_pubkey field of the
// enrol response: a door that presents its signing key at enrolment (the
// design's "key travels at enrolment"), a door that presents none (older doors
// stay legal), and a door that presents something that cannot be a key (refused
// rather than completed blind).
func TestEnrolCarriesTheFacadeKey(t *testing.T) {
	realKey := deviceKey("the-facade-signing-key").Public().(ed25519.PublicKey)

	tests := []struct {
		name      string
		facadePub string
		wantErr   bool
		wantKey   ed25519.PublicKey
	}{
		{"a 64-hex facade_pubkey is carried", hex.EncodeToString(realKey), false, realKey},
		{"an absent facade_pubkey stays legal — older doors", "", false, nil},
		{"a facade_pubkey that is not hex refuses enrolment", "not-hex-at-all", true, nil},
		{"a facade_pubkey of the wrong length refuses enrolment", "deadbeef", true, nil},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			door := &doorDouble{facadePub: tt.facadePub}
			srv := httptest.NewServer(door)
			t.Cleanup(srv.Close)

			c, err := New(srv.URL, testBinding(), deviceKey("enrol-test"), gate.Unenrolled, nil)
			if err != nil {
				t.Fatalf("New: %v", err)
			}
			err = c.Enrol(context.Background(), gate.ActorEnrollingOwner, true, gate.DeliverArtifact)

			if tt.wantErr {
				if err == nil {
					t.Fatal("Enrol accepted a facade_pubkey that cannot be a key")
				}
				if c.State() == gate.Enrolled {
					t.Error("a refused enrolment left the client believing it is enrolled")
				}
				return
			}
			if err != nil {
				t.Fatalf("Enrol: %v", err)
			}
			got := c.FacadeKey()
			if tt.wantKey == nil {
				if got != nil {
					t.Errorf("FacadeKey = %x, want nil for a door that presented none", got)
				}
				return
			}
			if got == nil || !got.Equal(tt.wantKey) {
				t.Errorf("FacadeKey = %x, want %x", got, tt.wantKey)
			}
		})
	}
}

// TestAttestationSignsTheNonceWithTheDeviceKey covers the challenge–response
// and the session caching.
func TestAttestationSignsTheNonceWithTheDeviceKey(t *testing.T) {
	door := &doorDouble{}
	srv := httptest.NewServer(door)
	t.Cleanup(srv.Close)

	c, err := New(srv.URL, testBinding(), deviceKey("enrol-test"), gate.Unenrolled, nil)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	if err := c.Enrol(context.Background(), gate.ActorEnrollingOwner, true, gate.DeliverArtifact); err != nil {
		t.Fatalf("Enrol: %v", err)
	}

	header, err := c.Authorization(context.Background())
	if err != nil {
		t.Fatalf("Authorization: %v", err)
	}
	if !strings.HasPrefix(header, "Bearer ") {
		t.Errorf("Authorization header = %q, want a Bearer token", header)
	}

	// A live session is reused rather than re-attested on every request.
	again, err := c.Authorization(context.Background())
	if err != nil {
		t.Fatalf("Authorization (second): %v", err)
	}
	if again != header {
		t.Errorf("the session was not reused: %q then %q", header, again)
	}
	door.mu.Lock()
	sessions := door.sessions
	door.mu.Unlock()
	if sessions != 1 {
		t.Errorf("attested %d times for two requests, want 1", sessions)
	}

	// A 401 invalidates it, and the next request re-attests.
	c.Invalidate()
	if _, err := c.Authorization(context.Background()); err != nil {
		t.Fatalf("re-attest after Invalidate: %v", err)
	}
	door.mu.Lock()
	sessions = door.sessions
	door.mu.Unlock()
	if sessions != 2 {
		t.Errorf("attested %d times after invalidation, want 2", sessions)
	}
}

// TestAttestationRefusalIsUniform checks that a refused attestation surfaces as
// one indistinguishable error, matching the real door's single uniform refusal.
func TestAttestationRefusalIsUniform(t *testing.T) {
	door := &doorDouble{refuseAll: true}
	srv := httptest.NewServer(door)
	t.Cleanup(srv.Close)

	c, err := New(srv.URL, testBinding(), deviceKey("enrol-test"), gate.Enrolled, nil)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	if _, err := c.Authorization(context.Background()); !errors.Is(err, ErrAttestationFailed) {
		t.Fatalf("Authorization returned %v, want an ErrAttestationFailed", err)
	}
}

// TestNewRefusesABadClient covers construction-time refusals.
func TestNewRefusesABadClient(t *testing.T) {
	tests := []struct {
		name    string
		key     ed25519.PrivateKey
		binding identity.Binding
	}{
		{"no device key", nil, testBinding()},
		{"short device key", make([]byte, 8), testBinding()},
		{"no owner in the binding", deviceKey("x"), identity.Binding{Claw: "c", User: "u"}},
		{"no claw in the binding", deviceKey("x"), identity.Binding{Owner: "o", User: "u"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if _, err := New("http://example.invalid", tt.binding, tt.key, gate.Unenrolled, nil); err == nil {
				t.Fatal("New accepted a client it should have refused")
			}
		})
	}
}

// TestSignIsDeterministicForTheSameKey checks the device-key signing surface
// the submission path uses.
func TestSignIsDeterministicForTheSameKey(t *testing.T) {
	c, err := New("http://example.invalid", testBinding(), deviceKey("enrol-test"), gate.Enrolled, nil)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	msg := []byte("ghillie-submission-v1 whatever")
	sig := c.Sign(msg)
	raw, err := hex.DecodeString(sig)
	if err != nil {
		t.Fatalf("signature is not hex: %v", err)
	}
	if !ed25519.Verify(c.PublicKey(), msg, raw) {
		t.Error("the signature does not verify against the claw's own public key")
	}
	if sig != c.Sign(msg) {
		t.Error("Ed25519 signatures over the same message differ — something is wrong with the key")
	}
}
