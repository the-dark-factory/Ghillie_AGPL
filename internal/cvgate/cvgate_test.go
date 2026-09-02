package cvgate

import (
	"os"
	"path/filepath"
	"testing"
)

// wireRealDecider points the gate at the REAL proven front, the house test
// convention (fill, guard): honour an already-set env, else the well-known
// build path, else skip — a run without the decider proves nothing about the
// ladder and must say so rather than fail.
func wireRealDecider(t *testing.T) {
	t.Helper()
	if os.Getenv(EnvDecider) != "" {
		return
	}
	p := filepath.Join(os.Getenv("HOME"), "dev", "ada-factory", "wu-cv-disclosure", "edge", "cv_disclosure_front")
	if _, err := os.Stat(p); err != nil {
		t.Skipf("real cv disclosure front not present: %v", err)
	}
	t.Setenv(EnvDecider, p)
}

// canaryCV mirrors ObVault/experiments/canary-identity — the same synthetic
// values the leakage scan greps for, so this test and the demo measure the
// same thing.
func canaryCV() *CV {
	return New(map[Field]string{
		Sector: "public sector", Seniority: "senior", Region: "North East England",
		Years: "25", Skills: "stakeholder engagement, ceremonial duties",
		Surname: "Casterbridge-Vane", Postcode: "ZZ7 4QX",
		Email: "r.casterbridge-vane@example.com", Phone: "07700 900518",
		NINumber:    "QQ 51 84 03 C",
		RefereeName: "Marchmont Halloway", RefereePhone: "07700 900742",
	})
}

// TestTheLadder is the product in one table: what an enquirer earns, and when.
func TestTheLadder(t *testing.T) {
	wireRealDecider(t)
	tests := []struct {
		name         string
		c            Conditions
		wantOK       []Field
		wantWithheld []Field
	}{
		{
			name:         "a cold agency gets the useful half and nothing personal",
			c:            Conditions{},
			wantOK:       []Field{Sector, Seniority, Region, Years, Skills},
			wantWithheld: []Field{Surname, Postcode, Email, Phone, NINumber, RefereeName, RefereePhone},
		},
		{
			name:         "naming the client alone is not enough",
			c:            Conditions{ClientNamed: true},
			wantWithheld: []Field{Surname, Email, Phone},
		},
		{
			name:         "client + vacancy earns a name to address",
			c:            Conditions{ClientNamed: true, VacancyRef: true},
			wantOK:       []Field{Surname, Postcode},
			wantWithheld: []Field{Email, Phone, NINumber},
		},
		{
			name:         "stating the salary earns contact by email",
			c:            Conditions{ClientNamed: true, VacancyRef: true, SalaryStated: true},
			wantOK:       []Field{Surname, Email},
			wantWithheld: []Field{Phone, NINumber, RefereeName},
		},
		{
			name:         "agreeing not to forward earns the phone",
			c:            Conditions{ClientNamed: true, VacancyRef: true, SalaryStated: true, NoForwarding: true},
			wantOK:       []Field{Surname, Email, Phone, RefereePhone},
			wantWithheld: []Field{NINumber, RefereeName},
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			d := canaryCV().Answer(tc.c)
			for _, f := range tc.wantOK {
				if _, ok := d.Released[f]; !ok {
					t.Errorf("%s should have been released: %s", f, d.Withheld[f])
				}
			}
			for _, f := range tc.wantWithheld {
				if v, leaked := d.Released[f]; leaked {
					t.Errorf("LEAK: %s released as %q", f, v)
				}
			}
		})
	}
}

// TestNoConditionSetUnlocksTier4 — the NI number and the referee's name are the
// owner's to give, and no amount of agency co-operation earns them. Tested
// exhaustively over every combination of the four agency-side conditions.
func TestNoConditionSetUnlocksTier4(t *testing.T) {
	for i := 0; i < 16; i++ {
		c := Conditions{
			ClientNamed:  i&1 != 0,
			VacancyRef:   i&2 != 0,
			SalaryStated: i&4 != 0,
			NoForwarding: i&8 != 0,
		}
		d := canaryCV().Answer(c)
		for _, f := range []Field{NINumber, RefereeName} {
			if _, leaked := d.Released[f]; leaked {
				t.Fatalf("LEAK: %s released with conditions %+v and no owner consent", f, c)
			}
		}
	}
}

// TestOwnerConsentOpensEverything — the owner is not locked out of their own CV.
func TestOwnerConsentOpensEverything(t *testing.T) {
	wireRealDecider(t)
	d := canaryCV().Answer(Conditions{OwnerConsent: true})
	if len(d.Withheld) != 0 {
		t.Fatalf("owner consent should release all; withheld %v", d.WithheldFields())
	}
}

// TestRefusalsAreActionable — a flat "no" is worth less to both sides than a
// no that says what is still owed.
func TestRefusalsAreActionable(t *testing.T) {
	wireRealDecider(t)
	d := canaryCV().Answer(Conditions{})
	for _, f := range []Field{Surname, Email, Phone} {
		why := d.Withheld[f]
		if why == "" {
			t.Errorf("%s withheld with no reason given", f)
		}
		if !contains(why, "name the end client") {
			t.Errorf("%s reason should name what is owed, got %q", f, why)
		}
	}
	if why := d.Withheld[NINumber]; !contains(why, "owner") {
		t.Errorf("tier-4 refusal should say it needs the owner, got %q", why)
	}
}

// TestUnknownFieldFailsClosed — a field the ladder does not know is withheld,
// not waved through.
func TestUnknownFieldFailsClosed(t *testing.T) {
	if ok, _ := MayRelease(Field("passport-number"), Conditions{OwnerConsent: true}); ok {
		t.Fatal("an unknown field was released — the gate failed OPEN")
	}
}

func contains(h, n string) bool {
	return len(h) >= len(n) && (h == n || len(n) == 0 || indexOf(h, n) >= 0)
}
func indexOf(h, n string) int {
	for i := 0; i+len(n) <= len(h); i++ {
		if h[i:i+len(n)] == n {
			return i
		}
	}
	return -1
}

// --- front-decided tests -----------------------------------------------------
// The tests above run against whatever decider is wired. These pin the WIRING
// itself: that the verdict comes from the proven core, and that its absence
// refuses rather than waves through.

func TestUnwiredDeciderRefusesEverything(t *testing.T) {
	t.Setenv(EnvDecider, "")
	d := canaryCV().Answer(Conditions{OwnerConsent: true})
	if len(d.Released) != 0 {
		t.Fatalf("with no decider wired, %v was released — the gate failed OPEN",
			d.ReleasedFields())
	}
	if why := d.Withheld[Phone]; !contains(why, "no proven decider") {
		t.Errorf("refusal should name the missing decider, got %q", why)
	}
}

func TestMissingDeciderBinaryRefuses(t *testing.T) {
	t.Setenv(EnvDecider, "/nonexistent/cv_disclosure_front")
	if ok, _ := MayRelease(Sector, Conditions{OwnerConsent: true}); ok {
		t.Fatal("a missing decider binary released a field — failed OPEN")
	}
}
