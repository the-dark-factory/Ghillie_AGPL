package identity

import (
	"encoding/json"
	"fmt"
	"reflect"
	"strings"
	"testing"
)

// secret is the Apple account reference used throughout. It is deliberately
// distinctive so that a leak anywhere is unmistakable in an assertion.
const secret = "apple-acct-000111222333-PURCHASER"

// TestTheFourIdentitiesAreDistinctTypes is the identity-separation test.
//
// ★ THE REAL GUARANTEE IS A COMPILE-TIME ONE and cannot be written as a runtime
// assertion: ClawID, OwnerID and UserID are distinct defined types, so
// `var c ClawID = someOwnerID` does not compile and one can never be passed
// where another is wanted. What this test does is stop that guarantee being
// quietly removed — if anyone collapses them into one type or into bare string,
// the reflect checks below fail and the commit has to argue for itself.
func TestTheFourIdentitiesAreDistinctTypes(t *testing.T) {
	claw := ClawID("claw-1")
	owner := OwnerID("acme-it")
	user := UserID("tony")
	apple := NewAppleAccountRef(secret)

	types := map[string]reflect.Type{
		"ClawID":          reflect.TypeOf(claw),
		"OwnerID":         reflect.TypeOf(owner),
		"UserID":          reflect.TypeOf(user),
		"AppleAccountRef": reflect.TypeOf(apple),
	}
	seen := map[reflect.Type]string{}
	for name, typ := range types {
		if other, dup := seen[typ]; dup {
			t.Errorf("%s and %s are THE SAME TYPE — two identities have been conflated, which is the classic authorisation bug", name, other)
		}
		seen[typ] = name
		if typ.Kind() == reflect.String && typ == reflect.TypeOf("") {
			t.Errorf("%s is a bare string — it must be a defined type so it cannot be substituted for another identity", name)
		}
	}

	// The three string-backed identities must not be assignable to one another
	// even though they share an underlying kind.
	if reflect.TypeOf(claw).AssignableTo(reflect.TypeOf(owner)) {
		t.Error("a ClawID is assignable to an OwnerID — a device could stand in for its owner")
	}
	if reflect.TypeOf(user).AssignableTo(reflect.TypeOf(owner)) {
		t.Error("a UserID is assignable to an OwnerID — the person at the keyboard could stand in for the authority that may revoke them")
	}
	if reflect.TypeOf(user).AssignableTo(reflect.TypeOf(claw)) {
		t.Error("a UserID is assignable to a ClawID — a person could stand in for a device")
	}
}

// TestAppleAccountNeverRendersItself is the PII test.
//
// The reference must not escape through ANY of the ordinary routes: printing,
// formatting with any verb, JSON encoding, or being embedded in a larger value
// that is then printed or encoded. Those are exactly the routes by which a log
// line, an outcome report and a quarantine file get written.
func TestAppleAccountNeverRendersItself(t *testing.T) {
	apple := NewAppleAccountRef(secret)

	renderings := map[string]string{
		"String()": apple.String(),
		"%s":       fmt.Sprintf("%s", apple),
		"%v":       fmt.Sprintf("%v", apple),
		"%+v":      fmt.Sprintf("%+v", apple),
		"%#v":      fmt.Sprintf("%#v", apple),
		"%q":       fmt.Sprintf("%q", apple),
		"%d":       fmt.Sprintf("%d", apple),
		"Print":    fmt.Sprint(apple),
	}
	for how, out := range renderings {
		if strings.Contains(out, secret) {
			t.Errorf("%s LEAKED the purchaser reference: %s", how, out)
		}
		if !strings.Contains(out, RedactedAppleAccount) {
			t.Errorf("%s did not render the redaction, got %q", how, out)
		}
	}

	encoded, err := json.Marshal(apple)
	if err != nil {
		t.Fatalf("MarshalJSON: %v", err)
	}
	if strings.Contains(string(encoded), secret) {
		t.Errorf("JSON encoding LEAKED the purchaser reference: %s", encoded)
	}
	if !strings.Contains(string(encoded), RedactedAppleAccount) {
		t.Errorf("JSON encoding did not carry the redaction: %s", encoded)
	}

	// Embedded in something bigger — the shape an outcome report or a
	// quarantine notice actually has.
	type notice struct {
		Ref   string          `json:"ref"`
		Buyer AppleAccountRef `json:"buyer"`
	}
	body, err := json.Marshal(notice{Ref: "deadbeef", Buyer: apple})
	if err != nil {
		t.Fatalf("marshal notice: %v", err)
	}
	if strings.Contains(string(body), secret) {
		t.Errorf("an embedded AppleAccountRef LEAKED into a notice: %s", body)
	}

	// The one legitimate way out, which exists for the enrolment payload only.
	if apple.Reveal() != secret {
		t.Errorf("Reveal did not return the reference — the enrolment binding could not be established")
	}
}

// TestBindingRendersWithThePurchaserRedacted covers the type that actually gets
// logged at startup.
func TestBindingRendersWithThePurchaserRedacted(t *testing.T) {
	b := Binding{
		Claw:  ClawID("claw-1"),
		Owner: OwnerID("acme-it"),
		User:  UserID("tony"),
		Apple: NewAppleAccountRef(secret),
	}
	for _, out := range []string{b.String(), fmt.Sprintf("%v", b), fmt.Sprintf("%+v", b), fmt.Sprint(b)} {
		if strings.Contains(out, secret) {
			t.Errorf("a Binding rendering LEAKED the purchaser: %s", out)
		}
	}
	if !strings.Contains(b.String(), "claw-1") || !strings.Contains(b.String(), "acme-it") || !strings.Contains(b.String(), "tony") {
		t.Errorf("the binding should still name the three non-PII identities: %s", b.String())
	}
}

// TestBindingValidation checks which identities are required. The purchaser is
// OPTIONAL: a claw with nobody's credits behind it is an ordinary unpaid
// install and refusing to enrol it would be wrong.
func TestBindingValidation(t *testing.T) {
	tests := []struct {
		name    string
		binding Binding
		wantErr bool
	}{
		{
			name:    "complete",
			binding: Binding{Claw: "c", Owner: "o", User: "u", Apple: NewAppleAccountRef(secret)},
		},
		{
			name:    "no purchaser is lawful",
			binding: Binding{Claw: "c", Owner: "o", User: "u"},
		},
		{
			name:    "no claw",
			binding: Binding{Owner: "o", User: "u"},
			wantErr: true,
		},
		{
			name:    "no owner — enrolment is an owner act",
			binding: Binding{Claw: "c", User: "u"},
			wantErr: true,
		},
		{
			name:    "no user",
			binding: Binding{Claw: "c", Owner: "o"},
			wantErr: true,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.binding.Validate()
			if (err != nil) != tt.wantErr {
				t.Errorf("Validate() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}

// TestPresentDisclosesNothing checks that asking whether a purchaser is bound
// does not reveal who.
func TestPresentDisclosesNothing(t *testing.T) {
	if NewAppleAccountRef("").Present() {
		t.Error("an empty reference reports Present")
	}
	if !NewAppleAccountRef(secret).Present() {
		t.Error("a bound reference does not report Present")
	}
	if NewAppleAccountRef("   ").Present() {
		t.Error("whitespace counted as a bound purchaser")
	}
}
