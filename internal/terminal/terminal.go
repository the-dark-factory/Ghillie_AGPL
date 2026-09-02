// Package terminal is the ghillie customer-side terminal: the poll loop, the
// signature check, the decode, the gate call, the act-or-refuse, and the
// refusal report.
//
// THERE IS NO DECISION LOGIC IN THIS PACKAGE BY DESIGN. Every judgement it
// appears to make is a call into internal/gate, which is a faithful mirror of
// the proven cores (Facade_Command_Pkg ledger 112, Poll_Freshness_Pkg ledger
// 120). The pattern is the provenLaneGate adapter at the bottom of
// ada-factory/cmd/specifier/forge_wiring.go. What lives here is plumbing:
// HTTP, hex, files, ordering, reporting.
//
// DIRECTION. This is a client and only a client. It opens every connection; it
// never listens. There is no http.Server anywhere in this repository's ghillie
// side and there must never be one.
package terminal

import (
	"bytes"
	"context"
	"crypto/ed25519"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"time"

	"github.com/tonygair/ghillie/internal/brief"
	"github.com/tonygair/ghillie/internal/credit"
	"github.com/tonygair/ghillie/internal/frame"
	"github.com/tonygair/ghillie/internal/gate"
	"github.com/tonygair/ghillie/internal/identity"
	"github.com/tonygair/ghillie/internal/locale"
	"github.com/tonygair/ghillie/internal/notes"
	"github.com/tonygair/ghillie/internal/protocol"
)

// ConsentSource supplies the human agreement present AT THIS MACHINE for one
// specific act.
//
// ★ IT IS AN INTERFACE, NOT A VALUE, AND THAT IS THE WHOLE POINT. Fresh_Explicit
// consent means a human at this machine was asked about THIS ACT and said yes. A
// stored value is a standing answer to a question nobody asked, which is exactly
// what Fresh_Explicit is defined not to be. Making the source a call means the
// real implementation — a per-act local prompt — drops in without the gate path
// changing, and gate.Evaluate already takes consent as a parameter.
//
// Consent is established at the local UI and NEVER on the wire. It is the one
// control a compromised facade cannot route around, because it cannot be a
// person standing at someone else's computer.
type ConsentSource interface {
	ConsentFor(ctx context.Context, c gate.Command) gate.Consent
}

// StaticConsent is a ConsentSource that always answers the same way.
//
// ⚠ IT IS A STUB AND IT SAYS SO. It is the v0 command-line flag wearing the new
// interface, kept because it is what the tests and the demo need, and because
// the value is at least still LOCAL — it never comes from the wire, so the gate
// is wired correctly even while the consent source is not yet real.
type StaticConsent gate.Consent

// ConsentFor returns the fixed consent level, ignoring which act is asked about.
func (s StaticConsent) ConsentFor(context.Context, gate.Command) gate.Consent {
	return gate.Consent(s)
}

// String renders the fixed level.
func (s StaticConsent) String() string { return gate.Consent(s).String() }

// Authorizer supplies the Authorization header for outward requests, attesting
// when it has no live session. internal/enrol.Client implements it.
//
// Nil is a legitimate configuration and means "send no header" — which is what
// the mock facade accepts and what the REAL door refuses with a 401.
type Authorizer interface {
	Authorization(ctx context.Context) (string, error)
	Invalidate()
}

// Signer signs a submission with the claw's DEVICE key.
type Signer interface {
	Sign(message []byte) string
}

// Interviewer conducts the interview a brief describes and reports what it
// found. It never judges what it found — see internal/interview.
type Interviewer interface {
	Conduct(ctx context.Context, b *brief.Brief) ([]protocol.Answer, error)
}

