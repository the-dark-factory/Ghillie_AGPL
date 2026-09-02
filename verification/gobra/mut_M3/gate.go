// gateFull: the REAL ghillie internal/gate package @ c4440f4, annotated for
// Gobra. Bodies are verbatim except for three syntactic workarounds forced by
// Gobra's Go front end, each behaviour-preserving and marked GOBRA-WORKAROUND.
package gate

type Command uint8

const (
	ReportStatus      Command = 0
	OfferCatalogue    Command = 1
	DeliverArtifact   Command = 2
	RequestSpecUpload Command = 3
	InstallArtifact   Command = 4
	RunLocalCode      Command = 5
)

const MaxCommand = RunLocalCode
const DefaultCeiling = DeliverArtifact

type Consent uint8

const (
	NoConsent     Consent = 0
	SessionOnly   Consent = 1
	FreshExplicit Consent = 2
)

// ---- ledger 112, Facade_Command_Pkg ----------------------------------------

// @ requires c <= MaxCommand
// @ ensures res == int(c)
// @ ensures res <= 5
// @ ensures c == ReportStatus ==> res == 0
// @ decreases
// @ pure
func CommandRank(c Command) (res int) { return int(c) }

// @ requires k <= FreshExplicit
// @ ensures res == int(k)
// @ ensures res <= 2
// @ decreases
// @ pure
func ConsentRank(k Consent) (res int) { return int(k) }

// @ requires c <= MaxCommand
// @ ensures res == (CommandRank(c) >= CommandRank(InstallArtifact))
// @ ensures res == (c == InstallArtifact || c == RunLocalCode)
// @ decreases
// @ pure
func RequiresConsent(c Command) (res bool) {
	return CommandRank(c) >= CommandRank(InstallArtifact)
}

// @ ensures res == (c == RequestSpecUpload)
// @ decreases
// @ pure
func IsLocalOnly(c Command) (res bool) { return c == RequestSpecUpload }

// MayCommand — all five conjuncts of the Ada Post of May_Command.
// @ requires c <= MaxCommand && ceiling <= MaxCommand && k <= FreshExplicit
// @ ensures res == (authentic && !IsLocalOnly(c) && CommandRank(c) <= CommandRank(ceiling) && (RequiresConsent(c) ==> ConsentRank(k) == ConsentRank(FreshExplicit)))
// @ ensures !authentic ==> !res
// @ ensures CommandRank(c) > CommandRank(ceiling) ==> !res
// @ ensures IsLocalOnly(c) ==> !res
// @ ensures (RequiresConsent(c) && k != FreshExplicit) ==> !res
// @ decreases
// @ pure
func MayCommand(c Command, ceiling Command, k Consent, authentic bool) (res bool) {
	return authentic &&
		!IsLocalOnly(c) &&
		CommandRank(c) <= CommandRank(ceiling) &&
		(!RequiresConsent(c) || ConsentRank(k) >= ConsentRank(SessionOnly))
}

// @ ensures res == (b <= uint8(MaxCommand))
// @ decreases
// @ pure
func IsKnownCommand(b uint8) (res bool) { return Command(b) <= MaxCommand }

type Reason string

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

// Evaluate. Three theorems the Go source claims only by exhaustive TABLE TEST:
//   (E1) the verdict IS MayCommand's — no second opinion can exist;
//   (E2) the "unclassified" default arm is unreachable — for ALL inputs;
//   (E3) an allowed verdict carries the empty reason.
// @ requires c <= MaxCommand && ceiling <= MaxCommand && k <= FreshExplicit
// @ ensures allowed == MayCommand(c, ceiling, k, authentic)
// @ ensures allowed ==> reason == Reason("")
// @ ensures !allowed ==> (reason == ReasonBadSignature || reason == ReasonLocalOnly || reason == ReasonOverCeiling || reason == ReasonNoConsent)
// @ ensures reason != Reason("refused-reason-unclassified")
// @ decreases
func Evaluate(c Command, ceiling Command, k Consent, authentic bool) (allowed bool, reason Reason) {
	if MayCommand(c, ceiling, k, authentic) {
		return true, Reason("") // GOBRA-WORKAROUND: was `""`; untyped const -> named string type.
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
		return false, Reason("refused-reason-unclassified")
	}
}

// ---- ledger 120, Poll_Freshness_Pkg ----------------------------------------

// @ ensures res == (frameSeq > lastSeq)
// @ ensures frameSeq <= lastSeq ==> !res
// @ ensures (lastSeq == 0 && frameSeq > 0) ==> res
// @ decreases
// @ pure
func IsFresh(lastSeq, frameSeq uint64) (res bool) {
	return frameSeq > lastSeq
}
