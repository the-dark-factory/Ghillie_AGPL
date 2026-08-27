package glass

// uiboot.go answers the methods a CURRENT control UI calls while opening the
// chat page, discovered against the real client on 2026-08-26 (the api survey
// of 08-25 predated its calendar train: the page loads its transcript through
// chat.startup, not chat.history, and a refused chat.startup reads as
// retryable — the pane retries forever and renders skeletons).
//
// Every answer here is the truth about THIS ghillie: one session, one agent
// named ghillie, no models, no cron, no swarm, no pending questions. Nothing
// is stubbed to look richer than it is — a control UI attached to a ghillie
// shows a ghillie.

import (
	"encoding/json"
	"fmt"
	"strings"
)

// acceptableSessionKey reports whether a client-supplied key names our one
// session. Their UI namespaces keys as "agent:<agentId>:<key>", so both the
// bare key and any agent-prefixed form of it are the same session.
func acceptableSessionKey(k string) bool {
	if k == "" || k == SessionKey {
		return true
	}
	return strings.HasSuffix(k, ":"+SessionKey)
}

// CanonicalSessionKey is the session's name AS THE CONTROL UI CANONICALIZES
// IT: "agent:<agentId>:<key>". The UI ties its transcript surface to the row
// from sessions.list under this exact key — a bare key reads to it as a
// SECOND session, and the pane routed at the canonical key then has no row
// at all and never leaves its loading state (docs/gateway/protocol.md
// sessions.list; observed live as the duplicate sidebar rows).
const CanonicalSessionKey = "agent:main:" + SessionKey

// startupParams is what chat.startup and chat.metadata carry.
type startupParams struct {
	SessionKey string `json:"sessionKey"`
	AgentID    string `json:"agentId"`
	Limit      int    `json:"limit"`
	Cursor     string `json:"cursor"`
}

// chatMetadata is the truthful metadata block: no slash commands, no models
// to pick between, no swarm.
func chatMetadata() map[string]any {
	return map[string]any{
		"commands":     []any{},
		"models":       []any{},
		"swarmEnabled": false,
	}
}

// handleChatStartup answers "chat.startup": the transcript as one complete
// page plus the metadata block. It is chat.history's shape with the startup
// extras, which is exactly what the client merges.
func (s *Server) handleChatStartup(req requestFrame) (out []byte, fatal bool) {
	var p startupParams
	if len(req.Params) > 0 {
		if err := json.Unmarshal(req.Params, &p); err != nil {
			return s.encodeErr(req.ID, "bad_request", "chat.startup params are not valid JSON"), false
		}
	}
	if !acceptableSessionKey(p.SessionKey) {
		return s.encodeErr(req.ID, "not_found",
			fmt.Sprintf("ghillie has one session, %q; %q is not it", SessionKey, p.SessionKey)), false
	}
	payload := s.historyPayload(p)
	payload["metadata"] = chatMetadata()
	return s.encodeOK(req.ID, payload), false
}

// historyPayload builds the transcript answer for chat.startup and
// chat.history alike, honouring the client's cursor protocol: a CURSORLESS
// request gets the whole transcript as one complete page; a request carrying
// a cursor we minted gets kind "delta" with only what moved since; a cursor
// we cannot place gets kind "reset", which tells the client to re-page — the
// three answers their pane's loader actually distinguishes. Answering a
// cursor request with a full page re-applies the transcript and re-arms the
// pane's loading state, forever (found live, 2026-08-26).
func (s *Server) historyPayload(p startupParams) map[string]any {
	s.mu.Lock()
	msgs := make([]message, len(s.transcript))
	copy(msgs, s.transcript)
	s.mu.Unlock()

	// The key is echoed AS THE CLIENT ASKED IT: their UI addresses our one
	// session by its own canonical "agent:<id>:<key>" name, and an answer
	// naming a different key reads to it as some other session's.
	key := p.SessionKey
	if key == "" {
		key = SessionKey
	}
	sessionInfo := map[string]any{"key": key, "sessionId": key}
	cursor := fmt.Sprintf("t-%d", len(msgs))

	if p.Cursor != "" {
		var since int
		if _, err := fmt.Sscanf(p.Cursor, "t-%d", &since); err != nil || since < 0 || since > len(msgs) {
			return map[string]any{"kind": "reset", "sessionInfo": sessionInfo}
		}
		// Delta entries are session.message PAYLOADS, not bare messages: the
		// client replays each through the same reducer as a live event
		// (docs/gateway/protocol.md: "replay each messages entry through the
		// same reducer as a live session.message payload").
		delta := make([]map[string]any, 0, len(msgs)-since)
		for _, m := range msgs[since:] {
			delta = append(delta, sessionMessagePayload(m))
		}
		return map[string]any{
			"kind":        "delta",
			"messages":    delta,
			"deltaCursor": cursor,
			"sessionInfo": sessionInfo,
		}
	}
	return map[string]any{
		"messages":         msgs,
		"offset":           0,
		"hasMore":          false,
		"totalMessages":    len(msgs),
		"completeSnapshot": true,
		"deltaCursor":      cursor,
		"sessionId":        key,
		"sessionInfo":      sessionInfo,
	}
}

