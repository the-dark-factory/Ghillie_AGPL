package glass

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/http"
	"sync"
	"time"

	"github.com/tonygair/ghillie/internal/gate"
)

// ProtocolVersion is the OpenClaw gateway wire version this server speaks.
// Their PROTOCOL_VERSION is 4 and MIN_CLIENT_PROTOCOL_VERSION is 4, so a general
// control UI must be answered with exactly 4.
const ProtocolVersion = 4

// Frame envelopes, mirroring @openclaw/gateway-protocol schema/frames.ts. Their
// schemas are closedObject(), meaning UNKNOWN FIELDS ARE REJECTED — every
// struct here uses omitempty so ghillie never emits a key their validator has
// no slot for.
type requestFrame struct {
	Type        string          `json:"type"`
	ID          string          `json:"id"`
	Method      string          `json:"method"`
	Params      json.RawMessage `json:"params,omitempty"`
	TraceParent string          `json:"traceparent,omitempty"`
}

type errorShape struct {
	Code         string `json:"code"`
	Message      string `json:"message"`
	Retryable    *bool  `json:"retryable,omitempty"`
	RetryAfterMs *int   `json:"retryAfterMs,omitempty"`
}

type responseFrame struct {
	Type    string      `json:"type"`
	ID      string      `json:"id"`
	OK      bool        `json:"ok"`
	Payload interface{} `json:"payload,omitempty"`
	Error   *errorShape `json:"error,omitempty"`
}

// helloOk mirrors HelloOkSchema. server.connId, features and snapshot are all
// REQUIRED by their validator; omitting any of them fails the connection.
type helloOk struct {
	Type     string `json:"type"`
	Protocol int    `json:"protocol"`
	Server   struct {
		Version string `json:"version"`
		BuildID string `json:"buildId,omitempty"`
		BootID  string `json:"bootId,omitempty"`
		ConnID  string `json:"connId"`
	} `json:"server"`
	Features struct {
		Methods      []string `json:"methods"`
		Events       []string `json:"events"`
		Capabilities []string `json:"capabilities,omitempty"`
	} `json:"features"`
	Snapshot snapshot `json:"snapshot"`
	// Auth and Policy are REQUIRED by HelloOkSchema — docs/gateway/protocol.md:
	// "`server`, `features`, `snapshot`, `policy`, and `auth` are all required".
	// The UI gates what it renders on auth.scopes; a hello without them is a
	// socket that may do nothing, drawn as exactly that. (Learned the slow way,
	// then read the manual: feedback_documentation_before_wire_archaeology.)
	Auth struct {
		Role   string   `json:"role"`
		Scopes []string `json:"scopes"`
	} `json:"auth"`
	Policy struct {
		MaxPayload       int `json:"maxPayload"`
		MaxBufferedBytes int `json:"maxBufferedBytes"`
		TickIntervalMs   int `json:"tickIntervalMs"`
	} `json:"policy"`
}

// snapshot mirrors SnapshotSchema. presence/health/stateVersion/uptimeMs are
// required; health may legitimately be empty until a producer fills it, which
// their own comment states.
type snapshot struct {
	Presence     []presenceEntry `json:"presence"`
	Health       struct{}        `json:"health"`
	StateVersion stateVersion    `json:"stateVersion"`
	UptimeMs     int64           `json:"uptimeMs"`
}

type presenceEntry struct {
	Host     string `json:"host,omitempty"`
	Version  string `json:"version,omitempty"`
	Platform string `json:"platform,omitempty"`
	Mode     string `json:"mode,omitempty"`
	Text     string `json:"text,omitempty"`
}

type stateVersion struct {
	Presence int `json:"presence"`
	Health   int `json:"health"`
}

