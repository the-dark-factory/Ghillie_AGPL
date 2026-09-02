package glass

import (
	"context"
	"encoding/json"
	"fmt"
	"testing"
	"time"
)

func sendFrame(t *testing.T, id, sessionKey, msg, idem string) []byte {
	t.Helper()
	params := map[string]any{
		"sessionKey":     sessionKey,
		"message":        msg,
		"idempotencyKey": idem,
		// Additive fields their real client sends alongside. Choking on them
		// would fail against the shipped UI.
		"deliver":  false,
		"fastMode": "auto",
	}
	raw, err := json.Marshal(params)
	if err != nil {
		t.Fatalf("marshal params: %v", err)
	}
	frame, err := json.Marshal(requestFrame{Type: "req", ID: id, Method: "chat.send", Params: raw})
	if err != nil {
		t.Fatalf("marshal frame: %v", err)
	}
	return frame
}

func decodeRes(t *testing.T, out []byte) (ok bool, payload map[string]any) {
	t.Helper()
	var res struct {
		OK      bool           `json:"ok"`
		Payload map[string]any `json:"payload"`
	}
	if err := json.Unmarshal(out, &res); err != nil {
		t.Fatalf("response is not JSON: %v\n%s", err, out)
	}
	return res.OK, res.Payload
}

// TestChatSendReachesAWaitingInterview is the surface working end to end:
// the interview waits, the client speaks, the interview hears exactly those
// words, and the transcript holds them.
func TestChatSendReachesAWaitingInterview(t *testing.T) {
	s := testServer()

	got := make(chan string, 1)
	ready := make(chan struct{})
	go func() {
		close(ready)
		reply, err := s.AwaitReply(context.Background())
		if err != nil {
			got <- "ERR: " + err.Error()
			return
		}
		got <- reply
	}()
	<-ready

	// The waiter registers under s.mu; poll until it is visible so the send
	// takes the handoff path rather than the queue path.
	deadline := time.Now().Add(2 * time.Second)
	for {
		s.mu.Lock()
		registered := s.waiter != nil
		s.mu.Unlock()
		if registered {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("AwaitReply never registered")
		}
		time.Sleep(time.Millisecond)
	}

	out, fatal := s.handle(sendFrame(t, "r1", SessionKey, "a green door, hinges left", "idem-1"))
	if fatal {
		t.Fatalf("chat.send should not be fatal")
	}
	ok, payload := decodeRes(t, out)
	if !ok {
		t.Fatalf("chat.send refused: %s", out)
	}
	if payload["runId"] != "idem-1" || payload["status"] != "ok" {
		t.Fatalf("ack should carry runId=idempotencyKey and status ok, got %v", payload)
	}

	select {
	case reply := <-got:
		if reply != "a green door, hinges left" {
			t.Fatalf("interview heard %q", reply)
		}
	case <-time.After(2 * time.Second):
		t.Fatalf("interview never heard the reply")
	}

	tr := s.Transcript()
	if len(tr) != 1 || tr[0].Role != "user" || textOf(tr[0]) != "a green door, hinges left" {
		t.Fatalf("transcript should hold the utterance verbatim, got %+v", tr)
	}
}

// TestChatSendQueuesWhenNobodyWaits — an answer given before the question is
// re-put is still an answer; it must queue, not vanish.
func TestChatSendQueuesWhenNobodyWaits(t *testing.T) {
	s := testServer()

	out, _ := s.handle(sendFrame(t, "r1", SessionKey, "first", "idem-1"))
	if ok, _ := decodeRes(t, out); !ok {
		t.Fatalf("send refused: %s", out)
	}
	out, _ = s.handle(sendFrame(t, "r2", SessionKey, "second", "idem-2"))
	if ok, _ := decodeRes(t, out); !ok {
		t.Fatalf("send refused: %s", out)
	}

	for i, want := range []string{"first", "second"} {
		reply, err := s.AwaitReply(context.Background())
		if err != nil {
			t.Fatalf("AwaitReply %d: %v", i, err)
		}
		if reply != want {
			t.Fatalf("AwaitReply %d = %q, want %q (arrival order)", i, reply, want)
		}
	}
}

