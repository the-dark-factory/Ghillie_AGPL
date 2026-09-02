package glass

// Chat — the slice of the gateway protocol that OpenClaw's chat page speaks,
// carried so that page can be ghillie's interview surface.
//
// DOCTRINE, restated for this file because it is where the temptation lives:
// the chat methods CARRY, they do not DECIDE. There is no responder here, no
// model call, no synthesized reply. What ghillie says arrives through SayLine;
// what the client says leaves through AwaitReply; both are transcript entries
// first and wire frames second. The interview's conduct cores (ledgers 122/123)
// stay the only things deciding who may speak — a glass that answered for
// ghillie would be a second, unproven mouth.
//
// ONE SESSION. A ghillie is one interlocutor conducting one interview, so the
// gateway exposes exactly one session row. Their protocol is built for a fleet;
// answering with a fleet of one is conformant and honest.

import (
	"context"
	"encoding/json"
	"fmt"
)

// SessionKey names ghillie's single chat session on the wire.
const SessionKey = "ghillie:interview"

// message is a transcript entry in the permissive shape the control UI's
// normalizer reads (message-normalizer.ts: role, content, timestamp, id — all
// optional, unknown keys tolerated). Everything here is emitted, so every
// field is set.
// contentBlock is one block of a message's content. The control UI renders
// content ONLY as an array of typed blocks (a bare string draws as nothing —
// verified against the real page 2026-08-26); ghillie's utterances are always
// exactly one text block.
type contentBlock struct {
	Type string `json:"type"`
	Text string `json:"text"`
}

type message struct {
	ID        string         `json:"id"`
	Role      string         `json:"role"`
	Content   []contentBlock `json:"content"`
	Timestamp int64          `json:"timestamp"`
}

// chatEvent mirrors ChatFinalEventSchema. Ghillie speaks in whole lines, not
// token streams — the interview's Say is a completed utterance — so the stream
// states it emits are "final" per utterance. A delta stream can be added
// without breaking anything because the union is additive on `state`.
type chatEvent struct {
	RunID      string   `json:"runId"`
	SessionKey string   `json:"sessionKey"`
	Seq        int      `json:"seq"`
	State      string   `json:"state"`
	Message    *message `json:"message,omitempty"`
}

// eventFrame mirrors EventFrameSchema.
type eventFrame struct {
	Type    string      `json:"type"`
	Event   string      `json:"event"`
	Payload interface{} `json:"payload,omitempty"`
	Seq     int         `json:"seq,omitempty"`
}

// sessionRow is the subset of SessionRowSchema ghillie reports. Their row
// schema is permissive; omitted fields read as absent, not null.
type sessionRow struct {
	Key         string `json:"key"`
	Kind        string `json:"kind"`
	Label       string `json:"label,omitempty"`
	DisplayName string `json:"displayName,omitempty"`
	UpdatedAt   int64  `json:"updatedAt,omitempty"`
}

// chatSendParams reads the fields of ChatSendParamsSchema this server acts on.
// The schema carries many more (fastMode, queueMode, attachments, ...); they
// are client-side conduct preferences, and ghillie's conduct is compiled in,
// so they are deliberately not read rather than silently half-honoured.
type chatSendParams struct {
	SessionKey     string `json:"sessionKey"`
	Message        string `json:"message"`
	IdempotencyKey string `json:"idempotencyKey"`
}

// chatHistoryParams reads the session addressing of ChatHistoryParamsSchema.
// Cursors and limits are accepted and ignored: the whole transcript of one
// interview fits in one page, and pretending to paginate would be a lie the
// client acts on.
type chatHistoryParams struct {
	SessionKey string `json:"sessionKey"`
	Cursor     string `json:"cursor"`
}

// ErrClientGone reports that AwaitReply ended because the context did, which
// in an interview means the client left rather than answered.
// (Defined with fmt.Errorf so the message carries no state; it wraps nothing.)
var ErrClientGone = fmt.Errorf("glass: client gone before replying")

