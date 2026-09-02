// Package mind is the OWNER'S INFERENCE SOCKET: the seam where the person who
// downloaded this ghillie plugs in a model that runs on THEIR machine.
//
// ★ THE RULING (Tony, 2026-08-28). A downloaded ghillie does not draw the
// collective's inference. "They need to set the inference from their machine."
// There is therefore no default remote endpoint in this package, no provider
// key, no fallback to the facade and no fallback to anything the collective
// runs. The only endpoint is the one the owner names, and by construction it
// must be on the owner's own computer.
//
// WHAT THE MIND IS NOT. It is not a decision-maker. It never sees a proof, it
// never authors a refusal, it never touches an admission and it does not speak
// through the conduct wall. Those are the proven cores' work and they stay
// deterministic — a model that changes its mind between runs cannot be allowed
// anywhere near a judgement the product claims is settled. This package is
// imported by the conversational surface and by nothing else; the isolation is
// asserted by TestMindIsNotImportedByTheDecisionPaths in cmd/ghillie.
//
// NO MIND, NO PRETENCE. When nothing answers at the mind URL, this package
// returns a refusal that says exactly that, in the owner's own terms, and the
// caller says so out loud. Everything the ghillie does without a mind —
// catalogue, install, proofs, the facade protocol, the interview — keeps
// working untouched. Silence is reported, never simulated.
package mind

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"strings"
	"time"
)

// The errors this package returns. Each is a distinct situation the owner can
// act on, so each is distinguishable with errors.Is rather than by matching
// text.
var (
	// ErrNotConfigured reports a mind URL that is unusable before anything was
	// contacted — malformed, wrong scheme, or off this machine.
	ErrNotConfigured = errors.New("mind: not configured")

	// ErrNoMind reports that nothing answers at the mind URL. It is not a
	// fault: a ghillie with no mind is a complete product, and the caller is
	// expected to say so plainly rather than to invent an answer.
	ErrNoMind = errors.New("mind: no mind is set on this machine")

	// ErrNoModel reports a server that answers but serves no model to pick.
	ErrNoModel = errors.New("mind: the server answers but serves no model")

	// ErrChatFailed reports a mind that was reached and could not answer.
	ErrChatFailed = errors.New("mind: the model did not answer")
)

// Dialect names the API a reached server was found to speak. It is reported to
// the owner verbatim: which wire protocol answered is a fact about their
// machine, and guessing silently is how "it works" turns into "it worked".
type Dialect string

const (
	// DialectOllama is ollama's own /api/chat. First-class: it is what
	// -mind-url defaults to and what the quickstart tells the owner to install.
	DialectOllama Dialect = "ollama /api/chat"

	// DialectOpenAI is the OpenAI-compatible /v1/chat/completions many local
	// servers also speak — vLLM, llamafile, LM Studio, llama.cpp's server.
	// It is a FALLBACK PROBE on the same URL, never a route off this machine.
	DialectOpenAI Dialect = "OpenAI-compatible /v1/chat/completions"
)

// Message is one turn of a conversation, in the shape both dialects share.
type Message struct {
	// Role is "system", "user" or "assistant".
	Role string `json:"role"`

	// Content is the text of the turn.
	Content string `json:"content"`
}

// Config is everything a Client needs. Every field is set by the OWNER at
// their own machine — a flag or an environment variable, never a brief, never
// the wire, never a page.
type Config struct {
	// URL is the base of the owner's inference server, e.g.
	// http://127.0.0.1:11434. It must be loopback unless AdoptExternal.
	URL string

	// Model is the model to speak with. Empty means "ask the server what it
	// serves and take the first", which the caller then states out loud.
	Model string

	// AdoptExternal permits a NON-LOOPBACK URL.
	//
	// OFF by default, and the reason is sterner than the ears'. The ears hand
	// a remote host the owner's voice while a question is on the floor. The
	// mind is handed EVERYTHING the ghillie is told — the whole conversation,
	// for as long as it lasts. Turning this on is the owner saying they own the
	// far end of that wire.
	AdoptExternal bool

	// Timeout bounds one exchange. Zero means two minutes, which a large model
	// on a laptop can genuinely need.
	Timeout time.Duration

	// HTTPClient overrides the client used, for tests. Nil means a client
	// built from Timeout.
	HTTPClient *http.Client
}

// Client is a reached mind: a URL, the dialect it was found to speak, and the
// model that will answer.
type Client struct {
	url     string
	model   string
	dialect Dialect
	http    *http.Client
}

// URL returns the endpoint this client speaks to.
func (c *Client) URL() string { return c.url }

// Model returns the model that answers. It is never empty on a live client:
// when the owner named none, Open picked one and said which.
func (c *Client) Model() string { return c.model }

// Dialect returns the API the server was detected to speak.
func (c *Client) Dialect() Dialect { return c.dialect }

// Speaking is the one line the surface prints so the owner always knows whose
// words they are reading. It follows the v0.1.3 locale-pack naming: name the
// thing that is actually in force, and where it came from.
func (c *Client) Speaking() string {
	return fmt.Sprintf("mind: %s at %s, speaking %s", c.model, c.url, c.dialect)
}

