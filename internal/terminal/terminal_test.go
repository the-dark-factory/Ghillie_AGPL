package terminal

import (
	"context"
	"crypto/ed25519"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/tonygair/ghillie/internal/frame"
	"github.com/tonygair/ghillie/internal/gate"
	"github.com/tonygair/ghillie/internal/protocol"
)

// testLogger collects the terminal's narration so a test can assert on what the
// USER would have been shown, not merely on what was reported outward.
type testLogger struct {
	mu    sync.Mutex
	lines []string
}

func (l *testLogger) Printf(format string, v ...any) {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.lines = append(l.lines, strings.TrimSpace(fmt.Sprintf(format, v...)))
}

func (l *testLogger) text() string {
	l.mu.Lock()
	defer l.mu.Unlock()
	return strings.Join(l.lines, "\n")
}

// stubFacade is a minimal in-test facade: it hands out one batch of envelopes on
// the first poll, then 204s, and it keeps every outcome the terminal reports.
type stubFacade struct {
	mu       sync.Mutex
	batches  [][]protocol.Envelope
	next     int
	reported []protocol.Outcome
}

func (f *stubFacade) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if r.Method == http.MethodGet {
		// This stub is a poll-only facade; the notes fetch treats 404 as "a
		// facade without the channel", which is the pre-notes world these
		// tests were written in.
		http.NotFound(w, r)
		return
	}
	var req protocol.PollRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "bad body", http.StatusBadRequest)
		return
	}
	f.mu.Lock()
	f.reported = append(f.reported, req.Reports...)
	var batch []protocol.Envelope
	if f.next < len(f.batches) {
		batch = f.batches[f.next]
		f.next++
	}
	f.mu.Unlock()

	if len(batch) == 0 {
		w.WriteHeader(http.StatusNoContent)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(protocol.PollResponse{Instructions: batch}); err != nil {
		http.Error(w, "encode", http.StatusInternalServerError)
	}
}

func (f *stubFacade) outcomes() []protocol.Outcome {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]protocol.Outcome(nil), f.reported...)
}

// signer is a deterministic test key pair.
func signer(seed string) ed25519.PrivateKey {
	sum := sha256.Sum256([]byte(seed))
	return ed25519.NewKeyFromSeed(sum[:])
}

// envelope builds a signed instruction envelope with the given key.
func envelope(key ed25519.PrivateKey, instr frame.Instruction) protocol.Envelope {
	wire := instr.Marshal()
	return protocol.Envelope{
		Frame:     hex.EncodeToString(wire[:]),
		Signature: hex.EncodeToString(ed25519.Sign(key, wire[:])),
	}
}

// wantOutcome is one expected verdict in the tables below.
type wantOutcome struct {
	seq      uint64
	admitted bool
	reason   gate.Reason
}

// runTerminal drives a terminal against a stub facade until the batches are
// exhausted and the final reports have gone out.
func runTerminal(t *testing.T, batches [][]protocol.Envelope, key ed25519.PrivateKey, ceiling gate.Command, consent gate.Consent) (*stubFacade, *testLogger, string) {
	t.Helper()

	f := &stubFacade{batches: batches}
	srv := httptest.NewServer(f)
	t.Cleanup(srv.Close)

	dir := t.TempDir()
	logger := &testLogger{}
	term, err := New(Config{
		FacadeURL:     srv.URL,
		ClawID:        "test-claw",
		Ceiling:       ceiling,
		Consent:       StaticConsent(consent),
		QuarantineDir: filepath.Join(dir, "quarantine"),
		StateFile:     filepath.Join(dir, "state.json"),
		PollInterval:  time.Millisecond,
		MaxPolls:      len(batches) + 3,
	}, key.Public().(ed25519.PublicKey), logger)
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err := term.Run(ctx); err != nil {
		t.Fatalf("Run: %v", err)
	}
	return f, logger, dir
}

