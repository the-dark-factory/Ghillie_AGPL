package gate

// This file mirrors User_Access_Pkg (ada-factory ledger 115) — may this person
// still act on this machine, and who may take that away.
//
// The Ada, verbatim:
//
//	function May_Act (Standing : User_Standing) return Boolean
//	  is ((Standing = In_Good_Standing))
//
//	function May_Revoke_User (Requester : Requester_Type; Authentic : Boolean;
//	                          Machine_Enrolled : Boolean; Serving_Grapple : Boolean;
//	                          User_Present : Boolean; User_Consents : Boolean)
//	  return Boolean
//	  is ((Authentic and then Requester = Commanding_Grapple
//	       and then Machine_Enrolled and then Serving_Grapple))
//
// ★ THE THEOREM THAT MATTERS MOST HERE IS AN ABSENCE. User_Present and
// User_Consents are PARAMETERS THE DECISION PROVABLY IGNORES. A removal that
// requires the removed person's cooperation, presence or logout is not a
// removal, and the person being removed is precisely who must not be able to
// block it. Proving a fact is not the same as making it an input, and the two
// postconditions that pin this are:
//
//	(if (... and then not User_Present) then May_Revoke_User'Result)
//	(if (... and then not User_Consents) then May_Revoke_User'Result)
//
// They are kept as arguments in this Go for exactly the same reason the Ada
// keeps them: so that their IRRELEVANCE IS TESTABLE rather than invisible. A
// reviewer can see that they are accepted and see that they are unused.
//
// ⚠ DO NOT CONFUSE THIS CONSENT WITH THE GATE'S. Facade_Command_Pkg (112)
// requires Fresh_Explicit consent for what ADDS capability to a machine
// (install, run). Removing a person's capability is the organisation's own act
// about its own authority and needs no consent at the keyboard. The two consent
// notions live in the same package here and are deliberately different types.
//
// HONEST LABEL: unproven shim, cross-checked against ledger 115 by exhaustive
// table test. Where this Go and the Ada disagree, THE ADA IS RIGHT.

// UserStanding is User_Access_Pkg.User_Standing. Revoked is terminal from the
// machine's point of view; reinstatement is a fresh grant by the authority, not
// a reversal here.
type UserStanding uint8

// What the fleet currently says about this person on this machine.
const (
	InGoodStanding UserStanding = iota
	RevokedStanding
)

// Requester is User_Access_Pkg.Requester_Type: who is asking for a revocation.
//
// ⚠ IT IS A SEPARATE TYPE FROM Actor, EXACTLY AS IN THE ADA, even though the
// members read the same. Enrolment authority and user-revocation authority are
// different questions asked of different cores, and collapsing them into one Go
// type would be the first step of exactly the conflation this design is trying
// to make impossible.
type Requester uint8

// The four parties the user-access core distinguishes.
const (
	RequesterNobody Requester = iota
	RequesterLocalUser
	RequesterEnrollingOwner
	RequesterCommandingGrapple
)

// MayAct mirrors User_Access_Pkg.May_Act: whether this person may still do
// anything at all on this machine.
func MayAct(standing UserStanding) bool { return standing == InGoodStanding }

// MayRevokeUser mirrors User_Access_Pkg.May_Revoke_User.
//
// userPresent and userConsents are ACCEPTED AND DELIBERATELY UNUSED — see the
// file comment. Their irrelevance is the theorem, and useraccess_test.go proves
// it by flipping both across the whole decision space and asserting the verdict
// never moves.
func MayRevokeUser(requester Requester, authentic, machineEnrolled, servingGrapple, userPresent, userConsents bool) bool {
	_, _ = userPresent, userConsents // proved irrelevant; named so the reader sees the absence
	return authentic &&
		requester == RequesterCommandingGrapple &&
		machineEnrolled &&
		servingGrapple
}

// String renders a user standing as the name used in the Ada enumeration.
func (s UserStanding) String() string {
	switch s {
	case InGoodStanding:
		return "In_Good_Standing"
	case RevokedStanding:
		return "Revoked"
	default:
		return "UNKNOWN_USER_STANDING"
	}
}

// String renders a requester as the name used in the Ada enumeration.
func (r Requester) String() string {
	switch r {
	case RequesterNobody:
		return "Nobody"
	case RequesterLocalUser:
		return "Local_User"
	case RequesterEnrollingOwner:
		return "Enrolling_Owner"
	case RequesterCommandingGrapple:
		return "Commanding_Grapple"
	default:
		return "UNKNOWN_REQUESTER"
	}
}
