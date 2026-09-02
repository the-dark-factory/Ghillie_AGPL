package notes

import (
	"crypto/ed25519"
	"encoding/hex"
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"
)

func testKey(t *testing.T) (ed25519.PublicKey, ed25519.PrivateKey) {
	t.Helper()
	seed := make([]byte, ed25519.SeedSize)
	for i := range seed {
		seed[i] = byte(0x5A ^ i)
	}
	priv := ed25519.NewKeyFromSeed(seed)
	return priv.Public().(ed25519.PublicKey), priv
}

func signed(t *testing.T, priv ed25519.PrivateKey, n Note) Note {
	t.Helper()
	n.Signature = hex.EncodeToString(ed25519.Sign(priv, n.SigningBytes()))
	return n
}

func batch(t *testing.T, ns ...Note) string {
	t.Helper()
	b, err := json.Marshal(map[string]any{"notes": ns})
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

func now() string { return time.Now().UTC().Format(time.RFC3339) }

func TestGoodNoteSurvivesAndRenders(t *testing.T) {
	pub, priv := testKey(t)
	n := signed(t, priv, Note{
		Correspondent: "tok-abc", From: "the forge queue",
		State: Progress, Fraction: 0.33, At: now(),
	})
	good, refused, err := Decode(strings.NewReader(batch(t, n)), pub)
	if err != nil {
		t.Fatal(err)
	}
	if len(refused) != 0 {
		t.Fatalf("refused a good note: %v", refused)
	}
	if len(good) != 1 {
		t.Fatalf("got %d notes, want 1", len(good))
	}
	if s := good[0].Sentence(); !strings.Contains(s, "33%") {
		t.Fatalf("sentence = %q, want the honest fraction in it", s)
	}
}

// The signature is the whole defence against a spoofed correspondent, since a
// note is shown to a person in ghillie's window.
func TestUnsignedAndTamperedAreRefused(t *testing.T) {
	pub, priv := testKey(t)
	base := Note{Correspondent: "tok-abc", From: "a shop", State: Finished, At: now()}

	t.Run("unsigned", func(t *testing.T) {
		_, refused, err := Decode(strings.NewReader(batch(t, base)), pub)
		if err != nil {
			t.Fatal(err)
		}
		if len(refused) != 1 || !errors.Is(refused[0], ErrBadSignature) {
			t.Fatalf("refused = %v, want a signature refusal", refused)
		}
	})

	t.Run("tampered after signing", func(t *testing.T) {
		n := signed(t, priv, base)
		n.Text = "call 0800-NOT-GHILLIE" // the phishing case, exactly
		_, refused, err := Decode(strings.NewReader(batch(t, n)), pub)
		if err != nil {
			t.Fatal(err)
		}
		if len(refused) != 1 || !errors.Is(refused[0], ErrBadSignature) {
			t.Fatalf("tampered note was not refused: %v", refused)
		}
	})

	t.Run("no key pinned fails closed", func(t *testing.T) {
		n := signed(t, priv, base)
		_, refused, err := Decode(strings.NewReader(batch(t, n)), nil)
		if err != nil {
			t.Fatal(err)
		}
		if len(refused) != 1 || !errors.Is(refused[0], ErrBadSignature) {
			t.Fatalf("unverifiable note was not refused: %v", refused)
		}
	})
}

// The closed vocabulary is the property that keeps notes from becoming a
// second command channel.
func TestClosedVocabularyAndHonestFractions(t *testing.T) {
	pub, priv := testKey(t)
	cases := []struct {
		name string
		note Note
		want error
	}{
		{"invented state", Note{Correspondent: "t", From: "x", State: "INSTALL_THIS", At: now()}, ErrUnknownState},
		{"fraction on FINISHED", Note{Correspondent: "t", From: "x", State: Finished, Fraction: 0.99, At: now()}, ErrMalformed},
		{"fraction out of range", Note{Correspondent: "t", From: "x", State: Progress, Fraction: 1.5, At: now()}, ErrMalformed},
		{"no correspondent", Note{From: "x", State: Started, At: now()}, ErrMalformed},
		{"bad timestamp", Note{Correspondent: "t", From: "x", State: Started, At: "tuesday"}, ErrMalformed},
		{"oversized text", Note{Correspondent: "t", From: "x", State: Started, Text: strings.Repeat("a", 501), At: now()}, ErrMalformed},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, refused, err := Decode(strings.NewReader(batch(t, signed(t, priv, tc.note))), pub)
			if err != nil {
				t.Fatal(err)
			}
			if len(refused) != 1 || !errors.Is(refused[0], tc.want) {
				t.Fatalf("refused = %v, want %v", refused, tc.want)
			}
		})
	}
}

// An unknown FIELD is the conduct-wall case: a correspondent must not be able
// to carry structure this type has no place for.
func TestUnknownFieldRefusesTheWholeBatch(t *testing.T) {
	pub, _ := testKey(t)
	body := `{"notes":[{"correspondent":"t","from":"x","state":"STARTED","at":"2026-08-05T20:00:00Z","signature":"00","may_interrupt":true}]}`
	_, _, err := Decode(strings.NewReader(body), pub)
	if err == nil || !errors.Is(err, ErrMalformed) {
		t.Fatalf("err = %v, want the batch refused whole", err)
	}
}

// One bad note must not silence a person's whole correspondence.
func TestBadNoteDoesNotSilenceGoodOnes(t *testing.T) {
	pub, priv := testKey(t)
	good := signed(t, priv, Note{Correspondent: "t1", From: "the queue", State: SpecReady, At: now()})
	bad := Note{Correspondent: "t2", From: "an impostor", State: Failed, At: now()} // unsigned

	kept, refused, err := Decode(strings.NewReader(batch(t, bad, good)), pub)
	if err != nil {
		t.Fatal(err)
	}
	if len(kept) != 1 || kept[0].From != "the queue" {
		t.Fatalf("kept = %+v, want only the good note", kept)
	}
	if len(refused) != 1 {
		t.Fatalf("refused = %v, want exactly the bad one", refused)
	}
}

func TestTerminalRetiresCorrespondence(t *testing.T) {
	for _, s := range []State{Finished, Failed, Withdrawn} {
		if !s.Terminal() {
			t.Fatalf("%s should be terminal", s)
		}
	}
	for _, s := range []State{Accepted, Started, Progress, SpecReady, Delayed, Blocked, WorkerDown} {
		if s.Terminal() {
			t.Fatalf("%s should not be terminal", s)
		}
	}
}

// A correspondent's words are QUOTED as theirs, never spoken in ghillie's
// voice — nobody gets to borrow the assistant's mouth.
func TestCorrespondentTextIsQuotedNotVoiced(t *testing.T) {
	n := Note{From: "a shop", State: Delayed, Text: "your order is late", At: now()}
	s := n.Sentence()
	if !strings.Contains(s, "Their words:") || !strings.Contains(s, `"your order is late"`) {
		t.Fatalf("sentence = %q, want the correspondent's words attributed and quoted", s)
	}
}
