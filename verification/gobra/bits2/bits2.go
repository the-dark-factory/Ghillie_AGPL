package bits2

// A: OR of shifted bytes (the real codec shape)
// @ ensures r == v
// @ decreases
func A(v uint16) (r uint16) {
	return uint16(byte(v>>8))<<8 | uint16(byte(v))
}

// B: same but ADD instead of OR (equivalent: the fields do not overlap)
// @ ensures r == v
// @ decreases
func B(v uint16) (r uint16) {
	return uint16(byte(v>>8))*256 + uint16(byte(v))
}

// C: does Gobra know byte(x) == x % 256 ?
// @ ensures r == uint16(v % 256)
// @ decreases
func C(v uint16) (r uint16) { return uint16(byte(v)) }

// D: does Gobra know v>>8 == v / 256 ?
// @ ensures r == v / 256
// @ decreases
func D(v uint16) (r uint16) { return v >> 8 }

// E: does Gobra know x<<8 == x * 256 ?
// @ ensures r == uint16(v) * 256
// @ decreases
func E(v uint8) (r uint16) { return uint16(v) << 8 }

// F: pure integer identity, no bit ops at all
// @ ensures r == v
// @ decreases
func F(v uint16) (r uint16) { return (v/256)*256 + v%256 }
