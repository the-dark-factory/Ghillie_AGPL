package fill

// The fill seam holds NO decision — these tables pin the GLUE to its stated
// conduct: facts established honestly (store lookup), the proven front obeyed
// verbatim, the conservative door (ask unaided) on every path where the
// decider could not be asked, and nothing invented, ever.

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/tonygair/ghillie/internal/brief"
	"github.com/tonygair/ghillie/internal/protocol"
)

func testNow() time.Time { return time.Date(2026, 8, 26, 6, 0, 0, 0, time.UTC) }

func TestStoreLookup(t *testing.T) {
	s := &Store{}
	if err := s.Allow("owner_timezone", "Europe/London", testNow()); err != nil {
		t.Fatal(err)
	}
	if err := s.Allow("delivery_window", "mornings", testNow()); err != nil {
		t.Fatal(err)
	}
	if err := s.Revoke("delivery_window", testNow()); err != nil {
		t.Fatal(err)
	}

	cases := []struct {
		class     string
		wantKnown bool
		wantRule  string
		wantText  string
	}{
		{"owner_timezone", true, "licensed", "Europe/London"},
		{"delivery_window", true, "reaches_not", "mornings"}, // revoked keeps the answer: he still knows
		{"never_granted", false, "reaches_not", ""},
		{"", false, "reaches_not", ""}, // untagged items match nothing by construction
	}
	for _, c := range cases {
		text, known, rule := s.Lookup(c.class)
		if known != c.wantKnown || rule != c.wantRule || text != c.wantText {
			t.Errorf("Lookup(%q) = (%q, %v, %q), want (%q, %v, %q)",
				c.class, text, known, rule, c.wantText, c.wantKnown, c.wantRule)
		}
	}
}

