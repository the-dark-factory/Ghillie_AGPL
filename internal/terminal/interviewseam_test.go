package terminal

import (
	"context"
	"crypto/ed25519"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/tonygair/ghillie/internal/brief"
	"github.com/tonygair/ghillie/internal/credit"
	"github.com/tonygair/ghillie/internal/frame"
	"github.com/tonygair/ghillie/internal/gate"
	"github.com/tonygair/ghillie/internal/identity"
	"github.com/tonygair/ghillie/internal/protocol"
)

// appleSecret is the purchaser reference used throughout. It is distinctive so
// that a leak into any log, report or file is unmistakable.
const appleSecret = "apple-acct-424242-PURCHASER-PII"

// briefRef is the ArtifactRef the scripted delivery carries, and therefore the
// brief id the body must echo back.
const briefRef uint64 = 0xDEADBEEFCAFEF00D

// briefRefHex is briefRef as the claw formats it when checking the body against
// the signed frame.
const briefRefHex = "deadbeefcafef00d"

// v1Facade is a stub facade with the whole v1 surface: poll, brief, credit,
// specs. Everything is scriptable so each failure can be exercised on its own.
type v1Facade struct {
	mu sync.Mutex

	batches []([]protocol.Envelope)
	next    int

	reported    []protocol.Outcome
	submissions []protocol.Submission
	polled      []protocol.PollRequest

	// briefBody, when set, is served verbatim for a brief GET. Setting it to a
	// body carrying a conduct field is how the conduct wall is exercised
	// end-to-end rather than only in the decoder's unit test.
	briefBody string

	creditSufficient bool
	creditNote       string
	creditBalance    int64

	requireAuth bool
	// validToken, when set, is the ONLY Authorization value accepted. It exists
	// so a stale session can be played, which is the case a 401 is really for.
	validToken string
	authSeen   []string
}

func (f *v1Facade) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	presented := r.Header.Get("Authorization")
	f.mu.Lock()
	f.authSeen = append(f.authSeen, presented)
	requireAuth, valid := f.requireAuth, f.validToken
	f.mu.Unlock()

	if requireAuth && (presented == "" || (valid != "" && presented != valid)) {
		http.Error(w, "poll requires attestation", http.StatusUnauthorized)
		return
	}

	switch {
	case strings.HasSuffix(r.URL.Path, "/poll"):
		f.servePoll(w, r)
	case strings.HasPrefix(r.URL.Path, "/briefs/"):
		f.serveBrief(w, r)
	case strings.HasSuffix(r.URL.Path, "/credit"):
		f.serveCredit(w)
	case r.URL.Path == protocol.SpecsPath:
		f.serveSpecs(w, r)
	default:
		http.NotFound(w, r)
	}
}

func (f *v1Facade) servePoll(w http.ResponseWriter, r *http.Request) {
	var req protocol.PollRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "bad body", http.StatusBadRequest)
		return
	}
	f.mu.Lock()
	f.polled = append(f.polled, req)
	f.reported = append(f.reported, req.Reports...)
	f.submissions = append(f.submissions, req.Answers...)
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

func (f *v1Facade) serveBrief(w http.ResponseWriter, r *http.Request) {
	f.mu.Lock()
	body := f.briefBody
	f.mu.Unlock()
	if body == "" {
		id := strings.TrimPrefix(r.URL.Path, "/briefs/")
		version := r.URL.Query().Get("version")
		body = fmt.Sprintf(`{"brief_id":%q,"version":%s,"items":[
		  {"id":1,"want":"What the software is actually for."},
		  {"id":2,"want":"Who uses it, and what they are in the middle of doing."},
		  {"id":3,"want":"What it has to talk to."}]}`, id, version)
	}
	w.Header().Set("Content-Type", "application/json")
	if _, err := w.Write([]byte(body)); err != nil {
		http.Error(w, "write", http.StatusInternalServerError)
	}
}