// requireLoopback refuses any mind URL that is not plainly on this machine.
//
// This is the STRUCTURAL half of the ruling. The product's claim is that a
// downloaded ghillie thinks on its owner's own computer; a claim like that is
// kept by construction, not by everyone remembering to pass the right flag.
// Every address the host resolves to must be loopback — resolving, rather than
// string-matching "127.0.0.1", is what makes a hostile hosts-file entry for
// "localhost" fail closed instead of quietly shipping the owner's whole
// conversation off-box.
func requireLoopback(raw string) error {
	parsed, err := url.Parse(raw)
	if err != nil {
		return fmt.Errorf("%w: mind URL %q is not a URL: %v", ErrNotConfigured, raw, err)
	}
	switch parsed.Scheme {
	case "http", "https":
	default:
		return fmt.Errorf("%w: mind URL %q must be http or https, not %q",
			ErrNotConfigured, raw, parsed.Scheme)
	}
	host := parsed.Hostname()
	if host == "" {
		return fmt.Errorf("%w: mind URL %q names no host", ErrNotConfigured, raw)
	}
	addrs, err := net.LookupHost(host)
	if err != nil {
		return fmt.Errorf("%w: mind host %q does not resolve: %v", ErrNotConfigured, host, err)
	}
	for _, a := range addrs {
		ip := net.ParseIP(a)
		if ip == nil || !ip.IsLoopback() {
			return fmt.Errorf("%w: mind host %q resolves to %s, which is NOT loopback. "+
				"The mind sees EVERYTHING this ghillie is told — every question, every answer, "+
				"the whole sitting — so it is not sent to a machine that is not yours. "+
				"Point -mind-url at your own model, or pass -mind-adopt-external to say that "+
				"far end is yours and you accept what it will be shown",
				ErrNotConfigured, host, a)
		}
	}
	return nil
}

// Open reaches the owner's mind: it validates the URL, detects which dialect
// answers there, settles the model, and returns a client ready to speak.
//
// THE ORDER IS DELIBERATE and each step refuses on its own terms. The URL is
// checked BEFORE anything is contacted, so an off-box endpoint is never
// reached — not even probed — while somebody works out whether it was allowed.
// Detection then tries ollama first, because that is what the quickstart
// installs, and falls back to the OpenAI-compatible route on the SAME URL.
func Open(ctx context.Context, cfg Config) (*Client, error) {
	if strings.TrimSpace(cfg.URL) == "" {
		return nil, fmt.Errorf("%w: mind URL is empty", ErrNotConfigured)
	}
	base := strings.TrimRight(strings.TrimSpace(cfg.URL), "/")

	// Validate before probing. Adopting first and asking afterwards is how an
	// endpoint gets trusted before anyone checked where it pointed.
	if !cfg.AdoptExternal {
		if err := requireLoopback(base); err != nil {
			return nil, err
		}
	}

	client := cfg.HTTPClient
	if client == nil {
		timeout := cfg.Timeout
		if timeout <= 0 {
			timeout = 2 * time.Minute
		}
		client = &http.Client{Timeout: timeout}
	}

	dialect, models, err := detect(ctx, client, base)
	if err != nil {
		return nil, err
	}

	model := strings.TrimSpace(cfg.Model)
	if model == "" {
		if len(models) == 0 {
			return nil, fmt.Errorf("%w: %s speaks %s but lists nothing to talk to — "+
				"pull a model (ollama pull llama3.2) or name one with -mind-model",
				ErrNoModel, base, dialect)
		}
		model = models[0]
	}
	return &Client{url: base, model: model, dialect: dialect, http: client}, nil
}

// detect asks the URL which API it speaks, ollama first, and returns the
// models it lists. A server that answers neither list is reported as no mind
// at all rather than assumed to be one of them.
func detect(ctx context.Context, client *http.Client, base string) (Dialect, []string, error) {
	if models, err := listOllama(ctx, client, base); err == nil {
		return DialectOllama, models, nil
	}
	if models, err := listOpenAI(ctx, client, base); err == nil {
		return DialectOpenAI, models, nil
	}
	return "", nil, fmt.Errorf("%w: nothing answers at %s. "+
		"He works perfectly well without one — the catalogue, installs, proofs and the facade "+
		"protocol are untouched. To give him a mind, run a model on this machine "+
		"(ollama serve) and point -mind-url at it", ErrNoMind, base)
}

// ollamaTags is the shape of ollama's /api/tags.
type ollamaTags struct {
	Models []struct {
		Name string `json:"name"`
	} `json:"models"`
}

// listOllama reports the models ollama serves at base.
func listOllama(ctx context.Context, client *http.Client, base string) ([]string, error) {
	var tags ollamaTags
	if err := getJSON(ctx, client, base+"/api/tags", &tags); err != nil {
		return nil, err
	}
	names := make([]string, 0, len(tags.Models))
	for _, m := range tags.Models {
		if m.Name != "" {
			names = append(names, m.Name)
		}
	}
	return names, nil
}

// openAIModels is the shape of the OpenAI-compatible /v1/models.
type openAIModels struct {
	Data []struct {
		ID string `json:"id"`
	} `json:"data"`
}