// TestAdmissionOutcomes is the end-to-end table: for each scripted instruction
// shape, the verdict and reason class the terminal must report.
func TestAdmissionOutcomes(t *testing.T) {
	good := signer("terminal-test-good")
	impostor := signer("terminal-test-impostor")

	tests := []struct {
		name    string
		ceiling gate.Command
		consent gate.Consent
		build   func() []protocol.Envelope
		want    []wantOutcome
	}{
		{
			name:    "delivery at the default ceiling is admitted",
			ceiling: gate.DefaultCeiling,
			consent: gate.NoConsent,
			build: func() []protocol.Envelope {
				return []protocol.Envelope{envelope(good, frame.Instruction{Command: uint8(gate.DeliverArtifact), Protocol: frame.ProtocolV0, ArtifactRef: 0xABCD, Version: 3, Seq: 1})}
			},
			want: []wantOutcome{{seq: 1, admitted: true}},
		},
		{
			name:    "install above the ceiling is refused",
			ceiling: gate.DefaultCeiling,
			consent: gate.FreshExplicit,
			build: func() []protocol.Envelope {
				return []protocol.Envelope{envelope(good, frame.Instruction{Command: uint8(gate.InstallArtifact), Protocol: frame.ProtocolV0, Seq: 1})}
			},
			want: []wantOutcome{{seq: 1, reason: gate.ReasonOverCeiling}},
		},
		{
			name:    "run local code above the ceiling is refused",
			ceiling: gate.DefaultCeiling,
			consent: gate.FreshExplicit,
			build: func() []protocol.Envelope {
				return []protocol.Envelope{envelope(good, frame.Instruction{Command: uint8(gate.RunLocalCode), Protocol: frame.ProtocolV0, Seq: 1})}
			},
			want: []wantOutcome{{seq: 1, reason: gate.ReasonOverCeiling}},
		},
		{
			name:    "spec upload is refused at the MAXIMUM ceiling with fresh consent",
			ceiling: gate.RunLocalCode,
			consent: gate.FreshExplicit,
			build: func() []protocol.Envelope {
				return []protocol.Envelope{envelope(good, frame.Instruction{Command: uint8(gate.RequestSpecUpload), Protocol: frame.ProtocolV0, Seq: 1})}
			},
			want: []wantOutcome{{seq: 1, reason: gate.ReasonLocalOnly}},
		},
		{
			name:    "install at a raised ceiling without fresh consent is refused",
			ceiling: gate.InstallArtifact,
			consent: gate.SessionOnly,
			build: func() []protocol.Envelope {
				return []protocol.Envelope{envelope(good, frame.Instruction{Command: uint8(gate.InstallArtifact), Protocol: frame.ProtocolV0, Seq: 1})}
			},
			want: []wantOutcome{{seq: 1, reason: gate.ReasonNoConsent}},
		},
		{
			name:    "a frame signed by an unpinned key is refused",
			ceiling: gate.DefaultCeiling,
			consent: gate.NoConsent,
			build: func() []protocol.Envelope {
				return []protocol.Envelope{envelope(impostor, frame.Instruction{Command: uint8(gate.DeliverArtifact), Protocol: frame.ProtocolV0, Seq: 1})}
			},
			want: []wantOutcome{{seq: 1, reason: gate.ReasonBadSignature}},
		},
		{
			name:    "a replayed frame is refused as stale",
			ceiling: gate.DefaultCeiling,
			consent: gate.NoConsent,
			build: func() []protocol.Envelope {
				first := envelope(good, frame.Instruction{Command: uint8(gate.DeliverArtifact), Protocol: frame.ProtocolV0, Seq: 4})
				return []protocol.Envelope{first, first}
			},
			want: []wantOutcome{{seq: 4, admitted: true}, {seq: 4, reason: gate.ReasonStaleSeq}},
		},
		{
			name:    "an older sequence after a newer one is refused as stale",
			ceiling: gate.DefaultCeiling,
			consent: gate.NoConsent,
			build: func() []protocol.Envelope {
				return []protocol.Envelope{
					envelope(good, frame.Instruction{Command: uint8(gate.DeliverArtifact), Protocol: frame.ProtocolV0, Seq: 9}),
					envelope(good, frame.Instruction{Command: uint8(gate.DeliverArtifact), Protocol: frame.ProtocolV0, Seq: 8}),
				}
			},
			want: []wantOutcome{{seq: 9, admitted: true}, {seq: 8, reason: gate.ReasonStaleSeq}},
		},
		{
			name:    "an unknown command byte is refused",
			ceiling: gate.RunLocalCode,
			consent: gate.FreshExplicit,
			build: func() []protocol.Envelope {
				return []protocol.Envelope{envelope(good, frame.Instruction{Command: 200, Protocol: frame.ProtocolV0, Seq: 1})}
			},
			want: []wantOutcome{{seq: 1, reason: gate.ReasonUnknownCommand}},
		},
		{
			name:    "an unsupported protocol version is refused",
			ceiling: gate.DefaultCeiling,
			consent: gate.NoConsent,
			build: func() []protocol.Envelope {
				return []protocol.Envelope{envelope(good, frame.Instruction{Command: uint8(gate.DeliverArtifact), Protocol: 99, Seq: 1})}
			},
			want: []wantOutcome{{seq: 1, reason: gate.ReasonBadProtocol}},
		},
		{
			name:    "a malformed frame is refused",
			ceiling: gate.DefaultCeiling,
			consent: gate.NoConsent,
			build: func() []protocol.Envelope {
				return []protocol.Envelope{{Frame: "not-hex", Signature: "00"}}
			},
			want: []wantOutcome{{seq: 0, reason: gate.ReasonMalformedFrame}},
		},
		{
			name:    "the whole demonstration sequence",
			ceiling: gate.DefaultCeiling,
			consent: gate.NoConsent,
			build: func() []protocol.Envelope {
				deliver := envelope(good, frame.Instruction{Command: uint8(gate.DeliverArtifact), Protocol: frame.ProtocolV0, Seq: 2})
				return []protocol.Envelope{
					envelope(good, frame.Instruction{Command: uint8(gate.ReportStatus), Protocol: frame.ProtocolV0, Seq: 1}),
					deliver,
					envelope(good, frame.Instruction{Command: uint8(gate.InstallArtifact), Protocol: frame.ProtocolV0, Seq: 3}),
					envelope(good, frame.Instruction{Command: uint8(gate.RunLocalCode), Protocol: frame.ProtocolV0, Seq: 4}),
					envelope(good, frame.Instruction{Command: uint8(gate.RequestSpecUpload), Protocol: frame.ProtocolV0, Seq: 5}),
					deliver,
					envelope(impostor, frame.Instruction{Command: uint8(gate.DeliverArtifact), Protocol: frame.ProtocolV0, Seq: 6}),
				}
			},
			want: []wantOutcome{
				{seq: 1, admitted: true},
				{seq: 2, admitted: true},
				{seq: 3, reason: gate.ReasonOverCeiling},
				{seq: 4, reason: gate.ReasonOverCeiling},
				{seq: 5, reason: gate.ReasonLocalOnly},
				{seq: 2, reason: gate.ReasonStaleSeq},
				{seq: 6, reason: gate.ReasonBadSignature},
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			f, _, _ := runTerminal(t, [][]protocol.Envelope{tt.build()}, good, tt.ceiling, tt.consent)
			got := f.outcomes()
			if len(got) != len(tt.want) {
				t.Fatalf("reported %d outcome(s), want %d: %+v", len(got), len(tt.want), got)
			}
			for i, w := range tt.want {
				if got[i].Seq != w.seq {
					t.Errorf("outcome %d: seq %d, want %d", i, got[i].Seq, w.seq)
				}
				if got[i].Admitted != w.admitted {
					t.Errorf("outcome %d (seq %d): admitted %v, want %v (note: %s)", i, w.seq, got[i].Admitted, w.admitted, got[i].Note)
				}
				if got[i].Reason != string(w.reason) {
					t.Errorf("outcome %d (seq %d): reason %q, want %q", i, w.seq, got[i].Reason, w.reason)
				}
			}
		})
	}
}

