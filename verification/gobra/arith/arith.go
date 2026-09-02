package arith

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

// 64-bit codec in pure integer arithmetic.
// @ ensures r == v
// @ decreases
func RT64(v uint64) (r uint64) {
	b0 := byte(v / 72057594037927936 % 256)
	b1 := byte(v / 281474976710656 % 256)
	b2 := byte(v / 1099511627776 % 256)
	b3 := byte(v / 4294967296 % 256)
	b4 := byte(v / 16777216 % 256)
	b5 := byte(v / 65536 % 256)
	b6 := byte(v / 256 % 256)
	b7 := byte(v % 256)
	return uint64(b0)*72057594037927936 + uint64(b1)*281474976710656 +
		uint64(b2)*1099511627776 + uint64(b3)*4294967296 +
		uint64(b4)*16777216 + uint64(b5)*65536 + uint64(b6)*256 + uint64(b7)
}