// handleChatMetadata answers "chat.metadata" with the same truthful block.
func (s *Server) handleChatMetadata(req requestFrame) (out []byte, fatal bool) {
	return s.encodeOK(req.ID, chatMetadata()), false
}

// handleSessionsDescribe answers "sessions.describe" for our one session, and
// {session: null} for any other key — the same answer their gateway gives for
// a session it does not hold.
func (s *Server) handleSessionsDescribe(req requestFrame) (out []byte, fatal bool) {
	var p struct {
		Key string `json:"key"`
	}
	if len(req.Params) > 0 {
		if err := json.Unmarshal(req.Params, &p); err != nil {
			return s.encodeErr(req.ID, "bad_request", "sessions.describe params are not valid JSON"), false
		}
	}
	if !acceptableSessionKey(p.Key) {
		return s.encodeOK(req.ID, map[string]any{"session": nil}), false
	}
	s.mu.Lock()
	updated := int64(0)
	if n := len(s.transcript); n > 0 {
		updated = s.transcript[n-1].Timestamp
	}
	s.mu.Unlock()
	key := p.Key
	if key == "" {
		key = SessionKey
	}
	return s.encodeOK(req.ID, map[string]any{
		"session": sessionRow{
			Key:         key,
			Kind:        "direct",
			Label:       "Ghillie",
			DisplayName: "Ghillie",
			UpdatedAt:   updated,
		},
	}), false
}

// handleSessionsSubscribe answers both "sessions.subscribe" and
// "sessions.messages.subscribe". Every attached connection already receives
// every chat event — the transcript is one and broadcast is the only delivery
// there is — so subscribing is acknowledging what is already true.
func (s *Server) handleSessionsSubscribe(req requestFrame) (out []byte, fatal bool) {
	return s.encodeOK(req.ID, map[string]any{"subscribed": true}), false
}

// handleAgentIdentityGet answers "agent.identity.get": there is one agent
// here and it is ghillie. No avatar is claimed — the UI renders its default.
func (s *Server) handleAgentIdentityGet(req requestFrame) (out []byte, fatal bool) {
	return s.encodeOK(req.ID, map[string]any{
		"agentId": "main",
		"name":    "Ghillie",
	}), false
}

// handleQuestionList answers "question.list": ghillie's questions arrive in
// the chat itself, so this gateway-level queue is truthfully empty.
func (s *Server) handleQuestionList(req requestFrame) (out []byte, fatal bool) {
	return s.encodeOK(req.ID, map[string]any{"questions": []any{}}), false
}

// handleAgentsList answers "agents.list": one agent, ghillie, and it is the
// default. Shape per AgentsListResultSchema (agents-models-skills.ts).
//
// The model.primary field names WHAT ANSWERS on this session: ghillie's
// compiled interview conduct — not an LLM, and not configurable. It is
// declared because the control UI's composer replaces itself with a
// "connect an AI provider" banner for an agent with no model, and a person
// who cannot type cannot be interviewed. The name is the honest one.
func (s *Server) handleAgentsList(req requestFrame) (out []byte, fatal bool) {
	return s.encodeOK(req.ID, map[string]any{
		"defaultId": "main",
		"mainKey":   "main",
		"scope":     "global",
		"agents": []any{map[string]any{
			"id":    "main",
			"name":  "Ghillie",
			"model": map[string]any{"primary": "ghillie/interview-conduct"},
		}},
	}), false
}

// handleEmptily answers the boot-time inventory methods whose truthful answer
// here is an empty collection: ghillie schedules no cron jobs, runs no tasks,
// holds no models, keeps no gateway-level approvals and stores no files. One
// handler because the truth is one fact — there is nothing of the kind.
func (s *Server) handleEmptily(req requestFrame) (out []byte, fatal bool) {
	var payload map[string]any
	switch req.Method {
	case "cron.list":
		payload = map[string]any{"jobs": []any{}}
	case "tasks.list":
		payload = map[string]any{"tasks": []any{}}
	case "models.list":
		payload = map[string]any{"models": []any{}}
	case "models.authStatus":
		payload = map[string]any{}
	case "config.get":
		payload = map[string]any{"config": map[string]any{}}
	case "plugin.approval.list", "openclaw.approval.list", "exec.approval.list":
		payload = map[string]any{"approvals": []any{}}
	case "sessions.files.list":
		payload = map[string]any{"files": []any{}}
	case "sessions.observer.visibility":
		payload = map[string]any{}
	default:
		return s.encodeErr(req.ID, "internal",
			fmt.Sprintf("handleEmptily routed a method it does not know: %q", req.Method)), false
	}
	return s.encodeOK(req.ID, payload), false
}
