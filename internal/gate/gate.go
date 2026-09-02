// Package gate reproduces, in Go, the proven admission decisions the ghillie
// terminal is built around. It contains NO POLICY OF ITS OWN.
//
// This file mirrors Facade_Command_Pkg (ada-factory ledger 112). Its shape is
// copied from the provenLaneGate adapter at the bottom of
// ada-factory/cmd/specifier/forge_wiring.go — "There is no logic here by
// design". Every predicate below is a transliteration of the corresponding Ada
// expression function, kept line-for-line so that a diff against
// facade_command_pkg.ads is a readable exercise.
//
// The Ada, verbatim:
//
//	function May_Command (C : Command_Type; Ceiling : Command_Type;
//	                      K : Consent_Type; Authentic : Boolean) return Boolean
//	  is (Authentic
//	      and then not Is_Local_Only (C)
//	      and then Command_Rank (C) <= Command_Rank (Ceiling)
//	      and then (if Requires_Consent (C)
//	                then Consent_Rank (K) = Consent_Type'Pos (Fresh_Explicit)))
//
// with proved postconditions: not authentic ⇒ refused; rank above ceiling ⇒
// refused; local-only ⇒ refused; consent-requiring without Fresh_Explicit ⇒
// refused.
//
// HONEST LABEL. This is UNPROVEN SHIM cross-checked against ledger 112 by
// exhaustive table test (gate_test.go walks every command × ceiling × consent ×
// authentic combination and asserts the verdicts the Ada postconditions state).
// The proven core is the authority: WHERE THIS GO AND THE ADA SPEC DISAGREE,
// THE ADA IS RIGHT. The production replacement is a C-ABI call into the core
// itself, at which point this file becomes a call-out and not a copy.
package gate

// Command is Facade_Command_Pkg.Command_Type, in the same order — ascending
// BLAST RADIUS, which is the ordering the whole gate rests on. Report_Status
// merely answers; Run_Local_Code is total compromise of the customer's machine.
//
// The numeric values ARE the wire encoding of the frame's Command byte, and are
// the ranks: Command_Rank (C) is Command_Type'Pos (C).
type Command uint8

// The proven command vocabulary. No protocol command exists that the gate has
// no rank for — that is by construction, not by convention.
const (
	ReportStatus      Command = 0
	OfferCatalogue    Command = 1
	DeliverArtifact   Command = 2
	RequestSpecUpload Command = 3
	InstallArtifact   Command = 4
	RunLocalCode      Command = 5
)

// MaxCommand is the highest rank in the proven enumeration (Command_Rank'Result
// <= 5, a postcondition of Command_Rank).
const MaxCommand = RunLocalCode

// DefaultCeiling is the ceiling of a fresh, unenrolled install: rank 2,
// Deliver_Artifact (design decision 1, decided 2026-07-29). Status and
// catalogue notifications flow and deliveries land in quarantine; nothing
// installs or executes without the local machine deliberately raising the
// ceiling AND a human giving fresh per-act consent.
const DefaultCeiling = DeliverArtifact

// Consent is Facade_Command_Pkg.Consent_Type: ascending strength of the human
// agreement present at the local machine. None = nobody agreed to anything;
// Session = the user is present and working, which is NOT agreement;
// FreshExplicit = the user was asked about THIS act and said yes.
//
// Consent is established at the local UI and never on the wire. It is the one
// control a compromised facade cannot route around, because it cannot be a
// person standing at someone else's computer.
type Consent uint8

// The proven consent levels.
const (
	NoConsent     Consent = 0
	SessionOnly   Consent = 1
	FreshExplicit Consent = 2
)

// CommandRank mirrors Facade_Command_Pkg.Command_Rank: the position of the
// command in the blast-radius ordering.
func CommandRank(c Command) int { return int(c) }

// ConsentRank mirrors Facade_Command_Pkg.Consent_Rank.
func ConsentRank(k Consent) int { return int(k) }

// RequiresConsent mirrors Facade_Command_Pkg.Requires_Consent: the two most
// dangerous ranks require a fresh human agreement to this specific act.
func RequiresConsent(c Command) bool {
	return CommandRank(c) >= CommandRank(InstallArtifact)
}

// IsLocalOnly mirrors Facade_Command_Pkg.Is_Local_Only: some commands are
// local-only by construction and may never be issued remotely at any rank or
// with any consent. Request_Spec_Upload is the whole of that set — it appears in
// the vocabulary precisely so its refusal is a theorem rather than an absence.
func IsLocalOnly(c Command) bool { return c == RequestSpecUpload }

// MayCommand mirrors Facade_Command_Pkg.May_Command, the per-instruction
// admission gate. Authentic is a Boolean handed in from elsewhere: whether the
// message genuinely came from the facade is a signature question, answered by
// the caller. Authentication is necessary and NOT SUFFICIENT — a compromised
// facade sending a perfectly authentic message is exactly the case this gate
// exists for.
func MayCommand(c Command, ceiling Command, k Consent, authentic bool) bool {
	// The CONTROL ARM (see unguarded.go). Under the `unguarded` build tag the
	// ceiling and the consent requirement stop being enforced, so the leakage
	// experiment can attribute what leaks to the ABSENCE OF THE GATE rather
	// than to a difference of vendor. Authenticity is still required: this is
	// unGUARDED, not unAUTHENTICATED — a build that accepted unsigned frames
	// would be measuring a second variable and proving nothing about the first.
	//
	// Guarded is a CONSTANT, so in every normal build the compiler deletes this
	// branch entirely and MayCommand is byte-for-byte the function that mirrors
	// ledger 112. The escape hatch cannot be reached at runtime; it has to be
	// deliberately compiled in.
	if !Guarded {
		return authentic
	}
	return authentic &&
		!IsLocalOnly(c) &&
		CommandRank(c) <= CommandRank(ceiling) &&
		(!RequiresConsent(c) || ConsentRank(k) == ConsentRank(FreshExplicit))
}