// TestRefusalIsReportedNotDropped states the property on its own: every refused
// instruction reaches BOTH the user's log and the facade's report.
func TestRefusalIsReportedNotDropped(t *testing.T) {
	good := signer("terminal-test-good")
	batch := []protocol.Envelope{
		envelope(good, frame.Instruction{Command: uint8(gate.InstallArtifact), Protocol: frame.ProtocolV0, Seq: 1}),
	}
	f, logger, _ := runTerminal(t, [][]protocol.Envelope{batch}, good, gate.DefaultCeiling, gate.NoConsent)

	if got := f.outcomes(); len(got) != 1 || got[0].Admitted || got[0].Reason != string(gate.ReasonOverCeiling) {
		t.Fatalf("facade did not receive the refusal: %+v", got)
	}
	text := logger.text()
	if !strings.Contains(text, "REFUSED") || !strings.Contains(text, string(gate.ReasonOverCeiling)) {
		t.Errorf("the user was not shown the refusal.\n%s", text)
	}
}

// TestNothingIsExecutedOrInstalled checks the two deliberate stubs: even with
// the ceiling at maximum and fresh consent, an admitted Install_Artifact or
// Run_Local_Code performs no act, and nothing lands in quarantine for them.
func TestNothingIsExecutedOrInstalled(t *testing.T) {
	good := signer("terminal-test-good")
	batch := []protocol.Envelope{
		envelope(good, frame.Instruction{Command: uint8(gate.InstallArtifact), Protocol: frame.ProtocolV0, Seq: 1}),
		envelope(good, frame.Instruction{Command: uint8(gate.RunLocalCode), Protocol: frame.ProtocolV0, Seq: 2}),
	}
	f, logger, dir := runTerminal(t, [][]protocol.Envelope{batch}, good, gate.RunLocalCode, gate.FreshExplicit)

	got := f.outcomes()
	if len(got) != 2 {
		t.Fatalf("want 2 outcomes, got %d", len(got))
	}
	for _, o := range got {
		if !o.Admitted {
			t.Errorf("seq %d: expected the gate to ADMIT at max ceiling with fresh consent, got refusal %q", o.Seq, o.Reason)
		}
		if !strings.Contains(o.Note, "NOTHING WAS") {
			t.Errorf("seq %d: note does not say nothing happened: %q", o.Seq, o.Note)
		}
	}
	entries, err := os.ReadDir(filepath.Join(dir, "quarantine"))
	if err == nil && len(entries) != 0 {
		t.Errorf("quarantine should be empty for install/run, got %d entries", len(entries))
	}
	if !strings.Contains(logger.text(), "ADMITTED") {
		t.Errorf("expected an ADMITTED line in the user's log")
	}
}

