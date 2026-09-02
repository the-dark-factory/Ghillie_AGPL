//go:build unguarded

// unguarded.go — the CONTROL ARM. This file exists only under the `unguarded`
// build tag, and it turns ghillie's admission gate off.
//
// WHY THIS EXISTS. The factory intends to measure how much personal data an
// agent leaks doing a real task (project_agent_leakage_measurement). Measuring
// a stock third-party agent and publishing "it leaked" would be a hit piece on
// the very population the factory intends to SELL to (clawkind), and worse, it
// would not isolate anything: a difference between two vendors' agents could be
// explained a dozen ways.
//
// So the honest control is OUR OWN SOFTWARE WITH THE GATE REMOVED. Same code,
// same prompts, same task, one variable. Whatever the ungated build leaks, the
// leak is attributable to the ABSENCE OF THE GATE and not to anyone's brand.
// It also means the first agent publicly shown leaking is ours, which is the
// only version of this experiment we are entitled to run.
//
// ⚠ THIS BUILD IS NOT A PRODUCT AND MUST NEVER REACH A USER.
//   - it is never built by `make build` or `make all` — only `make unguarded`
//   - the binary is named ghillie-UNGUARDED, never ghillie
//   - it refuses to start without GHILLIE_UNGUARDED_I_UNDERSTAND=yes
//   - it prints a banner to stderr on every start
//   - the proven-core mirror tests do not pass under this tag, deliberately:
//     an ungated build has no business claiming equivalence with ledger 112.
package gate

import (
	"fmt"
	"os"
)

// Guarded is FALSE here. MayCommand consults it and stops enforcing the
// ceiling and the consent requirement.
const Guarded = false

func init() {
	const ack = "GHILLIE_UNGUARDED_I_UNDERSTAND"
	if os.Getenv(ack) != "yes" {
		fmt.Fprintf(os.Stderr,
			"\nghillie-unguarded: REFUSING TO START.\n"+
				"This build has its admission gate REMOVED. It is a measurement control for\n"+
				"the leakage experiment, not a product, and it will disclose things the real\n"+
				"ghillie refuses to disclose.\n"+
				"If that is genuinely what you want, set %s=yes.\n\n", ack)
		os.Exit(2)
	}
	fmt.Fprintf(os.Stderr,
		"\n**** ghillie-UNGUARDED — ADMISSION GATE DISABLED ****\n"+
			"**** control arm for the leakage measurement.      ****\n"+
			"**** nothing said here is refused on your behalf.  ****\n\n")
}
