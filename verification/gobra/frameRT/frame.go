// frameRT: the ledger 119 round-trip theorem, Decode(Encode(R)).F == R.F.
// binary.BigEndian.PutUintNN is inlined as explicit shifts (GOBRA-WORKAROUND:
// Gobra cannot slice a local array, which is what the stdlib calls need).
package frameRT

const Size = 22

type Instruction struct {
	Command     uint8
	Protocol    uint8
	ArtifactRef uint64
	Version     uint32
	Seq         uint64
}

// @ ensures b[0] == i.Command
// @ ensures b[1] == i.Protocol
// @ ensures b[2] == byte(i.ArtifactRef >> 56) && b[3] == byte(i.ArtifactRef >> 48) && b[4] == byte(i.ArtifactRef >> 40) && b[5] == byte(i.ArtifactRef >> 32)
// @ ensures b[6] == byte(i.ArtifactRef >> 24) && b[7] == byte(i.ArtifactRef >> 16) && b[8] == byte(i.ArtifactRef >> 8) && b[9] == byte(i.ArtifactRef)
// @ ensures b[10] == byte(i.Version >> 24) && b[11] == byte(i.Version >> 16) && b[12] == byte(i.Version >> 8) && b[13] == byte(i.Version)
// @ ensures b[14] == byte(i.Seq >> 56) && b[15] == byte(i.Seq >> 48) && b[16] == byte(i.Seq >> 40) && b[17] == byte(i.Seq >> 32)
// @ ensures b[18] == byte(i.Seq >> 24) && b[19] == byte(i.Seq >> 16) && b[20] == byte(i.Seq >> 8) && b[21] == byte(i.Seq)
// @ decreases
func Marshal(i Instruction) (b [Size]byte) {
	b[0] = i.Command
	b[1] = i.Protocol
	b[2] = byte(i.ArtifactRef >> 56)
	b[3] = byte(i.ArtifactRef >> 48)
	b[4] = byte(i.ArtifactRef >> 40)
	b[5] = byte(i.ArtifactRef >> 32)
	b[6] = byte(i.ArtifactRef >> 24)
	b[7] = byte(i.ArtifactRef >> 16)
	b[8] = byte(i.ArtifactRef >> 8)
	b[9] = byte(i.ArtifactRef)
	b[10] = byte(i.Version >> 24)
	b[11] = byte(i.Version >> 16)
	b[12] = byte(i.Version >> 8)
	b[13] = byte(i.Version)
	b[14] = byte(i.Seq >> 56)
	b[15] = byte(i.Seq >> 48)
	b[16] = byte(i.Seq >> 40)
	b[17] = byte(i.Seq >> 32)
	b[18] = byte(i.Seq >> 24)
	b[19] = byte(i.Seq >> 16)
	b[20] = byte(i.Seq >> 8)
	b[21] = byte(i.Seq)
	return b
}

// @ ensures out.Command == b[0]
// @ ensures out.Protocol == b[1]
// @ ensures out.ArtifactRef == uint64(b[2])<<56 | uint64(b[3])<<48 | uint64(b[4])<<40 | uint64(b[5])<<32 | uint64(b[6])<<24 | uint64(b[7])<<16 | uint64(b[8])<<8 | uint64(b[9])
// @ ensures out.Version == uint32(b[10])<<24 | uint32(b[11])<<16 | uint32(b[12])<<8 | uint32(b[13])
// @ ensures out.Seq == uint64(b[14])<<56 | uint64(b[15])<<48 | uint64(b[16])<<40 | uint64(b[17])<<32 | uint64(b[18])<<24 | uint64(b[19])<<16 | uint64(b[20])<<8 | uint64(b[21])
// @ decreases
func Unmarshal(b [Size]byte) (out Instruction) {
	return Instruction{
		Command:     b[0],
		Protocol:    b[1],
		ArtifactRef: uint64(b[2])<<56 | uint64(b[3])<<48 | uint64(b[4])<<40 | uint64(b[5])<<32 | uint64(b[6])<<24 | uint64(b[7])<<16 | uint64(b[8])<<8 | uint64(b[9]),
		Version:     uint32(b[10])<<24 | uint32(b[11])<<16 | uint32(b[12])<<8 | uint32(b[13]),
		Seq:         uint64(b[14])<<56 | uint64(b[15])<<48 | uint64(b[16])<<40 | uint64(b[17])<<32 | uint64(b[18])<<24 | uint64(b[19])<<16 | uint64(b[20])<<8 | uint64(b[21]),
	}
}

// THE LEDGER 119 THEOREM, per field.
// @ ensures out.Command == i.Command
// @ ensures out.Protocol == i.Protocol
// @ ensures out.ArtifactRef == i.ArtifactRef
// @ ensures out.Version == i.Version
// @ ensures out.Seq == i.Seq
// @ decreases
func RoundTrip(i Instruction) (out Instruction) {
	return Unmarshal(Marshal(i))
}