// Config is everything the terminal needs. Ceiling, Consent and UserStanding are
// LOCAL STATE declared by this machine — they never arrive over the wire, and no
// facade response can alter them.
type Config struct {
	FacadeURL     string
	ClawID        string
	Ceiling       gate.Command
	Consent       ConsentSource
	QuarantineDir string
	StateFile     string
	PollInterval  time.Duration
	MaxPolls      int // 0 = poll until the context is cancelled
	IdleExit      int // exit after this many consecutive empty polls; 0 = never

	// LocalDoor, when set, names the ONE loopback listener this terminal has
	// open — the glass, the person's own door. The startup banner then says
	// so instead of claiming no listener at all: a banner that says "no open
	// port" over an open port is a checker that under-claims, and those are
	// as dangerous as the ones that over-claim. The facade path is outward-
	// only either way.
	LocalDoor string

	// Identity is the FOUR-WAY binding: this claw, its enrolling owner, the
	// person at the keyboard, and (PII, never logged) the account that bought
	// the credits. See internal/identity.
	Identity identity.Binding

	// UserStanding is what the fleet says about the person at the keyboard
	// (User_Access_Pkg, ledger 115). A revoked user may do nothing here, and
	// nothing from them is needed to make that bite.
	UserStanding gate.UserStanding

	// Auth attests and supplies the poll's Authorization header. Nil means no
	// header, which the real facade door refuses.
	Auth Authorizer

	// Interview conducts a fetched brief. Nil means this terminal does not
	// interview: a delivery is quarantined as a notice and nothing is fetched.
	Interview Interviewer

	// Signer signs submissions with the device key. Required when Interview is
	// set, because an unsigned submission is not a submission.
	Signer Signer

	// Credit is the FACADE'S credit authority. The local balance is a courtesy;
	// this is the decision. Nil means no authority is configured, which is the
	// absence of a yes and therefore a refusal.
	Credit credit.Authority

	// Notes, when set, receives each new correspondent note AS DATA instead
	// of the "post │ " log rendering — a surface that wants a ledger (the
	// GUI's job list) rather than a line. The structural property is the
	// sink's to keep exactly as it is the log's: record and render, never
	// dispatch. Nil = the log rendering (the CLI's shape).
	Notes NotesSink
}

// NotesSink consumes verified correspondent notes for a richer surface.
type NotesSink interface {
	Note(n notes.Note)
}

// Logger is the sink for the terminal's human-facing narration. Refusals go
// here, not only to the facade: a refusal the facade can see is telemetry, a
// refusal the USER can see is the point.
type Logger interface {
	Printf(format string, v ...any)
}

// Terminal is one customer-side claw.
type Terminal struct {
	cfg       Config
	facadeKey ed25519.PublicKey
	client    *http.Client
	log       Logger

	// lastSeq is the durable edge outside the proven freshness core. Ledger 120
	// defines empty state as zero, so zero is a correct starting value and no
	// "not yet loaded" sentinel is needed.
	lastSeq uint64
	pending []protocol.Outcome

	// answers is the SIBLING QUEUE to pending: signed submissions that have not
	// yet been delivered. It drains at the same point in the loop, and for the
	// same reason — anything queued after the request has gone belongs to the
	// next one. See submit for why there are two delivery routes and one
	// authority.
	answers []protocol.Submission

	// seenNotes / seenRing / retired are the correspondent channel's in-memory
	// state: rendered-note dedupe (bounded, FIFO eviction) and retired
	// correspondents (a terminal note ends a correspondence — stragglers drop).
	// See notesfetch.go. In-memory like the answers queue: a restart may
	// re-render, which is noise, never authority.
	seenNotes map[string]bool
	seenRing  []string
	retired   map[string]bool
}

// state is the small durable edge outside the proven freshness core: the
// last-seen sequence number. Ledger 120 decides freshness; storing the number
// is this package's job.
type state struct {
	LastSeq uint64 `json:"last_seq"`
}

// ErrNoFacadeKey reports a terminal built without a pinned facade public key.
var ErrNoFacadeKey = errors.New("terminal: no pinned facade public key")

