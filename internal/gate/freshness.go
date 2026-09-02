package gate

// This file mirrors Poll_Freshness_Pkg (ada-factory ledger 120, forged and
// admitted 2026-07-29, grade 3/3) — the anti-replay decider. As with gate.go
// there is no logic here by design.
//
// The Ada, verbatim:
//
//	function Is_Fresh (Last_Seq : Unsigned_64; Frame_Seq : Unsigned_64)
//	   return Boolean is (Frame_Seq > Last_Seq)
//	   with Post => (Is_Fresh'Result = (Frame_Seq > Last_Seq))
//	     and then (if Frame_Seq <= Last_Seq then not Is_Fresh'Result)
//	     and then (if Last_Seq = 0 and then Frame_Seq > 0 then Is_Fresh'Result);
//
// Theorems: STRICTLY-INCREASES, STALE-IS-REFUSED, FIRST-FRAME-ACCEPTED.
//
// HONEST LABEL: unproven shim, cross-checked against ledger 120 by table test.

// IsFresh reports whether a frame's sequence number is strictly newer than the
// last sequence this claw accepted. A frame that is not strictly newer is
// REFUSED — replay stops being "harmless-ish" and becomes a theorem.
//
// The stored last-sequence lives in the thin edge outside the proven core; this
// function only decides. Empty state is last-sequence zero, so any positive
// first frame is fresh.
func IsFresh(lastSeq, frameSeq uint64) bool {
	return frameSeq > lastSeq
}
