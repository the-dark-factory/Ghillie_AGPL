package mind

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// TestRequireLoopback is the structural half of the ruling: a downloaded
// ghillie thinks on its OWNER'S machine, enforced by construction rather than
// by remembering a flag.
func TestRequireLoopback(t *testing.T) {
	tests := []struct {
		name    string
		url     string
		wantErr bool
	}{
		{name: "loopback v4 — the ollama default", url: "http://127.0.0.1:11434", wantErr: false},
		{name: "loopback v6", url: "http://[::1]:11434", wantErr: false},
		{name: "localhost resolves to loopback", url: "http://localhost:11434", wantErr: false},
		{name: "loopback with a trailing path", url: "http://127.0.0.1:8080/inference", wantErr: false},
		{name: "https loopback", url: "https://127.0.0.1:11434", wantErr: false},
		{name: "public IP refused", url: "http://8.8.8.8:11434", wantErr: true},
		{name: "private LAN IP refused", url: "http://192.168.0.42:11434", wantErr: true},
		{name: "off-box host refused", url: "http://example.com", wantErr: true},
		{name: "a provider is still off-box", url: "https://api.example-ai.com/v1", wantErr: true},
		{name: "non-http scheme refused", url: "ftp://127.0.0.1:11434", wantErr: true},
		{name: "no host refused", url: "http://", wantErr: true},
		{name: "not a url refused", url: "://nonsense", wantErr: true},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			err := requireLoopback(tc.url)
			if tc.wantErr && err == nil {
				t.Fatalf("requireLoopback(%q) = nil, want refusal — the whole sitting could leave the machine", tc.url)
			}
			if !tc.wantErr && err != nil {
				t.Fatalf("requireLoopback(%q) = %v, want nil", tc.url, err)
			}
			if tc.wantErr && !errors.Is(err, ErrNotConfigured) {
				t.Errorf("error = %v, want it to wrap ErrNotConfigured", err)
			}
		})
	}
}

// TestOpenRefusesOffBoxBeforeContacting: an off-box URL must be refused
// WITHOUT being reached. Probing first and asking afterwards is how an endpoint
// gets handed the owner's conversation before anyone checked where it pointed.
func TestOpenRefusesOffBoxBeforeContacting(t *testing.T) {
	reached := false
	srv := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
		reached = true
	}))
	defer srv.Close()

	_, err := Open(context.Background(), Config{URL: "http://example.com:11434"})
	if err == nil {
		t.Fatal("off-box mind URL accepted")
	}
	if !errors.Is(err, ErrNotConfigured) {
		t.Errorf("error = %v, want ErrNotConfigured", err)
	}
	if reached {
		t.Error("the off-box URL was contacted before it was refused")
	}
	// The refusal must be sterner than the ears': it must say what the mind sees.
	if !strings.Contains(err.Error(), "EVERYTHING this ghillie is told") {
		t.Errorf("refusal does not say what the mind is shown: %v", err)
	}
}

// TestAdoptExternalIsTheOnlyWayOffBox: the flag exists and it is the ONLY
// thing that lets a non-loopback URL through.
func TestAdoptExternalIsTheOnlyWayOffBox(t *testing.T) {
	srv := ollamaServer(t, []string{"llama3.2:latest"}, "aye")
	defer srv.Close()

	tests := []struct {
		name          string
		adoptExternal bool
		wantOpen      bool
	}{
		{name: "off by default, refused", adoptExternal: false, wantOpen: false},
		{name: "on by the owner, allowed", adoptExternal: true, wantOpen: true},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			// httptest binds loopback, so point at it by a name that is not,
			// via a client that dials the test server regardless.
			cfg := Config{
				URL:           "http://example.com",
				AdoptExternal: tc.adoptExternal,
				HTTPClient:    redirectingClient(srv.URL),
			}
			_, err := Open(context.Background(), cfg)
			if tc.wantOpen && err != nil {
				t.Fatalf("Open with adopt-external = %v, want it to open", err)
			}
			if !tc.wantOpen && err == nil {
				t.Fatal("Open without adopt-external succeeded off-box")
			}
		})
	}
}

