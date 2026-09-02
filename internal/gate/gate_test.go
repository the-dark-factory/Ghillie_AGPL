package gate

import "testing"

// allCommands, allCeilings, allConsents and allAuthentic span the entire
// decision space the Ada gate is defined over: 6 × 6 × 3 × 2 = 216 cases.
var (
	allCommands  = []Command{ReportStatus, OfferCatalogue, DeliverArtifact, RequestSpecUpload, InstallArtifact, RunLocalCode}
	allConsents  = []Consent{NoConsent, SessionOnly, FreshExplicit}
	allAuthentic = []bool{false, true}
)

// TestMayCommandExhaustive walks every (command × ceiling × consent ×
// authentic) combination and asserts, for each, the four PROVED POSTCONDITIONS
// of Facade_Command_Pkg.May_Command plus its result equation.
//
// The expectation is deliberately NOT computed by calling MayCommand. It is
// restated using literal enumeration comparisons rather than the rank
// functions, so that a bug in CommandRank or RequiresConsent cannot hide by
// being shared between the code and its test.
func TestMayCommandExhaustive(t *testing.T) {
	cases := 0
	for _, c := range allCommands {
		for _, ceiling := range allCommands {
			for _, k := range allConsents {
				for _, authentic := range allAuthentic {
					cases++
					got := MayCommand(c, ceiling, k, authentic)

					// The Ada result equation, restated with literals.
					want := authentic &&
						c != RequestSpecUpload &&
						c <= ceiling &&
						(c < InstallArtifact || k == FreshExplicit)
					if got != want {
						t.Errorf("May_Command(%v, ceiling=%v, %v, authentic=%v) = %v, want %v", c, ceiling, k, authentic, got, want)
					}

					// Post: (if not Authentic then not May_Command'Result)
					if !authentic && got {
						t.Errorf("AUTHENTICATION-IS-NECESSARY violated: %v admitted with authentic=false (ceiling=%v, %v)", c, ceiling, k)
					}
					// Post: (if Command_Rank (C) > Command_Rank (Ceiling) then not May_Command'Result)
					if CommandRank(c) > CommandRank(ceiling) && got {
						t.Errorf("ceiling postcondition violated: %v (rank %d) admitted at ceiling %v (rank %d)", c, CommandRank(c), ceiling, CommandRank(ceiling))
					}
					// Post: (if Is_Local_Only (C) then not May_Command'Result)
					if IsLocalOnly(c) && got {
						t.Errorf("NO-REMOTE-REACH-FOR-LOCAL-WORK violated: %v admitted (ceiling=%v, %v, authentic=%v)", c, ceiling, k, authentic)
					}
					// Post: (if (Requires_Consent (C) and then K /= Fresh_Explicit)
					//        then not May_Command'Result)
					if RequiresConsent(c) && k != FreshExplicit && got {
						t.Errorf("DANGEROUS-NEEDS-A-HUMAN violated: %v admitted with consent %v (ceiling=%v)", c, k, ceiling)
					}
				}
			}
		}
	}
	if want := len(allCommands) * len(allCommands) * len(allConsents) * len(allAuthentic); cases != want {
		t.Fatalf("covered %d combinations, want %d", cases, want)
	}
	t.Logf("exhaustive: %d (command × ceiling × consent × authentic) combinations checked against ledger 112 postconditions", cases)
}

// TestLocalOnlyRefusedEverywhere states the theorem on its own, because it is
// the one the protocol is sold on: an Is_Local_Only command is refused at EVERY
// ceiling and EVERY consent level, including the maximum of both.
func TestLocalOnlyRefusedEverywhere(t *testing.T) {
	for _, ceiling := range allCommands {
		for _, k := range allConsents {
			if MayCommand(RequestSpecUpload, ceiling, k, true) {
				t.Errorf("Request_Spec_Upload admitted at ceiling=%v consent=%v — the local-only theorem does not hold", ceiling, k)
			}
			allowed, reason := Evaluate(RequestSpecUpload, ceiling, k, true)
			if allowed || reason != ReasonLocalOnly {
				t.Errorf("Evaluate(Request_Spec_Upload, ceiling=%v, %v) = (%v, %q), want (false, %q)", ceiling, k, allowed, reason, ReasonLocalOnly)
			}
		}
	}
}