// TestChatSendIdempotency — a transport retry carries the same idempotencyKey
// and must not become a second utterance.
func TestChatSendIdempotency(t *testing.T) {
	s := testServer()

	for _, id := range []string{"r1", "r2"} {
		out, _ := s.handle(sendFrame(t, id, SessionKey, "once", "idem-same"))
		if ok, _ := decodeRes(t, out); !ok {
			t.Fatalf("send %s refused: %s", id, out)
		}
	}
	if n := len(s.Transcript()); n != 1 {
		t.Fatalf("retried send became %d transcript entries, want 1", n)
	}
}

// TestChatSendRefusals — table of malformed sends, each refused non-fatally
// with the code the client branches on.
func TestChatSendRefusals(t *testing.T) {
	cases := []struct {
		name       string
		sessionKey string
		message    string
		idem       string
		wantCode   string
	}{
		{"wrong session", "someone-else", "hi", "i1", "not_found"},
		{"no idempotency key", SessionKey, "hi", "", "bad_request"},
		{"empty message", SessionKey, "", "i2", "bad_request"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			s := testServer()
			out, fatal := s.handle(sendFrame(t, "r1", tc.sessionKey, tc.message, tc.idem))
			if fatal {
				t.Fatalf("a refused send must not kill the connection")
			}
			var res struct {
				OK    bool `json:"ok"`
				Error *struct {
					Code string `json:"code"`
				} `json:"error"`
			}
			if err := json.Unmarshal(out, &res); err != nil {
				t.Fatalf("response is not JSON: %v", err)
			}
			if res.OK || res.Error == nil || res.Error.Code != tc.wantCode {
				t.Fatalf("want refusal %q, got %s", tc.wantCode, out)
			}
		})
	}
}

// TestSayLineAppendsAssistantUtterance — SayLine is the only mouth; what it
// says lands in the transcript under ghillie's name.
func TestSayLineAppendsAssistantUtterance(t *testing.T) {
	s := testServer()
	if err := s.SayLine(context.Background(), "What does the machine make?"); err != nil {
		t.Fatalf("SayLine: %v", err)
	}
	tr := s.Transcript()
	if len(tr) != 1 || tr[0].Role != "assistant" || textOf(tr[0]) != "What does the machine make?" {
		t.Fatalf("transcript = %+v", tr)
	}
}

// TestChatHistoryReplaysTranscript — the cursor protocol as the real client
// speaks it (corrected against the live UI 2026-08-26): a CURSORLESS request
// gets the whole exchange as one complete page; re-asking WITH the returned
// cursor gets kind "delta" holding only what moved since (nothing, here); an
// unplaceable cursor gets kind "reset". Answering a cursor request with a
// full page re-arms the pane's loading state forever — that was the bug.
func TestChatHistoryReplaysTranscript(t *testing.T) {
	s := testServer()
	if err := s.SayLine(context.Background(), "Q1"); err != nil {
		t.Fatalf("SayLine: %v", err)
	}
	if out, _ := s.handle(sendFrame(t, "r1", SessionKey, "A1", "i1")); out == nil {
		t.Fatalf("send failed")
	}

	params, err := json.Marshal(map[string]any{"sessionKey": SessionKey, "limit": 50})
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	frame, err := json.Marshal(requestFrame{Type: "req", ID: "rh", Method: "chat.history", Params: params})
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	out, fatal := s.handle(frame)
	if fatal {
		t.Fatalf("chat.history should not be fatal")
	}
	ok, payload := decodeRes(t, out)
	if !ok {
		t.Fatalf("chat.history refused: %s", out)
	}
	if kind, has := payload["kind"]; has {
		t.Fatalf("cursorless history must be a page, not a cursor result; kind = %v", kind)
	}
	msgs, _ := payload["messages"].([]any)
	if len(msgs) != 2 {
		t.Fatalf("history holds %d messages, want 2", len(msgs))
	}
	if payload["deltaCursor"] != "t-2" || payload["completeSnapshot"] != true {
		t.Fatalf("page fields wrong: cursor=%v complete=%v", payload["deltaCursor"], payload["completeSnapshot"])
	}
	if payload["sessionInfo"] == nil {
		t.Fatalf("sessionInfo is required by their closed schema and is missing")
	}

	history := func(cursor string) map[string]any {
		params, merr := json.Marshal(map[string]any{"sessionKey": SessionKey, "cursor": cursor})
		if merr != nil {
			t.Fatalf("marshal: %v", merr)
		}
		frame, merr := json.Marshal(requestFrame{Type: "req", ID: "rc", Method: "chat.history", Params: params})
		if merr != nil {
			t.Fatalf("marshal: %v", merr)
		}
		out, fatal := s.handle(frame)
		if fatal {
			t.Fatalf("chat.history should not be fatal")
		}
		ok, p := decodeRes(t, out)
		if !ok {
			t.Fatalf("chat.history refused: %s", out)
		}
		return p
	}

	delta := history("t-2")
	if delta["kind"] != "delta" {
		t.Fatalf("cursor request kind = %v, want delta", delta["kind"])
	}
	if dm, _ := delta["messages"].([]any); len(dm) != 0 {
		t.Fatalf("nothing moved since t-2 but delta holds %d message(s)", len(dm))
	}

	if reset := history("garbage"); reset["kind"] != "reset" {
		t.Fatalf("unplaceable cursor kind = %v, want reset", reset["kind"])
	}
}