// TestDeliveryLandsInQuarantine checks that an admitted delivery writes a
// notice and nothing else.
func TestDeliveryLandsInQuarantine(t *testing.T) {
	good := signer("terminal-test-good")
	batch := []protocol.Envelope{
		envelope(good, frame.Instruction{Command: uint8(gate.DeliverArtifact), Protocol: frame.ProtocolV0, ArtifactRef: 0xDEADBEEF, Version: 4, Seq: 1}),
	}
	_, _, dir := runTerminal(t, [][]protocol.Envelope{batch}, good, gate.DefaultCeiling, gate.NoConsent)

	entries, err := os.ReadDir(filepath.Join(dir, "quarantine"))
	if err != nil {
		t.Fatalf("read quarantine: %v", err)
	}
	if len(entries) != 1 {
		t.Fatalf("want 1 quarantined notice, got %d", len(entries))
	}
	body, err := os.ReadFile(filepath.Join(dir, "quarantine", entries[0].Name()))
	if err != nil {
		t.Fatalf("read notice: %v", err)
	}
	var notice map[string]any
	if err := json.Unmarshal(body, &notice); err != nil {
		t.Fatalf("parse notice: %v", err)
	}
	if state, _ := notice["state"].(string); !strings.Contains(state, "QUARANTINED") {
		t.Errorf("notice does not record quarantine state: %v", notice["state"])
	}
	if entries[0].Type().Perm()&0o111 != 0 {
		t.Errorf("quarantined file is executable — it must never be")
	}
}

