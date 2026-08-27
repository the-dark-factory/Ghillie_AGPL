package frame

import (
	"encoding/hex"
	"errors"
	"testing"
)

// TestMarshalUnmarshalRoundTrip mirrors the proven core's round-trip
// postcondition (Decode (Encode'Result) = R, per field) on the Go side. It is
// the weaker, testing-grade sibling of that theorem; the golden test is what
// ties the bytes themselves to the proven core.
func TestMarshalUnmarshalRoundTrip(t *testing.T) {
	tests := []struct {
		name string
		in   Instruction
	}{
		{"zero", Instruction{}},
		{"all ones", Instruction{Command: 0xFF, Protocol: 0xFF, ArtifactRef: ^uint64(0), Version: ^uint32(0), Seq: ^uint64(0)}},
		{"typical deliver", Instruction{Command: 2, Protocol: ProtocolV0, ArtifactRef: 0xDEADBEEFCAFEF00D, Version: 7, Seq: 3}},
		{"seq only", Instruction{Seq: 1}},
		{"version only", Instruction{Version: 1}},
		{"artifact ref only", Instruction{ArtifactRef: 1}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			b := tt.in.Marshal()
			got, err := Unmarshal(b[:])
			if err != nil {
				t.Fatalf("Unmarshal: %v", err)
			}
			if got != tt.in {
				t.Errorf("round trip: got %+v, want %+v", got, tt.in)
			}
		})
	}
}

// TestUnmarshalLength checks the one judgement the codec does make: a buffer
// that is not exactly Size bytes is refused rather than padded or truncated.
func TestUnmarshalLength(t *testing.T) {
	tests := []struct {
		name    string
		n       int
		wantErr bool
	}{
		{"empty", 0, true},
		{"one short", Size - 1, true},
		{"exact", Size, false},
		{"one long", Size + 1, true},
		{"double", Size * 2, true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := Unmarshal(make([]byte, tt.n))
			switch {
			case tt.wantErr && err == nil:
				t.Fatalf("Unmarshal(%d bytes): want error, got nil", tt.n)
			case !tt.wantErr && err != nil:
				t.Fatalf("Unmarshal(%d bytes): unexpected error %v", tt.n, err)
			case tt.wantErr && !errors.Is(err, ErrShortFrame):
				t.Fatalf("Unmarshal(%d bytes): error %v does not wrap ErrShortFrame", tt.n, err)
			}
		})
	}
}

// TestFieldOffsets pins the byte offsets independently of the golden file, so a
// layout change is caught even if someone regenerates testdata carelessly.
func TestFieldOffsets(t *testing.T) {
	tests := []struct {
		name    string
		in      Instruction
		wantHex string
	}{
		{"command at 0", Instruction{Command: 0xAB}, "ab" + "00" + "0000000000000000" + "00000000" + "0000000000000000"},
		{"protocol at 1", Instruction{Protocol: 0xCD}, "00" + "cd" + "0000000000000000" + "00000000" + "0000000000000000"},
		{"artifact ref at 2..9", Instruction{ArtifactRef: 0x0123456789ABCDEF}, "00" + "00" + "0123456789abcdef" + "00000000" + "0000000000000000"},
		{"version at 10..13", Instruction{Version: 0x01234567}, "00" + "00" + "0000000000000000" + "01234567" + "0000000000000000"},
		{"seq at 14..21", Instruction{Seq: 0xFEDCBA9876543210}, "00" + "00" + "0000000000000000" + "00000000" + "fedcba9876543210"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			b := tt.in.Marshal()
			if got := hex.EncodeToString(b[:]); got != tt.wantHex {
				t.Errorf("layout: got %s, want %s", got, tt.wantHex)
			}
		})
	}
}