// SayLine records one ghillie utterance and shows it to every attached client.
//
// This is the ONLY way words appear under ghillie's name on this wire. It is
// exported for the interview's Surface adapter; nothing inside this package
// calls it.
func (s *Server) SayLine(ctx context.Context, line string) error {
	if err := ctx.Err(); err != nil {
		return fmt.Errorf("glass: say: %w", err)
	}
	s.mu.Lock()
	m := s.appendLocked("assistant", line)
	ev := s.eventLocked("final", &m)
	sm := s.sessionMessageLocked(&m)
	s.mu.Unlock()
	s.broadcast(ev)
	s.broadcast(sm)
	return nil
}

// AwaitReply blocks until a client utterance is available and returns it.
//
// Utterances that arrived while nobody was waiting are NOT dropped: they queue
// in arrival order, because a person who answers promptly has still answered.
// The wait has no deadline of its own — how long ghillie waits is conduct,
// compiled in upstream, exactly as interview.Surface.Ask requires.
func (s *Server) AwaitReply(ctx context.Context) (reply string, err error) {
	s.mu.Lock()
	if len(s.replies) > 0 {
		reply, s.replies = s.replies[0], s.replies[1:]
		s.mu.Unlock()
		return reply, nil
	}
	ch := make(chan string, 1)
	s.waiter = ch
	s.mu.Unlock()

	select {
	case reply = <-ch:
		return reply, nil
	case <-ctx.Done():
		s.mu.Lock()
		if s.waiter == ch {
			s.waiter = nil
		}
		s.mu.Unlock()
		return "", ErrClientGone
	}
}

// Transcript returns a copy of the conversation so far, oldest first.
func (s *Server) Transcript() []message {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make([]message, len(s.transcript))
	copy(out, s.transcript)
	return out
}

// appendLocked adds a transcript entry. Callers hold s.mu.
func (s *Server) appendLocked(role, content string) message {
	s.msgSeq++
	m := message{
		ID:        fmt.Sprintf("msg-%d", s.msgSeq),
		Role:      role,
		Content:   []contentBlock{{Type: "text", Text: content}},
		Timestamp: s.Now().UnixMilli(),
	}
	s.transcript = append(s.transcript, m)
	return m
}

// eventLocked builds the next chat event. Callers hold s.mu. Each utterance is
// its own run: the interview's unit of exchange is the utterance, and the
// page's per-run grouping then matches what actually happened.
func (s *Server) eventLocked(state string, m *message) eventFrame {
	s.runSeq++
	s.evSeq++
	return eventFrame{
		Type:  "event",
		Event: "chat",
		Seq:   s.evSeq,
		Payload: chatEvent{
			RunID:      fmt.Sprintf("run-%d", s.runSeq),
			SessionKey: SessionKey,
			Seq:        0,
			State:      state,
			Message:    m,
		},
	}
}

// sessionMessagePayload wraps one transcript entry the way a session.message
// event carries it — and the way a chat.startup delta entry must be shaped,
// because the client replays delta entries "through the same reducer as a
// live session.message payload" (docs/gateway/protocol.md, chat family).
func sessionMessagePayload(m message) map[string]any {
	return map[string]any{
		"sessionKey": CanonicalSessionKey,
		"message":    m,
	}
}

// sessionMessageLocked builds the session.message event for one appended
// transcript entry. Callers hold s.mu. This is the event family the control
// UI's subscribed transcript actually consumes; the "chat" event above is the
// UI-chat-update family and does not feed it.
func (s *Server) sessionMessageLocked(m *message) eventFrame {
	s.evSeq++
	return eventFrame{
		Type:    "event",
		Event:   "session.message",
		Seq:     s.evSeq,
		Payload: sessionMessagePayload(*m),
	}
}

// broadcast writes one event frame to every attached connection. A connection
// that cannot be written is left for its own read loop to reap — the frame
// stream to the others must not stall on one dead peer.
func (s *Server) broadcast(ev eventFrame) {
	body, err := json.Marshal(ev)
	if err != nil {
		return
	}
	s.mu.Lock()
	conns := make([]*conn, 0, len(s.conns))
	for c := range s.conns {
		conns = append(conns, c)
	}
	s.mu.Unlock()
	for _, c := range conns {
		_ = c.writeText(body)
	}
}

// attach registers a connection for events; detach removes it.
func (s *Server) attach(c *conn) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.conns == nil {
		s.conns = map[*conn]bool{}
	}
	s.conns[c] = true
}

func (s *Server) detach(c *conn) {
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.conns, c)
}

