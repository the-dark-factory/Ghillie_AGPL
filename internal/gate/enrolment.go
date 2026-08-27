package gate

// This file mirrors Claw_Enrolment_Pkg (ada-factory ledger 113) — who commands
// this machine, and how that authority may change. As with gate.go and
// freshness.go there is no logic here by design.
//
// The Ada, verbatim:
//
//	function Is_Enrolled (S : Enrolment_State) return Boolean is (S = Enrolled)
//
//	function May_Enrol (S : Enrolment_State; Requester : Actor_Type;
//	                    Authentic : Boolean) return Boolean
//	  is (Authentic and then Requester = Enrolling_Owner and then S = Unenrolled)
//
//	function May_Revoke (S : Enrolment_State; Requester : Actor_Type;
//	                     Authentic : Boolean) return Boolean
//	  is (Authentic and then Requester = Enrolling_Owner and then S = Enrolled)
//
//	function May_Command_Claw (S : Enrolment_State; Requester : Actor_Type;
//	                           Authentic : Boolean; Is_Serving_Grapple : Boolean)
//	  return Boolean
//	  is (Authentic and then Requester = Commanding_Grapple
//	      and then S = Enrolled and then Is_Serving_Grapple)
//
//	function Serves_Exactly_One (S : Enrolment_State; Is_Serving_Grapple : Boolean)
//	  return Boolean is (Is_Enrolled (S) = Is_Serving_Grapple)
//
// The three load-bearing properties, in the core's own words:
//
//   - ENROLMENT-IS-AN-OWNER-ACT. Authority transfers only by a deliberate act of
//     the enrolling owner. A Grapple may never recruit a claw.
//   - NO-TWO-MASTERS. A claw serves at most one Grapple and cannot be silently
//     re-enrolled while already serving.
//   - REVOCATION-BELONGS-TO-THE-OWNER, never to the person at the keyboard.
//     Owner and user are therefore DISTINCT PRINCIPALS — which is why
//     internal/identity gives them distinct Go types that cannot be swapped.
//
// SCOPE BOUNDARY, restated from the .ads because it is easy to lose in
// translation: this core DECIDES only. It verifies no signatures and identifies
// nobody. Whether a request genuinely came from the claimed party is a signature
// question answered elsewhere and handed in as the Authentic Boolean.
//
// HONEST LABEL: unproven shim, cross-checked against ledger 113 by exhaustive
// table test. Where this Go and the Ada disagree, THE ADA IS RIGHT.

// EnrolmentState is Claw_Enrolment_Pkg.Enrolment_State, in the same order. One
// binary, two lawful states, an explicit transition.
type EnrolmentState uint8

// The two lawful states of a claw.
const (
	Unenrolled EnrolmentState = iota
	Enrolled
)

// Actor is Claw_Enrolment_Pkg.Actor_Type: who is asking. Commanding_Grapple
// appears here so that its requests can be REFUSED, not granted.
type Actor uint8

// The four parties the enrolment core distinguishes.
const (
	ActorNobody Actor = iota
	ActorLocalUser
	ActorEnrollingOwner
	ActorCommandingGrapple
)

// IsEnrolled mirrors Claw_Enrolment_Pkg.Is_Enrolled.
func IsEnrolled(s EnrolmentState) bool { return s == Enrolled }

// MayEnrol mirrors Claw_Enrolment_Pkg.May_Enrol. Only the enrolling owner, only
// from Unenrolled, never the Grapple.
func MayEnrol(s EnrolmentState, requester Actor, authentic bool) bool {
	return authentic && requester == ActorEnrollingOwner && s == Unenrolled
}

// MayRevoke mirrors Claw_Enrolment_Pkg.May_Revoke. Revocation of the MACHINE is
// the owner's act; the person at the keyboard cannot unenrol a machine from its
// own IT authority, which is precisely what the organisation is buying.
func MayRevoke(s EnrolmentState, requester Actor, authentic bool) bool {
	return authentic && requester == ActorEnrollingOwner && s == Enrolled
}

// MayCommandClaw mirrors Claw_Enrolment_Pkg.May_Command_Claw. Note that even the
// enrolling owner does not command directly — commands run through the Grapple
// the claw was submitted to, so there is exactly one command path to audit.
func MayCommandClaw(s EnrolmentState, requester Actor, authentic, isServingGrapple bool) bool {
	return authentic && requester == ActorCommandingGrapple && s == Enrolled && isServingGrapple
}

// ServesExactlyOne mirrors Claw_Enrolment_Pkg.Serves_Exactly_One: the NO-TWO-
// MASTERS consistency check. Enrolled and not serving, or serving while
// unenrolled, are both incoherent states and this reports them.
func ServesExactlyOne(s EnrolmentState, isServingGrapple bool) bool {
	return IsEnrolled(s) == isServingGrapple
}

// String renders an enrolment state as the name used in the Ada enumeration.
func (s EnrolmentState) String() string {
	switch s {
	case Unenrolled:
		return "Unenrolled"
	case Enrolled:
		return "Enrolled"
	default:
		return "UNKNOWN_ENROLMENT_STATE"
	}
}

// String renders an actor as the name used in the Ada enumeration.
func (a Actor) String() string {
	switch a {
	case ActorNobody:
		return "Nobody"
	case ActorLocalUser:
		return "Local_User"
	case ActorEnrollingOwner:
		return "Enrolling_Owner"
	case ActorCommandingGrapple:
		return "Commanding_Grapple"
	default:
		return "UNKNOWN_ACTOR"
	}
}
