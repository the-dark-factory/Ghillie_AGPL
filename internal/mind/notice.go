package mind

// notice.go holds the words the owner reads when there is no mind on this
// machine. It is a separate file because it is the part of this package that
// is a PROMISE rather than plumbing: no mind, no pretence.

import "fmt"

// NoMindNotice is what a conversational surface says when nothing answers at
// the mind URL.
//
// ★ IT NEVER APOLOGISES FOR A MISSING FEATURE, BECAUSE NOTHING IS MISSING. A
// ghillie with no mind is the whole product: the catalogue reads, abilities
// install, proofs verify, the facade protocol runs, the interview is conducted.
// What is absent is free conversation, and the honest thing is to name that
// absence and say where the owner would plug one in — not to answer anyway
// from a canned script, and never to reach for somebody else's inference.
func NoMindNotice(url string) string {
	return fmt.Sprintf(
		"no mind is set on this machine — point -mind-url at your own model (looked at %s).\n"+
			"  He needs no mind to do his job: the catalogue, ability installs, proofs, the\n"+
			"  facade protocol and the interview all work exactly as they do now. A mind adds\n"+
			"  free conversation, and it is YOURS: install ollama, pull a model, run it here.\n"+
			"  Nothing is asked of anyone else's compute, and nothing you say leaves this machine.",
		url)
}