// connectParams is the subset of ConnectParamsSchema the handshake decides on.
// Their schema carries more (caps, permissions, scopes, device, ...); this
// struct reads only what the version negotiation needs and ignores the rest,
// because tolerating additive client fields is required by their own
// additive-first policy.
type connectParams struct {
	MinProtocol int `json:"minProtocol"`
	MaxProtocol int `json:"maxProtocol"`
	Client      struct {
		ID          string `json:"id"`
		DisplayName string `json:"displayName"`
		Version     string `json:"version"`
		Platform    string `json:"platform"`
		Mode        string `json:"mode"`
	} `json:"client"`
	Caps        []string        `json:"caps"`
	Scopes      []string        `json:"scopes"`
	Permissions map[string]bool `json:"permissions"`
	// Device is the optional device-identity block. Full device auth
	// (signature over the challenge) is its own slice; what THIS handshake
	// verifies is the CHALLENGE NONCE ECHO whenever a device block is
	// offered — a client that presents a nonce must present the one this
	// connection was issued. Tokenless local pages omit the block entirely,
	// by their design, and that is not a verification gap here.
	Device *struct {
		ID    string `json:"id"`
		Nonce string `json:"nonce"`
	} `json:"device"`
}

// Server answers the gateway handshake for one ghillie.
type Server struct {
	// Version is reported to the client as server.version.
	Version string
	// NewConnID mints a connection identifier. Injected so tests are deterministic.
	NewConnID func() string
	// Now supplies the clock, injected for the same reason.
	Now func() time.Time

	// OriginPatterns lists additional browser origins allowed to open a socket.
	// Empty means same-origin only. Widening this is a security decision: any
	// page from a listed origin can drive this ghillie.
	OriginPatterns []string

	// Debug, when non-nil, narrates every inbound frame and refusal — for
	// bring-up against a real client. It sees method names and sizes, never
	// a reason to stay on in production: leave nil there.
	Debug interface{ Printf(string, ...any) }

	started time.Time

	// mu guards everything below it: the chat transcript, the event counters,
	// the attached connections and the reply queue. One mutex because they
	// move together — a transcript entry, its event and its delivery are one
	// happening.
	mu         sync.Mutex
	conns      map[*conn]bool
	transcript []message
	replies    []string
	waiter     chan string
	seenSends  map[string]bool
	msgSeq     int
	runSeq     int
	evSeq      int

	// pending holds approvals awaiting a reviewer. It is keyed by approval id
	// and carries the ORIGINAL act, so the decision is re-checked against the
	// gate rather than trusted from the wire: a client that echoed back a
	// different command than the one displayed must not be able to widen what
	// it was granted.
	pending map[string]pendingAct
}

// pendingAct is the server-side half of a raised approval — what the reviewer is
// actually deciding about, kept out of the client's reach.
type pendingAct struct {
	command     gate.Command
	ceiling     gate.Command
	authentic   bool
	expiresAtMs int64
}

// NewServer returns a Server with production defaults.
func NewServer(version string) *Server {
	return &Server{
		Version:   version,
		NewConnID: func() string { return fmt.Sprintf("conn-%d", time.Now().UnixNano()) },
		Now:       time.Now,
		started:   time.Now(),
		pending:   map[string]pendingAct{},
	}
}

// methods and events are what ghillie will actually answer. Declaring a method
// here that ghillie does not implement would be a lie the UI acts on, so this
// list stays honest and grows as frames land.
func (s *Server) advertised() ([]string, []string) {
	return []string{
			"connect", "approval.resolve",
			"sessions.list", "sessions.describe",
			"sessions.subscribe", "sessions.messages.subscribe",
			"sessions.files.list", "sessions.observer.visibility",
			"chat.history", "chat.startup", "chat.metadata", "chat.send",
			"agent.identity.get", "agents.list", "question.list",
			"cron.list", "tasks.list", "models.list", "models.authStatus",
			"config.get",
			"plugin.approval.list", "openclaw.approval.list", "exec.approval.list",
		}, []string{
			"chat", "session.message", "tick",
		}
}

