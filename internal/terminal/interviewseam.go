package terminal

// interviewseam.go is the v1 seam: what happens after the gate has ADMITTED a
// Deliver_Artifact whose ArtifactRef names an interview brief.
//
// ★ NO NEW COMMAND AND NO CHANGE TO THE FRAME. The brief arrives as
// Deliver_Artifact — rank 2, which is the default ceiling — carrying the brief
// id in ArtifactRef and the revision in Version. Facade_Command_Pkg's vocabulary
// (ledger 112) is closed and proven, Command_Type'Pos IS the wire byte, and
// inserting a member anywhere but the end would renumber the wire and invalidate
// all 31 golden vectors. So the vocabulary is left entirely alone.
//
// ★ THE 22-BYTE SIGNED FRAME REMAINS THE ONLY AUTHORITY-BEARING OBJECT. The
// brief BODY comes down an ordinary unsigned GET, and that is safe precisely
// because the body carries no authority: the signed frame said which brief and
// which revision, the gate admitted it, and fetchBrief checks the body back
// against the signed frame before a word of it is used. A facade that serves a
// different brief than the one it signed for is caught here.

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"time"

	"github.com/tonygair/ghillie/internal/brief"
	"github.com/tonygair/ghillie/internal/credit"
	"github.com/tonygair/ghillie/internal/frame"
	"github.com/tonygair/ghillie/internal/gate"
	"github.com/tonygair/ghillie/internal/protocol"
)

// ActConductInterview names the act whose cost the facade is asked about. It is
// a string on the wire rather than an enum because the FACADE decides what acts
// cost, and this machine is not entitled to an opinion about that either.
const ActConductInterview = "conduct-interview"

// ErrBriefMismatch reports a fetched body that is not the brief the SIGNED
// frame announced.
var ErrBriefMismatch = errors.New("terminal: the fetched brief is not the one the signed frame named")

// ErrUserRevoked reports an act refused because the person at the keyboard is
// no longer in good standing (User_Access_Pkg, ledger 115).
var ErrUserRevoked = errors.New("terminal: the person at this keyboard has been revoked")

// deliverBrief runs the whole v1 seam for an admitted delivery: standing check,
// credit authority, outward fetch, interview, signed submission.
//
// The ORDER is deliberate and each step refuses on its own terms:
//
//  1. May the person at this keyboard act at all? (ledger 115)
//  2. Does the FACADE say there is credit for this? (never the local number)
//  3. Fetch the body and check it against the signed frame.
//  4. Conduct the interview. Ghillie asks; it judges nothing.
//  5. Sign the answers with the device key and queue them.
func (t *Terminal) deliverBrief(ctx context.Context, instr frame.Instruction) (string, error) {
	// 1. LEDGER 115. A revoked user may do nothing here, and nothing from them
	//    is required to make that bite — not their presence, not their consent.
	if !gate.MayAct(t.cfg.UserStanding) {
		t.log.Printf("  REFUSED   the interview — user standing is %s (ledger 115: May_Act is false)", t.cfg.UserStanding)
		return "", fmt.Errorf("%w: standing %s", ErrUserRevoked, t.cfg.UserStanding)
	}

	// 2. ★ CREDIT: COURTESY vs AUTHORITY. The balance this machine displays is a
	//    courtesy and authorises nothing. The decision is the facade's, because
	//    a declaration computed on a machine we do not control is not a
	//    permission — that is the whole lesson of the client-trusted
	//    senderIsOwner field.
	decision, err := credit.Authorise(ctx, t.cfg.Credit, ActConductInterview)
	if err != nil {
		t.log.Printf("  REFUSED   the interview — %v", err)
		t.log.Printf("            (a locally-displayed balance never authorises an act; the factory holds the decision)")
		return "", err
	}
	t.log.Printf("  the factory authorises the interview (its balance: %d) — %s", decision.Balance, decision.Note)

	b, err := t.fetchBrief(ctx, instr)
	if err != nil {
		return "", err
	}
	t.log.Printf("  brief %s revision %d fetched — %d item(s), conduct fields: NONE POSSIBLE", b.BriefID, b.Version, len(b.Items))

	answers, err := t.cfg.Interview.Conduct(ctx, b)
	if err != nil {
		return "", fmt.Errorf("conduct brief %s: %w", b.BriefID, err)
	}

	sub, err := t.sign(b, answers)
	if err != nil {
		return "", err
	}
	t.answers = append(t.answers, sub)

	got := 0
	for _, a := range answers {
		if a.Answered {
			got++
		}
	}
	return fmt.Sprintf("brief %s conducted: %d item(s), %d answered, %d not — queued as signed submission %s (ghillie does not judge whether that is enough)",
		b.BriefID, len(answers), got, len(answers)-got, sub.SubmissionID), nil
}