// New builds a terminal against a pinned facade signing key.
//
// The key is PINNED, not discovered. In the full protocol it travels to the
// device during enrolment, which is an owner's act (Claw_Enrolment_Pkg, ledger
// 113). v0 takes it from a file placed there out of band, which is the same
// trust relationship with the ceremony stubbed out.
func New(cfg Config, facadeKey ed25519.PublicKey, log Logger) (*Terminal, error) {
	if len(facadeKey) != ed25519.PublicKeySize {
		return nil, fmt.Errorf("%w: got %d bytes, want %d", ErrNoFacadeKey, len(facadeKey), ed25519.PublicKeySize)
	}
	if cfg.PollInterval <= 0 {
		cfg.PollInterval = 30 * time.Second
	}
	if cfg.Consent == nil {
		// No consent source is NO CONSENT, never a permissive default. Ledger
		// 112 refuses every consent-requiring command at this level.
		cfg.Consent = StaticConsent(gate.NoConsent)
	}
	if cfg.Interview != nil && cfg.Signer == nil {
		return nil, errors.New("terminal: an interviewing terminal needs a Signer — an unsigned submission is not a submission")
	}
	return &Terminal{
		cfg:       cfg,
		facadeKey: facadeKey,
		client:    &http.Client{Timeout: 30 * time.Second},
		log:       log,
	}, nil
}

// Run is the poll loop. It returns when the context is cancelled, when MaxPolls
// polls have been made, or when IdleExit consecutive empty polls have gone by.
func (t *Terminal) Run(ctx context.Context) error {
	if err := t.loadState(); err != nil {
		return fmt.Errorf("load state: %w", err)
	}
	t.initNotes()
	t.log.Printf("ghillie %s → %s", t.cfg.ClawID, t.cfg.FacadeURL)
	t.log.Printf("  ceiling  %s (rank %d)   last-seq %d", t.cfg.Ceiling, gate.CommandRank(t.cfg.Ceiling), t.lastSeq)
	// The binding renders with the purchaser REDACTED — see internal/identity.
	t.log.Printf("  identity %s", t.cfg.Identity)
	t.log.Printf("  user standing %s (ledger 115: a revoked user may act on nothing here)", t.cfg.UserStanding)
	if t.cfg.LocalDoor == "" {
		t.log.Printf("  polling outward every %s — no listener, no open port, no inbound path", t.cfg.PollInterval)
	} else {
		t.log.Printf("  polling outward every %s — the facade path is outward-only; the one open door is the glass at %s, loopback, the person's own", t.cfg.PollInterval, t.cfg.LocalDoor)
	}

	idle := 0
	for polls := 0; ; polls++ {
		if err := ctx.Err(); err != nil {
			return nil
		}
		if t.cfg.MaxPolls > 0 && polls >= t.cfg.MaxPolls {
			t.log.Printf("poll limit reached (%d) — stopping", t.cfg.MaxPolls)
			return nil
		}

		// Answers leave by ghillie's OWN INITIATIVE, before the poll and not as
		// part of answering one. The facade can never pull them: ledger 112
		// proves Is_Local_Only (Request_Spec_Upload) at every ceiling and every
		// consent level.
		t.submit(ctx)

		// The correspondent channel rides the same cadence as the poll but is
		// a separate, authority-free fetch: progress arrives even on polls
		// that carry no instructions, and a broken notes channel can never
		// stall the instruction loop.
		t.fetchNotes(ctx)

		instructions, err := t.poll(ctx)
		if err != nil {
			t.log.Printf("poll: %v", err)
		} else if len(instructions) == 0 {
			idle++
			// An empty response is the non-disclosure identity: it means
			// "nothing for you" and it means "there are instructions you may
			// not have", and the terminal cannot and should not tell which.
			if t.cfg.IdleExit > 0 && idle >= t.cfg.IdleExit {
				t.log.Printf("%d consecutive empty polls — stopping", idle)
				return nil
			}
		} else {
			idle = 0
			for _, env := range instructions {
				t.handle(ctx, env)
			}
			if err := t.saveState(); err != nil {
				t.log.Printf("save state: %v", err)
			}
		}

		if !sleepCtx(ctx, t.cfg.PollInterval) {
			return nil
		}
	}
}

