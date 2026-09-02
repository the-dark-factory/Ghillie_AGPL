// EXPERIMENT B: the Ada postconditions of ledger 112 / 120 transliterated
// with the DOMAIN PRECONDITION Ada gets free from its enumeration type. Predicate bodies are verbatim from
// ghillie internal/gate/gate.go @ c4440f4.
package gateB

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

type Consent uint8

const (
	NoConsent     Consent = 0
	SessionOnly   Consent = 1
	FreshExplicit Consent = 2
)

// Ada Post: Command_Rank'Result = Command_Type'Pos (C)
//       and then Command_Rank'Result <= 5
//       and then (if C = Report_Status then Command_Rank'Result = 0);
// @ requires c <= MaxCommand   // <-- ADDED. Ada gets this from Command_Type itself.
// @ ensures res == int(c)
// @ ensures res <= 5
// @ ensures c == ReportStatus ==> res == 0
// @ decreases
// @ pure
func CommandRank(c Command) (res int) { return int(c) }

// Ada Post: Consent_Rank'Result = Consent_Type'Pos (K)
//       and then Consent_Rank'Result <= 2;
// @ requires k <= FreshExplicit  // <-- ADDED. Ada gets this from Consent_Type itself.
// @ ensures res == int(k)
// @ ensures res <= 2
// @ decreases
// @ pure
func ConsentRank(k Consent) (res int) { return int(k) }

// Ada Post: Requires_Consent'Result = (Command_Rank (C) >= Command_Type'Pos (Install_Artifact));
// @ requires c <= MaxCommand
// @ ensures res == (CommandRank(c) >= CommandRank(InstallArtifact))
// @ decreases
// @ pure
func RequiresConsent(c Command) (res bool) {
	return CommandRank(c) >= CommandRank(InstallArtifact)
}

// Ada Post: Is_Local_Only'Result = (C = Request_Spec_Upload);
// @ ensures res == (c == RequestSpecUpload)
// @ decreases
// @ pure
func IsLocalOnly(c Command) (res bool) { return c == RequestSpecUpload }

// Ada Post (all five conjuncts), transliterated:
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
		(!RequiresConsent(c) || ConsentRank(k) == ConsentRank(FreshExplicit))
}