// TestStatePersistsAcrossRuns checks the durable edge of ledger 120: a sequence
// accepted in one run is stale in the next.
func TestStatePersistsAcrossRuns(t *testing.T) {
	good := signer("terminal-test-good")
	dir := t.TempDir()
	stateFile := filepath.Join(dir, "state.json")

	instr := frame.Instruction{Command: uint8(gate.DeliverArtifact), Protocol: frame.ProtocolV0, Seq: 42}
	env := envelope(good, instr)

	runOnce := func() []protocol.Outcome {
		f := &stubFacade{batches: [][]protocol.Envelope{{env}}}
		srv := httptest.NewServer(f)
		defer srv.Close()

		term, err := New(Config{
			FacadeURL:     srv.URL,
			ClawID:        "test-claw",
			Ceiling:       gate.DefaultCeiling,
			Consent:       StaticConsent(gate.NoConsent),
			QuarantineDir: filepath.Join(dir, "quarantine"),
			StateFile:     stateFile,
			PollInterval:  time.Millisecond,
			MaxPolls:      3,
		}, good.Public().(ed25519.PublicKey), &testLogger{})
		if err != nil {
			t.Fatalf("New: %v", err)
		}
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		if err := term.Run(ctx); err != nil {
			t.Fatalf("Run: %v", err)
		}
		return f.outcomes()
	}

	first := runOnce()
	if len(first) != 1 || !first[0].Admitted {
		t.Fatalf("first run should admit seq 42: %+v", first)
	}
	second := runOnce()
	if len(second) != 1 || second[0].Admitted || second[0].Reason != string(gate.ReasonStaleSeq) {
		t.Fatalf("second run should refuse the replay as stale: %+v", second)
	}
}

// stubAuth is an Authorizer that always says yes and counts invalidations, so a
// test can see whether a 401 dropped the session.
type stubAuth struct {
	mu          sync.Mutex
	invalidated int
}

func (a *stubAuth) Authorization(context.Context) (string, error) { return "Bearer stub", nil }

func (a *stubAuth) Invalidate() {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.invalidated++
}

func (a *stubAuth) invalidations() int {
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.invalidated
}