func (f *v1Facade) serveCredit(w http.ResponseWriter) {
	f.mu.Lock()
	sufficient, note, balance := f.creditSufficient, f.creditNote, f.creditBalance
	f.mu.Unlock()

	status := http.StatusOK
	if !sufficient {
		status = http.StatusPaymentRequired
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	if err := json.NewEncoder(w).Encode(credit.Decision{Sufficient: sufficient, Note: note, Balance: balance}); err != nil {
		return
	}
}

func (f *v1Facade) serveSpecs(w http.ResponseWriter, r *http.Request) {
	var sub protocol.Submission
	if err := json.NewDecoder(r.Body).Decode(&sub); err != nil {
		http.Error(w, "bad submission", http.StatusBadRequest)
		return
	}
	f.mu.Lock()
	f.submissions = append(f.submissions, sub)
	f.mu.Unlock()
	w.WriteHeader(http.StatusOK)
}

func (f *v1Facade) outcomes() []protocol.Outcome {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]protocol.Outcome(nil), f.reported...)
}

func (f *v1Facade) got() []protocol.Submission {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]protocol.Submission(nil), f.submissions...)
}

// scriptedInterview stands in for the real interview front end, so the terminal
// seam can be tested without a keyboard. It records the brief it was handed.
type scriptedInterview struct {
	answers []protocol.Answer
	err     error
	saw     *brief.Brief
}

func (s *scriptedInterview) Conduct(_ context.Context, b *brief.Brief) ([]protocol.Answer, error) {
	s.saw = b
	if s.err != nil {
		return nil, s.err
	}
	return s.answers, nil
}

// fixedSigner signs with a device key the test also holds, so signatures can be
// verified rather than merely counted.
type fixedSigner struct{ key ed25519.PrivateKey }

func (f fixedSigner) Sign(message []byte) string {
	return fmt.Sprintf("%x", ed25519.Sign(f.key, message))
}

// v1Run drives a terminal with the whole v1 configuration.
func v1Run(t *testing.T, f *v1Facade, cfg func(*Config)) (*testLogger, string) {
	t.Helper()

	srv := httptest.NewServer(f)
	t.Cleanup(srv.Close)

	dir := t.TempDir()
	logger := &testLogger{}
	deviceKey := signer("v1-device-key")

	c := Config{
		FacadeURL:     srv.URL,
		ClawID:        "test-claw",
		Ceiling:       gate.DefaultCeiling,
		Consent:       StaticConsent(gate.NoConsent),
		QuarantineDir: filepath.Join(dir, "quarantine"),
		StateFile:     filepath.Join(dir, "state.json"),
		PollInterval:  time.Millisecond,
		MaxPolls:      len(f.batches) + 3,
		Identity: identity.Binding{
			Claw:  identity.ClawID("test-claw"),
			Owner: identity.OwnerID("acme-it"),
			User:  identity.UserID("tony"),
			Apple: identity.NewAppleAccountRef(appleSecret),
		},
		UserStanding: gate.InGoodStanding,
		Signer:       fixedSigner{key: deviceKey},
	}
	cfg(&c)
	// The credit authority attests with the SAME session the poll uses — asking
	// about credit is not a special door. It is built after cfg so that a test
	// setting an Authorizer gets one that authenticates too.
	c.Credit = credit.NewHTTPAuthority(srv.URL, "test-claw", c.Auth, nil)

	term, err := New(c, signer("terminal-test-good").Public().(ed25519.PublicKey), logger)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err := term.Run(ctx); err != nil {
		t.Fatalf("Run: %v", err)
	}
	return logger, dir
}

// deliveryBatch is one signed Deliver_Artifact naming the brief.
func deliveryBatch(seq uint64) []protocol.Envelope {
	good := signer("terminal-test-good")
	return []protocol.Envelope{envelope(good, frame.Instruction{
		Command:     uint8(gate.DeliverArtifact),
		Protocol:    frame.ProtocolV0,
		ArtifactRef: briefRef,
		Version:     4,
		Seq:         seq,
	})}
}

