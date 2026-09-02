package gate

import "testing"

// The full domains of the two identity cores.
var (
	allEnrolmentStates = []EnrolmentState{Unenrolled, Enrolled}
	allActors          = []Actor{ActorNobody, ActorLocalUser, ActorEnrollingOwner, ActorCommandingGrapple}
	allStandings       = []UserStanding{InGoodStanding, RevokedStanding}
	allRequesters      = []Requester{RequesterNobody, RequesterLocalUser, RequesterEnrollingOwner, RequesterCommandingGrapple}
	allBools           = []bool{false, true}
)

// TestClawEnrolmentExhaustive walks every (state × actor × authentic ×
// serving) combination — 2 × 4 × 2 × 2 = 32 — and asserts every proved
// postcondition of Claw_Enrolment_Pkg (ledger 113). The expectations are
// restated with literals rather than by calling the functions again.
func TestClawEnrolmentExhaustive(t *testing.T) {
	cases := 0
	for _, s := range allEnrolmentStates {
		for _, actor := range allActors {
			for _, authentic := range allBools {
				for _, serving := range allBools {
					cases++

					// --- Is_Enrolled ---
					if IsEnrolled(s) != (s == Enrolled) {
						t.Errorf("Is_Enrolled(%v) = %v, want %v", s, IsEnrolled(s), s == Enrolled)
					}
					if s == Unenrolled && IsEnrolled(s) {
						t.Errorf("Is_Enrolled(Unenrolled) must be false")
					}

					// --- May_Enrol: ENROLMENT-IS-AN-OWNER-ACT, NO-TWO-MASTERS ---
					enrol := MayEnrol(s, actor, authentic)
					if want := authentic && actor == ActorEnrollingOwner && s == Unenrolled; enrol != want {
						t.Errorf("May_Enrol(%v, %v, authentic=%v) = %v, want %v", s, actor, authentic, enrol, want)
					}
					if actor != ActorEnrollingOwner && enrol {
						t.Errorf("ENROLMENT-IS-AN-OWNER-ACT violated: %v enrolled a claw", actor)
					}
					if actor == ActorCommandingGrapple && enrol {
						t.Error("a Grapple recruited a claw — ENROLMENT-IS-AN-OWNER-ACT does not hold")
					}
					if s == Enrolled && enrol {
						t.Error("NO-TWO-MASTERS violated: an already-enrolled claw was re-enrolled")
					}
					if !authentic && enrol {
						t.Errorf("an unauthenticated request enrolled a claw (%v, %v)", s, actor)
					}

					// --- May_Revoke: REVOCATION-BELONGS-TO-THE-OWNER ---
					revoke := MayRevoke(s, actor, authentic)
					if want := authentic && actor == ActorEnrollingOwner && s == Enrolled; revoke != want {
						t.Errorf("May_Revoke(%v, %v, authentic=%v) = %v, want %v", s, actor, authentic, revoke, want)
					}
					if actor == ActorLocalUser && revoke {
						t.Error("the person at the keyboard unenrolled the machine — REVOCATION-BELONGS-TO-THE-OWNER does not hold")
					}
					if s == Unenrolled && revoke {
						t.Error("an unenrolled claw was revoked")
					}

					// --- May_Command_Claw ---
					command := MayCommandClaw(s, actor, authentic, serving)
					if want := authentic && actor == ActorCommandingGrapple && s == Enrolled && serving; command != want {
						t.Errorf("May_Command_Claw(%v, %v, authentic=%v, serving=%v) = %v, want %v", s, actor, authentic, serving, command, want)
					}
					if s == Unenrolled && command {
						t.Error("an unenrolled claw was commanded")
					}
					if !serving && command {
						t.Error("a claw serving no Grapple was commanded")
					}
					if actor == ActorLocalUser && command {
						t.Error("the local user commanded the claw as if they were the Grapple")
					}
					if actor == ActorEnrollingOwner && command {
						t.Error("the owner commanded the claw directly, bypassing the Grapple — there must be exactly one command path to audit")
					}

					// --- Serves_Exactly_One ---
					one := ServesExactlyOne(s, serving)
					if want := IsEnrolled(s) == serving; one != want {
						t.Errorf("Serves_Exactly_One(%v, serving=%v) = %v, want %v", s, serving, one, want)
					}
					if s == Enrolled && !serving && one {
						t.Error("Serves_Exactly_One accepted enrolled-but-serving-nobody")
					}
					if s == Unenrolled && serving && one {
						t.Error("Serves_Exactly_One accepted unenrolled-but-serving")
					}
				}
			}
		}
	}
	if want := len(allEnrolmentStates) * len(allActors) * len(allBools) * len(allBools); cases != want {
		t.Fatalf("covered %d combinations, want %d", cases, want)
	}
	t.Logf("exhaustive: %d (state × actor × authentic × serving) combinations checked against ledger 113 postconditions", cases)
}

