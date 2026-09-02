package voice

// Tests without an ear: nothing here pretends to judge sound. What is tested
// is STRUCTURE — that every breath in a plan carries the engine's license,
// that the exec boundary refuses dishonest output, that the breath pick is
// deterministic and stays inside the approved speaker/class discipline, and
// that the WAV plumbing round-trips. Whether it sounds right is Tony's ear's
// jurisdiction, exercised by the live smoke run, not by a unit test.

import (
	"context"
	"errors"
	"fmt"
	"math"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestPlanValidateHoldsTheAuthorityRule holds Plan.Validate to the settled
// pipeline's rule: the engine decides whether and how many — every breath must
// carry an engine verdict, and a breath the engine did not schedule is refused.
func TestPlanValidateHoldsTheAuthorityRule(t *testing.T) {
	cases := []struct {
		name    string
		plan    Plan
		wantErr bool
	}{
		{
			name: "validated and forced breaths pass",
			plan: Plan{
				Breaths: []PlanBreath{
					{Words: "built. | Its", Sample: "medium_M_1089_a.wav", Engine: PlanEngine{Tick: 12, Depth: 430, Verdict: "validated"}},
					{Words: "down, | away", Sample: "medium_M_1089_b.wav", Engine: PlanEngine{Tick: 267, Depth: 620, Verdict: "forced"}},
					{Words: "(no text candidate)", Sample: "short_M_1089_c.wav", Engine: PlanEngine{Tick: 300, Depth: 400, Verdict: "forced/off-plan"}},
				},
				EngineEventsTotal: 3,
			},
		},
		{
			name: "a breathless utterance is legal — the engine may decline everything",
			plan: Plan{Breaths: nil, EngineEventsTotal: 2},
		},
		{
			name: "a breath with no engine verdict is an invented breath",
			plan: Plan{
				Breaths:           []PlanBreath{{Words: "x | y", Sample: "s.wav"}},
				EngineEventsTotal: 1,
			},
			wantErr: true,
		},
		{
			name: "a verdict outside the harness vocabulary is refused",
			plan: Plan{
				Breaths:           []PlanBreath{{Engine: PlanEngine{Tick: 3, Depth: 400, Verdict: "aesthetic"}}},
				EngineEventsTotal: 1,
			},
			wantErr: true,
		},
		{
			name: "tick zero cannot have come from the 1-based schedule",
			plan: Plan{
				Breaths:           []PlanBreath{{Engine: PlanEngine{Tick: 0, Depth: 400, Verdict: "validated"}}},
				EngineEventsTotal: 1,
			},
			wantErr: true,
		},
		{
			name: "non-positive depth is not an engine command",
			plan: Plan{
				Breaths:           []PlanBreath{{Engine: PlanEngine{Tick: 5, Depth: 0, Verdict: "validated"}}},
				EngineEventsTotal: 1,
			},
			wantErr: true,
		},
		{
			name: "more breaths than engine events means somebody added one",
			plan: Plan{
				Breaths: []PlanBreath{
					{Engine: PlanEngine{Tick: 1, Depth: 400, Verdict: "validated"}},
					{Engine: PlanEngine{Tick: 9, Depth: 400, Verdict: "validated"}},
				},
				EngineEventsTotal: 1,
			},
			wantErr: true,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			err := tc.plan.Validate()
			if tc.wantErr && !errors.Is(err, ErrPlanViolation) {
				t.Fatalf("Validate() = %v, want ErrPlanViolation", err)
			}
			if !tc.wantErr && err != nil {
				t.Fatalf("Validate() = %v, want nil", err)
			}
		})
	}
}

// TestArgsIsReproducible pins the exported invocation shape: script then
// output, text via TP_TEXT — what a person types to reproduce a render by hand.
func TestArgsIsReproducible(t *testing.T) {
	tp := &TextPlan{ScriptPath: "/r/demo/textplan.py"}
	got := tp.Args("/out/utt.wav")
	if len(got) != 2 || got[0] != "/r/demo/textplan.py" || got[1] != "/out/utt.wav" {
		t.Fatalf("Args = %v", got)
	}
}

// TestRenderRefusesMissingConfigAndTmp holds the renderer to its explicit
// preconditions, including the standing rule that renders never land in /tmp.
func TestRenderRefusesMissingConfigAndTmp(t *testing.T) {
	cases := []struct {
		name string
		tp   *TextPlan
	}{
		{"no python", &TextPlan{ScriptPath: "s", RenderDir: "/x"}},
		{"no script", &TextPlan{PythonPath: "p", RenderDir: "/x"}},
		{"no render dir", &TextPlan{PythonPath: "p", ScriptPath: "s"}},
		{"render dir under /tmp", &TextPlan{PythonPath: "p", ScriptPath: "s", RenderDir: "/tmp/renders"}},
		{"render dir under /private/tmp", &TextPlan{PythonPath: "p", ScriptPath: "s", RenderDir: "/private/tmp/renders"}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if _, err := tc.tp.Render(context.Background(), "hello"); !errors.Is(err, ErrNotConfigured) {
				t.Fatalf("Render = %v, want ErrNotConfigured", err)
			}
		})
	}
}

