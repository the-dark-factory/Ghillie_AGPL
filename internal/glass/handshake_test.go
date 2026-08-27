package glass

import (
	"encoding/json"
	"strings"
	"testing"
	"time"
)

func testServer() *Server {
	s := NewServer("ghillie-test")
	s.NewConnID = func() string { return "conn-fixed" }
	s.Now = func() time.Time { return s.started.Add(1500 * time.Millisecond) }
	return s
}

func connectFrame(t *testing.T, min, max int) []byte {
	t.Helper()
	params := map[string]any{
		"minProtocol": min,
		"maxProtocol": max,
		"client": map[string]any{
			"id": "control-ui", "version": "2026.8.1",
			"platform": "web", "mode": "control",
		},
		// Additive fields their real client sends. A server that choked on
		// these would fail against the shipped UI.
		"caps":        []string{"chat"},
		"scopes":      []string{"operator.read"},
		"permissions": map[string]bool{"operator.read": true},
		"device":      map[string]any{"id": "dev-1"},
	}
	raw, err := json.Marshal(params)
	if err != nil {
		t.Fatalf("marshal params: %v", err)
	}
	frame, err := json.Marshal(requestFrame{Type: "req", ID: "r1", Method: "connect", Params: raw})
	if err != nil {
		t.Fatalf("marshal frame: %v", err)
	}
	return frame
}

// TestConnectYieldsHelloOk is the whole point of the spike: a gateway-v4
// "connect" must come back as a hello-ok carrying every field their closed
// schema requires.
func TestConnectYieldsHelloOk(t *testing.T) {
	out, fatal := testServer().handle(connectFrame(t, 4, 4))
	if fatal {
		t.Fatalf("connect should not be fatal")
	}

	var res struct {
		Type    string          `json:"type"`
		ID      string          `json:"id"`
		OK      bool            `json:"ok"`
		Payload json.RawMessage `json:"payload"`
	}
	if err := json.Unmarshal(out, &res); err != nil {
		t.Fatalf("response is not JSON: %v", err)
	}
	if res.Type != "res" || res.ID != "r1" || !res.OK {
		t.Fatalf("want ok res for r1, got type=%q id=%q ok=%v", res.Type, res.ID, res.OK)
	}

	// Decode into a map so MISSING required keys are visible; a typed decode
	// would silently zero them and the test would pass on an invalid hello.
	var h map[string]any
	if err := json.Unmarshal(res.Payload, &h); err != nil {
		t.Fatalf("payload is not JSON: %v", err)
	}
	if h["type"] != "hello-ok" {
		t.Errorf("payload.type = %v, want hello-ok", h["type"])
	}
	if got, ok := h["protocol"].(float64); !ok || int(got) != ProtocolVersion {
		t.Errorf("payload.protocol = %v, want %d", h["protocol"], ProtocolVersion)
	}

	server, ok := h["server"].(map[string]any)
	if !ok {
		t.Fatalf("payload.server missing")
	}
	for _, k := range []string{"version", "connId"} {
		if v, present := server[k]; !present || v == "" {
			t.Errorf("server.%s is required and non-empty, got %v", k, v)
		}
	}

	features, ok := h["features"].(map[string]any)
	if !ok {
		t.Fatalf("payload.features missing")
	}
	for _, k := range []string{"methods", "events"} {
		if _, present := features[k]; !present {
			t.Errorf("features.%s is required", k)
		}
	}

	snap, ok := h["snapshot"].(map[string]any)
	if !ok {
		t.Fatalf("payload.snapshot missing")
	}
	for _, k := range []string{"presence", "health", "stateVersion", "uptimeMs"} {
		if _, present := snap[k]; !present {
			t.Errorf("snapshot.%s is required by SnapshotSchema", k)
		}
	}
	sv, ok := snap["stateVersion"].(map[string]any)
	if !ok {
		t.Fatalf("snapshot.stateVersion missing")
	}
	for _, k := range []string{"presence", "health"} {
		if _, present := sv[k]; !present {
			t.Errorf("stateVersion.%s is required", k)
		}
	}
	if snap["uptimeMs"].(float64) != 1500 {
		t.Errorf("uptimeMs = %v, want 1500", snap["uptimeMs"])
	}
}

// TestNoUnknownKeys guards the closedObject() property: their validator rejects
// fields it has no slot for, so an omitempty slip would break the real UI while
// every Go test still passed.
func TestNoUnknownKeys(t *testing.T) {
	out, _ := testServer().handle(connectFrame(t, 4, 4))
	var res struct {
		Payload map[string]any `json:"payload"`
	}
	if err := json.Unmarshal(out, &res); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	allowed := map[string]bool{"type": true, "protocol": true, "server": true, "features": true, "snapshot": true, "auth": true, "policy": true} // all five of server/features/snapshot/policy/auth REQUIRED per docs/gateway/protocol.md
	for k := range res.Payload {
		if !allowed[k] {
			t.Errorf("hello-ok carries %q, which HelloOkSchema has no slot for", k)
		}
	}
	server := res.Payload["server"].(map[string]any)
	allowedServer := map[string]bool{"version": true, "buildId": true, "bootId": true, "connId": true, "controlUiBuildSource": true}
	for k := range server {
		if !allowedServer[k] {
			t.Errorf("hello-ok.server carries %q, which the schema has no slot for", k)
		}
	}
}

// TestProtocolNegotiationRefuses — answering a version the client did not ask
// for is how two peers disagree silently. Refusal is the correct behaviour.
func TestProtocolNegotiationRefuses(t *testing.T) {
	for _, tc := range []struct {
		name     string
		min, max int
	}{
		{"client too old", 2, 3},
		{"client too new", 5, 6},
	} {
		t.Run(tc.name, func(t *testing.T) {
			out, fatal := testServer().handle(connectFrame(t, tc.min, tc.max))
			if !fatal {
				t.Errorf("version mismatch should end the connection")
			}
			if !strings.Contains(string(out), "protocol_unsupported") {
				t.Errorf("want protocol_unsupported, got %s", out)
			}
		})
	}
}

// TestUnknownMethodRefused — this package decides nothing, so anything it does
// not implement must be refused rather than tolerated.
func TestUnknownMethodRefused(t *testing.T) {
	frame, err := json.Marshal(requestFrame{Type: "req", ID: "r9", Method: "session.create"})
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	out, fatal := testServer().handle(frame)
	if fatal {
		t.Errorf("an unknown method should not kill the connection")
	}
	if !strings.Contains(string(out), "unsupported_method") {
		t.Errorf("want unsupported_method, got %s", out)
	}
}

// TestAdvertisedMethodsAreHonest — declaring a method ghillie cannot answer is
// a lie the UI would act on.
func TestAdvertisedMethodsAreHonest(t *testing.T) {
	methods, _ := testServer().advertised()
	for _, m := range methods {
		frame, err := json.Marshal(requestFrame{Type: "req", ID: "r1", Method: m, Params: json.RawMessage(`{"minProtocol":4,"maxProtocol":4}`)})
		if err != nil {
			t.Fatalf("marshal: %v", err)
		}
		out, _ := testServer().handle(frame)
		if strings.Contains(string(out), "unsupported_method") {
			t.Errorf("advertised method %q is not implemented", m)
		}
	}
}
