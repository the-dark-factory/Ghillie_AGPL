package main

// The two client-side trust decisions live in main because they are GLUE, not
// judgement: which facade key to hold the wire to, and which device key IS this
// claw. These tables hold that glue to its stated policy — trust on first use,
// never a silent re-pin, a device identity generated once and never clobbered.

import (
	"crypto/ed25519"
	"crypto/sha256"
	"encoding/hex"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// testKey derives a deterministic key pair for the tables below.
func testKey(seed string) ed25519.PublicKey {
	sum := sha256.Sum256([]byte(seed))
	return ed25519.NewKeyFromSeed(sum[:]).Public().(ed25519.PublicKey)
}

// TestResolveFacadeKey is the whole TOFU policy as a table: who wins, when a
// pin is written, and — the part that matters most — when the answer is a hard
// refusal rather than a quiet preference.
func TestResolveFacadeKey(t *testing.T) {
	doorKey := testKey("the-door")
	otherKey := testKey("an-impostor")

	tests := []struct {
		name string
		// inputs
		explicit ed25519.PublicKey
		enrolled ed25519.PublicKey
		pinFile  string // "" = no pin file; otherwise hex content to pre-write
		// expectations
		wantKey     ed25519.PublicKey
		wantErr     string // substring of the error, "" = no error
		wantPinned  bool   // a pin file must exist afterwards holding wantKey
		wantNote    bool   // a first-use note is returned
		wantPinKept string // pin file content must still decode to this key's hex ("" = don't check)
	}{
		{
			name:    "no key from anywhere is an error, not a default",
			wantErr: "no facade key",
		},
		{
			name:     "an explicit key alone is the authority",
			explicit: doorKey,
			wantKey:  doorKey,
		},
		{
			name:    "a pinned key alone carries the trust",
			pinFile: hex.EncodeToString(doorKey),
			wantKey: doorKey,
		},
		{
			name:       "first use: the door's key is pinned and trusted",
			enrolled:   doorKey,
			wantKey:    doorKey,
			wantPinned: true,
			wantNote:   true,
		},
		{
			name:        "a door matching the pin is held to it, and the pin stands",
			enrolled:    doorKey,
			pinFile:     hex.EncodeToString(doorKey),
			wantKey:     doorKey,
			wantPinKept: hex.EncodeToString(doorKey),
		},
		{
			name:     "an explicit key matching the door wins without writing a pin",
			explicit: doorKey,
			enrolled: doorKey,
			wantKey:  doorKey,
		},
		{
			name:        "★ a door disagreeing with the pin is a HARD refusal, never a re-pin",
			enrolled:    otherKey,
			pinFile:     hex.EncodeToString(doorKey),
			wantErr:     "facade key mismatch",
			wantPinKept: hex.EncodeToString(doorKey),
		},
		{
			name:     "★ a door disagreeing with the explicit key is a HARD refusal",
			explicit: doorKey,
			enrolled: otherKey,
			wantErr:  "facade key mismatch",
		},
		{
			name:    "a corrupt pin is an error, not a guess",
			pinFile: "not-hex",
			wantErr: "not hex",
		},
		{
			name:    "a pin of the wrong length is an error, not a guess",
			pinFile: "deadbeef",
			wantErr: "want 32",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			dir := t.TempDir()
			pinPath := filepath.Join(dir, "ghillie-facade.pin")
			if tt.pinFile != "" {
				if err := os.WriteFile(pinPath, []byte(tt.pinFile+"\n"), 0o600); err != nil {
					t.Fatalf("pre-write pin: %v", err)
				}
			}

			key, note, err := resolveFacadeKey(tt.explicit, tt.enrolled, "explicit.pub", pinPath)

			if tt.wantErr != "" {
				if err == nil {
					t.Fatalf("want an error containing %q, got nil (key %x)", tt.wantErr, key)
				}
				if !strings.Contains(err.Error(), tt.wantErr) {
					t.Fatalf("error %q does not name the refusal %q", err, tt.wantErr)
				}
			} else {
				if err != nil {
					t.Fatalf("resolveFacadeKey: %v", err)
				}
				if key == nil || !key.Equal(tt.wantKey) {
					t.Errorf("resolved key %x, want %x", key, tt.wantKey)
				}
			}

			if tt.wantNote && note == "" {
				t.Error("first use should say out loud that it trusted and pinned; the note is empty")
			}
			if tt.wantPinned {
				body, rerr := os.ReadFile(pinPath)
				if rerr != nil {
					t.Fatalf("the pin was not written: %v", rerr)
				}
				if strings.TrimSpace(string(body)) != hex.EncodeToString(tt.wantKey) {
					t.Errorf("pin file holds %q, want the door's key", strings.TrimSpace(string(body)))
				}
				info, serr := os.Stat(pinPath)
				if serr != nil {
					t.Fatalf("stat pin: %v", serr)
				}
				if perm := info.Mode().Perm(); perm != 0o600 {
					t.Errorf("pin file mode %o, want 0600", perm)
				}
			}
			if tt.wantPinKept != "" {
				body, rerr := os.ReadFile(pinPath)
				if rerr != nil {
					t.Fatalf("read pin after resolution: %v", rerr)
				}
				if strings.TrimSpace(string(body)) != tt.wantPinKept {
					t.Errorf("★ THE PIN WAS REWRITTEN: holds %q, want the original — re-pinning is an operator act, never this code's", strings.TrimSpace(string(body)))
				}
			}
		})
	}
}

