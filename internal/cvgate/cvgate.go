// Package cvgate is the owner's CV and the rule about who may see which part
// of it.
//
// THE PRODUCT IS THE REFUSAL. Ghillie holds the document locally — it never
// sits on an agency's database — and hands out the least that answers the
// question. An enquirer earns fields by MEETING CONDITIONS, not by asking
// nicely and not by asking twice.
//
// ★ The point is REACH, not caution (feedback_constraints_enable_creativity_framing).
// Because the boundary is enforced and every release is recorded, the owner can
// let ghillie talk to FAR more agencies, and talk more freely, than they would
// ever let an ungated agent do with the real document. An agent that cannot
// prove where its line is has to be kept away from anything that matters.
//
// # The ladder
//
// Fields unlock in tiers as an enquirer establishes more about itself. Nothing
// personal is free, and the top tier is not reachable by an agency at all — the
// owner grants it per request, in person.
//
//	Tier 0  always            profile facts that identify nobody
//	Tier 1  + named client    the surname; you may address a human
//	        + vacancy ref
//	Tier 2  + salary stated   email; you may contact them
//	Tier 3  + no forwarding   phone; you may ring them
//	Tier 4  owner only        NI number, referee — never unlocked by conditions
//
// # Interim note — the verdict is verdict-nature
//
// Whether a field may be released is a VERDICT, and verdicts belong in a
// forged, proven core fronted as an external decider this shell obeys (the
// Parlour_Case_Pkg pattern; the same interim flag the suggestions pool carries
// on its transition table). The Go below is the stand-in at current volume,
// FLAGGED rather than hidden: when the core is forged, MayRelease moves behind
// its front and this package stops judging.
package cvgate

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"sort"
	"strings"

	"github.com/tonygair/ghillie/internal/gate"
)

// Field names one item of the owner's CV.
type Field string

const (
	// Tier 0 — identify nobody. Deliberately generous: this is the half that
	// makes ghillie USEFUL to a recruiter without costing the owner anything.
	Sector    Field = "sector"
	Seniority Field = "seniority"
	Region    Field = "region"
	Years     Field = "years"
	Skills    Field = "skills"

	// Tier 1 and up — the protected set. Kept in step with the canary
	// identity's protected list (ObVault/experiments/canary-identity).
	Surname      Field = "surname"
	Email        Field = "email"
	Phone        Field = "phone"
	NINumber     Field = "ni-number"
	Postcode     Field = "postcode"
	RefereeName  Field = "referee-name"
	RefereePhone Field = "referee-phone"
)

// Conditions is what an enquirer has ESTABLISHED about itself. Each is a claim
// the enquirer made and ghillie recorded — not a promise ghillie believes on
// trust. An unmet condition is the normal case, not an error.
type Conditions struct {
	// ClientNamed: a real END CLIENT, not "a leading firm in the sector".
	// The single most-refused thing in recruitment, and the reason this skill
	// exists.
	ClientNamed bool
	// VacancyRef: a specific role that exists, identified.
	VacancyRef bool
	// SalaryStated: a figure or a band. Not "competitive".
	SalaryStated bool
	// NoForwarding: the enquirer has agreed not to pass the details on.
	NoForwarding bool
	// OwnerConsent: the owner said yes to THIS request, personally. The only
	// route to Tier 4, and never inferable from the other four.
	OwnerConsent bool
}

// tier mirrors the proven core's ladder FOR REPORTING ONLY — missingFor uses
// it to say what is still owed. It decides nothing; decide() does.
// If this ever disagrees with the core, the CORE is right.
func (c Conditions) tier() int {
	switch {
	case c.OwnerConsent:
		return 4
	case c.ClientNamed && c.VacancyRef && c.SalaryStated && c.NoForwarding:
		return 3
	case c.ClientNamed && c.VacancyRef && c.SalaryStated:
		return 2
	case c.ClientNamed && c.VacancyRef:
		return 1
	default:
		return 0
	}
}

// fieldTier is the ladder. INTERIM — see the package doc; this table is the
// verdict a forged core should own.
var fieldTier = map[Field]int{
	Sector: 0, Seniority: 0, Region: 0, Years: 0, Skills: 0,
	Surname: 1, Postcode: 1,
	Email: 2,
	Phone: 3, RefereePhone: 3,
	NINumber: 4, RefereeName: 4,
}

// EnvDecider names the proven front. Set it to the built
// cv_disclosure_front and the verdict stops being Go's.
const EnvDecider = "CV_DISCLOSURE_DECIDER"

// Errors from the front. Every one is a REFUSAL: an unreachable decider means
// nothing is released, never that everything is.
var (
	ErrDeciderUnwired = errors.New("cvgate: " + EnvDecider + " unset — no proven decider, so nothing is released")
	ErrDeciderMissing = errors.New("cvgate: decider binary not found")
	ErrDeciderRun     = errors.New("cvgate: decider would not run")
	ErrDeciderVerdict = errors.New("cvgate: decider gave no readable verdict")
)

func flag(b bool) string {
	if b {
		return "1"
	}
	return "0"
}

