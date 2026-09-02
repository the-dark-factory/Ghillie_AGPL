package negctl

// TRUE postcondition — must verify.
// @ ensures res == a + b
func Add(a, b int) (res int) {
	return a + b
}

// FALSE postcondition — Gobra MUST reject this.
// @ ensures res == a + b + 1
func AddWrong(a, b int) (res int) {
	return a + b
}
