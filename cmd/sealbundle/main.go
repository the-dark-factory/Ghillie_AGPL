// Command sealbundle is the factory's side of the confidential tier: it turns
// a built source bundle into a delivery sealed to ONE enrolled claw, and it
// mints the factory's own encryption identity for the return direction.
//
// Two subcommands, deliberately tiny:
//
//	sealbundle -gen -key-file factory-encrypt.key
//	    Generate (once) the factory's X25519 identity and print its public
//	    recipient. The private file is 0600; publishing the recipient SIGNED
//	    (by the admitter's ceremony, alongside the verify receipts) is the
//	    operator's step, recorded where the receipts are.
//
//	sealbundle -to-claw binding.json -device-pub <hex> -in bundle.tar.gz -out bundle.tar.gz.age
//	    Seal a bundle to a claw's published encryption key — but only after
//	    the binding verifies against the claw's device public key. An
//	    unverified recipient is never sealed to: that refusal is the whole
//	    reason the binding exists.
//
// The mechanism is open on purpose; only payloads are confidential.
package main

import (
	"crypto/ed25519"
	"encoding/hex"
	"encoding/json"
	"flag"
	"fmt"
	"os"

	"github.com/tonygair/ghillie/internal/keys"
)

func main() {
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, "sealbundle:", err)
		os.Exit(1)
	}
}

func run() error {
	var (
		gen       = flag.Bool("gen", false, "generate (once) the factory encryption identity and print its recipient")
		keyFile   = flag.String("key-file", "factory-encrypt.key", "the factory's X25519 identity file (age format, 0600)")
		binding   = flag.String("to-claw", "", "JSON file holding the claw's published binding {encrypt_pubkey, encrypt_pubkey_sig}")
		devicePub = flag.String("device-pub", "", "the claw's Ed25519 device public key, hex — the binding must verify against it")
		in        = flag.String("in", "", "the built bundle archive to seal")
		out       = flag.String("out", "", "where the sealed delivery is written")
	)
	flag.Parse()

	if *gen {
		id, created, err := keys.LoadOrCreate(*keyFile)
		if err != nil {
			return err
		}
		if created {
			fmt.Fprintf(os.Stderr, "identity generated and kept at %s (0600)\n", *keyFile)
		}
		fmt.Println(id.Recipient().String())
		return nil
	}

	if *binding == "" || *devicePub == "" || *in == "" || *out == "" {
		return fmt.Errorf("sealing needs -to-claw, -device-pub, -in and -out (or -gen to mint the factory identity)")
	}
	raw, err := os.ReadFile(*binding)
	if err != nil {
		return err
	}
	var b keys.Binding
	if err := json.Unmarshal(raw, &b); err != nil {
		return fmt.Errorf("binding %s: %w", *binding, err)
	}
	pub, err := hex.DecodeString(*devicePub)
	if err != nil || len(pub) != ed25519.PublicKeySize {
		return fmt.Errorf("-device-pub must be %d hex-encoded bytes", ed25519.PublicKeySize)
	}
	to, err := keys.VerifiedRecipient(b, ed25519.PublicKey(pub))
	if err != nil {
		return fmt.Errorf("REFUSED to seal: %w", err)
	}
	plain, err := os.ReadFile(*in)
	if err != nil {
		return err
	}
	sealed, err := keys.Seal(plain, to)
	if err != nil {
		return err
	}
	if err := os.WriteFile(*out, sealed, 0o644); err != nil {
		return err
	}
	fmt.Fprintf(os.Stderr, "sealed %s (%d bytes) -> %s (%d bytes) for %s\n",
		*in, len(plain), *out, len(sealed), b.Recipient)
	return nil
}
