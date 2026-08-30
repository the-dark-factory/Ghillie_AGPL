package keys

import (
	"bytes"
	"crypto/ed25519"
	"errors"
	"os"
	"path/filepath"
	"testing"
)

func TestLoadOrCreate(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "ghillie-encrypt.key")

	id1, created, err := LoadOrCreate(path)
	if err != nil {
		t.Fatalf("first load: %v", err)
	}
	if !created {
		t.Error("first load should create")
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatalf("stat: %v", err)
	}
	if perm := info.Mode().Perm(); perm != 0o600 {
		t.Errorf("key file mode %o, want 0600", perm)
	}

	id2, created, err := LoadOrCreate(path)
	if err != nil {
		t.Fatalf("second load: %v", err)
	}
	if created {
		t.Error("second load should reuse, not create")
	}
	if id1.Recipient().String() != id2.Recipient().String() {
		t.Error("reload produced a different identity")
	}

	if err := os.WriteFile(path, []byte("not a key\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, _, err := LoadOrCreate(path); !errors.Is(err, ErrNoIdentity) {
		t.Errorf("malformed file: err = %v, want ErrNoIdentity", err)
	}
}

func TestBindingAndSealing(t *testing.T) {
	dir := t.TempDir()
	id, _, err := LoadOrCreate(filepath.Join(dir, "a.key"))
	if err != nil {
		t.Fatal(err)
	}
	otherID, _, err := LoadOrCreate(filepath.Join(dir, "b.key"))
	if err != nil {
		t.Fatal(err)
	}
	devicePub, deviceKey, err := ed25519.GenerateKey(nil)
	if err != nil {
		t.Fatal(err)
	}
	imposterPub, _, err := ed25519.GenerateKey(nil)
	if err != nil {
		t.Fatal(err)
	}

	bound := Bind(id, deviceKey)
	plaintext := []byte("IDENTIFICATION DIVISION. PROGRAM-ID. SECRET.")

	tests := []struct {
		name string
		run  func(t *testing.T)
	}{
		{
			name: "verified recipient round-trips",
			run: func(t *testing.T) {
				to, err := VerifiedRecipient(bound, devicePub)
				if err != nil {
					t.Fatalf("verify: %v", err)
				}
				sealed, err := Seal(plaintext, to)
				if err != nil {
					t.Fatalf("seal: %v", err)
				}
				got, err := Open(sealed, id)
				if err != nil {
					t.Fatalf("open: %v", err)
				}
				if !bytes.Equal(got, plaintext) {
					t.Error("round-trip mismatch")
				}
			},
		},
		{
			name: "impostor device key refuses the binding",
			run: func(t *testing.T) {
				if _, err := VerifiedRecipient(bound, imposterPub); !errors.Is(err, ErrBadBinding) {
					t.Errorf("err = %v, want ErrBadBinding", err)
				}
			},
		},
		{
			name: "swapped recipient refuses the binding",
			run: func(t *testing.T) {
				forged := Binding{Recipient: otherID.Recipient().String(), Signature: bound.Signature}
				if _, err := VerifiedRecipient(forged, devicePub); !errors.Is(err, ErrBadBinding) {
					t.Errorf("err = %v, want ErrBadBinding", err)
				}
			},
		},
		{
			name: "wrong key cannot open",
			run: func(t *testing.T) {
				to, err := VerifiedRecipient(bound, devicePub)
				if err != nil {
					t.Fatal(err)
				}
				sealed, err := Seal(plaintext, to)
				if err != nil {
					t.Fatal(err)
				}
				if _, err := Open(sealed, otherID); err == nil {
					t.Error("open with the wrong identity succeeded")
				}
			},
		},
		{
			name: "tampered ciphertext refuses",
			run: func(t *testing.T) {
				to, err := VerifiedRecipient(bound, devicePub)
				if err != nil {
					t.Fatal(err)
				}
				sealed, err := Seal(plaintext, to)
				if err != nil {
					t.Fatal(err)
				}
				sealed[len(sealed)-1] ^= 0x01
				if _, err := Open(sealed, id); err == nil {
					t.Error("tampered ciphertext opened")
				}
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, tt.run)
	}
}