// fetchBrief makes the OUTWARD GET for a brief body and decodes it strictly.
//
// ★ THE STRICT DECODE IS THE CONDUCT WALL (internal/brief). A brief carrying
// wait_time_ms, may_interrupt or explain_mechanism cannot be represented by the
// type and is refused at parse — wholesale, not field by field. The refusal
// propagates out of here as an error, so it lands in the outcome report and in
// the user's log like any other refusal.
func (t *Terminal) fetchBrief(ctx context.Context, instr frame.Instruction) (*brief.Brief, error) {
	url := t.cfg.FacadeURL + protocol.BriefPath(instr.ArtifactRef, instr.Version)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, fmt.Errorf("build brief request: %w", err)
	}
	req.Header.Set("Accept", "application/json")
	if err := t.authorize(ctx, req); err != nil {
		return nil, fmt.Errorf("fetch brief: %w", err)
	}

	resp, err := t.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("fetch brief %s: %w", url, err)
	}
	defer func() {
		if cerr := resp.Body.Close(); cerr != nil {
			t.log.Printf("close brief body: %v", cerr)
		}
	}()

	if resp.StatusCode == http.StatusUnauthorized && t.cfg.Auth != nil {
		t.cfg.Auth.Invalidate()
	}
	if resp.StatusCode != http.StatusOK {
		detail, rerr := io.ReadAll(io.LimitReader(resp.Body, 512))
		if rerr != nil {
			return nil, fmt.Errorf("fetch brief: HTTP %d (and reading the body failed: %w)", resp.StatusCode, rerr)
		}
		return nil, fmt.Errorf("fetch brief: HTTP %d: %s", resp.StatusCode, detail)
	}

	b, err := brief.Decode(resp.Body)
	if err != nil {
		if errors.Is(err, brief.ErrConductInBrief) {
			// Loud, because this is not a malformed message — it is an attempt
			// to change how this machine treats a person.
			t.log.Printf("  ⚠ CONDUCT WALL — %v", err)
			t.log.Printf("            conduct is COMPILED INTO ghillie and is not deliverable. The brief was refused whole.")
		}
		return nil, err
	}

	// ★ Check the body back against the SIGNED frame. The GET carried no
	// authority; this is where the signed frame's authority reaches the body.
	if want := strconv.FormatUint(instr.ArtifactRef, 16); b.BriefID != want {
		return nil, fmt.Errorf("%w: signed frame named %s, body says %s", ErrBriefMismatch, want, b.BriefID)
	}
	if b.Version != instr.Version {
		return nil, fmt.Errorf("%w: signed frame named revision %d, body says %d", ErrBriefMismatch, instr.Version, b.Version)
	}
	return b, nil
}

// sign builds and signs a submission with the claw's DEVICE key.
//
// ⚠ THE APPLE ACCOUNT REFERENCE IS NOT IN HERE, and must never be. A submission
// is answers about software, not a receipt. The only place the purchaser's
// reference travels is the enrolment payload (internal/enrol).
func (t *Terminal) sign(b *brief.Brief, answers []protocol.Answer) (protocol.Submission, error) {
	id, err := submissionID()
	if err != nil {
		return protocol.Submission{}, fmt.Errorf("mint submission id: %w", err)
	}
	sub := protocol.Submission{
		SubmissionID: id,
		ClawID:       t.cfg.ClawID,
		BriefID:      b.BriefID,
		Version:      b.Version,
		Answers:      answers,
		At:           time.Now().UTC().Format(time.RFC3339),
	}
	if t.cfg.Signer == nil {
		return protocol.Submission{}, errors.New("no signer: an unsigned submission is not a submission")
	}
	sub.Signature = t.cfg.Signer.Sign(sub.SigningBytes())
	return sub, nil
}