func TestStoreRoundTripAndPermissions(t *testing.T) {
	path := filepath.Join(t.TempDir(), "fill-rules.json")
	s, err := Load(path)
	if err != nil {
		t.Fatalf("missing file must be an empty store: %v", err)
	}
	if err := s.Allow("owner_timezone", "Europe/London", testNow()); err != nil {
		t.Fatal(err)
	}
	if err := s.Save(); err != nil {
		t.Fatal(err)
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm() != 0o600 {
		t.Errorf("rule store mode = %v, want 0600 — it holds the owner's standing answers", info.Mode().Perm())
	}
	again, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	if text, known, rule := again.Lookup("owner_timezone"); !known || rule != "licensed" || text != "Europe/London" {
		t.Errorf("reloaded store lost the rule: (%q, %v, %q)", text, known, rule)
	}
}

func TestLoadUnreadableStoreIsAnError(t *testing.T) {
	path := filepath.Join(t.TempDir(), "fill-rules.json")
	if err := os.WriteFile(path, []byte("{not json"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := Load(path); err == nil {
		t.Fatal("corrupt store must be an error (no_rule_record posture), never an empty store")
	}
}

func TestStoreOwnerActGuards(t *testing.T) {
	s := &Store{}
	if err := s.Allow("", "x", testNow()); err == nil {
		t.Error("empty item class must not grant")
	}
	if err := s.Allow("c", "  ", testNow()); err == nil {
		t.Error("empty answer must not grant — it would be an invented fill waiting to happen")
	}
	if err := s.Revoke("absent", testNow()); err == nil {
		t.Error("revoking an absent rule must say so")
	}
}

// fakeInner records what was actually conducted and answers every item.
type fakeInner struct{ got *brief.Brief }

func (f *fakeInner) Conduct(_ context.Context, b *brief.Brief) ([]protocol.Answer, error) {
	f.got = b
	out := make([]protocol.Answer, 0, len(b.Items))
	for _, it := range b.Items {
		out = append(out, protocol.Answer{ItemID: it.ID, State: "Answered", Answered: true, Text: "said-" + it.Want})
	}
	return out, nil
}

type testLog struct{ lines []string }

func (l *testLog) Printf(format string, v ...any) { l.lines = append(l.lines, format) }

// scriptDecider writes a stub front whose verdicts follow the REAL proven
// table, so the glue is exercised against the same words the real front
// prints. The integration test below runs the real front where it exists.
func scriptDecider(t *testing.T) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "decider")
	script := `#!/bin/sh
[ "$#" -eq 3 ] || exit 2
[ "$1" = decide ] || exit 2
case "$3" in
no_rule_record) echo refuse_item; exit 0;;
licensed|reaches_not) ;;
*) exit 3;;
esac
if [ "$2" = false ]; then echo ask_plain; exit 0; fi
if [ "$3" = licensed ]; then echo fill_silently; else echo ask_with_offer; fi
`
	if err := os.WriteFile(path, []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	return path
}

func TestFillerPartitions(t *testing.T) {
	store := &Store{}
	if err := store.Allow("owner_timezone", "Europe/London", testNow()); err != nil {
		t.Fatal(err)
	}
	if err := store.Allow("delivery_window", "mornings", testNow()); err != nil {
		t.Fatal(err)
	}
	if err := store.Revoke("delivery_window", testNow()); err != nil {
		t.Fatal(err)
	}

	inner := &fakeInner{}
	f := Wrap(inner, store, nil, scriptDecider(t), &testLog{})
	f.now = testNow

	b := &brief.Brief{BriefID: "b-1", Version: 1, Items: []brief.Item{
		{ID: 1, Want: "your timezone?", ItemClass: "owner_timezone"},        // → filled
		{ID: 2, Want: "when suits delivery?", ItemClass: "delivery_window"}, // → offered
		{ID: 3, Want: "what should it be called?"},                          // untagged → asked plain
	}}
	answers, err := f.Conduct(context.Background(), b)
	if err != nil {
		t.Fatal(err)
	}

	if len(answers) != 3 {
		t.Fatalf("got %d answers, want 3", len(answers))
	}
	if a := answers[0]; a.ItemID != 1 || a.State != StateFilled || !a.Answered || a.Text != "Europe/London" {
		t.Errorf("filled answer wrong: %+v", a)
	}
	if inner.got == nil || len(inner.got.Items) != 2 {
		t.Fatalf("inner conducted %+v, want exactly the two unfilled items", inner.got)
	}
	if !strings.Contains(inner.got.Items[0].Want, "I'd have said: mornings") {
		t.Errorf("offered item must carry the offer out loud: %q", inner.got.Items[0].Want)
	}
	if inner.got.Items[1].Want != "what should it be called?" {
		t.Errorf("plain item must be untouched: %q", inner.got.Items[1].Want)
	}
	if inner.got.BriefID != "b-1" || inner.got.Version != 1 {
		t.Errorf("residual brief lost its identity: %s v%d", inner.got.BriefID, inner.got.Version)
	}
}

func TestFillerAllFilledConductsNothing(t *testing.T) {
	store := &Store{}
	if err := store.Allow("owner_timezone", "Europe/London", testNow()); err != nil {
		t.Fatal(err)
	}
	inner := &fakeInner{}
	f := Wrap(inner, store, nil, scriptDecider(t), &testLog{})
	f.now = testNow
	answers, err := f.Conduct(context.Background(), &brief.Brief{BriefID: "b", Version: 1,
		Items: []brief.Item{{ID: 1, Want: "tz?", ItemClass: "owner_timezone"}}})
	if err != nil {
		t.Fatal(err)
	}
	if inner.got != nil {
		t.Error("nothing should be conducted when every item filled")
	}
	if len(answers) != 1 || answers[0].State != StateFilled {
		t.Errorf("answers = %+v", answers)
	}
}

func TestFillerFailsClosed(t *testing.T) {
	store := &Store{}
	if err := store.Allow("owner_timezone", "Europe/London", testNow()); err != nil {
		t.Fatal(err)
	}
	item := brief.Item{ID: 1, Want: "tz?", ItemClass: "owner_timezone"}

	t.Run("decider missing: everything asked", func(t *testing.T) {
		inner := &fakeInner{}
		f := Wrap(inner, store, nil, filepath.Join(t.TempDir(), "absent"), &testLog{})
		f.now = testNow
		answers, err := f.Conduct(context.Background(), &brief.Brief{BriefID: "b", Version: 1, Items: []brief.Item{item}})
		if err != nil {
			t.Fatal(err)
		}
		if len(answers) != 1 || answers[0].State == StateFilled {
			t.Errorf("a missing decider must never fill: %+v", answers)
		}
	})

	t.Run("unreadable store: refuse_item, asked unaided", func(t *testing.T) {
		inner := &fakeInner{}
		f := Wrap(inner, store, os.ErrPermission, scriptDecider(t), &testLog{})
		f.now = testNow
		answers, err := f.Conduct(context.Background(), &brief.Brief{BriefID: "b", Version: 1, Items: []brief.Item{item}})
		if err != nil {
			t.Fatal(err)
		}
		if len(answers) != 1 || answers[0].State == StateFilled {
			t.Errorf("an unreadable store must never fill, even for a licensed item: %+v", answers)
		}
		if inner.got == nil || len(inner.got.Items) != 1 || inner.got.Items[0].Want != "tz?" {
			t.Errorf("refused item must be asked unaided, question untouched: %+v", inner.got)
		}
	})
}

// TestFillerAgainstRealFront exercises the glue against the ACTUAL proven
// front where it is present — code agreeing with a stub is not code agreeing
// with the source of truth.
func TestFillerAgainstRealFront(t *testing.T) {
	real := os.Getenv(DeciderEnv)
	if real == "" {
		real = filepath.Join(os.Getenv("HOME"), "dev", "ada-factory", "wu-brief-fill-policy-front", "brief_fill_policy_front")
	}
	if _, err := os.Stat(real); err != nil {
		t.Skipf("real front not present: %v", err)
	}
	store := &Store{}
	if err := store.Allow("owner_timezone", "Europe/London", testNow()); err != nil {
		t.Fatal(err)
	}
	inner := &fakeInner{}
	f := Wrap(inner, store, nil, real, &testLog{})
	f.now = testNow
	answers, err := f.Conduct(context.Background(), &brief.Brief{BriefID: "b", Version: 1, Items: []brief.Item{
		{ID: 1, Want: "tz?", ItemClass: "owner_timezone"},
		{ID: 2, Want: "name?", ItemClass: ""},
	}})
	if err != nil {
		t.Fatal(err)
	}
	if len(answers) != 2 || answers[0].State != StateFilled || answers[0].Text != "Europe/London" {
		t.Errorf("against the real front: %+v", answers)
	}
	if inner.got == nil || len(inner.got.Items) != 1 || inner.got.Items[0].ID != 2 {
		t.Errorf("real front should leave exactly the untagged item to conduct: %+v", inner.got)
	}
}
