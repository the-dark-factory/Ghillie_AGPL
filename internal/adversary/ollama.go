package adversary

import (
	"os"

	"github.com/tonygair/ghillie/internal/purposetrial"
)

// DefaultAdversaryModel is the model that drives the adversarial pass by default:
// deepseek-r1:32b, the proven local adversarial tester (memory:
// fact_llm_tester_deepseek_adversarial_2026_08_26 — it generated hostile inputs
// on a local ollama). It is DELIBERATELY a different model from the one the
// agent-purpose-trial uses (purposetrial.OllamaModelID, qwen2.5-coder:32b): the
// critic that tries to BREAK an extension must not be the same voice that
// approved it, so the two passes stay independent.
const DefaultAdversaryModel = "deepseek-r1:32b"

// AdversaryModelEnv is the environment variable that overrides which local model
// drives the pass. Availability of any particular model is an ops concern; a
// missing model surfaces as an unreachable model and a fail-closed, incomplete
// record — never a silent pass.
const AdversaryModelEnv = "GHILLIE_ADVERSARY_MODEL"

// AdversaryModelID returns the model id that will drive the pass: the value of
// GHILLIE_ADVERSARY_MODEL when it is set and non-empty, else DefaultAdversaryModel.
func AdversaryModelID() (id string) {
	if v := os.Getenv(AdversaryModelEnv); v != "" {
		return v
	}
	return DefaultAdversaryModel
}

// NewAdversary returns the real Model that drives the pass: the owner's LOCAL
// ollama serving AdversaryModelID(), reached through purposetrial's ollama
// client (the same loopback endpoint, no cloud, no provider key) so this package
// does not duplicate that client. It also returns the model id it selected, to be
// recorded verbatim as the record's model_id.
//
// The concrete *purposetrial.OllamaModel is returned through the shared
// purposetrial.Model seam, so RunAdversarialPass drives the real ollama and a
// test stub identically.
func NewAdversary() (model purposetrial.Model, modelID string) {
	id := AdversaryModelID()
	return purposetrial.NewOllamaModelNamed("", id), id
}