// poll makes one outward request, carrying any unreported outcomes, and returns
// whatever instructions came back.
func (t *Terminal) poll(ctx context.Context) ([]protocol.Envelope, error) {
	reqBody := protocol.PollRequest{
		ClawID:  t.cfg.ClawID,
		Ceiling: uint8(t.cfg.Ceiling),
		Reports: t.pending,
		Answers: t.answers,
	}
	encoded, err := json.Marshal(reqBody)
	if err != nil {
		return nil, fmt.Errorf("encode poll request: %w", err)
	}
	url := t.cfg.FacadeURL + protocol.PollPath(t.cfg.ClawID)
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(encoded))
	if err != nil {
		return nil, fmt.Errorf("build poll request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	if err := t.authorize(ctx, req); err != nil {
		return nil, fmt.Errorf("poll: %w", err)
	}

	resp, err := t.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("poll %s: %w", url, err)
	}
	defer func() {
		if cerr := resp.Body.Close(); cerr != nil {
			t.log.Printf("close poll body: %v", cerr)
		}
	}()

	// A 401 means the session this claw believed in is not one the facade
	// honours. Drop it so the next poll re-attests, and report the poll as
	// failed rather than as empty — "unauthenticated" and "nothing for you"
	// must not be confused with each other.
	if resp.StatusCode == http.StatusUnauthorized {
		if t.cfg.Auth != nil {
			t.cfg.Auth.Invalidate()
		}
		return nil, errors.New("poll: HTTP 401 — the facade wants attestation; the session has been dropped and the next poll will re-attest")
	}

	// ★ THE QUEUES DRAIN ONLY ON ACCEPTANCE. An error response means the facade
	// did not take custody of what this request carried — a 500 that fired
	// after the server-side write is indistinguishable, from here, from one
	// that fired before it, and the honest reading of that ambiguity is "not
	// delivered". So on anything other than a 2xx the outcomes and signed
	// submissions STAY QUEUED and ride the next poll. Delivering twice is safe
	// (the facade dedupes submissions on SubmissionID, and an outcome repeated
	// is a repeated truth); delivering zero times is a silent loss. An earlier
	// build cleared the queues before looking at the status, which discarded
	// them on any non-401 error while logging success — that was a real bug,
	// found by survey, and this ordering is its fix.
	if resp.StatusCode < 200 || resp.StatusCode > 299 {
		body, rerr := io.ReadAll(io.LimitReader(resp.Body, 512))
		if rerr != nil {
			return nil, fmt.Errorf("poll: HTTP %d — %d outcome(s) and %d submission(s) kept queued for the next poll (and reading the body failed: %w)",
				resp.StatusCode, len(t.pending), len(t.answers), rerr)
		}
		return nil, fmt.Errorf("poll: HTTP %d — %d outcome(s) and %d submission(s) kept queued for the next poll: %s",
			resp.StatusCode, len(t.pending), len(t.answers), bytes.TrimSpace(body))
	}

	// The facade accepted the poll, so the reports reached it; anything queued
	// after this point belongs to the next poll.
	if len(t.pending) > 0 {
		t.log.Printf("reported %d outcome(s) to the facade", len(t.pending))
		t.pending = nil
	}
	// The answer queue drains at the same point and for the same reason.
	if len(t.answers) > 0 {
		t.log.Printf("carried %d signed submission(s) out on the poll", len(t.answers))
		t.answers = nil
	}

	if resp.StatusCode == http.StatusNoContent {
		return nil, nil
	}

	// H2 (security review 2026-08-07): this was the ONE unbounded read in the
	// repo, and it sat on the authority path — every other remote read here
	// (enrol, attest, credit, brief, notes, catalogue, whisper) was already
	// bounded. The most protected endpoint by design was the least protected by
	// code, which is exactly backwards: the facade is modelled as hostile, so a
	// hostile facade could stream until the terminal died.
	var out protocol.PollResponse
	if err := json.NewDecoder(io.LimitReader(resp.Body, 1<<16)).Decode(&out); err != nil {
		return nil, fmt.Errorf("decode poll response: %w", err)
	}
	// Bound the COUNT as well as the bytes. Refuse the poll whole rather than
	// truncating: a silently dropped tail would hide from the owner that the
	// facade sent instructions the terminal declined to look at.
	if len(out.Instructions) > protocol.MaxPollInstructions {
		return nil, fmt.Errorf("poll carried %d instructions, over the %d limit — refused whole",
			len(out.Instructions), protocol.MaxPollInstructions)
	}
	return out.Instructions, nil
}

