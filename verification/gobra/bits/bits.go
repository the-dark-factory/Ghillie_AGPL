package bits

// 1 byte
// @ ensures r == v
// @ decreases
func RT8(v uint8) (r uint8) { b := v; return b }

// 2 bytes
// @ ensures r == v
// @ decreases
func RT16(v uint16) (r uint16) {
	b0 := byte(v >> 8)
	b1 := byte(v)
	return uint16(b0)<<8 | uint16(b1)
}

// 4 bytes
// @ ensures r == v
// @ decreases
func RT32(v uint32) (r uint32) {
	b0 := byte(v >> 24)
	b1 := byte(v >> 16)
	b2 := byte(v >> 8)
	b3 := byte(v)
	return uint32(b0)<<24 | uint32(b1)<<16 | uint32(b2)<<8 | uint32(b3)
}

// 8 bytes
// @ ensures r == v
// @ decreases
func RT64(v uint64) (r uint64) {
	b0 := byte(v >> 56)
	b1 := byte(v >> 48)
	b2 := byte(v >> 40)
	b3 := byte(v >> 32)
	b4 := byte(v >> 24)
	b5 := byte(v >> 16)
	b6 := byte(v >> 8)
	b7 := byte(v)
	return uint64(b0)<<56 | uint64(b1)<<48 | uint64(b2)<<40 | uint64(b3)<<32 |
		uint64(b4)<<24 | uint64(b5)<<16 | uint64(b6)<<8 | uint64(b7)
}