// TestLoadOrCreateDeviceKey holds the device identity to its lifecycle: born
// once from entropy, kept 0600, loaded ever after, and NEVER clobbered when the
// file cannot be read as a key.
func TestLoadOrCreateDeviceKey(t *testing.T) {
	t.Run("generated once, then loaded identically", func(t *testing.T) {
		path := filepath.Join(t.TempDir(), "keys", "ghillie-device.key")

		first, created, err := loadOrCreateDeviceKey(path)
		if err != nil {
			t.Fatalf("first call: %v", err)
		}
		if !created {
			t.Error("first call should report the key as created")
		}
		info, err := os.Stat(path)
		if err != nil {
			t.Fatalf("stat device key: %v", err)
		}
		if perm := info.Mode().Perm(); perm != 0o600 {
			t.Errorf("device key file mode %o, want 0600 — the key is the claw's identity", perm)
		}

		second, created, err := loadOrCreateDeviceKey(path)
		if err != nil {
			t.Fatalf("second call: %v", err)
		}
		if created {
			t.Error("second call should LOAD, not create — an identity is born once")
		}
		if !first.Equal(second) {
			t.Error("the loaded key is not the generated key — the identity did not persist")
		}
	})

	t.Run("two claws get two identities", func(t *testing.T) {
		dir := t.TempDir()
		a, _, err := loadOrCreateDeviceKey(filepath.Join(dir, "a.key"))
		if err != nil {
			t.Fatalf("a: %v", err)
		}
		b, _, err := loadOrCreateDeviceKey(filepath.Join(dir, "b.key"))
		if err != nil {
			t.Fatalf("b: %v", err)
		}
		if a.Equal(b) {
			t.Error("two freshly generated device keys are equal — that is not entropy")
		}
	})

	corrupt := []struct {
		name string
		body string
	}{
		{"not hex", "this is not a key\n"},
		{"wrong length", "deadbeef\n"},
	}
	for _, tt := range corrupt {
		t.Run("corrupt file ("+tt.name+") errors and is not overwritten", func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "ghillie-device.key")
			if err := os.WriteFile(path, []byte(tt.body), 0o600); err != nil {
				t.Fatalf("pre-write: %v", err)
			}
			if _, _, err := loadOrCreateDeviceKey(path); err == nil {
				t.Fatal("a corrupt device key file should error, never be silently replaced")
			}
			body, err := os.ReadFile(path)
			if err != nil {
				t.Fatalf("re-read: %v", err)
			}
			if string(body) != tt.body {
				t.Error("★ THE CORRUPT FILE WAS OVERWRITTEN — a device key that might be recoverable was clobbered")
			}
		})
	}
}

// TestGlassHostClass holds the mechanical half of the bind policy to its
// stated classification: the POLICY lives in the proven Glass_Bind_Policy_Pkg
// behind GLASS_BIND_DECIDER; this table only pins that the glue hands the
// decider the class the parsed address actually is — and that anything it
// cannot parse goes over as named_remote, where the proven default refuses.
func TestGlassHostClass(t *testing.T) {
	cases := []struct {
		host string
		want string
	}{
		{"127.0.0.1", "loopback_v4"},
		{"127.9.9.9", "loopback_v4"},
		{"::1", "loopback_v6"},
		{"::ffff:127.0.0.1", "loopback_v4"},
		{"", "unspecified_all"},
		{"0.0.0.0", "unspecified_all"},
		{"::", "unspecified_all"},
		{"192.168.1.10", "named_remote"},
		{"8.8.8.8", "named_remote"},
		{"2001:db8::1", "named_remote"},
		{"localhost", "named_remote"}, // a NAME is not a parsed address; the proven default refuses it — give the IP form
		{"not-an-address", "named_remote"},
	}
	for _, c := range cases {
		if got := glassHostClass(c.host); got != c.want {
			t.Errorf("glassHostClass(%q) = %q, want %q", c.host, got, c.want)
		}
	}
}

