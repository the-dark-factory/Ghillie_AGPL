package probe

// P1: index-assign a local array (no slicing)
// @ ensures b[0] == x
func P1(x byte) (b [4]byte) {
	b[0] = x
	return b
}

// P2: slice a local array
func P2() []byte {
	var b [4]byte
	return b[:]
}

// P3: read-index a slice parameter
// @ requires len(s) == 4
// @ requires acc(&s[0], 1/2)
// @ ensures  acc(&s[0], 1/2)
// @ ensures  r == s[0]
func P3(s []byte) (r byte) { return s[0] }

// P4: shift arithmetic on uint64 -> byte
// @ ensures r == byte(v >> 56)
func P4(v uint64) (r byte) { return byte(v >> 56) }

// P5: build uint64 from bytes
// @ ensures r == uint64(a) << 8 | uint64(c)
func P5(a, c byte) (r uint64) { return uint64(a)<<8 | uint64(c) }
