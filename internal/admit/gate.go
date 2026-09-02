package admit

import (
	"context"
	"fmt"
	"strings"

	"github.com/tonygair/ghillie/internal/bundle"
)

// ParseFloor reads the owner's declared proof floor for this install site.
//
// The floor is GHILLIE'S policy, not the ability's property, and it is the
// field that lets one proven core serve hosts with entirely different proof
// ambitions. "none" is an honest answer and is the default: an owner who has
// declared no floor still gets operator, origin, integrity and containment
// decided for them, and can raise the floor later without a new decider,
// a new proof, or anybody's permission.
func ParseFloor(s string) (Floor, error) {
	switch strings.ToLower(strings.TrimSpace(s)) {
	case "", "none":
		return FloorNone, nil
	case "carried":
		return FloorCarried, nil
	case "reproved":
		return FloorReprovedHere, nil
	}
	return FloorNone, fmt.Errorf("admit: %q is not a floor — say none, carried or reproved", s)
}

// GateFor returns a bundle.Gate that establishes the six facts over the
// DESTINATION COPY and puts them to the proven decider.
//
// catalogueBase names the catalogue when this is a catalogue install, and is
// empty for an operator-supplied directory; the two take different
// constructors because the attesting authority genuinely differs, and
// pretending otherwise is how an unattested install comes to look attested.
//
// advisory reports a refusal and lets the install proceed. It exists because
// an owner turning a gate on for the first time is entitled to see what it
// would refuse before it starts refusing — and every advisory pass is
// stamped into the ledger, so an override is visible for ever after.
func GateFor(ctx context.Context, c Candidate, catalogueBase string, advisory bool) bundle.Gate {
	return func(destDir string) (bundle.Decision, error) {
		c.DestDir = destDir

		var (
			facts Facts
			err   error
		)
		if catalogueBase == "" {
			facts, err = FromDirectory(ctx, c)
		} else {
			facts, err = FromCatalogue(ctx, c, catalogueBase)
		}
		if err != nil {
			// The facts could not be established, so there is no tuple to
			// decide over. That is a refusal, not a pass.
			return bundle.Decision{
				Verdict:  RefuseUndecided.String(),
				Admitted: false,
				Reason:   err.Error(),
				Advisory: advisory,
			}, err
		}

		verdict, reason, decideErr := Decide(ctx, facts)
		d := bundle.Decision{
			Verdict:   verdict.String(),
			Admitted:  verdict.Admitted(),
			Reason:    verdict.Remedy(),
			Facts:     facts.String(),
			Floor:     facts.Floor().String(),
			Authority: facts.Authority(),
			Digest:    facts.Digest(),
			Advisory:  advisory,
		}
		if !verdict.Admitted() && reason != "" && verdict == RefuseUndecided {
			d.Reason = reason + " — " + verdict.Remedy()
		}
		return d, decideErr
	}
}
