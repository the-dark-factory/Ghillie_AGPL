package credit

import (
	"context"
	"errors"
	"reflect"
	"strings"
	"testing"
)

// stubAuthority is a facade that says whatever the test tells it to.
type stubAuthority struct {
	decision Decision
	err      error
	asked    []string
}

func (s *stubAuthority) Decide(_ context.Context, act string) (Decision, error) {
	s.asked = append(s.asked, act)
	return s.decision, s.err
}

// TestCourtesyCannotAuthoriseAnything is the structural half of the courtesy /
// authority split.
//
// ★ THE GUARANTEE IS THE ABSENCE OF A METHOD. Courtesy has no Sufficient, no
// Spend, no CanAfford and no comparison against a price — there is nothing a
// caller can do with it except render it. This test asserts that absence, so
// that adding such a method has to be a deliberate, argued change rather than a
// convenience someone slips in.
func TestCourtesyCannotAuthoriseAnything(t *testing.T) {
	typ := reflect.TypeOf(Courtesy{})
	forbidden := []string{"Sufficient", "Spend", "CanAfford", "Authorise", "Authorize", "Allow", "Permit", "Charge", "Deduct"}

	for i := range typ.NumMethod() {
		name := typ.Method(i).Name
		for _, bad := range forbidden {
			if strings.EqualFold(name, bad) {
				t.Errorf("Courtesy has a method %q — a locally-computed number must never be able to authorise an act", name)
			}
		}
		// Any method returning a bare bool is the shape of a permission.
		m := typ.Method(i).Type
		for r := range m.NumOut() {
			if m.Out(r).Kind() == reflect.Bool {
				t.Errorf("Courtesy.%s returns a bool — that is the shape of a permission, and the local balance never grants one", name)
			}
		}
	}
	// Pointer receiver methods too.
	ptr := reflect.TypeOf(&Courtesy{})
	for i := range ptr.NumMethod() {
		m := ptr.Method(i)
		for r := range m.Type.NumOut() {
			if m.Type.Out(r).Kind() == reflect.Bool {
				t.Errorf("(*Courtesy).%s returns a bool — the local balance never grants a permission", m.Name)
			}
		}
	}
}

// TestTheFacadeDecidesEvenWhenTheLocalNumberIsGenerous is the credit
// courtesy-vs-authority test the definition of done asks for.
//
// A locally-displayed balance of ten thousand does not move the verdict by a
// single unit. The only thing that decides is what came back from the facade.
func TestTheFacadeDecidesEvenWhenTheLocalNumberIsGenerous(t *testing.T) {
	generous := Courtesy{Units: 10_000, AsOf: "2026-07-30T00:00:00Z"}
	if !strings.Contains(generous.Line(), "10000") {
		t.Fatalf("the courtesy line should still display the number: %s", generous.Line())
	}
	if !strings.Contains(generous.Line(), "courtesy") || !strings.Contains(generous.Line(), "decides") {
		t.Errorf("the courtesy line must label itself advisory and name who decides: %s", generous.Line())
	}

	facadeSaysNo := &stubAuthority{decision: Decision{Sufficient: false, Note: "no credit on this account", Balance: 0}}
	if _, err := Authorise(context.Background(), facadeSaysNo, "conduct-interview"); !errors.Is(err, ErrNoCredit) {
		t.Fatalf("with the facade saying no, Authorise returned %v — the act was not refused", err)
	}

	facadeSaysYes := &stubAuthority{decision: Decision{Sufficient: true, Note: "fine", Balance: 3}}
	d, err := Authorise(context.Background(), facadeSaysYes, "conduct-interview")
	if err != nil {
		t.Fatalf("with the facade saying yes, Authorise refused: %v", err)
	}
	if d.Balance != 3 {
		t.Errorf("the FACADE's balance is the one reported, got %d want 3", d.Balance)
	}
	if len(facadeSaysYes.asked) != 1 || facadeSaysYes.asked[0] != "conduct-interview" {
		t.Errorf("the act was not named to the authority: %v", facadeSaysYes.asked)
	}
}

// TestUnreachableAuthorityRefuses checks that the failure is CLOSED. A facade
// that cannot be asked has not said yes.
func TestUnreachableAuthorityRefuses(t *testing.T) {
	tests := []struct {
		name      string
		authority Authority
		wantErr   error
	}{
		{
			name:      "no authority configured at all",
			authority: nil,
			wantErr:   ErrAuthorityUnreachable,
		},
		{
			name:      "the facade could not be reached",
			authority: &stubAuthority{err: errors.New("connection refused")},
			wantErr:   ErrAuthorityUnreachable,
		},
		{
			name:      "the facade said no",
			authority: &stubAuthority{decision: Decision{Sufficient: false}},
			wantErr:   ErrNoCredit,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := Authorise(context.Background(), tt.authority, "conduct-interview")
			if err == nil {
				t.Fatal("the act was AUTHORISED — an absent answer is not a yes")
			}
			if !errors.Is(err, tt.wantErr) {
				t.Errorf("error = %v, want one wrapping %v", err, tt.wantErr)
			}
		})
	}
}

// TestRefusalIsLegible checks that a refusal carries the facade's own words
// through to the client, rather than becoming a bare "denied".
func TestRefusalIsLegible(t *testing.T) {
	note := "top up in the companion app and ghillie will pick it up on the next poll"
	a := &stubAuthority{decision: Decision{Sufficient: false, Note: note}}
	_, err := Authorise(context.Background(), a, "conduct-interview")
	if err == nil {
		t.Fatal("not refused")
	}
	if !strings.Contains(err.Error(), note) {
		t.Errorf("the refusal lost the facade's explanation: %v", err)
	}
	if !strings.Contains(err.Error(), "conduct-interview") {
		t.Errorf("the refusal does not say which act was refused: %v", err)
	}
}

// TestCostGuidanceIsAvailableAndHonest checks that ghillie has the required
// line and that it says the true thing rather than a sales thing.
func TestCostGuidanceIsAvailableAndHonest(t *testing.T) {
	g := strings.ToLower(CostGuidance())
	if g == "" {
		t.Fatal("there is no cost guidance — ghillie is required to be open about cost")
	}
	if !strings.Contains(g, "costs") && !strings.Contains(g, "cost") {
		t.Errorf("the guidance does not mention cost: %s", g)
	}
	if !strings.Contains(g, "precise") && !strings.Contains(g, "detail") {
		t.Errorf("the guidance does not tie detail to cost, which is the honest part: %s", g)
	}
}

// TestStaleCourtesySaysSo checks that an unrefreshed number is labelled, since
// showing an old figure without saying so is the beginning of it being trusted.
func TestStaleCourtesySaysSo(t *testing.T) {
	if strings.Contains(Courtesy{Units: 5}.Line(), "not refreshed") {
		t.Error("a fresh courtesy claimed to be stale")
	}
	if !strings.Contains(Courtesy{Units: 5, Stale: true}.Line(), "not refreshed") {
		t.Error("a stale courtesy did not say so")
	}
}
