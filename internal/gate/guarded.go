//go:build !unguarded

package gate

// Guarded is TRUE in every normal build. See unguarded.go for the other half
// and for why the other half exists at all.
const Guarded = true