// TestMayCommandGoldenTable is a hand-derived table read off the Ada spec and
// the design's rank table, kept separate from the exhaustive sweep so a
// reviewer has concrete rows to check by eye.
func TestMayCommandGoldenTable(t *testing.T) {
	tests := []struct {
		name      string
		command   Command
		ceiling   Command
		consent   Consent
		authentic bool
		want      bool
	}{
		// Default ceiling (rank 2) — the fresh unenrolled install.
		{"status at default ceiling", ReportStatus, DefaultCeiling, NoConsent, true, true},
		{"catalogue at default ceiling", OfferCatalogue, DefaultCeiling, NoConsent, true, true},
		{"delivery at default ceiling", DeliverArtifact, DefaultCeiling, NoConsent, true, true},
		{"spec upload at default ceiling", RequestSpecUpload, DefaultCeiling, NoConsent, true, false},
		{"install at default ceiling", InstallArtifact, DefaultCeiling, FreshExplicit, true, false},
		{"run local at default ceiling", RunLocalCode, DefaultCeiling, FreshExplicit, true, false},

		// Authentication is necessary and not sufficient.
		{"status unsigned", ReportStatus, DefaultCeiling, NoConsent, false, false},
		{"delivery unsigned", DeliverArtifact, DefaultCeiling, FreshExplicit, false, false},
		{"install unsigned at max ceiling", InstallArtifact, RunLocalCode, FreshExplicit, false, false},

		// Consent: session presence is NOT agreement.
		{"install with no consent", InstallArtifact, InstallArtifact, NoConsent, true, false},
		{"install with session consent", InstallArtifact, InstallArtifact, SessionOnly, true, false},
		{"install with fresh consent", InstallArtifact, InstallArtifact, FreshExplicit, true, true},
		{"run local with session consent", RunLocalCode, RunLocalCode, SessionOnly, true, false},
		{"run local with fresh consent", RunLocalCode, RunLocalCode, FreshExplicit, true, true},

		// Consent is irrelevant below rank 4.
		{"delivery with no consent", DeliverArtifact, DeliverArtifact, NoConsent, true, true},

		// Local-only, at the top of everything.
		{"spec upload at max ceiling with fresh consent", RequestSpecUpload, RunLocalCode, FreshExplicit, true, false},

		// Exactly-at-ceiling is admitted; one above is not.
		{"catalogue at its own ceiling", OfferCatalogue, OfferCatalogue, NoConsent, true, true},
		{"delivery one above ceiling", DeliverArtifact, OfferCatalogue, NoConsent, true, false},
		{"status at floor ceiling", ReportStatus, ReportStatus, NoConsent, true, true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := MayCommand(tt.command, tt.ceiling, tt.consent, tt.authentic); got != tt.want {
				t.Errorf("May_Command(%v, ceiling=%v, %v, authentic=%v) = %v, want %v", tt.command, tt.ceiling, tt.consent, tt.authentic, got, tt.want)
			}
		})
	}
}