// ★ TestEndToEndInterviewLoop is the whole v1 loop: brief delivered as a signed
// Deliver_Artifact → fetched by an outward GET → conducted → answers queued →
// submitted, device-signed.
//
// The interrupted item is asserted here as well as in the interview package,
// because what matters is what reaches the FACTORY: an item that was cut off
// must arrive carrying its state and NOT carrying a claim to be answered.
func TestEndToEndInterviewLoop(t *testing.T) {
	interviewer := &scriptedInterview{answers: []protocol.Answer{
		{ItemID: 1, State: "Answered", Answered: true, Text: "Booking the vans.", Attempts: 1, At: "t"},
		{ItemID: 2, State: "Interrupted", Answered: false, CutShort: true, Text: "hang on — three depots", Attempts: 1, At: "t"},
		{ItemID: 3, State: "Asked", Answered: false, Text: "How do you score this?", Attempts: 1, At: "t"},
	}}
	f := &v1Facade{
		batches:          [][]protocol.Envelope{deliveryBatch(1)},
		creditSufficient: true,
		creditNote:       "the factory has credit for this",
		creditBalance:    250,
	}

	logger, dir := v1Run(t, f, func(c *Config) { c.Interview = interviewer })

	// The brief that was handed to the interview is the one the SIGNED frame
	// named.
	if interviewer.saw == nil {
		t.Fatal("no brief reached the interview — the outward fetch did not happen")
	}
	if interviewer.saw.BriefID != briefRefHex || interviewer.saw.Version != 4 {
		t.Errorf("the fetched brief is %s/%d, want %s/4", interviewer.saw.BriefID, interviewer.saw.Version, briefRefHex)
	}
	if len(interviewer.saw.Items) != 3 {
		t.Errorf("the brief carries %d items, want 3", len(interviewer.saw.Items))
	}

	// One signed submission reached the facade.
	subs := f.got()
	if len(subs) != 1 {
		t.Fatalf("the facade received %d submission(s), want 1", len(subs))
	}
	sub := subs[0]
	if sub.BriefID != briefRefHex || sub.Version != 4 || sub.ClawID != "test-claw" {
		t.Errorf("submission identifies the wrong thing: %+v", sub)
	}
	if len(sub.Answers) != 3 {
		t.Fatalf("submission carries %d answer(s), want 3", len(sub.Answers))
	}

	// ★ The signature verifies against the DEVICE key over SigningBytes.
	raw := make([]byte, 0)
	if _, err := fmt.Sscanf(sub.Signature, "%x", &raw); err != nil {
		t.Fatalf("signature is not hex: %v", err)
	}
	devicePub, ok := signer("v1-device-key").Public().(ed25519.PublicKey)
	if !ok {
		t.Fatal("device key has no public half")
	}
	if !ed25519.Verify(devicePub, sub.SigningBytes(), raw) {
		t.Error("the submission signature does not verify against the device key")
	}

	// ★ THE INTERRUPTED ITEM ARRIVES AS INTERRUPTED, NEVER AS ANSWERED.
	var interrupted *protocol.Answer
	for i := range sub.Answers {
		if sub.Answers[i].ItemID == 2 {
			interrupted = &sub.Answers[i]
		}
	}
	if interrupted == nil {
		t.Fatal("the interrupted item did not reach the factory at all")
	}
	if interrupted.Answered {
		t.Error("★ the interrupted item reached the factory marked ANSWERED")
	}
	if interrupted.State != "Interrupted" || !interrupted.CutShort {
		t.Errorf("the interrupted item lost its state on the way: %+v", interrupted)
	}

	// The submission was reported as an outcome too — nothing happens silently.
	var reportedSubmission bool
	for _, o := range f.outcomes() {
		if o.CommandName == "submission" && strings.Contains(o.Note, sub.SubmissionID) {
			reportedSubmission = true
		}
	}
	if !reportedSubmission {
		t.Error("the submission was not reported as an outcome")
	}

	text := logger.text()
	if !strings.Contains(text, "SUBMITTED") {
		t.Errorf("the user was not shown the submission:\n%s", text)
	}
	if !strings.Contains(text, "conduct fields: NONE POSSIBLE") {
		t.Errorf("the log does not record that the brief could carry no conduct:\n%s", text)
	}
	assertNoAppleLeak(t, text, dir)
}

