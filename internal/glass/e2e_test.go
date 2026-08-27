package glass

// e2e_test.go drives the server the way the control UI does: a real WebSocket
// over a real HTTP server, not calls into handle(). This is the test that
// catches what unit dispatch cannot — the attach/broadcast path, the write
// interleaving, and the frames as they actually cross the wire.

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/coder/websocket"
)

// dial opens a client socket to a test server.
func dial(t *testing.T, url string) *websocket.Conn {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	c, _, err := websocket.Dial(ctx, "ws"+strings.TrimPrefix(url, "http"), nil)
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	return c
}

func writeJSON(t *testing.T, c *websocket.Conn, v any) {
	t.Helper()
	body, err := json.Marshal(v)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := c.Write(ctx, websocket.MessageText, body); err != nil {
		t.Fatalf("write: %v", err)
	}
}

func readJSON(t *testing.T, c *websocket.Conn) map[string]any {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	_, data, err := c.Read(ctx)
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	var m map[string]any
	if err := json.Unmarshal(data, &m); err != nil {
		t.Fatalf("frame is not JSON: %v\n%s", err, data)
	}
	return m
}

// TestInterviewOverARealSocket — the whole slice in one sitting: a client
// connects, ghillie asks through the surface, the question arrives as a chat
// event, the client answers with chat.send, the interview hears it, and the
// transcript replays over chat.history.
func TestInterviewOverARealSocket(t *testing.T) {
	s := NewServer("ghillie-e2e")
	ts := httptest.NewServer(http.HandlerFunc(s.ServeHTTP))
	defer ts.Close()

	c := dial(t, ts.URL)
	defer c.Close(websocket.StatusNormalClosure, "")

	// 0. THE SERVER SPEAKS FIRST: the connect.challenge event, exactly as the
	// real gateway does — a v4 client waits for it before sending connect.
	challenge := readJSON(t, c)
	if challenge["event"] != "connect.challenge" {
		t.Fatalf("first frame must be the connect.challenge event, got: %v", challenge)
	}
	if cp, ok := challenge["payload"].(map[string]any); !ok || cp["nonce"] == "" || cp["nonce"] == nil {
		t.Fatalf("challenge carries no nonce: %v", challenge)
	}

	// 1. connect → hello-ok advertising the chat slice.
	writeJSON(t, c, map[string]any{
		"type": "req", "id": "r1", "method": "connect",
		"params": map[string]any{
			"minProtocol": 4, "maxProtocol": 4,
			"client": map[string]any{"id": "e2e", "version": "1", "platform": "test", "mode": "control"},
		},
	})
	hello := readJSON(t, c)
	if hello["ok"] != true {
		t.Fatalf("connect refused: %v", hello)
	}

	// 2. ghillie asks through the surface while the client is attached.
	askErr := make(chan error, 1)
	heard := make(chan string, 1)
	go func() {
		if err := s.SayLine(context.Background(), "What does the machine make?"); err != nil {
			askErr <- err
			return
		}
		reply, err := s.AwaitReply(context.Background())
		if err != nil {
			askErr <- err
			return
		}
		heard <- reply
	}()

	// 3. the question arrives as an event frame.
	ev := readJSON(t, c)
	if ev["type"] != "event" || ev["event"] != "chat" {
		t.Fatalf("expected a chat event, got %v", ev)
	}
	payload, _ := ev["payload"].(map[string]any)
	msg, _ := payload["message"].(map[string]any)
	blocks, _ := msg["content"].([]any)
	block, _ := func() (map[string]any, bool) {
		if len(blocks) != 1 {
			return nil, false
		}
		b, ok := blocks[0].(map[string]any)
		return b, ok
	}()
	if payload["state"] != "final" || block["type"] != "text" || block["text"] != "What does the machine make?" {
		t.Fatalf("event payload = %v", payload)
	}
	// Its session.message twin follows — the family the control UI's
	// subscribed transcript actually consumes.
	twin := readJSON(t, c)
	if twin["event"] != "session.message" {
		t.Fatalf("expected the session.message twin, got %v", twin)
	}

	// 4. the client answers.
	writeJSON(t, c, map[string]any{
		"type": "req", "id": "r2", "method": "chat.send",
		"params": map[string]any{
			"sessionKey":     SessionKey,
			"message":        "pressure valves",
			"idempotencyKey": "e2e-1",
		},
	})

	// The send produces the ack plus two echo events (chat and its
	// session.message twin), in some order.
	sawAck, sawEcho, sawSessionMessage := false, false, false
	for i := 0; i < 3; i++ {
		f := readJSON(t, c)
		switch f["type"] {
		case "res":
			if f["ok"] != true {
				t.Fatalf("chat.send refused: %v", f)
			}
			sawAck = true
		case "event":
			if f["event"] == "session.message" {
				sawSessionMessage = true
			} else {
				sawEcho = true
			}
		}
	}
	if !sawAck || !sawEcho || !sawSessionMessage {
		t.Fatalf("send should ack and echo on both families; ack=%v chat=%v session.message=%v", sawAck, sawEcho, sawSessionMessage)
	}

	// 5. the interview heard exactly what was typed.
	select {
	case reply := <-heard:
		if reply != "pressure valves" {
			t.Fatalf("interview heard %q", reply)
		}
	case err := <-askErr:
		t.Fatalf("surface failed: %v", err)
	case <-time.After(5 * time.Second):
		t.Fatalf("interview never heard the answer")
	}

	// 6. the exchange replays over chat.history.
	writeJSON(t, c, map[string]any{
		"type": "req", "id": "r3", "method": "chat.history",
		"params": map[string]any{"sessionKey": SessionKey},
	})
	hist := readJSON(t, c)
	hp, _ := hist["payload"].(map[string]any)
	msgs, _ := hp["messages"].([]any)
	if len(msgs) != 2 {
		t.Fatalf("history holds %d messages, want the question and the answer", len(msgs))
	}
}