// TestDetectDialect: ollama is first-class, the OpenAI-compatible route is a
// fallback probe on the SAME URL, and a server that speaks neither is reported
// as no mind rather than assumed to be one of them.
func TestDetectDialect(t *testing.T) {
	tests := []struct {
		name        string
		ollamaTags  []string // nil = /api/tags is not served
		openAIList  []string // nil = /v1/models is not served
		wantDialect Dialect
		wantErr     error
	}{
		{
			name:        "ollama answers, ollama wins",
			ollamaTags:  []string{"llama3.2:latest"},
			wantDialect: DialectOllama,
		},
		{
			name:        "no ollama, OpenAI-compatible answers",
			openAIList:  []string{"Qwen/Qwen2.5-7B-Instruct"},
			wantDialect: DialectOpenAI,
		},
		{
			name:        "both answer, ollama is preferred",
			ollamaTags:  []string{"llama3.2:latest"},
			openAIList:  []string{"llama3.2"},
			wantDialect: DialectOllama,
		},
		{
			name:    "neither answers — no mind, no guessing",
			wantErr: ErrNoMind,
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			srv := dialectServer(t, tc.ollamaTags, tc.openAIList, "aye")
			defer srv.Close()

			c, err := Open(context.Background(), Config{URL: srv.URL})
			if tc.wantErr != nil {
				if !errors.Is(err, tc.wantErr) {
					t.Fatalf("error = %v, want %v", err, tc.wantErr)
				}
				return
			}
			if err != nil {
				t.Fatalf("Open = %v, want a client", err)
			}
			if c.Dialect() != tc.wantDialect {
				t.Errorf("dialect = %q, want %q", c.Dialect(), tc.wantDialect)
			}
			// Detection is stated honestly in the line the owner reads.
			if !strings.Contains(c.Speaking(), string(tc.wantDialect)) {
				t.Errorf("Speaking() = %q, does not name the detected dialect", c.Speaking())
			}
		})
	}
}

// TestModelAutoPick: an empty -mind-model asks the server what it serves and
// takes the first; a named model is used as named and never second-guessed.
func TestModelAutoPick(t *testing.T) {
	tests := []struct {
		name      string
		served    []string
		asked     string
		wantModel string
		wantErr   error
	}{
		{
			name:      "empty picks the first served",
			served:    []string{"llama3.2:latest", "mistral:7b"},
			asked:     "",
			wantModel: "llama3.2:latest",
		},
		{
			name:      "a named model is used as named",
			served:    []string{"llama3.2:latest", "mistral:7b"},
			asked:     "mistral:7b",
			wantModel: "mistral:7b",
		},
		{
			name:      "a named model the list does not show is still tried",
			served:    []string{"llama3.2:latest"},
			asked:     "something-else:8b",
			wantModel: "something-else:8b",
		},
		{
			name:      "surrounding space is not part of a model name",
			served:    []string{"llama3.2:latest"},
			asked:     "  mistral:7b  ",
			wantModel: "mistral:7b",
		},
		{
			name:    "a server serving nothing cannot be auto-picked from",
			served:  []string{},
			asked:   "",
			wantErr: ErrNoModel,
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			srv := ollamaServer(t, tc.served, "aye")
			defer srv.Close()

			c, err := Open(context.Background(), Config{URL: srv.URL, Model: tc.asked})
			if tc.wantErr != nil {
				if !errors.Is(err, tc.wantErr) {
					t.Fatalf("error = %v, want %v", err, tc.wantErr)
				}
				return
			}
			if err != nil {
				t.Fatalf("Open = %v, want a client", err)
			}
			if c.Model() != tc.wantModel {
				t.Errorf("Model() = %q, want %q", c.Model(), tc.wantModel)
			}
			if !strings.Contains(c.Speaking(), tc.wantModel) {
				t.Errorf("Speaking() = %q, does not name the model that answers", c.Speaking())
			}
		})
	}
}

// TestChatBothDialects: a reached mind answers, whichever wire it speaks.
func TestChatBothDialects(t *testing.T) {
	tests := []struct {
		name       string
		ollamaTags []string
		openAIList []string
		want       string
	}{
		{name: "ollama /api/chat", ollamaTags: []string{"llama3.2:latest"}, want: "aye, that'll do"},
		{name: "OpenAI-compatible", openAIList: []string{"local-model"}, want: "aye, that'll do"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			srv := dialectServer(t, tc.ollamaTags, tc.openAIList, tc.want)
			defer srv.Close()

			c, err := Open(context.Background(), Config{URL: srv.URL})
			if err != nil {
				t.Fatalf("Open = %v", err)
			}
			got, err := c.Chat(context.Background(), []Message{{Role: "user", Content: "aye?"}})
			if err != nil {
				t.Fatalf("Chat = %v", err)
			}
			if got != tc.want {
				t.Errorf("Chat = %q, want %q", got, tc.want)
			}
		})
	}
}