// TestPollQueuesSurviveErrorResponses is the queue-loss regression table: the
// outcome queue and the signed-submission queue drain ONLY when the facade
// actually accepted the poll. An earlier build cleared them before looking at
// the status, which silently discarded them on any non-401 error while logging
// success — a report the facade never received, marked as delivered. Every
// error status here must leave both queues exactly as they were.
func TestPollQueuesSurviveErrorResponses(t *testing.T) {
	good := signer("terminal-test-good")

	instructions := []protocol.Envelope{
		envelope(good, frame.Instruction{Command: uint8(gate.ReportStatus), Protocol: frame.ProtocolV0, Seq: 1}),
	}
	okBody, err := json.Marshal(protocol.PollResponse{Instructions: instructions})
	if err != nil {
		t.Fatalf("encode poll response: %v", err)
	}
	emptyBody, err := json.Marshal(protocol.PollResponse{Instructions: nil})
	if err != nil {
		t.Fatalf("encode empty poll response: %v", err)
	}

	tests := []struct {
		name             string
		status           int
		body             []byte
		wantErr          bool
		wantRetained     bool
		wantInstructions int
		wantInvalidated  int
	}{
		{"HTTP 500 keeps both queues", http.StatusInternalServerError, []byte("boom"), true, true, 0, 0},
		{"HTTP 502 keeps both queues", http.StatusBadGateway, []byte("bad gateway"), true, true, 0, 0},
		{"HTTP 400 keeps both queues", http.StatusBadRequest, []byte("bad request"), true, true, 0, 0},
		{"HTTP 404 keeps both queues", http.StatusNotFound, []byte("no such claw"), true, true, 0, 0},
		{"200 with instructions clears both queues", http.StatusOK, okBody, false, false, 1, 0},
		{"200 with an empty list clears both queues", http.StatusOK, emptyBody, false, false, 0, 0},
		{"204 clears both queues", http.StatusNoContent, nil, false, false, 0, 0},
		{"401 keeps both queues and drops the session", http.StatusUnauthorized, []byte("attestation required"), true, true, 0, 1},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if tt.status == http.StatusOK {
					w.Header().Set("Content-Type", "application/json")
				}
				w.WriteHeader(tt.status)
				if len(tt.body) > 0 {
					if _, werr := w.Write(tt.body); werr != nil {
						t.Errorf("write body: %v", werr)
					}
				}
			}))
			t.Cleanup(srv.Close)

			auth := &stubAuth{}
			key := signer("terminal-test-good").Public().(ed25519.PublicKey)
			term, err := New(Config{
				FacadeURL: srv.URL,
				ClawID:    "test-claw",
				Ceiling:   gate.DefaultCeiling,
				Auth:      auth,
			}, key, &testLogger{})
			if err != nil {
				t.Fatalf("New: %v", err)
			}

			// One queued outcome and one queued signed submission, both of
			// which the facade has not yet accepted.
			term.pending = []protocol.Outcome{{Seq: 7, CommandName: "Report_Status", Admitted: true, At: "t"}}
			term.answers = []protocol.Submission{{SubmissionID: "sub-1", ClawID: "test-claw", BriefID: "b", Signature: "00"}}

			got, err := term.poll(context.Background())
			if tt.wantErr && err == nil {
				t.Fatal("poll: want an error, got nil")
			}
			if !tt.wantErr && err != nil {
				t.Fatalf("poll: %v", err)
			}
			if len(got) != tt.wantInstructions {
				t.Errorf("poll returned %d instruction(s), want %d", len(got), tt.wantInstructions)
			}

			if tt.wantRetained {
				if len(term.pending) != 1 || len(term.answers) != 1 {
					t.Errorf("★ QUEUE LOSS: after HTTP %d, %d outcome(s) and %d submission(s) remain queued, want 1 and 1 — the facade never accepted them", tt.status, len(term.pending), len(term.answers))
				}
			} else {
				if len(term.pending) != 0 || len(term.answers) != 0 {
					t.Errorf("after HTTP %d the queues should have drained: %d outcome(s), %d submission(s) still queued", tt.status, len(term.pending), len(term.answers))
				}
			}
			if inv := auth.invalidations(); inv != tt.wantInvalidated {
				t.Errorf("session invalidated %d time(s), want %d", inv, tt.wantInvalidated)
			}
		})
	}
}

// TestNewRejectsBadKey covers the one construction-time refusal.
func TestNewRejectsBadKey(t *testing.T) {
	tests := []struct {
		name string
		key  ed25519.PublicKey
	}{
		{"nil", nil},
		{"too short", make([]byte, 16)},
		{"too long", make([]byte, 64)},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if _, err := New(Config{}, tt.key, &testLogger{}); err == nil {
				t.Fatal("New: want error for a bad facade key, got nil")
			}
		})
	}
}