// fakeHarness writes a shell script that stands in for textplan.py at the real
// exec boundary: it writes a WAV and a sidecar of the given JSON, so Render's
// whole path — spawn, output check, sidecar validation — runs without python.
func fakeHarness(t *testing.T, dir, planJSON string) *TextPlan {
	t.Helper()
	script := filepath.Join(dir, "fake-textplan.sh")
	wav := make([]float64, 2400)
	tmp := filepath.Join(dir, "template.wav")
	if err := writeWAV(tmp, wav, 24000); err != nil {
		t.Fatalf("template wav: %v", err)
	}
	body := fmt.Sprintf("#!/bin/sh\ncp %s \"$1\"\ncat > \"$1.json\" <<'EOF'\n%s\nEOF\n", tmp, planJSON)
	if err := os.WriteFile(script, []byte(body), 0o755); err != nil {
		t.Fatalf("write fake harness: %v", err)
	}
	return &TextPlan{PythonPath: "/bin/sh", ScriptPath: script, RenderDir: filepath.Join(dir, "renders")}
}

// TestRenderAcceptsAnEngineLicensedPlan runs the exec boundary against a fake
// harness whose sidecar is honest, and checks the Rendering that comes back.
func TestRenderAcceptsAnEngineLicensedPlan(t *testing.T) {
	tp := fakeHarness(t, t.TempDir(), `{"text":"hello there.","breaths":[
	  {"words":"there. | And","sample":"medium_M_1089_x.wav",
	   "engine":{"tick":7,"depth":430,"verdict":"validated"}}],
	  "engine_events_total":1,"duration_ms":1234,"harness_rev":"breath-4-textplan-r2"}`)
	r, err := tp.Render(context.Background(), "hello there.")
	if err != nil {
		t.Fatalf("Render: %v", err)
	}
	if r.Breaths != 1 || r.Audio.Milliseconds() != 1234 {
		t.Fatalf("Rendering = %+v", r)
	}
	if _, err := os.Stat(r.WavPath); err != nil {
		t.Fatalf("wav missing: %v", err)
	}
	if _, err := os.Stat(r.PlanPath); err != nil {
		t.Fatalf("sidecar missing: %v", err)
	}
}

// TestRenderRefusesAnInventedBreathAtTheExecBoundary is test (a) where it
// bites: a harness whose sidecar carries a breath with no engine verdict is
// refused at Render, so an invented breath can never reach the surface at all.
func TestRenderRefusesAnInventedBreathAtTheExecBoundary(t *testing.T) {
	tp := fakeHarness(t, t.TempDir(), `{"text":"hm.","breaths":[
	  {"words":"hm. | so","sample":"medium_M_1089_x.wav"}],
	  "engine_events_total":1,"duration_ms":500,"harness_rev":"breath-4-textplan-r2"}`)
	if _, err := tp.Render(context.Background(), "hm."); !errors.Is(err, ErrPlanViolation) {
		t.Fatalf("Render = %v, want ErrPlanViolation", err)
	}
}

// TestRenderReportsAFailedHarnessHonestly checks the failure shape: non-zero
// exit is ErrRenderFailed carrying the command for hand reproduction.
func TestRenderReportsAFailedHarnessHonestly(t *testing.T) {
	dir := t.TempDir()
	script := filepath.Join(dir, "broken.sh")
	if err := os.WriteFile(script, []byte("#!/bin/sh\necho 'no such voice' >&2\nexit 3\n"), 0o755); err != nil {
		t.Fatalf("write: %v", err)
	}
	tp := &TextPlan{PythonPath: "/bin/sh", ScriptPath: script, RenderDir: filepath.Join(dir, "renders")}
	_, err := tp.Render(context.Background(), "hello")
	if !errors.Is(err, ErrRenderFailed) {
		t.Fatalf("Render = %v, want ErrRenderFailed", err)
	}
	if !strings.Contains(err.Error(), "no such voice") {
		t.Fatalf("error does not carry stderr: %v", err)
	}
}

