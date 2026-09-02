package bind

import (
	"context"
	"crypto/ed25519"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// clawKey makes a deterministic device key for a test.
func clawKey(t *testing.T, seed string) ed25519.PrivateKey {
	t.Helper()
	raw := make([]byte, ed25519.SeedSize)
	copy(raw, seed)
	return ed25519.NewKeyFromSeed(raw)
}

// ---- the shared wire contract --------------------------------------------

// TestCanonicalIsTheSharedContract pins the reduction the SIGNATURE is
// made over.
//
// ★ THIS TABLE IS DUPLICATED IN THE PORTAL (internal/binding). The two
// sides sign and verify the canonical form, so a disagreement about which
// characters count would make every signature fail and a member would be
// told their key was refused when in truth the two sides could not agree
// on what they were signing. Change one, change the other, same commit.
func TestCanonicalIsTheSharedContract(t *testing.T) {
	tests := []struct {
		name string
		in   string
		want string
	}{
		{name: "already canonical", in: "K7QM3XBP", want: "K7QM3XBP"},
		{name: "as the page shows it", in: "K7QM-3XBP", want: "K7QM3XBP"},
		{name: "lower case", in: "k7qm-3xbp", want: "K7QM3XBP"},
		{name: "surrounding whitespace", in: "  K7QM-3XBP\n", want: "K7QM3XBP"},
		{name: "spaces inside", in: "K7 QM 3X BP", want: "K7QM3XBP"},
		{name: "doubled separators", in: "K7QM--3XBP", want: "K7QM3XBP"},
		{name: "characters outside the alphabet are dropped", in: "K7QM-3XBP!!", want: "K7QM3XBP"},
		// 0/O/1/I/L/U are not in the alphabet at all — they are the
		// characters people mistype — so they reduce away.
		{name: "confusables are not alphabet", in: "OIL0U1", want: ""},
		{name: "nothing at all", in: "", want: ""},
		{name: "punctuation only", in: "----", want: ""},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := Canonical(tc.in); got != tc.want {
				t.Fatalf("Canonical(%q) = %q, want %q", tc.in, got, tc.want)
			}
		})
	}
}

// TestFingerprintGoldenVector pins the exact string the member compares
// by eye against their account page.
//
// ★ THE PORTAL HOLDS THE SAME VECTOR (internal/binding). Separate
// repositories, one contract: drift on either side fails a test rather
// than showing a member two different names for one key.
func TestFingerprintGoldenVector(t *testing.T) {
	// The all-zero Ed25519 seed. Its public half is the well-known
	// 3b6a27bcceb6a42d62a3a8d02a6f0d73653215771de243a63ac048a18b59da29,
	// whose SHA-256 begins 139e3940e64b5491.
	pub := ed25519.NewKeyFromSeed(make([]byte, ed25519.SeedSize)).Public().(ed25519.PublicKey)
	const want = "139e-3940-e64b-5491"
	if got := Fingerprint(pub); got != want {
		t.Fatalf("Fingerprint(zero-seed key) = %q, want %q — if this changed on purpose, "+
			"the portal's internal/binding golden vector must change in the same commit", got, want)
	}
}

func TestFingerprintShape(t *testing.T) {
	fp := Fingerprint(clawKey(t, "ghillie").Public().(ed25519.PublicKey))
	groups := strings.Split(fp, "-")
	if len(groups) != 4 {
		t.Fatalf("fingerprint %q has %d groups, want 4", fp, len(groups))
	}
	for _, g := range groups {
		if len(g) != 4 {
			t.Fatalf("fingerprint %q has a group of %d characters, want 4", fp, len(g))
		}
	}
}

// ---- sign and verify -----------------------------------------------------

// TestSignAndVerifyRoundTrip is the claw-side half of the wire: what this
// machine signs is exactly what the portal will check.
func TestSignAndVerifyRoundTrip(t *testing.T) {
	key := clawKey(t, "the-first-ghillie")
	pub := key.Public().(ed25519.PublicKey)

	tests := []struct {
		name string
		// typed is how the member entered it; checked is how the portal
		// receives it. Any pair that canonicalises alike must verify.
		typed   string
		checked string
	}{
		{name: "verbatim", typed: "K7QM-3XBP", checked: "K7QM-3XBP"},
		{name: "typed lower, checked as shown", typed: "k7qm-3xbp", checked: "K7QM-3XBP"},
		{name: "typed without the hyphen", typed: "K7QM3XBP", checked: "K7QM-3XBP"},
		{name: "typed with stray spaces", typed: " K7QM 3XBP ", checked: "K7QM-3XBP"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			sig, err := Sign(key, tc.typed)
			if err != nil {
				t.Fatalf("Sign: %v", err)
			}
			if !Verify(pub, tc.checked, sig) {
				t.Fatalf("a signature over %q did not verify against %q", tc.typed, tc.checked)
			}
		})
	}
}

