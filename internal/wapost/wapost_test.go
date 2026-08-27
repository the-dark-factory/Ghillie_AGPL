package wapost

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/tonygair/ghillie/internal/post"

	"go.mau.fi/whatsmeow/types"
	"go.mau.fi/whatsmeow/types/events"
)

func TestCanonicalNumber(t *testing.T) {
	cases := []struct{ in, want string }{
		{"+44 7700 900123", "447700900123"},
		{"447700900123", "447700900123"},
		{"(0044) 7700-900123", "00447700900123"},
		{"no digits", ""},
	}
	for _, tc := range cases {
		if got := CanonicalNumber(tc.in); got != tc.want {
			t.Errorf("CanonicalNumber(%q) = %q, want %q", tc.in, got, tc.want)
		}
	}
}

func TestLoadTableCanonicalizesNumberKeys(t *testing.T) {
	p := filepath.Join(t.TempDir(), "grants.json")
	if err := os.WriteFile(p, []byte(`{"+44 7700 900123": {"ceiling":"break_through","ask":"break_through","live":true}}`), 0o600); err != nil {
		t.Fatal(err)
	}
	table, err := LoadTable(p)
	if err != nil {
		t.Fatal(err)
	}
	if g, ok := table["447700900123"]; !ok || g.Ceiling != "break_through" {
		t.Fatalf("lookup by canonical number failed: %v %v", g, ok)
	}
}

func msgEvent(sender, chat string, group, fromMe bool) *events.Message {
	e := &events.Message{}
	e.Info.Sender = types.JID{User: sender, Server: types.DefaultUserServer}
	e.Info.IsFromMe = fromMe
	e.Info.PushName = "Claimed Name"
	e.Info.Timestamp = time.Unix(1756155600, 0)
	if group {
		e.Info.Chat = types.JID{User: chat, Server: types.GroupServer}
		e.Info.IsGroup = true
	} else {
		e.Info.Chat = types.JID{User: sender, Server: types.DefaultUserServer}
	}
	return e
}

func TestFromEvent(t *testing.T) {
	a, ok := FromEvent(msgEvent("447700900123", "", false, false))
	if !ok || a.Sender != "447700900123" || a.Chat != "" {
		t.Fatalf("direct arrival = %+v ok=%v", a, ok)
	}
	if a.At.Unix() != 1756155600 {
		t.Fatalf("arrival time = %v", a.At)
	}

	a, ok = FromEvent(msgEvent("447700900123", "1203630", true, false))
	if !ok || a.Chat != "1203630" {
		t.Fatalf("group arrival = %+v ok=%v (grants stay per-sender; the chat is context)", a, ok)
	}

	// The owner's own sends echoed back are nobody's arrival.
	if _, ok := FromEvent(msgEvent("447700900123", "", false, true)); ok {
		t.Fatalf("an IsFromMe echo must be ignored")
	}
	if _, ok := FromEvent(nil); ok {
		t.Fatalf("nil event must be ignored")
	}
}

// TestDecideArrivalSharesTheOneLaw drives the same real-front integration as
// internal/post when the Ada binary is present: mail and messages meet the
// SAME proven door.
func TestDecideArrivalSharesTheOneLaw(t *testing.T) {
	home, _ := os.UserHomeDir()
	real := filepath.Join(home, "dev", "ada-factory", "wu-delivery-policy-front", "delivery_policy_front")
	if _, err := os.Stat(real); err != nil {
		t.Skipf("no real front here: %v", err)
	}
	t.Setenv(post.EnvDecider, real)
	table := post.Table{
		"447700900123": {Ceiling: "break_through", Ask: "break_through", Live: true},
	}
	// The authorised child's number reaches an asleep owner in quiet hours.
	a, _ := FromEvent(msgEvent("447700900123", "", false, false))
	d, err := DecideArrival(context.Background(), post.Decider{}, table, a, "asleep", true)
	if err != nil {
		t.Fatal(err)
	}
	if d != post.PresentNow {
		t.Fatalf("break_through child = %s, want present_now", d)
	}
	// A stranger's number does not exist to the owner.
	s, _ := FromEvent(msgEvent("15551234567", "", false, false))
	d, err = DecideArrival(context.Background(), post.Decider{}, table, s, "free", false)
	if err != nil {
		t.Fatal(err)
	}
	if d != post.Refuse {
		t.Fatalf("stranger = %s, want refuse (the core's own theorem)", d)
	}
}