// ★ TestConductWallEndToEnd: a brief carrying a conduct field is refused at
// parse, the interview never runs, and the refusal is REPORTED.
func TestConductWallEndToEnd(t *testing.T) {
	tests := []struct {
		name  string
		field string
		body  string
	}{
		{
			name:  "wait_time_ms",
			field: "wait_time_ms",
			body:  `{"brief_id":"` + briefRefHex + `","version":4,"wait_time_ms":800,"items":[{"id":1,"want":"x"}]}`,
		},
		{
			name:  "may_interrupt",
			field: "may_interrupt",
			body:  `{"brief_id":"` + briefRefHex + `","version":4,"may_interrupt":true,"items":[{"id":1,"want":"x"}]}`,
		},
		{
			name:  "explain_mechanism",
			field: "explain_mechanism",
			body:  `{"brief_id":"` + briefRefHex + `","version":4,"explain_mechanism":true,"items":[{"id":1,"want":"x"}]}`,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			interviewer := &scriptedInterview{}
			f := &v1Facade{
				batches:          [][]protocol.Envelope{deliveryBatch(1)},
				briefBody:        tt.body,
				creditSufficient: true,
				creditBalance:    250,
			}
			logger, dir := v1Run(t, f, func(c *Config) { c.Interview = interviewer })

			if interviewer.saw != nil {
				t.Fatal("★ THE INTERVIEW RAN on a brief carrying conduct — the wall did not hold")
			}
			if len(f.got()) != 0 {
				t.Error("answers were submitted for a refused brief")
			}

			// The refusal is REPORTED, not swallowed.
			var reported bool
			for _, o := range f.outcomes() {
				if strings.Contains(o.Note, "CONDUCT DELIVERED OVER THE WIRE") && strings.Contains(o.Note, tt.field) {
					reported = true
				}
			}
			if !reported {
				t.Errorf("the conduct refusal was not reported to the facade: %+v", f.outcomes())
			}

			text := logger.text()
			if !strings.Contains(text, "CONDUCT WALL") {
				t.Errorf("the user was not shown the conduct refusal:\n%s", text)
			}
			if !strings.Contains(text, "refused") {
				t.Errorf("the log does not say the brief was refused:\n%s", text)
			}
			assertNoAppleLeak(t, text, dir)
		})
	}
}

// ★ TestCreditCourtesyNeverAuthorises: with the facade reporting no credit, the
// interview is refused legibly — even though the local courtesy figure is
// generous and the gate ADMITTED the instruction.
func TestCreditCourtesyNeverAuthorises(t *testing.T) {
	interviewer := &scriptedInterview{answers: []protocol.Answer{{ItemID: 1, State: "Answered", Answered: true}}}
	f := &v1Facade{
		batches:          [][]protocol.Envelope{deliveryBatch(1)},
		creditSufficient: false,
		creditNote:       "there is no credit on this account for an interview",
		creditBalance:    0,
	}
	logger, dir := v1Run(t, f, func(c *Config) { c.Interview = interviewer })

	if interviewer.saw != nil {
		t.Fatal("★ the interview ran with the factory reporting no credit")
	}
	if len(f.got()) != 0 {
		t.Error("answers were submitted for an act the factory refused")
	}

	text := logger.text()
	if !strings.Contains(text, "REFUSED") || !strings.Contains(text, "insufficient credit") {
		t.Errorf("the refusal was not legible to the user:\n%s", text)
	}
	if !strings.Contains(text, "there is no credit on this account") {
		t.Errorf("the facade's own explanation did not reach the user:\n%s", text)
	}
	if !strings.Contains(text, "never authorises") {
		t.Errorf("the log does not state that a local balance authorises nothing:\n%s", text)
	}

	// And the refusal reached the facade as an outcome.
	var reported bool
	for _, o := range f.outcomes() {
		if strings.Contains(o.Note, "insufficient credit") {
			reported = true
		}
	}
	if !reported {
		t.Errorf("the credit refusal was not reported: %+v", f.outcomes())
	}
	assertNoAppleLeak(t, text, dir)
}