// TestSignaturesThatMustNotVerify: the negative half, which is the half
// that matters.
func TestSignaturesThatMustNotVerify(t *testing.T) {
	key := clawKey(t, "the-first-ghillie")
	pub := key.Public().(ed25519.PublicKey)
	sig, err := Sign(key, "K7QM-3XBP")
	if err != nil {
		t.Fatalf("Sign: %v", err)
	}

	t.Run("a different code", func(t *testing.T) {
		if Verify(pub, "K7QM-3XBQ", sig) {
			t.Fatal("a signature verified against a different code")
		}
	})
	t.Run("a different key", func(t *testing.T) {
		other := clawKey(t, "impostor").Public().(ed25519.PublicKey)
		if Verify(other, "K7QM-3XBP", sig) {
			t.Fatal("a signature verified against another machine's key")
		}
	})
	t.Run("a tampered signature", func(t *testing.T) {
		bent := make([]byte, len(sig))
		copy(bent, sig)
		bent[0] ^= 0xff
		if Verify(pub, "K7QM-3XBP", bent) {
			t.Fatal("a tampered signature verified")
		}
	})
	t.Run("a key that is not a key", func(t *testing.T) {
		if Verify(ed25519.PublicKey("short"), "K7QM-3XBP", sig) {
			t.Fatal("a malformed key verified something")
		}
	})
}

func TestSignRefusesNonsense(t *testing.T) {
	t.Run("no key", func(t *testing.T) {
		if _, err := Sign(ed25519.PrivateKey("short"), "K7QM-3XBP"); !errors.Is(err, ErrNoKey) {
			t.Fatalf("Sign with a short key = %v, want ErrNoKey", err)
		}
	})
	t.Run("a code that reduces to nothing", func(t *testing.T) {
		key := clawKey(t, "ghillie")
		for _, bad := range []string{"", "----", "oil0u1"} {
			if _, err := Sign(key, bad); !errors.Is(err, ErrNoCode) {
				t.Fatalf("Sign(%q) = %v, want ErrNoCode", bad, err)
			}
		}
	})
}

// ---- the offer -----------------------------------------------------------

// portalStub stands in for the portal's /enrol door. It checks the
// signature the way the real one does, then answers with whatever the
// test told it to.
func portalStub(t *testing.T, status int, reply Result, seen *offer) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/enrol" {
			t.Errorf("the claw knocked at %q, want /enrol", r.URL.Path)
		}
		if r.Method != http.MethodPost {
			t.Errorf("the claw used %s, want POST", r.Method)
		}
		body, err := io.ReadAll(io.LimitReader(r.Body, maxBody))
		if err != nil {
			t.Errorf("read body: %v", err)
		}
		var got offer
		if jerr := json.Unmarshal(body, &got); jerr != nil {
			t.Errorf("the offer is not JSON: %v", jerr)
		}
		if seen != nil {
			*seen = got
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(status)
		if eerr := json.NewEncoder(w).Encode(reply); eerr != nil {
			t.Errorf("encode reply: %v", eerr)
		}
	}))
	t.Cleanup(srv.Close)
	return srv
}

// TestOfferSendsTheRightThings: the public key and a signature the portal
// can check, and never anything secret.
func TestOfferSendsTheRightThings(t *testing.T) {
	key := clawKey(t, "the-first-ghillie")
	pub := key.Public().(ed25519.PublicKey)
	var seen offer
	srv := portalStub(t, http.StatusOK, Result{
		Verdict: "admit", Admitted: true,
		Fingerprint: Fingerprint(pub), AccountID: "g-12345",
	}, &seen)

	got, err := Client{Base: srv.URL}.Offer(context.Background(), "k7qm-3xbp", key)
	if err != nil {
		t.Fatalf("Offer: %v", err)
	}
	if !got.Admitted || got.Verdict != "admit" {
		t.Fatalf("result = %+v", got)
	}
	if seen.PubKey != hex.EncodeToString(pub) {
		t.Fatalf("offered pubkey %q, want %q", seen.PubKey, hex.EncodeToString(pub))
	}
	if seen.Code != "K7QM3XBP" {
		t.Fatalf("offered code %q, want the canonical form", seen.Code)
	}
	sig, derr := hex.DecodeString(seen.Sig)
	if derr != nil {
		t.Fatalf("the offered signature is not hex: %v", derr)
	}
	if !Verify(pub, seen.Code, sig) {
		t.Fatal("the offered signature does not verify — the portal would refuse it")
	}
	// The private half must not be anywhere in the offer.
	raw, merr := json.Marshal(seen)
	if merr != nil {
		t.Fatalf("marshal: %v", merr)
	}
	if strings.Contains(string(raw), hex.EncodeToString(key.Seed())) {
		t.Fatalf("the offer carries the device key's PRIVATE half:\n%s", raw)
	}
}