// listOpenAI reports the models an OpenAI-compatible server serves at base.
func listOpenAI(ctx context.Context, client *http.Client, base string) ([]string, error) {
	var list openAIModels
	if err := getJSON(ctx, client, base+"/v1/models", &list); err != nil {
		return nil, err
	}
	names := make([]string, 0, len(list.Data))
	for _, m := range list.Data {
		if m.ID != "" {
			names = append(names, m.ID)
		}
	}
	return names, nil
}

// getJSON performs one GET and decodes the body. The probe is short: detection
// must not make an owner wait on a port nothing is behind.
func getJSON(ctx context.Context, client *http.Client, endpoint string, into any) error {
	probe, cancel := context.WithTimeout(ctx, 3*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(probe, http.MethodGet, endpoint, nil)
	if err != nil {
		return fmt.Errorf("mind: build request for %s: %w", endpoint, err)
	}
	resp, err := client.Do(req)
	if err != nil {
		return fmt.Errorf("mind: %s: %w", endpoint, err)
	}
	defer func() {
		if cerr := resp.Body.Close(); cerr != nil {
			_ = cerr // the body is read; a close error does not un-read it
		}
	}()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("mind: %s answered %s", endpoint, resp.Status)
	}
	body, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		return fmt.Errorf("mind: read %s: %w", endpoint, err)
	}
	if err := json.Unmarshal(body, into); err != nil {
		return fmt.Errorf("mind: %s is not the JSON expected: %w", endpoint, err)
	}
	return nil
}

// Chat puts the conversation to the model and returns what it said.
//
// Streaming is deliberately off: both dialects answer whole in one body, the
// surface here is a console that prints a paragraph at a time, and a streaming
// decoder would be code carrying no benefit the owner can see. When a surface
// arrives that can show words as they land, this is where it goes.
func (c *Client) Chat(ctx context.Context, messages []Message) (string, error) {
	if len(messages) == 0 {
		return "", fmt.Errorf("%w: nothing was said to it", ErrChatFailed)
	}
	switch c.dialect {
	case DialectOllama:
		return c.chatOllama(ctx, messages)
	case DialectOpenAI:
		return c.chatOpenAI(ctx, messages)
	default:
		return "", fmt.Errorf("%w: unknown dialect %q", ErrChatFailed, c.dialect)
	}
}

// chatOllama speaks ollama's /api/chat.
func (c *Client) chatOllama(ctx context.Context, messages []Message) (string, error) {
	body := map[string]any{
		"model":    c.model,
		"messages": messages,
		"stream":   false,
	}
	var out struct {
		Message Message `json:"message"`
	}
	if err := c.postJSON(ctx, c.url+"/api/chat", body, &out); err != nil {
		return "", err
	}
	if strings.TrimSpace(out.Message.Content) == "" {
		return "", fmt.Errorf("%w: %s returned an empty message", ErrChatFailed, c.model)
	}
	return out.Message.Content, nil
}

// chatOpenAI speaks the OpenAI-compatible /v1/chat/completions.
func (c *Client) chatOpenAI(ctx context.Context, messages []Message) (string, error) {
	body := map[string]any{
		"model":    c.model,
		"messages": messages,
		"stream":   false,
	}
	var out struct {
		Choices []struct {
			Message Message `json:"message"`
		} `json:"choices"`
	}
	if err := c.postJSON(ctx, c.url+"/v1/chat/completions", body, &out); err != nil {
		return "", err
	}
	if len(out.Choices) == 0 || strings.TrimSpace(out.Choices[0].Message.Content) == "" {
		return "", fmt.Errorf("%w: %s returned no choices", ErrChatFailed, c.model)
	}
	return out.Choices[0].Message.Content, nil
}

// postJSON performs one POST of a JSON body and decodes the answer.
func (c *Client) postJSON(ctx context.Context, endpoint string, body any, into any) error {
	encoded, err := json.Marshal(body)
	if err != nil {
		return fmt.Errorf("%w: encode request: %w", ErrChatFailed, err)
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(encoded))
	if err != nil {
		return fmt.Errorf("%w: build request for %s: %w", ErrChatFailed, endpoint, err)
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := c.http.Do(req)
	if err != nil {
		return fmt.Errorf("%w: %s: %w", ErrChatFailed, endpoint, err)
	}
	defer func() {
		if cerr := resp.Body.Close(); cerr != nil {
			_ = cerr
		}
	}()
	raw, err := io.ReadAll(io.LimitReader(resp.Body, 8<<20))
	if err != nil {
		return fmt.Errorf("%w: read %s: %w", ErrChatFailed, endpoint, err)
	}
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("%w: %s answered %s: %s", ErrChatFailed, endpoint, resp.Status, tail(string(raw), 300))
	}
	if err := json.Unmarshal(raw, into); err != nil {
		return fmt.Errorf("%w: %s is not the JSON expected: %w", ErrChatFailed, endpoint, err)
	}
	return nil
}

// tail returns the last n bytes of s, marked when it was cut.
func tail(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return "…" + s[len(s)-n:]
}