// ★ TestARevokedUserMayNotBeInterviewed: ledger 115 bites, and nothing from the
// revoked person is needed for it to bite.
func TestARevokedUserMayNotBeInterviewed(t *testing.T) {
	interviewer := &scriptedInterview{answers: []protocol.Answer{{ItemID: 1, State: "Answered", Answered: true}}}
	f := &v1Facade{
		batches:          [][]protocol.Envelope{deliveryBatch(1)},
		creditSufficient: true,
		creditBalance:    250,
	}
	logger, dir := v1Run(t, f, func(c *Config) {
		c.Interview = interviewer
		c.UserStanding = gate.RevokedStanding
	})

	if interviewer.saw != nil {
		t.Fatal("★ a revoked user was interviewed — May_Act was not consulted")
	}
	text := logger.text()
	if !strings.Contains(text, "REFUSED") || !strings.Contains(text, "Revoked") {
		t.Errorf("the revocation refusal was not shown to the user:\n%s", text)
	}
	if !strings.Contains(text, "ledger 115") {
		t.Errorf("the refusal does not name the core that decided it:\n%s", text)
	}
	assertNoAppleLeak(t, text, dir)
}

// TestABriefThatIsNotTheOneSignedFor checks the tie between the unsigned GET
// and the signed frame. A facade that signs for one brief and serves another is
// caught.
func TestABriefThatIsNotTheOneSignedFor(t *testing.T) {
	tests := []struct {
		name string
		body string
	}{
		{
			name: "a different brief id",
			body: `{"brief_id":"0000000000000001","version":4,"items":[{"id":1,"want":"x"}]}`,
		},
		{
			name: "a different revision",
			body: `{"brief_id":"` + briefRefHex + `","version":99,"items":[{"id":1,"want":"x"}]}`,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			interviewer := &scriptedInterview{}
			f := &v1Facade{
				batches:          [][]protocol.Envelope{deliveryBatch(1)},
				briefBody:        tt.body,
				creditSufficient: true,
				creditBalance:    250,
			}
			_, _ = v1Run(t, f, func(c *Config) { c.Interview = interviewer })

			if interviewer.saw != nil {
				t.Fatal("★ a substituted brief was conducted — the signed frame's authority did not reach the body")
			}
			var reported bool
			for _, o := range f.outcomes() {
				if strings.Contains(o.Note, "not the one the signed frame named") {
					reported = true
				}
			}
			if !reported {
				t.Errorf("the mismatch was not reported: %+v", f.outcomes())
			}
		})
	}
}

// TestTheAuthorizationHeaderIsSent covers the gap v0 had: against a door that
// wants attestation, the terminal must present a header rather than 401 on
// every poll.
func TestTheAuthorizationHeaderIsSent(t *testing.T) {
	f := &v1Facade{
		batches:          [][]protocol.Envelope{deliveryBatch(1)},
		requireAuth:      true,
		creditSufficient: true,
		creditBalance:    250,
	}
	interviewer := &scriptedInterview{answers: []protocol.Answer{{ItemID: 1, State: "Answered", Answered: true}}}
	_, _ = v1Run(t, f, func(c *Config) {
		c.Interview = interviewer
		c.Auth = stubAuthorizer{token: "Bearer test-session"}
	})

	f.mu.Lock()
	seen := append([]string(nil), f.authSeen...)
	f.mu.Unlock()

	if len(seen) == 0 {
		t.Fatal("the facade saw no requests at all")
	}
	for i, got := range seen {
		if got != "Bearer test-session" {
			t.Errorf("request %d carried Authorization %q, want the attested bearer", i, got)
		}
	}
	if interviewer.saw == nil {
		t.Error("the interview did not run against an authenticating door")
	}
}

