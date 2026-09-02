package purposetrial

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"time"
)

// OllamaModelID is the one model this harness drives: muse-glimmer:latest, served
// LOCALLY by the owner's own ollama. It is sovereign by construction — no cloud,
// no provider key, no remote endpoint — which is a hard requirement of this
// harness. It is recorded verbatim in every transcript's model_id.
// (Set to muse-glimmer 2026-09-02, Tony's direction; was qwen2.5-coder:32b, which
// was swapped off the local ollama. muse-glimmer's edge is agentic loops — apt for
// driving the purpose trial.)
const OllamaModelID = "muse-glimmer:latest"

// DefaultOllamaEndpoint is where a local ollama serves its generate API by
// default: loopback only. Nothing here reaches off the owner's machine.
const DefaultOllamaEndpoint = "http://127.0.0.1:11434/api/generate"

// ollamaRequestTimeout bounds one model call. A 32B model on local hardware can
// take a while to answer, so this is generous; a call that outlasts it is
// treated as the model being unreachable, and the transcript is written
// incomplete.
const ollamaRequestTimeout = 5 * time.Minute

// OllamaModel is the real Model: it calls a local ollama's /api/generate with
// raw net/http and encoding/json — no SDK, no extra dependency. It satisfies
// Model so the harness drives it exactly as it drives a test stub.
type OllamaModel struct {
	// endpoint is the ollama generate URL. Empty means DefaultOllamaEndpoint.
	endpoint string
	// model is the model name sent to ollama. Empty means OllamaModelID.
	model string
	// client is the HTTP client used for the call. Empty means a default client
	// bounded per-call by context.
	client *http.Client
}

// NewOllamaModel returns an OllamaModel pointing at the local ollama serving
// muse-glimmer:latest. endpoint may be empty to use DefaultOllamaEndpoint; it is a
// parameter only so a test could point at a loopback fake, never so this harness
// reaches a remote model.
func NewOllamaModel(endpoint string) *OllamaModel {
	if endpoint == "" {
		endpoint = DefaultOllamaEndpoint
	}
	return &OllamaModel{
		endpoint: endpoint,
		model:    OllamaModelID,
		client:   &http.Client{},
	}
}

// NewOllamaModelNamed is NewOllamaModel over a caller-named model. It exists so a
// SECOND, independent harness — the adversarial pass (internal/adversary) — can
// reuse this exact local-ollama client against a DIFFERENT local model without
// duplicating the HTTP client, honouring the same loopback endpoint. endpoint
// may be empty for DefaultOllamaEndpoint; model may be empty for OllamaModelID.
// Like NewOllamaModel it never reaches off the owner's machine.
func NewOllamaModelNamed(endpoint, model string) *OllamaModel {
	m := NewOllamaModel(endpoint)
	if model != "" {
		m.model = model
	}
	return m
}

// ollamaGenerateRequest is the body sent to ollama's /api/generate. Stream is
// false so the whole response comes back in one JSON object.
type ollamaGenerateRequest struct {
	Model  string `json:"model"`
	Prompt string `json:"prompt"`
	Stream bool   `json:"stream"`
}

// ollamaGenerateResponse is the shape ollama's /api/generate returns when
// stream is false: the model's whole text sits in Response.
type ollamaGenerateResponse struct {
	Response string `json:"response"`
	Done     bool   `json:"done"`
}

// Complete sends one prompt to the local ollama and returns the model's whole
// text response. It honours ctx, and additionally bounds the call by
// ollamaRequestTimeout. A transport error, a non-200 status, or an undecodable
// body all return an error — which the harness turns into a fail-closed,
// incomplete transcript rather than a silent success.
func (m *OllamaModel) Complete(ctx context.Context, prompt string) (response string, err error) {
	reqCtx, cancel := context.WithTimeout(ctx, ollamaRequestTimeout)
	defer cancel()

	body, err := json.Marshal(ollamaGenerateRequest{Model: m.model, Prompt: prompt, Stream: false})
	if err != nil {
		return "", fmt.Errorf("purposetrial: encoding the ollama request: %w", err)
	}

	req, err := http.NewRequestWithContext(reqCtx, http.MethodPost, m.endpoint, bytes.NewReader(body))
	if err != nil {
		return "", fmt.Errorf("purposetrial: building the ollama request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := m.client.Do(req)
	if err != nil {
		return "", fmt.Errorf("purposetrial: reaching the local ollama at %s: %w", m.endpoint, err)
	}
	defer func() {
		if cerr := resp.Body.Close(); cerr != nil && err == nil {
			err = fmt.Errorf("purposetrial: closing the ollama response: %w", cerr)
		}
	}()

	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("purposetrial: the local ollama answered with status %s", resp.Status)
	}

	var decoded ollamaGenerateResponse
	if err = json.NewDecoder(resp.Body).Decode(&decoded); err != nil {
		return "", fmt.Errorf("purposetrial: decoding the ollama response: %w", err)
	}
	return decoded.Response, nil
}