// ServeHTTP accepts the socket and runs the frame loop.
func (s *Server) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	c, err := accept(w, r, s.OriginPatterns)
	if err != nil {
		// Accept has already written a response on failure, including the
		// 403 for a rejected Origin. Saying more here would leak whether a
		// given origin is configured.
		return
	}
	defer c.Close()
	s.attach(c)
	defer s.detach(c)

	// ★ THE SERVER SPEAKS FIRST. A gateway v4 client waits for a
	// connect.challenge event before it will send connect, and its watchdog
	// closes the socket into a reconnect loop when the event never comes
	// (gateway-client client.ts: "gateway connect challenge timeout"). The
	// nonce is issued per connection. HONEST LIMIT: this slice does not yet
	// verify the nonce's echo or a device signature on the connect that
	// follows — issuing without verifying is stated here so nobody reads a
	// challenge frame as an authentication step.
	nonce, err := challengeNonce()
	if err != nil {
		return
	}
	challenge, err := json.Marshal(eventFrame{
		Type:  "event",
		Event: "connect.challenge",
		Payload: map[string]interface{}{
			"nonce": nonce,
			"ts":    s.Now().UnixMilli(),
		},
	})
	if err != nil {
		return
	}
	if err := c.writeText(challenge); err != nil {
		return
	}
	c.challengeNonce = nonce

	// ★ THE GATEWAY MUST BREATHE. The client watches for periodic activity
	// (tick events; policy default 30s) and closes the socket as dead after
	// roughly twice that silence — an idle interview would then reconnect
	// forever, re-arming the page's loading state each time (found live,
	// 2026-08-26, close code 4000 "tick timeout"). Any frame counts as
	// activity; an explicit tick is the honest heartbeat.
	stopTicks := make(chan struct{})
	defer close(stopTicks)
	go func() {
		t := time.NewTicker(10 * time.Second)
		defer t.Stop()
		for {
			select {
			case <-stopTicks:
				return
			case <-t.C:
				body, terr := json.Marshal(eventFrame{Type: "event", Event: "tick"})
				if terr != nil {
					return
				}
				if werr := c.writeText(body); werr != nil {
					return
				}
			}
		}
	}()
	s.serve(c)
}

// challengeNonce issues one connection's connect-challenge nonce from the
// system's entropy — never from the clock, which is guessable.
func challengeNonce() (string, error) {
	raw := make([]byte, 16)
	if _, err := rand.Read(raw); err != nil {
		return "", fmt.Errorf("glass: challenge nonce: %w", err)
	}
	return hex.EncodeToString(raw), nil
}

// serve reads frames until the peer goes away.
func (s *Server) serve(c *conn) {
	for {
		raw, err := c.readText()
		if err != nil {
			if s.Debug != nil {
				s.Debug.Printf("glass✂ read ended: %v", err)
			}
			return
		}
		out, fatal := s.handleFrom(c, raw)
		if out != nil {
			if werr := c.writeText(out); werr != nil {
				if s.Debug != nil {
					s.Debug.Printf("glass✂ write ended: %v", werr)
				}
				return
			}
		}
		if fatal {
			if s.Debug != nil {
				s.Debug.Printf("glass✂ FATAL frame, closing; raw=%.200s", raw)
			}
			// Tell the peer the frame stream broke rather than dropping it.
			_ = c.CloseWithProtocolError("gateway frame refused")
			return
		}
	}
}

// handleFrom dispatches one frame with its connection's context (today: the
// challenge nonce the connection was issued).
func (s *Server) handleFrom(c *conn, raw []byte) (out []byte, fatal bool) {
	return s.handleWithNonce(raw, c.challengeNonce)
}

// handle turns one inbound frame into one outbound frame with no connection
// context — the test entry point, and every method except connect.
func (s *Server) handle(raw []byte) (out []byte, fatal bool) {
	return s.handleWithNonce(raw, "")
}

// handleWithNonce turns one inbound frame into one outbound frame. It returns
// fatal=true when the connection should not continue.
func (s *Server) handleWithNonce(raw []byte, issuedNonce string) (out []byte, fatal bool) {
	var req requestFrame
	if err := json.Unmarshal(raw, &req); err != nil {
		return s.encodeErr("", "bad_request", "frame is not valid JSON"), true
	}
	if req.Type != "req" || req.ID == "" || req.Method == "" {
		return s.encodeErr(req.ID, "bad_request", "not a well-formed request frame"), true
	}

	if s.Debug != nil {
		s.Debug.Printf("glass⇐ %s id=%s params=%s", req.Method, req.ID, string(req.Params))
	}
	switch req.Method {
	case "connect":
		return s.handleConnect(req, issuedNonce)
	case "approval.resolve":
		return s.handleApprovalResolve(req)
	case "sessions.list":
		return s.handleSessionsList(req)
	case "sessions.describe":
		return s.handleSessionsDescribe(req)
	case "sessions.subscribe", "sessions.messages.subscribe":
		return s.handleSessionsSubscribe(req)
	case "chat.history":
		return s.handleChatHistory(req)
	case "chat.startup":
		return s.handleChatStartup(req)
	case "chat.metadata":
		return s.handleChatMetadata(req)
	case "chat.send":
		return s.handleChatSend(req)
	case "agent.identity.get":
		return s.handleAgentIdentityGet(req)
	case "question.list":
		return s.handleQuestionList(req)
	case "agents.list":
		return s.handleAgentsList(req)
	case "cron.list", "tasks.list", "models.list", "models.authStatus",
		"config.get", "plugin.approval.list", "openclaw.approval.list",
		"exec.approval.list", "sessions.files.list", "sessions.observer.visibility":
		return s.handleEmptily(req)
	default:
		// Refuse by default. A gateway that silently accepted unknown methods
		// would be deciding, and this package decides nothing.
		if s.Debug != nil {
			s.Debug.Printf("glass⇒ refusing unknown method %q", req.Method)
		}
		return s.encodeErr(req.ID, "unsupported_method",
			fmt.Sprintf("ghillie does not implement %q", req.Method)), false
	}
}

