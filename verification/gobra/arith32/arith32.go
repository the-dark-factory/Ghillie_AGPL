package arith32

// 32-bit codec in PURE INTEGER ARITHMETIC (no <<, >>, |).
// @ ensures r == v
// @ decreases
func RT32(v uint32) (r uint32) {
	b0 := byte(v / 16777216 % 256)
	b1 := byte(v / 65536 % 256)
	b2 := byte(v / 256 % 256)
	b3 := byte(v % 256)
	return uint32(b0)*16777216 + uint32(b1)*65536 + uint32(b2)*256 + uint32(b3)
}