// TestConnectNonceEchoIsVerified — a device block offering a nonce must offer
// THE nonce this connection was issued; somebody else's challenge is a replay
// and the connect is refused. A correct echo connects.
func TestConnectNonceEchoIsVerified(t *testing.T) {
	s := NewServer("ghillie-e2e")
	ts := httptest.NewServer(http.HandlerFunc(s.ServeHTTP))
	defer ts.Close()

	// Wrong nonce: refused, fatally.
	c := dial(t, ts.URL)
	challenge := readJSON(t, c)
	if challenge["event"] != "connect.challenge" {
		t.Fatalf("expected the challenge first, got %v", challenge)
	}
	writeJSON(t, c, map[string]any{
		"type": "req", "id": "r1", "method": "connect",
		"params": map[string]any{
			"minProtocol": 4, "maxProtocol": 4,
			"client": map[string]any{"id": "e2e", "version": "1", "platform": "test", "mode": "control"},
			"device": map[string]any{"id": "dev-1", "nonce": "not-the-issued-nonce"},
		},
	})
	res := readJSON(t, c)
	if res["ok"] == true {
		t.Fatalf("a replayed nonce must be refused: %v", res)
	}
	_ = c.Close(websocket.StatusNormalClosure, "")

	// Correct echo: connects.
	c2 := dial(t, ts.URL)
	defer c2.Close(websocket.StatusNormalClosure, "")
	ch2 := readJSON(t, c2)
	payload, _ := ch2["payload"].(map[string]any)
	issued, _ := payload["nonce"].(string)
	if issued == "" {
		t.Fatalf("challenge carried no nonce: %v", ch2)
	}
	writeJSON(t, c2, map[string]any{
		"type": "req", "id": "r2", "method": "connect",
		"params": map[string]any{
			"minProtocol": 4, "maxProtocol": 4,
			"client": map[string]any{"id": "e2e", "version": "1", "platform": "test", "mode": "control"},
			"device": map[string]any{"id": "dev-1", "nonce": issued},
		},
	})
	if hello := readJSON(t, c2); hello["ok"] != true {
		t.Fatalf("a correct echo must connect: %v", hello)
	}
}
