package main

// enrolcode.go — the `-enrol-code` verb: bind this machine to a membership.
//
// One round trip and out. The member is shown a pairing code on their
// account page, reads it to this terminal, and this machine signs it with
// its own device key and offers it. The factory's PROVEN core decides
// (Enrolment_Admission_Pkg, ada-factory ledger 226) and its word is printed
// here verbatim, admitted or refused.
//
// ⚠ NOT -enrol. That verb is the facade ceremony (Claw_Enrolment_Pkg,
// ledger 113) — who may submit this machine to a factory. This one ties the
// device key to a CUSTOMER MEMBERSHIP so the factory knows whose ghillie
// this is. See internal/bind's package doc; the two are separate promises
// and neither implies the other.

import (
	"context"
	"crypto/ed25519"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/tonygair/ghillie/internal/bind"
)

// runEnrolCode performs the binding and reports it. It never retries and
// never softens a refusal: the factory's word is the answer.
func runEnrolCode(ctx context.Context, o options) error {
	portal := strings.TrimRight(strings.TrimSpace(o.portalURL), "/")
	if portal == "" {
		return errors.New("-portal is empty: name the customer area to offer this key to, or leave the flag off for " + bind.DefaultPortal)
	}
	if bind.Canonical(o.enrolCode) == "" {
		return fmt.Errorf("%q is not a pairing code — it is the short code your account page showed you, "+
			"like K7QM-3XBP", o.enrolCode)
	}

	// The DEVICE KEY is this machine's identity, resolved exactly as the
	// polling path resolves it, so the key that binds is the key that
	// afterwards works. -device-seed remains the demo affordance it is
	// everywhere else, and says so out loud.
	var deviceKey ed25519.PrivateKey
	if o.deviceSeed != "" {
		fmt.Println("⚠ device key derived from -device-seed — a DEMO AFFORDANCE; a real install keeps a generated key")
		deviceKey = deviceKeyFromSeed(o.deviceSeed)
	} else {
		path := defaultTo(o.deviceKey, filepath.Join(filepath.Dir(o.stateFile), "ghillie-device.key"))
		key, created, err := loadOrCreateDeviceKey(path)
		if err != nil {
			return err
		}
		if created {
			fmt.Printf("device key generated and kept at %s (0600) — this machine's identity now lives here and nowhere else\n", path)
		}
		deviceKey = key
	}
	pub, ok := deviceKey.Public().(ed25519.PublicKey)
	if !ok {
		return errors.New("the device key has no public half — refusing to offer a key that is not a key")
	}

	fmt.Printf("offering this machine's key %s to %s\n", bind.Fingerprint(pub), portal)

	result, err := bind.Client{Base: portal}.Offer(ctx, o.enrolCode, deviceKey)
	if err != nil {
		// NO VERDICT WAS OBTAINED. That is not a refusal, and saying so
		// plainly matters: a member told "refused" would go looking for a
		// fault in their code or their key, when the truth is that nobody
		// ever decided anything about either.
		return fmt.Errorf("%w\n\nNothing was decided and nothing was bound. Your pairing code has NOT been used up — "+
			"it is still good until it expires, so this can simply be tried again", err)
	}

	if !result.Admitted {
		// The proven core's own word first, then the sentence. The word is
		// the thing to quote when asking why.
		fmt.Printf("\nREFUSED — %s\n%s\n", result.Verdict, bind.Explain(result.Verdict))
		fmt.Println("\nNothing was bound, and nothing on this machine changed.")
		return fmt.Errorf("the factory refused this binding: %s", result.Verdict)
	}

	// Admitted. Say the one thing the member should check by eye.
	fmt.Printf("\nADMITTED — %s\n", result.Verdict)
	fmt.Printf("key fingerprint : %s\n", result.Fingerprint)
	if result.AccountID != "" {
		fmt.Printf("bound to        : %s\n", result.AccountID)
	}
	fmt.Println("\nYou are bound. That fingerprint is now on your account page — if the two do not match,")
	fmt.Println("the key there is not this machine, and that is worth telling us about.")

	// A last, honest caution: the fingerprint the portal echoed should be
	// the one we computed. If it is not, something between here and there
	// is not carrying what we sent.
	if want := bind.Fingerprint(pub); result.Fingerprint != "" && result.Fingerprint != want {
		fmt.Fprintf(os.Stderr,
			"\n⚠ the factory named %s but this machine's key is %s — something in between is not carrying what was sent\n",
			result.Fingerprint, want)
	}
	return nil
}