// TestNoMindIsHonest: when nothing answers, the words the owner reads say so
// plainly, say what still works, and never name anybody else's inference.
func TestNoMindIsHonest(t *testing.T) {
	// A port with nothing behind it. Loopback, so the refusal is about
	// absence, not about location.
	const dead = "http://127.0.0.1:1"

	_, err := Open(context.Background(), Config{URL: dead})
	if !errors.Is(err, ErrNoMind) {
		t.Fatalf("error = %v, want ErrNoMind", err)
	}

	tests := []struct {
		name    string
		text    string
		wantAll []string
		wantNo  []string
	}{
		{
			name: "the Open error",
			text: err.Error(),
			wantAll: []string{
				"nothing answers",
				"works perfectly well without one",
				"ollama",
			},
			wantNo: []string{"facade.", "collective", "api.", "cloud"},
		},
		{
			name: "the surface notice",
			text: NoMindNotice(dead),
			wantAll: []string{
				"no mind is set on this machine",
				"-mind-url",
				"catalogue",
				"proofs",
				"facade protocol",
				"leaves this machine",
			},
			wantNo: []string{"collective", "cloud", "https://", "API key", "sign in"},
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			for _, want := range tc.wantAll {
				if !strings.Contains(tc.text, want) {
					t.Errorf("text does not mention %q:\n%s", want, tc.text)
				}
			}
			for _, no := range tc.wantNo {
				if strings.Contains(tc.text, no) {
					t.Errorf("text offers %q — no mind must never point at somebody else's inference:\n%s", no, tc.text)
				}
			}
		})
	}
}

// TestEmptyURLIsNotConfigured guards the degenerate case: an owner who cleared
// the flag gets a configuration refusal, not a nil-pointer panic.
func TestEmptyURLIsNotConfigured(t *testing.T) {
	for _, raw := range []string{"", "   "} {
		if _, err := Open(context.Background(), Config{URL: raw}); !errors.Is(err, ErrNotConfigured) {
			t.Errorf("Open(%q) = %v, want ErrNotConfigured", raw, err)
		}
	}
}

// --- test servers -----------------------------------------------------------

// dialectServer serves /api/tags when ollamaTags is non-nil and /v1/models
// when openAIList is non-nil, so a test can present either wire, both, or
// neither. reply is what both chat routes answer.
func dialectServer(t *testing.T, ollamaTags, openAIList []string, reply string) *httptest.Server {
	t.Helper()
	mux := http.NewServeMux()
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		http.NotFound(w, r)
	})
	if ollamaTags != nil {
		mux.HandleFunc("/api/tags", func(w http.ResponseWriter, _ *http.Request) {
			body := ollamaTags2{}
			for _, n := range ollamaTags {
				body.Models = append(body.Models, struct {
					Name string `json:"name"`
				}{Name: n})
			}
			writeJSON(t, w, body)
		})
		mux.HandleFunc("/api/chat", func(w http.ResponseWriter, _ *http.Request) {
			writeJSON(t, w, map[string]any{"message": Message{Role: "assistant", Content: reply}})
		})
	}
	if openAIList != nil {
		mux.HandleFunc("/v1/models", func(w http.ResponseWriter, _ *http.Request) {
			data := make([]map[string]string, 0, len(openAIList))
			for _, id := range openAIList {
				data = append(data, map[string]string{"id": id})
			}
			writeJSON(t, w, map[string]any{"data": data})
		})
		mux.HandleFunc("/v1/chat/completions", func(w http.ResponseWriter, _ *http.Request) {
			writeJSON(t, w, map[string]any{
				"choices": []map[string]any{{"message": Message{Role: "assistant", Content: reply}}},
			})
		})
	}
	return httptest.NewServer(mux)
}

// ollamaTags2 mirrors ollamaTags for the test writer's side of the wire.
type ollamaTags2 struct {
	Models []struct {
		Name string `json:"name"`
	} `json:"models"`
}

// ollamaServer is dialectServer with only the ollama wire.
func ollamaServer(t *testing.T, models []string, reply string) *httptest.Server {
	t.Helper()
	if models == nil {
		models = []string{}
	}
	return dialectServer(t, models, nil, reply)
}

func writeJSON(t *testing.T, w http.ResponseWriter, v any) {
	t.Helper()
	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(v); err != nil {
		t.Errorf("test server encode: %v", err)
	}
}

// redirectingClient dials the test server no matter what host the URL names,
// so a test can present an off-box HOSTNAME to code that must refuse it.
func redirectingClient(target string) *http.Client {
	return &http.Client{Transport: rewriteTransport{target: target}}
}

type rewriteTransport struct{ target string }

func (rt rewriteTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	rewritten := req.Clone(req.Context())
	base := strings.TrimPrefix(rt.target, "http://")
	rewritten.URL.Scheme = "http"
	rewritten.URL.Host = base
	rewritten.Host = base
	resp, err := http.DefaultTransport.RoundTrip(rewritten)
	if err != nil {
		return nil, err //nolint:wrapcheck // a test transport passes the transport's own error through
	}
	return resp, nil
}