// TestAn401InvalidatesTheSession checks that a 401 drops the session rather
// than being mistaken for an empty poll.
func TestAn401InvalidatesTheSession(t *testing.T) {
	// The facade honours one token; the claw presents a stale one. That is what
	// a 401 actually means, and it is not the same as an empty poll.
	f := &v1Facade{requireAuth: true, validToken: "Bearer fresh", batches: [][]protocol.Envelope{deliveryBatch(1)}}
	auth := &countingAuthorizer{}
	logger, _ := v1Run(t, f, func(c *Config) { c.Auth = auth })

	if auth.invalidated == 0 {
		t.Error("a 401 did not invalidate the session")
	}
	if !strings.Contains(logger.text(), "401") {
		t.Errorf("the 401 was not reported to the user:\n%s", logger.text())
	}
	if strings.Contains(logger.text(), "consecutive empty polls") {
		t.Error("a 401 was counted as an empty poll — unauthenticated and nothing-for-you must not be confused")
	}
}

// TestAnUnsubmittedAnswerRidesTheNextPoll checks the queue's promise: when the
// direct /specs post cannot land, nothing is lost.
func TestAnUnsubmittedAnswerRidesTheNextPoll(t *testing.T) {
	f := &v1Facade{
		batches:          [][]protocol.Envelope{deliveryBatch(1)},
		creditSufficient: true,
		creditBalance:    250,
	}
	// A facade whose /specs door is shut, but whose poll works.
	shut := &shutSpecsFacade{v1Facade: f}
	srv := httptest.NewServer(shut)
	t.Cleanup(srv.Close)

	dir := t.TempDir()
	logger := &testLogger{}
	interviewer := &scriptedInterview{answers: []protocol.Answer{{ItemID: 1, State: "Answered", Answered: true, At: "t"}}}

	term, err := New(Config{
		FacadeURL:     srv.URL,
		ClawID:        "test-claw",
		Ceiling:       gate.DefaultCeiling,
		Consent:       StaticConsent(gate.NoConsent),
		QuarantineDir: filepath.Join(dir, "quarantine"),
		StateFile:     filepath.Join(dir, "state.json"),
		PollInterval:  time.Millisecond,
		MaxPolls:      4,
		UserStanding:  gate.InGoodStanding,
		Interview:     interviewer,
		Signer:        fixedSigner{key: signer("v1-device-key")},
		Credit:        credit.NewHTTPAuthority(srv.URL, "test-claw", nil, nil),
	}, signer("terminal-test-good").Public().(ed25519.PublicKey), logger)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err := term.Run(ctx); err != nil {
		t.Fatalf("Run: %v", err)
	}

	f.mu.Lock()
	polls := append([]protocol.PollRequest(nil), f.polled...)
	f.mu.Unlock()

	var carried bool
	for _, p := range polls {
		if len(p.Answers) > 0 {
			carried = true
		}
	}
	if !carried {
		t.Error("the submission was lost: /specs was shut and the poll carried nothing")
	}
	if !strings.Contains(logger.text(), "kept queued") {
		t.Errorf("the log does not say the submission was kept:\n%s", logger.text())
	}
}

// shutSpecsFacade is a v1Facade whose /specs door refuses everything.
type shutSpecsFacade struct{ *v1Facade }

func (s *shutSpecsFacade) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if r.URL.Path == protocol.SpecsPath {
		http.Error(w, "no receiver yet", http.StatusNotFound)
		return
	}
	s.v1Facade.ServeHTTP(w, r)
}

// stubAuthorizer always presents the same header.
type stubAuthorizer struct{ token string }

func (s stubAuthorizer) Authorization(context.Context) (string, error) { return s.token, nil }
func (s stubAuthorizer) Invalidate()                                   {}

// countingAuthorizer counts invalidations.
type countingAuthorizer struct {
	mu          sync.Mutex
	invalidated int
}

func (c *countingAuthorizer) Authorization(context.Context) (string, error) {
	return "Bearer stale", nil
}

func (c *countingAuthorizer) Invalidate() {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.invalidated++
}

