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

	"github.com/tonygair/ghillie/internal/frame"
	"github.com/tonygair/ghillie/internal/gate"
	"github.com/tonygair/ghillie/internal/protocol"
)

// Config is everything the terminal needs. Ceiling and Consent are LOCAL STATE
// declared by this machine — they never arrive over the wire, and no facade
// response can alter them.
type Config struct {
	FacadeURL     string
	ClawID        string
	Ceiling       gate.Command
	Consent       gate.Consent
	QuarantineDir string
	StateFile     string
	PollInterval  time.Duration
	MaxPolls      int // 0 = poll until the context is cancelled
	IdleExit      int // exit after this many consecutive empty polls; 0 = never
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
	t.log.Printf("ghillie %s → %s", t.cfg.ClawID, t.cfg.FacadeURL)
	t.log.Printf("  ceiling  %s (rank %d)   consent %s   last-seq %d",
		t.cfg.Ceiling, gate.CommandRank(t.cfg.Ceiling), t.cfg.Consent, t.lastSeq)
	t.log.Printf("  polling outward every %s — no listener, no open port, no inbound path", t.cfg.PollInterval)

	idle := 0
	for polls := 0; ; polls++ {
		if err := ctx.Err(); err != nil {
			return nil
		}
		if t.cfg.MaxPolls > 0 && polls >= t.cfg.MaxPolls {
			t.log.Printf("poll limit reached (%d) — stopping", t.cfg.MaxPolls)
			return nil
		}

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
				t.handle(env)
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

	resp, err := t.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("poll %s: %w", url, err)
	}
	defer func() {
		if cerr := resp.Body.Close(); cerr != nil {
			t.log.Printf("close poll body: %v", cerr)
		}
	}()

	// The reports reached the facade; anything queued after this point belongs
	// to the next poll.
	if len(t.pending) > 0 {
		t.log.Printf("reported %d outcome(s) to the facade", len(t.pending))
		t.pending = nil
	}

	switch {
	case resp.StatusCode == http.StatusNoContent:
		return nil, nil
	case resp.StatusCode != http.StatusOK:
		body, rerr := io.ReadAll(io.LimitReader(resp.Body, 512))
		if rerr != nil {
			return nil, fmt.Errorf("poll: HTTP %d (and reading the body failed: %w)", resp.StatusCode, rerr)
		}
		return nil, fmt.Errorf("poll: HTTP %d: %s", resp.StatusCode, bytes.TrimSpace(body))
	}

	var out protocol.PollResponse
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		return nil, fmt.Errorf("decode poll response: %w", err)
	}
	return out.Instructions, nil
}

// handle runs one instruction through the whole admission path and records the
// outcome for the next poll. It never returns an error: an instruction that
// cannot be admitted is a refusal to report, not a failure of the terminal.
func (t *Terminal) handle(env protocol.Envelope) {
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
	allowed, reason := gate.Evaluate(command, t.cfg.Ceiling, t.cfg.Consent, authentic)
	if !allowed {
		t.refuse(instr.Seq, instr.Command, reason, t.explain(command, reason))
		return
	}

	note, err := t.act(command, instr)
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
func (t *Terminal) explain(c gate.Command, reason gate.Reason) string {
	switch reason {
	case gate.ReasonLocalOnly:
		return fmt.Sprintf("%s is local-only by construction — refused at EVERY ceiling and EVERY consent level (ledger 112, NO-REMOTE-REACH-FOR-LOCAL-WORK)", c)
	case gate.ReasonOverCeiling:
		return fmt.Sprintf("rank %d is above this machine's ceiling of %s (rank %d)", gate.CommandRank(c), t.cfg.Ceiling, gate.CommandRank(t.cfg.Ceiling))
	case gate.ReasonNoConsent:
		return fmt.Sprintf("%s needs Fresh_Explicit consent from a human at this machine; consent here is %s", c, t.cfg.Consent)
	default:
		return string(reason)
	}
}

// act performs an admitted instruction. Every arm is deliberately small, and
// two of them deliberately do nothing at all — see the notes.
func (t *Terminal) act(c gate.Command, instr frame.Instruction) (string, error) {
	switch c {
	case gate.ReportStatus:
		// The status body itself goes out on the next poll's report.
		return "status noted for the next report", nil

	case gate.OfferCatalogue:
		// Display-only. It installs nothing and fetches nothing.
		return fmt.Sprintf("catalogue index offered (ref %#016x, version %d) — display only, nothing installed", instr.ArtifactRef, instr.Version), nil

	case gate.DeliverArtifact:
		// A delivery NOTIFICATION. The artifact itself is fetched by a separate
		// outward GET, which v0 does not implement; what lands here is the
		// notice, written to quarantine. NOTHING IS EXECUTED, EVER.
		path, err := t.quarantine(instr)
		if err != nil {
			return "", fmt.Errorf("quarantine: %w", err)
		}
		return fmt.Sprintf("delivery notice quarantined at %s — not fetched, not installed, not executed", path), nil

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
func (t *Terminal) refuse(sequenceNum uint64, commandByte uint8, reason gate.Reason, detail string) {
	name := gate.Command(commandByte).String()
	t.log.Printf("  REFUSED   seq %d  %-20s  [%s] %s", sequenceNum, name, reason, detail)
	t.record(protocol.Outcome{
		Seq:         sequenceNum,
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