// handle runs one instruction through the whole admission path and records the
// outcome for the next poll. It never returns an error: an instruction that
// cannot be admitted is a refusal to report, not a failure of the terminal.
func (t *Terminal) handle(ctx context.Context, env protocol.Envelope) {
	raw, err := hex.DecodeString(env.Frame)
	if err != nil || len(raw) != frame.Size {
		t.refuse(0, 0, gate.ReasonMalformedFrame, fmt.Sprintf("frame is not %d hex-encoded bytes", frame.Size))
		return
	}

	// 1. AUTHENTICITY — over the exact frame bytes, before anything is decoded.
	//    A signature that does not verify makes Authentic false, and ledger 112
	//    proves that not-authentic is always refused. The check happens first so
	//    that no unauthenticated byte influences any later step, including the
	//    stored sequence number.
	sig, err := hex.DecodeString(env.Signature)
	authentic := err == nil &&
		len(sig) == ed25519.SignatureSize &&
		ed25519.Verify(t.facadeKey, raw, sig)

	// 2. DECODE — the layout is ledger 119's; this Go is cross-checked against
	//    it, not proven (see internal/frame).
	instr, err := frame.Unmarshal(raw)
	if err != nil {
		t.refuse(0, 0, gate.ReasonMalformedFrame, err.Error())
		return
	}

	if !authentic {
		// Reported with the command byte as received, clearly labelled as
		// unverified: the user is being shown what the wire claimed, not what
		// the terminal believes.
		t.refuse(instr.Seq, instr.Command, gate.ReasonBadSignature,
			"signature does not verify against the pinned facade key — command byte shown as received, unverified")
		return
	}

	// 3. BOUNDARY CHECKS Ada gets from its type system and Go does not.
	if instr.Protocol != frame.ProtocolV0 {
		t.refuse(instr.Seq, instr.Command, gate.ReasonBadProtocol,
			fmt.Sprintf("frame protocol version %d, this terminal speaks %d", instr.Protocol, frame.ProtocolV0))
		return
	}
	if !gate.IsKnownCommand(instr.Command) {
		t.refuse(instr.Seq, instr.Command, gate.ReasonUnknownCommand,
			fmt.Sprintf("command byte %d is outside the proven enumeration (0..%d) — the gate has no rank for it", instr.Command, gate.MaxCommand))
		return
	}
	command := gate.Command(instr.Command)

	// 4. FRESHNESS — Poll_Freshness_Pkg (ledger 120). Only authentic frames get
	//    this far, so an attacker replaying a frame cannot move the stored
	//    sequence number by forging one.
	if !gate.IsFresh(t.lastSeq, instr.Seq) {
		t.refuse(instr.Seq, instr.Command, gate.ReasonStaleSeq,
			fmt.Sprintf("sequence %d is not strictly newer than the last accepted %d — replay", instr.Seq, t.lastSeq))
		return
	}
	// The frame was authentic and fresh, so it has been SEEN. Advance the stored
	// sequence now, before the gate runs: whether the gate admits it or not, the
	// same frame must never be accepted twice.
	t.lastSeq = instr.Seq

	// 5. THE GATE — Facade_Command_Pkg (ledger 112). This is the only place a
	//    verdict is decided, and this file does not decide it.
	//
	//    Consent is ASKED FOR HERE, per act, from the local source — it is not a
	//    stored value consulted after the fact. Evaluate already took consent as
	//    a parameter, so making the source a call changed exactly this one line.
	consent := t.cfg.Consent.ConsentFor(ctx, command)
	allowed, reason := gate.Evaluate(command, t.cfg.Ceiling, consent, authentic)
	if !allowed {
		t.refuse(instr.Seq, instr.Command, reason, t.explain(command, consent, reason))
		return
	}

	note, err := t.act(ctx, command, instr)
	if err != nil {
		t.log.Printf("  ADMITTED  seq %d  %-20s  but the act failed: %v", instr.Seq, command, err)
		t.record(protocol.Outcome{
			Seq:         instr.Seq,
			Command:     instr.Command,
			CommandName: command.String(),
			Admitted:    true,
			Note:        "act failed: " + err.Error(),
			At:          time.Now().UTC().Format(time.RFC3339),
		})
		return
	}
	t.log.Printf("  ADMITTED  seq %d  %-20s  %s", instr.Seq, command, note)
	t.record(protocol.Outcome{
		Seq:         instr.Seq,
		Command:     instr.Command,
		CommandName: command.String(),
		Admitted:    true,
		Note:        note,
		At:          time.Now().UTC().Format(time.RFC3339),
	})
}