// TestConsentIsAskedPerActFromTheLocalSource checks the ConsentSource seam: the
// gate path asks, per act, rather than reading a stored value.
func TestConsentIsAskedPerActFromTheLocalSource(t *testing.T) {
	asked := &recordingConsent{answer: gate.FreshExplicit}
	good := signer("terminal-test-good")
	f := &v1Facade{
		batches: [][]protocol.Envelope{{
			envelope(good, frame.Instruction{Command: uint8(gate.ReportStatus), Protocol: frame.ProtocolV0, Seq: 1}),
			envelope(good, frame.Instruction{Command: uint8(gate.InstallArtifact), Protocol: frame.ProtocolV0, Seq: 2}),
		}},
	}
	_, _ = v1Run(t, f, func(c *Config) {
		c.Consent = asked
		c.Ceiling = gate.InstallArtifact
	})

	asked.mu.Lock()
	acts := append([]gate.Command(nil), asked.acts...)
	asked.mu.Unlock()

	if len(acts) != 2 {
		t.Fatalf("consent was asked for %d act(s), want 2 — it must be asked PER ACT", len(acts))
	}
	if acts[0] != gate.ReportStatus || acts[1] != gate.InstallArtifact {
		t.Errorf("consent was asked about %v, want [Report_Status Install_Artifact]", acts)
	}
}

// recordingConsent records which acts it was asked about.
type recordingConsent struct {
	mu     sync.Mutex
	acts   []gate.Command
	answer gate.Consent
}

func (r *recordingConsent) ConsentFor(_ context.Context, c gate.Command) gate.Consent {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.acts = append(r.acts, c)
	return r.answer
}

// ★ assertNoAppleLeak is the PII confinement assertion, applied to every place
// this terminal writes: the user's log, the outcome reports it queues, and the
// quarantine directory. The purchaser's reference must be in none of them.
func assertNoAppleLeak(t *testing.T, logText, dir string) {
	t.Helper()

	if strings.Contains(logText, appleSecret) {
		t.Errorf("★ THE APPLE ACCOUNT REFERENCE LEAKED INTO THE LOG:\n%s", logText)
	}

	quarantine := filepath.Join(dir, "quarantine")
	entries, err := os.ReadDir(quarantine)
	if errors.Is(err, os.ErrNotExist) {
		return
	}
	if err != nil {
		t.Fatalf("read quarantine: %v", err)
	}
	for _, e := range entries {
		body, err := os.ReadFile(filepath.Join(quarantine, e.Name()))
		if err != nil {
			t.Fatalf("read %s: %v", e.Name(), err)
		}
		if strings.Contains(string(body), appleSecret) {
			t.Errorf("★ THE APPLE ACCOUNT REFERENCE LEAKED INTO QUARANTINE FILE %s:\n%s", e.Name(), body)
		}
	}
}

// TestTheApplePurchaserReachesNoReport walks the outcome reports specifically,
// since those are the ones that leave the machine.
func TestTheApplePurchaserReachesNoReport(t *testing.T) {
	interviewer := &scriptedInterview{answers: []protocol.Answer{{ItemID: 1, State: "Answered", Answered: true, At: "t"}}}
	f := &v1Facade{
		batches:          [][]protocol.Envelope{deliveryBatch(1)},
		creditSufficient: true,
		creditBalance:    250,
	}
	logger, dir := v1Run(t, f, func(c *Config) { c.Interview = interviewer })

	for _, o := range f.outcomes() {
		encoded, err := json.Marshal(o)
		if err != nil {
			t.Fatalf("marshal outcome: %v", err)
		}
		if strings.Contains(string(encoded), appleSecret) {
			t.Errorf("★ THE APPLE ACCOUNT REFERENCE LEAKED INTO AN OUTCOME REPORT: %s", encoded)
		}
	}
	for _, s := range f.got() {
		encoded, err := json.Marshal(s)
		if err != nil {
			t.Fatalf("marshal submission: %v", err)
		}
		if strings.Contains(string(encoded), appleSecret) {
			t.Errorf("★ THE APPLE ACCOUNT REFERENCE LEAKED INTO A SUBMISSION: %s", encoded)
		}
	}
	assertNoAppleLeak(t, logger.text(), dir)
}