// submit posts every queued submission to the facade's /specs door, on
// GHILLIE'S OWN INITIATIVE.
//
// TWO ROUTES, ONE AUTHORITY. The signature over SigningBytes is what makes a
// submission a submission; whether it arrives by this direct post or riding the
// next poll changes nothing about it. The direct post is tried first because it
// is the design's route; anything it cannot deliver stays queued and goes out on
// the poll, so a submission is never lost because a door was shut. A facade that
// receives one twice dedupes on SubmissionID.
func (t *Terminal) submit(ctx context.Context) {
	if len(t.answers) == 0 {
		return
	}
	remaining := make([]protocol.Submission, 0, len(t.answers))
	for _, sub := range t.answers {
		if err := t.postSubmission(ctx, sub); err != nil {
			t.log.Printf("submit %s: %v — kept queued, it will ride the next poll", sub.SubmissionID, err)
			remaining = append(remaining, sub)
			continue
		}
		t.log.Printf("  SUBMITTED %s — %d answer(s), device-signed, sent on this machine's own initiative", sub.SubmissionID, len(sub.Answers))
		t.record(protocol.Outcome{
			CommandName: "submission",
			Admitted:    true,
			Note:        fmt.Sprintf("submission %s for brief %s revision %d delivered to %s", sub.SubmissionID, sub.BriefID, sub.Version, protocol.SpecsPath),
			At:          time.Now().UTC().Format(time.RFC3339),
		})
	}
	t.answers = remaining
}

// postSubmission makes one outward POST of one signed submission.
func (t *Terminal) postSubmission(ctx context.Context, sub protocol.Submission) error {
	encoded, err := json.Marshal(sub)
	if err != nil {
		return fmt.Errorf("encode submission: %w", err)
	}
	url := t.cfg.FacadeURL + protocol.SpecsPath
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(encoded))
	if err != nil {
		return fmt.Errorf("build submission request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	if err := t.authorize(ctx, req); err != nil {
		return err
	}

	resp, err := t.client.Do(req)
	if err != nil {
		return fmt.Errorf("post %s: %w", url, err)
	}
	defer func() {
		if cerr := resp.Body.Close(); cerr != nil {
			t.log.Printf("close submission body: %v", cerr)
		}
	}()

	if resp.StatusCode == http.StatusUnauthorized && t.cfg.Auth != nil {
		t.cfg.Auth.Invalidate()
	}
	if resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusAccepted && resp.StatusCode != http.StatusCreated {
		detail, rerr := io.ReadAll(io.LimitReader(resp.Body, 512))
		if rerr != nil {
			return fmt.Errorf("HTTP %d (and reading the body failed: %w)", resp.StatusCode, rerr)
		}
		return fmt.Errorf("HTTP %d: %s", resp.StatusCode, detail)
	}
	return nil
}

// authorize attaches the Authorization header, attesting if there is no live
// session. A nil Authorizer sends no header, which the mock facade accepts and
// the REAL facade door refuses with a 401 — that difference is real and is why
// the enrolment client exists.
func (t *Terminal) authorize(ctx context.Context, req *http.Request) error {
	if t.cfg.Auth == nil {
		return nil
	}
	value, err := t.cfg.Auth.Authorization(ctx)
	if err != nil {
		return fmt.Errorf("attest: %w", err)
	}
	req.Header.Set("Authorization", value)
	return nil
}

// submissionID mints a random id. It is random rather than a counter so that
// nothing about this claw's history is inferable from it.
func submissionID() (string, error) {
	var b [12]byte
	if _, err := rand.Read(b[:]); err != nil {
		return "", fmt.Errorf("read random: %w", err)
	}
	return hex.EncodeToString(b[:]), nil
}