// TestSessionsListReportsTheFleetOfOne — one ghillie, one row, and updatedAt
// tracks the last utterance.
func TestSessionsListReportsTheFleetOfOne(t *testing.T) {
	s := testServer()
	if _, fatal := s.handle(sendFrame(t, "r1", SessionKey, "hello", "i1")); fatal {
		t.Fatalf("send failed")
	}

	frame, err := json.Marshal(requestFrame{Type: "req", ID: "rl", Method: "sessions.list"})
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	out, fatal := s.handle(frame)
	if fatal {
		t.Fatalf("sessions.list should not be fatal")
	}
	ok, payload := decodeRes(t, out)
	if !ok {
		t.Fatalf("sessions.list refused: %s", out)
	}
	rows, _ := payload["sessions"].([]any)
	if len(rows) != 1 {
		t.Fatalf("sessions.list returned %d rows, want exactly 1", len(rows))
	}
	row, _ := rows[0].(map[string]any)
	if row["key"] != CanonicalSessionKey || row["kind"] != "direct" {
		t.Fatalf("row = %v", row)
	}
	if row["updatedAt"] == nil {
		t.Fatalf("updatedAt should track the last utterance")
	}
}

// TestAwaitReplyYieldsWhenClientGone — the wait ends with the context, and
// says so, rather than blocking an interview on a client that left.
func TestAwaitReplyYieldsWhenClientGone(t *testing.T) {
	s := testServer()
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() {
		_, err := s.AwaitReply(ctx)
		done <- err
	}()
	cancel()
	select {
	case err := <-done:
		if err == nil {
			t.Fatalf("AwaitReply should report the client gone")
		}
	case <-time.After(2 * time.Second):
		t.Fatalf("AwaitReply did not yield on cancel")
	}
	// The dead waiter must not swallow the next utterance.
	if _, fatal := s.handle(sendFrame(t, "r1", SessionKey, "late answer", "i9")); fatal {
		t.Fatalf("send failed")
	}
	reply, err := s.AwaitReply(context.Background())
	if err != nil {
		t.Fatalf("AwaitReply after cancel: %v", err)
	}
	if reply != "late answer" {
		t.Fatalf("reply = %q", reply)
	}
}

// TestAdvertisedStaysHonest — every advertised method dispatches to a real
// handler, so the features list cannot drift into advertising a lie.
func TestAdvertisedStaysHonest(t *testing.T) {
	s := testServer()
	methods, events := s.advertised()
	for _, m := range methods {
		if m == "connect" {
			continue // exercised by the handshake tests
		}
		frame, err := json.Marshal(requestFrame{Type: "req", ID: "probe", Method: m})
		if err != nil {
			t.Fatalf("marshal: %v", err)
		}
		out, _ := s.handle(frame)
		var res struct {
			Error *struct {
				Code string `json:"code"`
			} `json:"error"`
		}
		if err := json.Unmarshal(out, &res); err != nil {
			t.Fatalf("%s: response is not JSON: %v", m, err)
		}
		if res.Error != nil && res.Error.Code == "unsupported_method" {
			t.Fatalf("advertised method %q is not implemented", m)
		}
	}
	// Grown 2026-08-26, deliberately: session.message is the family the
	// control UI's subscribed transcript consumes (docs/gateway/protocol.md,
	// common event families), and tick is the liveness heartbeat its watchdog
	// demands. Both are genuinely emitted.
	if fmt.Sprint(events) != "[chat session.message tick]" {
		t.Fatalf("events = %v; this test exists to make growing the list a decision", events)
	}
}

// textOf flattens a one-block message for assertions.
func textOf(m message) string {
	if len(m.Content) != 1 || m.Content[0].Type != "text" {
		return ""
	}
	return m.Content[0].Text
}