// testBank builds a small bank on disk: two eligible medium/1089 samples, plus
// decoys in the wrong class and the wrong speaker.
func testBank(t *testing.T) *BreathBank {
	t.Helper()
	dir := t.TempDir()
	rows := []string{
		"file\tduration_s\tkind\tspeaker\tgender\tsource_utterance\tbreath_begin_s\tbreath_end_s",
		"medium_M_1089_aa.wav\t0.31\tmedium\t1089\tM\tx.wav\t1.0\t1.3",
		"medium_M_1089_bb.wav\t0.40\tmedium\t1089\tM\tx.wav\t4.5\t5.0",
		"long_M_1089_cc.wav\t0.53\tlong\t1089\tM\tx.wav\t3.1\t3.7",
		"medium_M_1188_dd.wav\t0.35\tmedium\t1188\tM\tx.wav\t2.0\t2.4",
	}
	if err := os.WriteFile(filepath.Join(dir, "manifest.tsv"), []byte(strings.Join(rows, "\n")+"\n"), 0o644); err != nil {
		t.Fatalf("manifest: %v", err)
	}
	breath := make([]float64, 7200)
	for i := range breath {
		breath[i] = 0.25 * math.Sin(2*math.Pi*180*float64(i)/24000)
	}
	for _, name := range []string{"medium_M_1089_aa.wav", "medium_M_1089_bb.wav", "long_M_1089_cc.wav", "medium_M_1188_dd.wav"} {
		if err := writeWAV(filepath.Join(dir, name), breath, 24000); err != nil {
			t.Fatalf("sample: %v", err)
		}
	}
	return &BreathBank{Dir: dir, Speaker: "1089", Class: "medium", QuietDB: -4.9}
}

// TestBreathPickIsDeterministicAndDisciplined: the same text always draws the
// same sample, and only the configured speaker and class are ever eligible.
func TestBreathPickIsDeterministicAndDisciplined(t *testing.T) {
	b := testBank(t)
	first, err := b.Pick("What is the software for?")
	if err != nil {
		t.Fatalf("Pick: %v", err)
	}
	again, err := b.Pick("What is the software for?")
	if err != nil {
		t.Fatalf("Pick again: %v", err)
	}
	if first != again {
		t.Fatalf("pick is not deterministic: %s vs %s", first, again)
	}
	base := filepath.Base(first)
	if !strings.HasPrefix(base, "medium_M_1089_") {
		t.Fatalf("pick %s left the approved speaker/class discipline", base)
	}
}

// TestBreathPickRefusesAnEmptyPool: no eligible sample is an explicit error,
// never a quiet borrow from another speaker.
func TestBreathPickRefusesAnEmptyPool(t *testing.T) {
	b := testBank(t)
	b.Speaker = "9999"
	if _, err := b.Pick("anything"); !errors.Is(err, ErrNotConfigured) {
		t.Fatalf("Pick = %v, want ErrNotConfigured", err)
	}
}

// TestOfferedFloorAppliesTheApprovedLevel checks the restated gain law: the
// prepared breath's peak equals rms(speech) × 0.35 × 10^(−4.9/20).
func TestOfferedFloorAppliesTheApprovedLevel(t *testing.T) {
	b := testBank(t)
	dir := t.TempDir()
	speech := make([]float64, 24000)
	for i := range speech {
		speech[i] = 0.5 * math.Sin(2*math.Pi*220*float64(i)/24000)
	}
	speechWav := filepath.Join(dir, "q.wav")
	if err := writeWAV(speechWav, speech, 24000); err != nil {
		t.Fatalf("speech wav: %v", err)
	}

	out, err := b.OfferedFloor(context.Background(), "A question?", speechWav)
	if err != nil {
		t.Fatalf("OfferedFloor: %v", err)
	}
	got, rate, err := readWAV(out)
	if err != nil {
		t.Fatalf("read prepared breath: %v", err)
	}
	if rate != 24000 {
		t.Fatalf("rate = %d", rate)
	}
	want := rmsOf(speech) * 0.35 * math.Pow(10, -4.9/20)
	if diff := math.Abs(peakOf(got) - want); diff > 0.002 {
		t.Fatalf("breath peak %.4f, want %.4f (±0.002)", peakOf(got), want)
	}
	if !strings.HasSuffix(out, ".breath.wav") {
		t.Fatalf("prepared breath lands at %s — expected beside the speech", out)
	}
}

// TestWavRoundTrip: what is written is what is read, within 16-bit precision.
func TestWavRoundTrip(t *testing.T) {
	dir := t.TempDir()
	in := make([]float64, 4800)
	for i := range in {
		in[i] = 0.7 * math.Sin(2*math.Pi*100*float64(i)/24000)
	}
	path := filepath.Join(dir, "rt.wav")
	if err := writeWAV(path, in, 24000); err != nil {
		t.Fatalf("write: %v", err)
	}
	out, rate, err := readWAV(path)
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	if rate != 24000 || len(out) != len(in) {
		t.Fatalf("rate %d len %d", rate, len(out))
	}
	for i := range in {
		if math.Abs(in[i]-out[i]) > 2.0/32768 {
			t.Fatalf("sample %d: %.6f vs %.6f", i, in[i], out[i])
		}
	}
}
