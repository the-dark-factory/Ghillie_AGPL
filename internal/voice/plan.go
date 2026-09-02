package voice

// plan.go reads and checks the plan sidecar that textplan.py writes beside
// every render. The check is the structural form of the pipeline's authority
// rule: Respire.Lungs (Ada, SPARK) decides WHETHER and HOW MANY breaths — so
// every breath in the sidecar must carry an engine verdict, and a breath
// without one is a breath somebody invented downstream of the engine.

import (
	"encoding/json"
	"fmt"
	"os"
)

// Plan is the subset of the sidecar this seam is entitled to an opinion on.
// The full sidecar carries much more (candidates, crossfade law, provenance);
// it stays on disk, untouched, as the render's evidence.
type Plan struct {
	// Text is the utterance as rendered.
	Text string `json:"text"`

	// Breaths are the breaths actually spliced into the utterance.
	Breaths []PlanBreath `json:"breaths"`

	// EngineEventsTotal is how many breath events the engine scheduled. It can
	// exceed len(Breaths): two events landing in one gap merge, and a forced
	// event with nowhere honest to go is dropped — reported, never hidden.
	EngineEventsTotal int `json:"engine_events_total"`

	// DurationMS is the rendered audio's duration.
	DurationMS int `json:"duration_ms"`

	// HarnessRev names the harness revision that produced the render.
	HarnessRev string `json:"harness_rev"`
}

// PlanBreath is one spliced breath and the engine event that licensed it.
type PlanBreath struct {
	// Words is the text seam the breath sits at.
	Words string `json:"words"`

	// Sample names the breath-bank sample spliced in.
	Sample string `json:"sample"`

	// Engine is the scheduling event. Its presence IS the license.
	Engine PlanEngine `json:"engine"`
}

// PlanEngine is the engine's record of one scheduled breath.
type PlanEngine struct {
	// Tick is the 1-based engine tick the event fired at.
	Tick int `json:"tick"`

	// Depth is the commanded breath depth.
	Depth int `json:"depth"`

	// Verdict is how the harness placed the event: "validated" (the engine
	// took an offered candidate), "forced" (debt hit the limit off-plan; moved
	// to the nearest unused candidate), or "forced/off-plan" (no candidate in
	// the window; retreated to the nearest acoustic pause).
	Verdict string `json:"verdict"`
}

// ReadPlan reads a plan sidecar from disk.
func ReadPlan(path string) (Plan, error) {
	body, err := os.ReadFile(path)
	if err != nil {
		return Plan{}, fmt.Errorf("%w: render wrote no plan sidecar at %s: %w", ErrRenderFailed, path, err)
	}
	var p Plan
	if err := json.Unmarshal(body, &p); err != nil {
		return Plan{}, fmt.Errorf("%w: plan sidecar %s is not the harness's JSON: %w", ErrRenderFailed, path, err)
	}
	return p, nil
}

// Validate holds the plan to the authority rule: every breath came from the
// engine schedule. An utterance with no breaths at all is legal — the engine
// is entitled to decline every opportunity — but a breath without an engine
// event, an impossible tick, or a verdict outside the harness's vocabulary is
// a violation and the render must not be trusted.
func (p Plan) Validate() error {
	for i, b := range p.Breaths {
		switch b.Engine.Verdict {
		case "validated", "forced", "forced/off-plan":
			// the three ways the harness records an ENGINE event
		default:
			return fmt.Errorf("%w: breath %d (%q, sample %s) carries engine verdict %q — not a scheduling verdict; this breath was not the engine's",
				ErrPlanViolation, i, b.Words, b.Sample, b.Engine.Verdict)
		}
		if b.Engine.Tick < 1 {
			return fmt.Errorf("%w: breath %d (%q) at engine tick %d — ticks are 1-based; this event cannot have come from the schedule",
				ErrPlanViolation, i, b.Words, b.Engine.Tick)
		}
		if b.Engine.Depth <= 0 {
			return fmt.Errorf("%w: breath %d (%q) with engine depth %d — the engine commands positive depths",
				ErrPlanViolation, i, b.Words, b.Engine.Depth)
		}
	}
	if len(p.Breaths) > p.EngineEventsTotal {
		return fmt.Errorf("%w: %d breaths spliced from %d engine events — at least %d breath(s) nobody scheduled",
			ErrPlanViolation, len(p.Breaths), p.EngineEventsTotal, len(p.Breaths)-p.EngineEventsTotal)
	}
	return nil
}