// handleSessionsList answers "sessions.list" with the fleet of one.
func (s *Server) handleSessionsList(req requestFrame) (out []byte, fatal bool) {
	s.mu.Lock()
	updated := int64(0)
	if n := len(s.transcript); n > 0 {
		updated = s.transcript[n-1].Timestamp
	}
	s.mu.Unlock()
	payload := map[string]any{
		"sessions": []sessionRow{{
			Key:         CanonicalSessionKey,
			Kind:        "direct",
			Label:       "Ghillie",
			DisplayName: "Ghillie",
			UpdatedAt:   updated,
		}},
	}
	return s.encodeOK(req.ID, payload), false
}

// handleChatHistory answers "chat.history" with the whole transcript as one
// delta page (ChatHistoryDeltaResultSchema). deltaCursor advances with the
// transcript so a client that re-asks can see whether anything moved.
func (s *Server) handleChatHistory(req requestFrame) (out []byte, fatal bool) {
	var p chatHistoryParams
	if len(req.Params) > 0 {
		if err := json.Unmarshal(req.Params, &p); err != nil {
			return s.encodeErr(req.ID, "bad_request", "chat.history params are not valid JSON"), false
		}
	}
	if !acceptableSessionKey(p.SessionKey) {
		return s.encodeErr(req.ID, "not_found",
			fmt.Sprintf("ghillie has one session, %q; %q is not it", SessionKey, p.SessionKey)), false
	}
	// One transcript, one answer: chat.history and chat.startup share the
	// cursor-honouring payload (uiboot.go historyPayload).
	return s.encodeOK(req.ID, s.historyPayload(startupParams{
		SessionKey: p.SessionKey,
		Cursor:     p.Cursor,
	})), false
}

// handleChatSend answers "chat.send": record the utterance, hand it to the
// waiting interview if there is one, and acknowledge.
//
// The ack is honest about what happened: "ok" — received and recorded. There
// is no "the model is thinking" state to report, because there is no model
// here; whether and when ghillie speaks next is the interview's decision.
func (s *Server) handleChatSend(req requestFrame) (out []byte, fatal bool) {
	var p chatSendParams
	if len(req.Params) > 0 {
		if err := json.Unmarshal(req.Params, &p); err != nil {
			return s.encodeErr(req.ID, "bad_request", "chat.send params are not valid JSON"), false
		}
	}
	if p.SessionKey == "" || !acceptableSessionKey(p.SessionKey) {
		return s.encodeErr(req.ID, "not_found",
			fmt.Sprintf("ghillie has one session, %q; %q is not it", SessionKey, p.SessionKey)), false
	}
	if p.IdempotencyKey == "" {
		return s.encodeErr(req.ID, "bad_request", "chat.send requires an idempotencyKey"), false
	}
	if p.Message == "" {
		return s.encodeErr(req.ID, "bad_request", "chat.send requires a message"), false
	}

	s.mu.Lock()
	// Idempotency: a retried send must not become a second utterance.
	if s.seenSends[p.IdempotencyKey] {
		s.mu.Unlock()
		return s.encodeOK(req.ID, map[string]any{"runId": p.IdempotencyKey, "status": "ok"}), false
	}
	if s.seenSends == nil {
		s.seenSends = map[string]bool{}
	}
	s.seenSends[p.IdempotencyKey] = true

	m := s.appendLocked("user", p.Message)
	ev := s.eventLocked("final", &m)
	sm := s.sessionMessageLocked(&m)
	var waiter chan string
	if s.waiter != nil {
		waiter, s.waiter = s.waiter, nil
	} else {
		s.replies = append(s.replies, p.Message)
	}
	s.mu.Unlock()

	// Echo the recorded utterance to every attached client (including the
	// sender) so all panes converge on the transcript, then wake the interview.
	s.broadcast(ev)
	s.broadcast(sm)
	if waiter != nil {
		waiter <- p.Message
	}
	return s.encodeOK(req.ID, map[string]any{"runId": p.IdempotencyKey, "status": "ok"}), false
}

// encodeOK builds a successful response frame.
func (s *Server) encodeOK(id string, payload interface{}) []byte {
	body, err := json.Marshal(responseFrame{Type: "res", ID: id, OK: true, Payload: payload})
	if err != nil {
		return s.encodeErr(id, "internal", "could not encode response")
	}
	return body
}