// handleConnect negotiates the wire version and answers hello-ok. When the
// client offers a device block, its nonce MUST be the challenge nonce this
// connection was issued — an echo of somebody else's challenge is a replay,
// and it is refused before anything else is considered.
func (s *Server) handleConnect(req requestFrame, issuedNonce string) (out []byte, fatal bool) {
	var p connectParams
	if len(req.Params) > 0 {
		if err := json.Unmarshal(req.Params, &p); err != nil {
			return s.encodeErr(req.ID, "bad_request", "connect params are not valid JSON"), true
		}
	}
	if p.MinProtocol <= 0 || p.MaxProtocol <= 0 {
		return s.encodeErr(req.ID, "bad_request", "connect requires minProtocol and maxProtocol"), true
	}
	if p.Device != nil && p.Device.Nonce != "" && p.Device.Nonce != issuedNonce {
		return s.encodeErr(req.ID, "bad_request",
			"connect device.nonce is not the challenge nonce this connection was issued — refused as a replay"), true
	}
	// Version negotiation is a REFUSAL, not a best effort: answering a protocol
	// the client did not ask for is how two peers end up disagreeing silently.
	if ProtocolVersion < p.MinProtocol || ProtocolVersion > p.MaxProtocol {
		return s.encodeErr(req.ID, "protocol_unsupported",
			fmt.Sprintf("ghillie speaks gateway v%d; client accepts v%d..v%d",
				ProtocolVersion, p.MinProtocol, p.MaxProtocol)), true
	}

	methods, events := s.advertised()
	var h helloOk
	h.Type = "hello-ok"
	h.Protocol = ProtocolVersion
	h.Server.Version = s.Version
	h.Server.ConnID = s.NewConnID()
	h.Features.Methods = methods
	h.Features.Events = events
	h.Snapshot = snapshot{
		Presence:     []presenceEntry{},
		StateVersion: stateVersion{Presence: 0, Health: 0},
		UptimeMs:     int64(s.Now().Sub(s.started) / time.Millisecond),
	}
	// The attached page is the OWNER at their own machine: operator role, with
	// read/write and the approvals scope the implemented approval.resolve
	// needs. Scopes not backed by an implemented method are not claimed.
	h.Auth.Role = "operator"
	h.Auth.Scopes = []string{"operator.read", "operator.write", "operator.approvals"}
	h.Policy.MaxPayload = maxFrameBytes
	h.Policy.MaxBufferedBytes = 2 * maxFrameBytes
	h.Policy.TickIntervalMs = 10_000

	body, err := json.Marshal(responseFrame{Type: "res", ID: req.ID, OK: true, Payload: h})
	if err != nil {
		return s.encodeErr(req.ID, "internal", "could not encode hello-ok"), true
	}
	return body, false
}

// encodeErr builds a response frame carrying an ErrorShape.
func (s *Server) encodeErr(id, code, msg string) []byte {
	if id == "" {
		id = "unknown"
	}
	body, err := json.Marshal(responseFrame{
		Type:  "res",
		ID:    id,
		OK:    false,
		Error: &errorShape{Code: code, Message: msg},
	})
	if err != nil {
		return []byte(`{"type":"res","id":"unknown","ok":false,"error":{"code":"internal","message":"encode failed"}}`)
	}
	return body
}