// TestRefusalIsCarriedNotInvented: the core's word reaches the caller
// unedited, and a refusal is an ANSWER, not an error.
func TestRefusalIsCarriedNotInvented(t *testing.T) {
	words := []string{
		"refuse_member_unknown",
		"refuse_signature_failed",
		"refuse_code_spent",
		"refuse_code_expired",
		"refuse_code_unknown",
		"refuse_key_bound",
	}
	for _, word := range words {
		t.Run(word, func(t *testing.T) {
			srv := portalStub(t, http.StatusForbidden, Result{Verdict: word}, nil)
			got, err := Client{Base: srv.URL}.Offer(context.Background(),
				"K7QM-3XBP", clawKey(t, "ghillie"))
			if err != nil {
				t.Fatalf("a refusal came back as an error, not an answer: %v", err)
			}
			if got.Verdict != word {
				t.Fatalf("verdict = %q, want %q carried verbatim", got.Verdict, word)
			}
			if got.Admitted {
				t.Fatal("a refusal reported itself as admitted")
			}
			// And it renders as something a person can act on.
			sentence := Explain(word)
			if sentence == "" {
				t.Fatalf("%q renders as nothing", word)
			}
			if strings.Contains(sentence, "does not have a sentence") {
				t.Fatalf("%q has no sentence, but it is one of the core's six", word)
			}
		})
	}
}

// TestExplainIsHonestAboutWordsItDoesNotKnow: a newer portal saying
// something this binary has never heard of is exactly when the member
// needs the raw term, not a smoothed-over guess.
func TestExplainIsHonestAboutWordsItDoesNotKnow(t *testing.T) {
	sentence := Explain("refuse_because_the_moon_is_wrong")
	if !strings.Contains(sentence, "does not have a sentence") {
		t.Fatalf("an unknown verdict was smoothed over: %q", sentence)
	}
}

// TestNoVerdictIsNeverAYes: every way the portal can fail to answer
// yields an error, and never an admission.
func TestNoVerdictIsNeverAYes(t *testing.T) {
	key := clawKey(t, "ghillie")
	tests := []struct {
		name    string
		handler http.HandlerFunc
		wantErr error
	}{
		{
			name: "the door is unwired and says so",
			handler: func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("Content-Type", "application/json")
				w.WriteHeader(http.StatusServiceUnavailable)
				if err := json.NewEncoder(w).Encode(Result{
					Error: "the enrolment door is not wired on this host",
				}); err != nil {
					t.Errorf("encode: %v", err)
				}
			},
			wantErr: ErrPortal,
		},
		{
			name: "the reply is not JSON",
			handler: func(w http.ResponseWriter, r *http.Request) {
				if _, err := w.Write([]byte("<html>a proxy's error page</html>")); err != nil {
					t.Errorf("write: %v", err)
				}
			},
			wantErr: ErrPortal,
		},
		{
			name: "the reply has neither verdict nor reason",
			handler: func(w http.ResponseWriter, r *http.Request) {
				if _, err := w.Write([]byte(`{}`)); err != nil {
					t.Errorf("write: %v", err)
				}
			},
			wantErr: ErrPortal,
		},
		{
			// The nastiest case: a door claiming success with no verdict.
			// It must not be read as an admission.
			name: "admitted:true with no verdict word",
			handler: func(w http.ResponseWriter, r *http.Request) {
				if _, err := w.Write([]byte(`{"admitted":true}`)); err != nil {
					t.Errorf("write: %v", err)
				}
			},
			wantErr: ErrPortal,
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			srv := httptest.NewServer(tc.handler)
			defer srv.Close()
			got, err := Client{Base: srv.URL}.Offer(context.Background(), "K7QM-3XBP", key)
			if !errors.Is(err, tc.wantErr) {
				t.Fatalf("error = %v, want %v", err, tc.wantErr)
			}
			if got.Admitted {
				t.Fatalf("no verdict was obtained, yet the result admits: %+v", got)
			}
		})
	}
}

func TestUnreachablePortalIsNotAYes(t *testing.T) {
	// A server that is closed before the call: nothing answers there.
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {}))
	base := srv.URL
	srv.Close()

	got, err := Client{Base: base}.Offer(context.Background(), "K7QM-3XBP", clawKey(t, "ghillie"))
	if !errors.Is(err, ErrPortal) {
		t.Fatalf("error = %v, want ErrPortal", err)
	}
	if got.Admitted {
		t.Fatal("an unreachable portal admitted something")
	}
}

func TestDefaultPortalIsTheCustomerArea(t *testing.T) {
	if !strings.HasPrefix(DefaultPortal, "https://") {
		t.Fatalf("the default portal %q is not https", DefaultPortal)
	}
	if !strings.Contains(DefaultPortal, "thedarkfactory.co.uk") {
		t.Fatalf("the default portal %q is not the Dark Factory's", DefaultPortal)
	}
}