// explain turns a reason class into the sentence a user reads. It states the
// guarantee, not the mechanism.
func (t *Terminal) explain(c gate.Command, consent gate.Consent, reason gate.Reason) string {
	switch reason {
	case gate.ReasonLocalOnly:
		return fmt.Sprintf(locale.TRefusal("refuse.local-only"), c)
	case gate.ReasonOverCeiling:
		return fmt.Sprintf(locale.TRefusal("refuse.over-ceiling"), gate.CommandRank(c), t.cfg.Ceiling, gate.CommandRank(t.cfg.Ceiling))
	case gate.ReasonNoConsent:
		return fmt.Sprintf(locale.TRefusal("refuse.no-consent"), c, consent)
	default:
		return string(reason)
	}
}

// act performs an admitted instruction. Every arm is deliberately small, and
// two of them deliberately do nothing at all — see the notes.
func (t *Terminal) act(ctx context.Context, c gate.Command, instr frame.Instruction) (string, error) {
	switch c {
	case gate.ReportStatus:
		// The status body itself goes out on the next poll's report.
		return locale.T("status.noted"), nil

	case gate.OfferCatalogue:
		// Display-only. It installs nothing and fetches nothing.
		return fmt.Sprintf(locale.T("catalogue.offered"), instr.ArtifactRef, instr.Version), nil

	case gate.DeliverArtifact:
		// A delivery NOTIFICATION. The artifact itself is fetched by a separate
		// OUTWARD GET — this is the hole v0 named here and v1 fills. The notice
		// still lands in quarantine first, and NOTHING IS EXECUTED, EVER.
		path, err := t.quarantine(instr)
		if err != nil {
			return "", fmt.Errorf("quarantine: %w", err)
		}
		notice := fmt.Sprintf(locale.T("delivery.notice"), path)

		if t.cfg.Interview == nil {
			return notice + locale.T("delivery.nointerv"), nil
		}
		conducted, err := t.deliverBrief(ctx, instr)
		if err != nil {
			return "", fmt.Errorf("%s; %w", notice, err)
		}
		return notice + "; " + conducted, nil

	case gate.InstallArtifact:
		// ⚠ STUB, AND DELIBERATELY SO. The gate admitted this, which means the
		// ceiling was raised to rank 4+ and a human gave fresh explicit consent.
		// v0 still does not install: Update_Admission_Pkg (ledger 114,
		// well-formed / signature valid / key known / version STRICTLY newer)
		// is not wired, and installing without it would be exactly the
		// unproven-decision-on-the-security-path this design exists to avoid.
		return "GATE ADMITTED — but NOTHING WAS INSTALLED: v0 has no installer and Update_Admission_Pkg (ledger 114) is not wired", nil

	case gate.RunLocalCode:
		// ⚠ STUB, AND PERMANENTLY SO FOR v0. Rank 5 is total compromise. It
		// exists in the vocabulary so the gate ranks it, not so v0 uses it.
		return "GATE ADMITTED — but NOTHING WAS EXECUTED: v0 has no executor and will not grow one", nil

	case gate.RequestSpecUpload:
		// Unreachable: ledger 112 proves this command is refused at every
		// ceiling and every consent level, so the gate can never admit it. Say
		// so loudly rather than doing anything.
		return "", errors.New("unreachable: Request_Spec_Upload is local-only and cannot be admitted — the gate mirror has drifted from ledger 112")

	default:
		return "", fmt.Errorf("no actuator for command rank %d", gate.CommandRank(c))
	}
}