// TestGlassBindVerdictFailsClosed pins the two no-answer paths: no decider
// configured, and a decider that exits non-zero. Both must refuse with an
// error — a listener must never open on a default.
func TestGlassBindVerdictFailsClosed(t *testing.T) {
	t.Setenv(glassDeciderEnv, "")
	if _, err := glassBindVerdict("loopback_v4"); err == nil {
		t.Fatal("no decider configured: want an error, got none")
	}
	t.Setenv(glassDeciderEnv, "/bin/false")
	if _, err := glassBindVerdict("loopback_v4"); err == nil {
		t.Fatal("decider exited non-zero: want an error, got none")
	}
}

// TestSettleVoice is the Mac-speaks-by-default rule as a table (Tony,
// 2026-08-26): -text-only always wins and contradicts out loud; an explicit
// -voice is honoured either way with its loud contract intact; the default
// speaks only where a console interview, the platform and the pipeline all
// line up — and only the DEFAULT carries text fallback, because a courtesy
// must never cost the sitting.
func TestSettleVoice(t *testing.T) {
	tests := []struct {
		name string
		// inputs
		voiceSet, voiceAsked, textOnly, doInterview, glass, ears bool
		platformOK, pipelinePresent                              bool
		// expectations
		wantSpeak, wantFallback, wantAuto bool
		wantErr                           string // substring, "" = no error
	}{
		{name: "mac interview with pipeline speaks by default",
			doInterview: true, platformOK: true, pipelinePresent: true,
			wantSpeak: true, wantFallback: true, wantAuto: true},
		{name: "text-only wins over the default",
			textOnly: true, doInterview: true, platformOK: true, pipelinePresent: true},
		{name: "text-only against explicit -voice contradicts out loud",
			textOnly: true, voiceSet: true, voiceAsked: true, doInterview: true,
			platformOK: true, pipelinePresent: true, wantErr: "contradict"},
		{name: "text-only with an explicit -voice=false is not a contradiction",
			textOnly: true, voiceSet: true, doInterview: true,
			platformOK: true, pipelinePresent: true},
		{name: "text-only refuses ears",
			textOnly: true, ears: true, doInterview: true,
			platformOK: true, pipelinePresent: true, wantErr: "-ears"},
		{name: "explicit -voice keeps its loud contract past a missing pipeline",
			voiceSet: true, voiceAsked: true, doInterview: true, platformOK: true,
			wantSpeak: true}, // no fallback, no auto: a later path error stays loud
		{name: "explicit -voice=false stays silent on a speaking-capable mac",
			voiceSet: true, doInterview: true, platformOK: true, pipelinePresent: true},
		{name: "the glass never speaks by default",
			doInterview: true, glass: true, platformOK: true, pipelinePresent: true},
		{name: "no interview, no default voice",
			platformOK: true, pipelinePresent: true},
		{name: "not a mac: the default never fires",
			doInterview: true, pipelinePresent: true},
		{name: "pipeline absent: the default declines rather than path-errors",
			doInterview: true, platformOK: true},
		{name: "ears may ride the defaulted voice",
			doInterview: true, ears: true, platformOK: true, pipelinePresent: true,
			wantSpeak: true, wantFallback: true, wantAuto: true},
	}
	for _, c := range tests {
		speak, fallback, auto, err := settleVoice(c.voiceSet, c.voiceAsked,
			c.textOnly, c.doInterview, c.glass, c.ears, c.platformOK, c.pipelinePresent)
		if c.wantErr != "" {
			if err == nil || !strings.Contains(err.Error(), c.wantErr) {
				t.Errorf("%s: err = %v, want substring %q", c.name, err, c.wantErr)
			}
			continue
		}
		if err != nil {
			t.Errorf("%s: unexpected error %v", c.name, err)
			continue
		}
		if speak != c.wantSpeak || fallback != c.wantFallback || auto != c.wantAuto {
			t.Errorf("%s: got (speak=%v fallback=%v auto=%v), want (%v %v %v)",
				c.name, speak, fallback, auto, c.wantSpeak, c.wantFallback, c.wantAuto)
		}
	}
}
