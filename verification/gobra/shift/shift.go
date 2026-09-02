package shift

// @ ensures r == v >> 8
// @ decreases
func S(v uint16) (r uint16) { return v >> 8 }