// IsKnownCommand reports whether a raw wire byte names a command the gate has a
// rank for.
//
// ⚠ THIS PREDICATE HAS NO ADA COUNTERPART, AND THAT IS THE POINT. In SPARK,
// Command_Type cannot hold a value outside the enumeration — the type system
// makes an unknown command unrepresentable, so the core never has to refuse
// one. Go's uint8 offers no such guarantee, so the shim must refuse at the
// decode boundary what Ada refuses at the type boundary. This is a gap in Go's
// expressiveness, not a disagreement with ledger 112.
func IsKnownCommand(b uint8) bool { return Command(b) <= MaxCommand }

// Reason classifies a refusal for reporting. A refused instruction is NOT
// silently dropped: the user is shown the instruction the facade sent and the
// proof-backed no. That visible refusal is the product, not a log line.
type Reason string

// The refusal reason classes. The first five are the design's vocabulary
// (§4, act/refuse → report); the last three are decode-boundary refusals that
// exist only because Go's types are weaker than SPARK's (see IsKnownCommand).
const (
	ReasonOverCeiling  Reason = "over-ceiling"
	ReasonLocalOnly    Reason = "local-only"
	ReasonNoConsent    Reason = "no-consent"
	ReasonBadSignature Reason = "bad-signature"
	ReasonStaleSeq     Reason = "stale-seq"

	ReasonUnknownCommand Reason = "unknown-command"
	ReasonBadProtocol    Reason = "unsupported-protocol"
	ReasonMalformedFrame Reason = "malformed-frame"
)

// Evaluate returns the gate's verdict and, when the verdict is refuse, the
// reason class to report.
//
// The VERDICT is MayCommand's and only MayCommand's — this function calls it
// rather than re-deriving it, so no second opinion can exist. The reason is
// diagnosis after the fact, computed from the same proven predicates.
//
// Reason precedence when several apply is local-only, then over-ceiling, then
// no-consent. That ordering is presentational: local-only is reported first
// because it is the refusal that holds at EVERY ceiling and EVERY consent
// level, so naming a ceiling for it would understate the guarantee.
func Evaluate(c Command, ceiling Command, k Consent, authentic bool) (allowed bool, reason Reason) {
	if MayCommand(c, ceiling, k, authentic) {
		return true, ""
	}
	switch {
	case !authentic:
		return false, ReasonBadSignature
	case IsLocalOnly(c):
		return false, ReasonLocalOnly
	case CommandRank(c) > CommandRank(ceiling):
		return false, ReasonOverCeiling
	case RequiresConsent(c) && ConsentRank(k) != ConsentRank(FreshExplicit):
		return false, ReasonNoConsent
	default:
		// Unreachable while this file mirrors ledger 112 faithfully: the four
		// cases above are exactly the four proved postconditions that can make
		// May_Command false. Reaching here means the mirror has drifted from
		// the Ada, so say so rather than inventing a reason. gate_test.go
		// asserts this arm is never taken across the exhaustive combination
		// space.
		return false, Reason("refused-reason-unclassified")
	}
}

// String renders a command as the name used in the Ada enumeration, for
// refusal reports a human reads.
func (c Command) String() string {
	switch c {
	case ReportStatus:
		return "Report_Status"
	case OfferCatalogue:
		return "Offer_Catalogue"
	case DeliverArtifact:
		return "Deliver_Artifact"
	case RequestSpecUpload:
		return "Request_Spec_Upload"
	case InstallArtifact:
		return "Install_Artifact"
	case RunLocalCode:
		return "Run_Local_Code"
	default:
		return "UNKNOWN_COMMAND"
	}
}

// String renders a consent level as the name used in the Ada enumeration.
func (k Consent) String() string {
	switch k {
	case NoConsent:
		return "None"
	case SessionOnly:
		return "Session"
	case FreshExplicit:
		return "Fresh_Explicit"
	default:
		return "UNKNOWN_CONSENT"
	}
}

// ParseCommand resolves a command name to its rank, for command-line
// configuration of the ceiling. It accepts the Ada enumeration names,
// case-insensitively, and nothing else.
func ParseCommand(s string) (Command, bool) {
	for c := ReportStatus; c <= MaxCommand; c++ {
		if equalFold(c.String(), s) {
			return c, true
		}
	}
	return 0, false
}

// ParseConsent resolves a consent-level name to its rank.
func ParseConsent(s string) (Consent, bool) {
	for k := NoConsent; k <= FreshExplicit; k++ {
		if equalFold(k.String(), s) {
			return k, true
		}
	}
	return 0, false
}

// equalFold compares ASCII strings case-insensitively. Local rather than
// strings.EqualFold only to keep this package dependency-free, which makes the
// diff against the .ads the only thing a reviewer has to read.
func equalFold(a, b string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range len(a) {
		x, y := a[i], b[i]
		if 'A' <= x && x <= 'Z' {
			x += 'a' - 'A'
		}
		if 'A' <= y && y <= 'Z' {
			y += 'a' - 'A'
		}
		if x != y {
			return false
		}
	}
	return true
}