// TestUserAccessExhaustive walks every (requester × authentic × enrolled ×
// serving × present × consents) combination — 4 × 2⁵ = 128 — and asserts every
// proved postcondition of User_Access_Pkg (ledger 115).
func TestUserAccessExhaustive(t *testing.T) {
	cases := 0
	for _, r := range allRequesters {
		for _, authentic := range allBools {
			for _, enrolled := range allBools {
				for _, serving := range allBools {
					for _, present := range allBools {
						for _, consents := range allBools {
							cases++
							got := MayRevokeUser(r, authentic, enrolled, serving, present, consents)

							want := authentic && r == RequesterCommandingGrapple && enrolled && serving
							if got != want {
								t.Errorf("May_Revoke_User(%v, authentic=%v, enrolled=%v, serving=%v, present=%v, consents=%v) = %v, want %v",
									r, authentic, enrolled, serving, present, consents, got, want)
							}
							if r == RequesterLocalUser && got {
								t.Error("the person at the keyboard revoked a user — revocation flows through the Grapple only")
							}
							if r == RequesterEnrollingOwner && got {
								t.Error("the owner revoked a user directly — user revocation gets no second door")
							}
							if !enrolled && got {
								t.Error("a user was revoked on an unenrolled machine")
							}
							if !serving && got {
								t.Error("a user was revoked by a Grapple this machine does not serve")
							}
							// ★ The two postconditions that make removal work:
							// an otherwise-lawful revocation succeeds whether or
							// not the person is there and whether or not they agree.
							if authentic && r == RequesterCommandingGrapple && enrolled && serving {
								if !present && !got {
									t.Error("an absent user could not be revoked — a removal that needs the person present is not a removal")
								}
								if !consents && !got {
									t.Error("a non-consenting user could not be revoked — the person removed must not be able to block it")
								}
							}
						}
					}
				}
			}
		}
	}
	if want := len(allRequesters) * 32; cases != want {
		t.Fatalf("covered %d combinations, want %d", cases, want)
	}
	t.Logf("exhaustive: %d (requester × authentic × enrolled × serving × present × consents) combinations checked against ledger 115 postconditions", cases)
}

// TestPresenceAndConsentAreProvablyIgnored states ledger 115's headline
// property directly: User_Present and User_Consents are parameters the decision
// ignores. Flipping either, anywhere in the space, must never move the verdict.
func TestPresenceAndConsentAreProvablyIgnored(t *testing.T) {
	for _, r := range allRequesters {
		for _, authentic := range allBools {
			for _, enrolled := range allBools {
				for _, serving := range allBools {
					base := MayRevokeUser(r, authentic, enrolled, serving, false, false)
					for _, present := range allBools {
						for _, consents := range allBools {
							if got := MayRevokeUser(r, authentic, enrolled, serving, present, consents); got != base {
								t.Fatalf("May_Revoke_User moved from %v to %v when only presence/consent changed (%v, authentic=%v, enrolled=%v, serving=%v, present=%v, consents=%v)",
									base, got, r, authentic, enrolled, serving, present, consents)
							}
						}
					}
				}
			}
		}
	}
}

// TestMayActPostconditions covers the small core: a revoked user may do
// nothing, immediately, on every action.
func TestMayActPostconditions(t *testing.T) {
	for _, s := range allStandings {
		if MayAct(s) != (s == InGoodStanding) {
			t.Errorf("May_Act(%v) = %v, want %v", s, MayAct(s), s == InGoodStanding)
		}
		if s == RevokedStanding && MayAct(s) {
			t.Error("May_Act(Revoked) = true — sacking someone did not work")
		}
	}
}

// TestActorAndRequesterAreDistinctTypes pins the deliberate duplication. The
// Ada declares Actor_Type and Requester_Type separately; this Go does too, so
// an actor cannot be passed where a requester is wanted. The check is that the
// enumerations agree in ORDER (they are the same four parties) while remaining
// separate types — the compiler enforces the second half, and this test would
// need editing if anyone "tidied" one into the other.
func TestActorAndRequesterAreDistinctTypes(t *testing.T) {
	pairs := []struct {
		actor     Actor
		requester Requester
		name      string
	}{
		{ActorNobody, RequesterNobody, "Nobody"},
		{ActorLocalUser, RequesterLocalUser, "Local_User"},
		{ActorEnrollingOwner, RequesterEnrollingOwner, "Enrolling_Owner"},
		{ActorCommandingGrapple, RequesterCommandingGrapple, "Commanding_Grapple"},
	}
	for _, p := range pairs {
		if p.actor.String() != p.name || p.requester.String() != p.name {
			t.Errorf("%s: Actor renders %q, Requester renders %q", p.name, p.actor.String(), p.requester.String())
		}
		if uint8(p.actor) != uint8(p.requester) {
			t.Errorf("%s: Actor position %d, Requester position %d — the enumerations have drifted apart", p.name, p.actor, p.requester)
		}
	}
}