// TestEvaluateReasons pins the reported reason class for each refusal shape.
func TestEvaluateReasons(t *testing.T) {
	tests := []struct {
		name       string
		command    Command
		ceiling    Command
		consent    Consent
		authentic  bool
		want       bool
		wantReason Reason
	}{
		{"admitted carries no reason", DeliverArtifact, DefaultCeiling, NoConsent, true, true, ""},
		{"bad signature", DeliverArtifact, DefaultCeiling, NoConsent, false, false, ReasonBadSignature},
		{"bad signature outranks everything", RequestSpecUpload, ReportStatus, NoConsent, false, false, ReasonBadSignature},
		{"local only", RequestSpecUpload, DefaultCeiling, NoConsent, true, false, ReasonLocalOnly},
		{"local only outranks over-ceiling", RequestSpecUpload, ReportStatus, NoConsent, true, false, ReasonLocalOnly},
		{"over ceiling", InstallArtifact, DefaultCeiling, FreshExplicit, true, false, ReasonOverCeiling},
		{"over ceiling outranks no-consent", InstallArtifact, DefaultCeiling, NoConsent, true, false, ReasonOverCeiling},
		{"run local over ceiling", RunLocalCode, DefaultCeiling, FreshExplicit, true, false, ReasonOverCeiling},
		{"no consent", InstallArtifact, InstallArtifact, NoConsent, true, false, ReasonNoConsent},
		{"session is not consent", InstallArtifact, InstallArtifact, SessionOnly, true, false, ReasonNoConsent},
		{"run local no consent", RunLocalCode, RunLocalCode, SessionOnly, true, false, ReasonNoConsent},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			allowed, reason := Evaluate(tt.command, tt.ceiling, tt.consent, tt.authentic)
			if allowed != tt.want || reason != tt.wantReason {
				t.Errorf("Evaluate(%v, ceiling=%v, %v, authentic=%v) = (%v, %q), want (%v, %q)",
					tt.command, tt.ceiling, tt.consent, tt.authentic, allowed, reason, tt.want, tt.wantReason)
			}
		})
	}
}

// TestEvaluateNeverDisagreesWithMayCommand checks the whole space: Evaluate's
// verdict is May_Command's verdict, every refusal carries a classified reason,
// and every admission carries none. The "unclassified" arm must never be taken —
// if it ever is, this mirror has drifted from the Ada.
func TestEvaluateNeverDisagreesWithMayCommand(t *testing.T) {
	classified := map[Reason]bool{
		ReasonBadSignature: true,
		ReasonLocalOnly:    true,
		ReasonOverCeiling:  true,
		ReasonNoConsent:    true,
	}
	for _, c := range allCommands {
		for _, ceiling := range allCommands {
			for _, k := range allConsents {
				for _, authentic := range allAuthentic {
					allowed, reason := Evaluate(c, ceiling, k, authentic)
					if want := MayCommand(c, ceiling, k, authentic); allowed != want {
						t.Fatalf("Evaluate verdict %v != May_Command verdict %v for (%v, ceiling=%v, %v, authentic=%v)", allowed, want, c, ceiling, k, authentic)
					}
					if allowed && reason != "" {
						t.Errorf("admitted (%v, ceiling=%v, %v) carries reason %q", c, ceiling, k, reason)
					}
					if !allowed && !classified[reason] {
						t.Errorf("refused (%v, ceiling=%v, %v, authentic=%v) carries unclassified reason %q", c, ceiling, k, authentic, reason)
					}
				}
			}
		}
	}
}

// TestRankAndPredicatePostconditions checks the smaller proved postconditions
// of Command_Rank, Consent_Rank, Requires_Consent and Is_Local_Only.
func TestRankAndPredicatePostconditions(t *testing.T) {
	for i, c := range allCommands {
		if got := CommandRank(c); got != i {
			t.Errorf("Command_Rank(%v) = %d, want %d (Command_Type'Pos)", c, got, i)
		}
		if got := CommandRank(c); got > 5 {
			t.Errorf("Command_Rank(%v) = %d, want <= 5", c, got)
		}
		if c == ReportStatus && CommandRank(c) != 0 {
			t.Errorf("Command_Rank(Report_Status) = %d, want 0", CommandRank(c))
		}
		if want := c >= InstallArtifact; RequiresConsent(c) != want {
			t.Errorf("Requires_Consent(%v) = %v, want %v", c, RequiresConsent(c), want)
		}
		if want := c == RequestSpecUpload; IsLocalOnly(c) != want {
			t.Errorf("Is_Local_Only(%v) = %v, want %v", c, IsLocalOnly(c), want)
		}
	}
	for i, k := range allConsents {
		if got := ConsentRank(k); got != i {
			t.Errorf("Consent_Rank(%v) = %d, want %d (Consent_Type'Pos)", k, got, i)
		}
		if got := ConsentRank(k); got > 2 {
			t.Errorf("Consent_Rank(%v) = %d, want <= 2", k, got)
		}
	}
}