// quarantine writes a delivery notice to the quarantine directory. It records
// what was delivered; it does not fetch, unpack, install or run anything.
func (t *Terminal) quarantine(instr frame.Instruction) (string, error) {
	dir := t.cfg.QuarantineDir
	if dir == "" {
		return "", errors.New("no quarantine directory configured")
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", fmt.Errorf("create quarantine dir: %w", err)
	}
	name := fmt.Sprintf("delivery-%016x-v%d-seq%d.json", instr.ArtifactRef, instr.Version, instr.Seq)
	path := filepath.Join(dir, name)
	notice := map[string]any{
		"artifact_ref": fmt.Sprintf("%016x", instr.ArtifactRef),
		"version":      instr.Version,
		"seq":          instr.Seq,
		"received_at":  time.Now().UTC().Format(time.RFC3339),
		"state":        "QUARANTINED — notice only. Nothing fetched, nothing installed, nothing executed.",
	}
	body, err := json.MarshalIndent(notice, "", "  ")
	if err != nil {
		return "", fmt.Errorf("encode delivery notice: %w", err)
	}
	if err := os.WriteFile(path, append(body, '\n'), 0o600); err != nil {
		return "", fmt.Errorf("write delivery notice: %w", err)
	}
	return path, nil
}

// refuse narrates a refusal to the user and queues it for the facade. A refused
// instruction is NOT silently dropped.
func (t *Terminal) refuse(seq uint64, commandByte uint8, reason gate.Reason, detail string) {
	name := gate.Command(commandByte).String()
	t.log.Printf("  REFUSED   seq %d  %-20s  [%s] %s", seq, name, reason, detail)
	t.record(protocol.Outcome{
		Seq:         seq,
		Command:     commandByte,
		CommandName: name,
		Admitted:    false,
		Reason:      string(reason),
		Note:        detail,
		At:          time.Now().UTC().Format(time.RFC3339),
	})
}

// record queues an outcome for the next outward poll.
func (t *Terminal) record(o protocol.Outcome) { t.pending = append(t.pending, o) }

// loadState reads the last-seen sequence number. A missing file is empty state,
// which ledger 120 defines as last-sequence zero — so any positive first frame
// is fresh.
func (t *Terminal) loadState() error {
	if t.cfg.StateFile == "" {
		t.lastSeq = 0
		return nil
	}
	body, err := os.ReadFile(t.cfg.StateFile)
	if errors.Is(err, os.ErrNotExist) {
		t.lastSeq = 0
		return nil
	}
	if err != nil {
		return fmt.Errorf("read %s: %w", t.cfg.StateFile, err)
	}
	var s state
	if err := json.Unmarshal(body, &s); err != nil {
		return fmt.Errorf("parse %s: %w", t.cfg.StateFile, err)
	}
	t.lastSeq = s.LastSeq
	return nil
}

// saveState persists the last-seen sequence number.
func (t *Terminal) saveState() error {
	if t.cfg.StateFile == "" {
		return nil
	}
	if dir := filepath.Dir(t.cfg.StateFile); dir != "." {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			return fmt.Errorf("create state dir: %w", err)
		}
	}
	body, err := json.Marshal(state{LastSeq: t.lastSeq})
	if err != nil {
		return fmt.Errorf("encode state: %w", err)
	}
	if err := os.WriteFile(t.cfg.StateFile, append(body, '\n'), 0o600); err != nil {
		return fmt.Errorf("write %s: %w", t.cfg.StateFile, err)
	}
	return nil
}

// sleepCtx waits for d, or until the context is cancelled. It reports false
// when the wait was cut short.
func sleepCtx(ctx context.Context, d time.Duration) bool {
	timer := time.NewTimer(d)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return false
	case <-timer.C:
		return true
	}
}