// decide asks the PROVEN core, via the front, whether a field at this tier may
// be released. There is NO LOCAL FALLBACK, deliberately: a shell that answers
// for itself when the decider is absent is a shell that decides, and the whole
// arrangement exists so that it does not. Absent front means refuse.
//
// This mirrors credit.FrontDecider exactly — same shape, same reasoning, same
// refusal-on-absence.
func decide(required int, c Conditions) (bool, error) {
	path := os.Getenv(EnvDecider)
	if path == "" {
		return false, ErrDeciderUnwired
	}
	if _, err := os.Stat(path); err != nil {
		return false, fmt.Errorf("%w: %s: %v", ErrDeciderMissing, path, err)
	}
	out, err := exec.Command(path,
		fmt.Sprint(required),
		flag(c.ClientNamed), flag(c.VacancyRef), flag(c.SalaryStated),
		flag(c.NoForwarding), flag(c.OwnerConsent),
	).Output()
	if err != nil {
		// The front exits 2 and prints "false" on anything malformed. That is
		// a legitimate REFUSAL, not a failure — read it rather than discarding
		// it, but only ever as a refusal.
		if strings.TrimSpace(string(out)) == "false" {
			return false, nil
		}
		return false, fmt.Errorf("%w: %s: %v", ErrDeciderRun, path, err)
	}
	switch strings.TrimSpace(string(out)) {
	case "true":
		return true, nil
	case "false":
		return false, nil
	default:
		return false, fmt.Errorf("%w: %q", ErrDeciderVerdict, strings.TrimSpace(string(out)))
	}
}

// MayRelease reports whether a field may go to an enquirer with these
// conditions, and WHY NOT when it may not. The reason is not decoration: a
// refusal the enquirer can act on ("name the client") is worth more to both
// sides than a flat no, and it is what makes the gate look like cooperation
// rather than obstruction.
func MayRelease(f Field, c Conditions) (bool, string) {
	need, known := fieldTier[f]
	if !known {
		// Fail closed. An unknown field is not a free field.
		return false, fmt.Sprintf("%q is not a field this gate knows, so it is withheld", f)
	}
	// THE CONTROL ARM. Under the `unguarded` build tag the gate is not consulted
	// at all and every field is released. gate.Guarded is a CONSTANT, so in every
	// normal build the compiler deletes this branch and the front is the only
	// route to a verdict — the bypass cannot be reached at runtime, it must be
	// deliberately compiled in.
	//
	// This is what makes the leak APPARENT on camera. A refusal is visually
	// obvious; a disclosure just looks like an agent being helpful. With the
	// canary identity on screen beforehand, the ungated pane emitting
	// "07700 900518" is unmistakable to a viewer who knows nothing about agents.
	if !gate.Guarded {
		return true, ""
	}

	// ★ THE VERDICT IS NOT OURS. Cv_Disclosure_Pkg.May_Release is
	// gnatprove-discharged (2026-08-23, re-proved independently: every
	// postcondition and precondition proved, Always_Terminates proved), and
	// Owner_Only_Holds states as its own theorem that no combination of the
	// four agency-side conditions reaches a tier-4 field. Go asks; Ada decides.
	ok, err := decide(need, c)
	if err != nil {
		// Fail closed and SAY SO. A refusal the reader cannot distinguish from
		// "you did not qualify" would hide a broken decider behind a correct-
		// looking answer.
		return false, "withheld: " + err.Error()
	}
	if ok {
		return true, ""
	}
	return false, missingFor(need, c)
}

// missingFor names what is still owed to reach a tier — the actionable half.
func missingFor(need int, c Conditions) string {
	if need >= 4 {
		return "released only with the owner's personal consent for this request — no set of conditions unlocks it"
	}
	var want []string
	if need >= 1 && !c.ClientNamed {
		want = append(want, "name the end client")
	}
	if need >= 1 && !c.VacancyRef {
		want = append(want, "give the vacancy reference")
	}
	if need >= 2 && !c.SalaryStated {
		want = append(want, "state the salary or band")
	}
	if need >= 3 && !c.NoForwarding {
		want = append(want, "agree not to forward the details")
	}
	if len(want) == 0 {
		return "conditions not met"
	}
	return "withheld until you " + strings.Join(want, ", ")
}

// Disclosure is what one enquiry yielded: what went out, what did not, and the
// reason for each refusal. It is the ledger row AND the answer — the owner can
// always see exactly what left, which is the thing nobody currently has.
type Disclosure struct {
	Released map[Field]string
	Withheld map[Field]string
}

// ReleasedFields and WithheldFields give a stable order for reporting; map
// iteration order would make a demo's output wobble between runs.
func (d Disclosure) ReleasedFields() []Field { return sortedKeys(d.Released) }
func (d Disclosure) WithheldFields() []Field { return sortedKeys(d.Withheld) }

func sortedKeys(m map[Field]string) []Field {
	out := make([]Field, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	sort.Slice(out, func(i, j int) bool { return out[i] < out[j] })
	return out
}

// CV is the owner's document, held locally.
type CV struct{ fields map[Field]string }

// New builds a CV from field values.
func New(fields map[Field]string) *CV {
	cp := make(map[Field]string, len(fields))
	for k, v := range fields {
		cp[k] = v
	}
	return &CV{fields: cp}
}

// Answer runs an enquiry over the whole CV. EVERY field is considered and
// every refusal is reported: a field silently omitted would be indistinguishable
// from a field the owner does not have, and the enquirer could not act on it.
func (cv *CV) Answer(c Conditions) Disclosure {
	d := Disclosure{Released: map[Field]string{}, Withheld: map[Field]string{}}
	for f, v := range cv.fields {
		if ok, why := MayRelease(f, c); ok {
			d.Released[f] = v
		} else {
			d.Withheld[f] = why
		}
	}
	return d
}