// TestIsKnownCommand covers the boundary Ada's type system enforces statically
// and Go must enforce at run time: a wire byte outside the proven enumeration
// names no command, so the gate has no rank for it and it must be refused.
func TestIsKnownCommand(t *testing.T) {
	tests := []struct {
		name string
		b    uint8
		want bool
	}{
		{"report status", 0, true},
		{"run local code", 5, true},
		{"one past the enumeration", 6, false},
		{"seven", 7, false},
		{"max byte", 255, false},
		{"high bit", 128, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := IsKnownCommand(tt.b); got != tt.want {
				t.Errorf("IsKnownCommand(%d) = %v, want %v", tt.b, got, tt.want)
			}
		})
	}
}

// TestIsFresh checks the three theorems of Poll_Freshness_Pkg (ledger 120):
// STRICTLY-INCREASES, STALE-IS-REFUSED, FIRST-FRAME-ACCEPTED.
func TestIsFresh(t *testing.T) {
	const u64Max = ^uint64(0)
	tests := []struct {
		name     string
		lastSeq  uint64
		frameSeq uint64
		want     bool
		theorem  string
	}{
		{"first frame from empty state", 0, 1, true, "FIRST-FRAME-ACCEPTED"},
		{"large first frame from empty state", 0, u64Max, true, "FIRST-FRAME-ACCEPTED"},
		{"zero frame from empty state", 0, 0, false, "STALE-IS-REFUSED"},
		{"strictly newer", 5, 6, true, "STRICTLY-INCREASES"},
		{"far newer", 5, 5000, true, "STRICTLY-INCREASES"},
		{"equal is replay", 5, 5, false, "STALE-IS-REFUSED"},
		{"older is replay", 5, 4, false, "STALE-IS-REFUSED"},
		{"zero against nonzero", 5, 0, false, "STALE-IS-REFUSED"},
		{"at the ceiling of the type", u64Max, u64Max, false, "STALE-IS-REFUSED"},
		{"one below the ceiling", u64Max - 1, u64Max, true, "STRICTLY-INCREASES"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := IsFresh(tt.lastSeq, tt.frameSeq); got != tt.want {
				t.Errorf("Is_Fresh(last=%d, frame=%d) = %v, want %v (%s)", tt.lastSeq, tt.frameSeq, got, tt.want, tt.theorem)
			}
		})
	}
}

// TestParseCommandAndConsent covers the configuration surface: ceiling and
// consent are named with the Ada enumeration's own identifiers, and nothing
// else parses.
func TestParseCommandAndConsent(t *testing.T) {
	commandTests := []struct {
		in   string
		want Command
		ok   bool
	}{
		{"Report_Status", ReportStatus, true},
		{"deliver_artifact", DeliverArtifact, true},
		{"RUN_LOCAL_CODE", RunLocalCode, true},
		{"Request_Spec_Upload", RequestSpecUpload, true},
		{"deliver", 0, false},
		{"", 0, false},
		{"2", 0, false},
	}
	for _, tt := range commandTests {
		t.Run("command/"+tt.in, func(t *testing.T) {
			got, ok := ParseCommand(tt.in)
			if ok != tt.ok || (ok && got != tt.want) {
				t.Errorf("ParseCommand(%q) = (%v, %v), want (%v, %v)", tt.in, got, ok, tt.want, tt.ok)
			}
		})
	}

	consentTests := []struct {
		in   string
		want Consent
		ok   bool
	}{
		{"None", NoConsent, true},
		{"session", SessionOnly, true},
		{"Fresh_Explicit", FreshExplicit, true},
		{"fresh", 0, false},
		{"", 0, false},
	}
	for _, tt := range consentTests {
		t.Run("consent/"+tt.in, func(t *testing.T) {
			got, ok := ParseConsent(tt.in)
			if ok != tt.ok || (ok && got != tt.want) {
				t.Errorf("ParseConsent(%q) = (%v, %v), want (%v, %v)", tt.in, got, ok, tt.want, tt.ok)
			}
		})
	}
}
