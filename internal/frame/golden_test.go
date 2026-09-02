package frame

import (
	"bufio"
	"encoding/hex"
	"os"
	"strconv"
	"strings"
	"testing"
)

// TestGoldenVectorsMatchLedger119 is the v0 mitigation for an unproven
// marshaller sitting on the security path: it asserts that this package's
// Marshal is BYTE-IDENTICAL to the proven Ada Encode of
// Facade_Instruction_Codec_Pkg (ledger 119) over a vector set covering field
// extremes, zero, all-ones, each field isolated, every command rank, and
// alternating bit patterns across every field boundary.
//
// The vectors in testdata were produced by BUILDING AND RUNNING that core, not
// by reasoning about it. testdata/README.md records exactly how, including the
// source hash that ties the copy used to the ledger record.
//
// What this test does and does not establish: it establishes equivalence ON
// THESE VECTORS. It is not a proof of equivalence, and this Go code remains
// unproven shim. See the package comment.
func TestGoldenVectorsMatchLedger119(t *testing.T) {
	vectors := loadVectors(t)
	if len(vectors) == 0 {
		t.Fatal("no golden vectors loaded — testdata missing or empty")
	}
	t.Logf("checking %d vectors from the proven Ada Encode (ledger 119)", len(vectors))

	for _, v := range vectors {
		t.Run(v.name, func(t *testing.T) {
			got := v.in.Marshal()
			gotHex := hex.EncodeToString(got[:])
			if gotHex != v.wantHex {
				t.Errorf("Marshal mismatch against proven Ada Encode\n  input:    %+v\n  Go:       %s\n  Ada(119): %s", v.in, gotHex, v.wantHex)
			}
		})
	}
}

// TestGoldenVectorsDecodeBack checks the other direction over the same vectors:
// Unmarshal of the PROVEN core's own bytes must reproduce the fields the core
// was given. A marshaller that agrees on encoding but disagrees on decoding
// would still be wrong on the wire.
func TestGoldenVectorsDecodeBack(t *testing.T) {
	for _, v := range loadVectors(t) {
		t.Run(v.name, func(t *testing.T) {
			raw, err := hex.DecodeString(v.wantHex)
			if err != nil {
				t.Fatalf("bad vector hex: %v", err)
			}
			got, err := Unmarshal(raw)
			if err != nil {
				t.Fatalf("Unmarshal of proven Ada bytes failed: %v", err)
			}
			if got != v.in {
				t.Errorf("Unmarshal of Ada bytes\n  got:  %+v\n  want: %+v", got, v.in)
			}
		})
	}
}

// goldenVector is one line of testdata: the fields the Ada core was handed and
// the bytes it produced from them.
type goldenVector struct {
	name    string
	in      Instruction
	wantHex string
}

const goldenPath = "testdata/ledger119_vectors.txt"

// loadVectors parses the golden vector file. Format, one vector per line:
//
//	name|command|protocol|artifact_ref|version|seq|wire_hex
func loadVectors(t *testing.T) []goldenVector {
	t.Helper()
	f, err := os.Open(goldenPath)
	if err != nil {
		t.Fatalf("open golden vectors: %v", err)
	}
	defer func() {
		if cerr := f.Close(); cerr != nil {
			t.Errorf("close golden vectors: %v", cerr)
		}
	}()

	var out []goldenVector
	sc := bufio.NewScanner(f)
	for line := 1; sc.Scan(); line++ {
		text := strings.TrimSpace(sc.Text())
		if text == "" || strings.HasPrefix(text, "#") {
			continue
		}
		parts := strings.Split(text, "|")
		if len(parts) != 7 {
			t.Fatalf("%s:%d: want 7 fields, got %d", goldenPath, line, len(parts))
		}
		command, err := strconv.ParseUint(parts[1], 10, 8)
		if err != nil {
			t.Fatalf("%s:%d: command: %v", goldenPath, line, err)
		}
		protocol, err := strconv.ParseUint(parts[2], 10, 8)
		if err != nil {
			t.Fatalf("%s:%d: protocol: %v", goldenPath, line, err)
		}
		artifactRef, err := strconv.ParseUint(parts[3], 10, 64)
		if err != nil {
			t.Fatalf("%s:%d: artifact_ref: %v", goldenPath, line, err)
		}
		version, err := strconv.ParseUint(parts[4], 10, 32)
		if err != nil {
			t.Fatalf("%s:%d: version: %v", goldenPath, line, err)
		}
		seq, err := strconv.ParseUint(parts[5], 10, 64)
		if err != nil {
			t.Fatalf("%s:%d: seq: %v", goldenPath, line, err)
		}
		wantHex := strings.ToLower(parts[6])
		if len(wantHex) != Size*2 {
			t.Fatalf("%s:%d: wire hex is %d chars, want %d", goldenPath, line, len(wantHex), Size*2)
		}
		out = append(out, goldenVector{
			name: parts[0],
			in: Instruction{
				Command:     uint8(command),
				Protocol:    uint8(protocol),
				ArtifactRef: artifactRef,
				Version:     uint32(version),
				Seq:         seq,
			},
			wantHex: wantHex,
		})
	}
	if err := sc.Err(); err != nil {
		t.Fatalf("read golden vectors: %v", err)
	}
	return out
}
