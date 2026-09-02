// ordering: the terminal.go admission path with all I/O replaced by TRUSTED
// STUBS, so the ordering discipline itself can be proved. Control flow is a
// transliteration of Terminal.handle @ c4440f4.
package ordering

type Command uint8

const (
	ReportStatus      Command = 0
	RequestSpecUpload Command = 3
	InstallArtifact   Command = 4
	RunLocalCode      Command = 5
)

const MaxCommand = RunLocalCode
const ProtocolV0 = 1

type Consent uint8

const FreshExplicit Consent = 2

// @ requires c <= MaxCommand
// @ ensures res == int(c)
// @ decreases
// @ pure
func CommandRank(c Command) (res int) { return int(c) }

// @ requires k <= FreshExplicit
// @ ensures res == int(k)
// @ decreases
// @ pure
func ConsentRank(k Consent) (res int) { return int(k) }

// @ requires c <= MaxCommand
// @ ensures res == (CommandRank(c) >= CommandRank(InstallArtifact))
// @ decreases
// @ pure
func RequiresConsent(c Command) (res bool) { return CommandRank(c) >= CommandRank(InstallArtifact) }

// @ ensures res == (c == RequestSpecUpload)
// @ decreases
// @ pure
func IsLocalOnly(c Command) (res bool) { return c == RequestSpecUpload }

// @ requires c <= MaxCommand && ceiling <= MaxCommand && k <= FreshExplicit
// @ ensures res == (authentic && !IsLocalOnly(c) && CommandRank(c) <= CommandRank(ceiling) && (RequiresConsent(c) ==> ConsentRank(k) == ConsentRank(FreshExplicit)))
// @ ensures !authentic ==> !res
// @ ensures IsLocalOnly(c) ==> !res
// @ decreases
// @ pure
func MayCommand(c Command, ceiling Command, k Consent, authentic bool) (res bool) {
	return authentic && !IsLocalOnly(c) &&
		CommandRank(c) <= CommandRank(ceiling) &&
		(!RequiresConsent(c) || ConsentRank(k) == ConsentRank(FreshExplicit))
}

// @ ensures res == (frameSeq > lastSeq)
// @ decreases
// @ pure
func IsFresh(lastSeq, frameSeq uint64) (res bool) { return frameSeq > lastSeq }

type Instruction struct {
	Command     uint8
	Protocol    uint8
	ArtifactRef uint64
	Version     uint32
	Frameseq    uint64
}

// ---- TRUSTED STUBS (crypto/ed25519, internal/frame) ------------------------
// Abstract and uninterpreted: the proof assumes NOTHING about them except that
// they are deterministic functions of their arguments. That is exactly the
// trust boundary a C-ABI front would also have.

// @ decreases
// @ pure
func VerifySig(key uint64, raw [22]byte, sig [64]byte) (ok bool)

// @ decreases
// @ pure
func Unmarshal(raw [22]byte) (out Instruction)

// ---- THE ADMISSION PATH ----------------------------------------------------
// Transliteration of Terminal.handle. Returns whether the instruction was
// admitted, and the new stored sequence number.
//
// THEOREMS:
//  T1 ADMIT-IMPLIES-AUTHENTIC   admitted ==> the signature verified over the RAW bytes
//  T2 ADMIT-IMPLIES-FRESH       admitted ==> the sequence was strictly newer
//  T3 ADMIT-IMPLIES-GATE        admitted ==> MayCommand held (no second opinion)
//  T4 ADMIT-IMPLIES-INBOUNDS    admitted ==> protocol and command byte were in range
//  T5 SEQ-ADVANCES-ONLY-IF-AUTHENTIC  the stored seq moves only for an authentic, fresh frame
//  T6 SEQ-MONOTONE              the stored seq never goes backwards
//
// @ requires ceiling <= MaxCommand && k <= FreshExplicit
// @ ensures  admitted ==> VerifySig(key, raw, sig)
// @ ensures  admitted ==> IsFresh(lastSeq, Unmarshal(raw).Frameseq)
// @ ensures  admitted ==> Unmarshal(raw).Protocol == ProtocolV0
// @ ensures  admitted ==> Unmarshal(raw).Command <= uint8(MaxCommand)
// @ ensures  admitted ==> MayCommand(Command(Unmarshal(raw).Command), ceiling, k, true)
// @ ensures  newLastSeq != lastSeq ==> VerifySig(key, raw, sig)
// @ ensures  newLastSeq != lastSeq ==> newLastSeq == Unmarshal(raw).Frameseq
// @ ensures  newLastSeq >= lastSeq
// @ decreases
func Handle(key uint64, raw [22]byte, sig [64]byte, lastSeq uint64,
	ceiling Command, k Consent) (admitted bool, newLastSeq uint64) {

	// 1. AUTHENTICITY over the exact raw bytes, BEFORE anything is decoded.
	authentic := VerifySig(key, raw, sig)

	// 2. DECODE.
	instr := Unmarshal(raw)

	if !authentic {
		return false, lastSeq
	}

	// 3. BOUNDARY CHECKS Ada gets from its type system.
	if instr.Protocol != ProtocolV0 {
		return false, lastSeq
	}
	if instr.Command > uint8(MaxCommand) {
		return false, lastSeq
	}
	command := Command(instr.Command)

	// 4. FRESHNESS, before the gate runs.
	newLastSeq = instr.Frameseq
	if !IsFresh(lastSeq, instr.Frameseq) {
		return false, newLastSeq
	}

	// 5. THE GATE.
	if !MayCommand(command, ceiling, k, authentic) {
		return false, newLastSeq
	}
	return true, newLastSeq
}
