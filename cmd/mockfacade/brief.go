package main

// brief.go is the TEST DOUBLE's factory side: it serves interview briefs,
// receives signed answer submissions, records enrolments, runs the attestation
// challenge–response, and answers the credit question.
//
// ═══════════════════════════════════════════════════════════════════════════
//
//	⚠  STILL A TEST DOUBLE. NOT THE REAL FACADE. NOT DEPLOYABLE.
//
// ═══════════════════════════════════════════════════════════════════════════
//
// What it fakes, badly and on purpose, beyond what main.go already lists:
//   - the brief content is a HARD-CODED LIST. The real factory composes a brief
//     from what it has scored so far, and ghillie never sees that machinery.
//   - it will happily serve a brief carrying CONDUCT fields on request. That is
//     the entire point of the -brief flag: it plays the compromised facade so
//     the conduct wall can be watched refusing.
//   - a submission's signature is verified against the enrolled device key, but
//     nothing is stored, scored or acted on. The real receiver does not exist —
//     it is work item D and is separately gated.
//   - the credit answer is a FLAG. The real one is Charge_Economy_Pkg (ledger
//     87) and Credit_Grant_Pkg (ledger 86) over a real balance.

import (
	"crypto/ed25519"
	"crypto/rand"
	"crypto/subtle"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/tonygair/ghillie/internal/protocol"
)

// wants is the interview content this double serves.
//
// ★ CONTENT LIVES AT THE FACTORY, WHICH IS WHY IT LIVES HERE AND NOT IN
// GHILLIE. These six are lifted from the discarded voice prototype, where they
// were already written and already good. Ghillie carries none of them: it asks
// what the brief says to ask.
var wants = []string{
	"What the software is actually for — the job it does, in your own words, not in feature form.",
	"Who uses it, and what they are in the middle of doing at the moment they reach for it.",
	"What it has to talk to — existing systems, machines, files, spreadsheets, people's habits.",
	"What 'wrong' looks like — the failure that would actually cost you something, and what it would cost.",
	"Volume and pace — how much of this, how often, and how fast it has to come back.",
	"How you will know it earned its keep — the thing you would point at in six months.",
}

// briefMode selects what this double serves when asked for a brief.
type briefMode string

// The brief modes. The three conduct modes exist so the CONDUCT WALL can be
// demonstrated refusing a real response rather than a unit-test fixture.
const (
	briefItems            briefMode = "items"             // a lawful brief: items only
	briefConductWait      briefMode = "conduct-wait"      // carries wait_time_ms
	briefConductInterrupt briefMode = "conduct-interrupt" // carries may_interrupt
	briefConductDisclose  briefMode = "conduct-disclose"  // carries explain_mechanism
)

// parseBriefMode resolves the -brief flag.
func parseBriefMode(s string) (briefMode, error) {
	switch briefMode(strings.ToLower(strings.TrimSpace(s))) {
	case briefItems:
		return briefItems, nil
	case briefConductWait:
		return briefConductWait, nil
	case briefConductInterrupt:
		return briefConductInterrupt, nil
	case briefConductDisclose:
		return briefConductDisclose, nil
	default:
		return "", fmt.Errorf("unknown brief mode %q (items, conduct-wait, conduct-interrupt, conduct-disclose)", s)
	}
}

// enrolled is one recorded enrolment. The double keeps it in memory; the real
// registry is internal/clawreg in the factory tree.
type enrolled struct {
	pubKey  ed25519.PublicKey
	ceiling int
}

// registry is the double's claw registry plus its ephemeral attestation state.
type registry struct {
	mu         sync.Mutex
	claws      map[string]enrolled
	challenges map[string][]byte
	sessions   map[string]string // session token → claw id
}

func newRegistry() *registry {
	return &registry{
		claws:      map[string]enrolled{},
		challenges: map[string][]byte{},
		sessions:   map[string]string{},
	}
}

// handleEnrol records a claw's device key and ceiling.
//
// ⚠ IT DOES NOT CHECK THAT THE REQUESTER IS AN OWNER, and the real door does
// not either — the ledger-113 check is recorded as MISSING on the facade side.
// The claw refuses locally before it ever gets here (internal/enrol), which is
// the right place for the claw's own conscience but is not a substitute for the
// facade having one.
func (r *registry) handleEnrol(w http.ResponseWriter, req *http.Request) {
	id := req.PathValue("id")
	var body struct {
		PubKey  string `json:"pubkey"`
		Ceiling *int   `json:"ceiling"`

		ClawID string `json:"claw_id"`
		Owner  string `json:"owner_id"`
		User   string `json:"user_id"`

		// ⚠ PII ARRIVES HERE AND IS NOT LOGGED. The double reads the field so
		// the binding's shape is exercised end to end, and deliberately never
		// prints it — the same discipline the real receiver will need.
		AppleAccount string `json:"apple_account_ref"`
	}
	if err := json.NewDecoder(io.LimitReader(req.Body, 1<<16)).Decode(&body); err != nil {
		http.Error(w, "bad enrolment body", http.StatusBadRequest)
		return
	}
	raw, err := hex.DecodeString(body.PubKey)
	if err != nil || len(raw) != ed25519.PublicKeySize {
		http.Error(w, "pubkey is not 32 hex-encoded bytes (Ed25519)", http.StatusBadRequest)
		return
	}
	ceiling := 2
	if body.Ceiling != nil {
		ceiling = *body.Ceiling
	}

	r.mu.Lock()
	r.claws[id] = enrolled{pubKey: ed25519.PublicKey(raw), ceiling: ceiling}
	r.mu.Unlock()

	bound := "no"
	if strings.TrimSpace(body.AppleAccount) != "" {
		bound = "yes (value not logged — PII)"
	}
	log.Printf("enrolled %s: owner=%s user=%s ceiling=%d purchaser-bound=%s", id, body.Owner, body.User, ceiling, bound)

	writeJSON(w, http.StatusOK, map[string]any{"id": id, "ceiling": ceiling})
}

// handleAttest is the challenge–response, in the same two phases over one route
// that the real door uses.
func (r *registry) handleAttest(w http.ResponseWriter, req *http.Request) {
	id := req.PathValue("id")
	raw, err := io.ReadAll(io.LimitReader(req.Body, 1<<16))
	if err != nil {
		http.Error(w, "bad request", http.StatusBadRequest)
		return
	}
	var body struct {
		Nonce string `json:"nonce"`
		Sig   string `json:"sig"`
	}
	if len(raw) > 0 {
		if err := json.Unmarshal(raw, &body); err != nil {
			http.Error(w, "bad request", http.StatusBadRequest)
			return
		}
	}

	// Challenge phase. A nonce is issued for ANY id: whether a claw is enrolled
	// is only ever revealed at verify, never here.
	if body.Sig == "" {
		var n [32]byte
		if _, err := rand.Read(n[:]); err != nil {
			http.Error(w, "unavailable", http.StatusServiceUnavailable)
			return
		}
		r.mu.Lock()
		r.challenges[id] = n[:]
		r.mu.Unlock()
		writeJSON(w, http.StatusOK, map[string]any{"nonce": hex.EncodeToString(n[:])})
		return
	}

	// Verify phase. Every failure gets the one uniform refusal.
	r.mu.Lock()
	claw, known := r.claws[id]
	stored, outstanding := r.challenges[id]
	delete(r.challenges, id)
	r.mu.Unlock()

	if !known || !outstanding {
		attestFailed(w)
		return
	}
	presented, err := hex.DecodeString(body.Nonce)
	if err != nil || subtle.ConstantTimeCompare(presented, stored) != 1 {
		attestFailed(w)
		return
	}
	sig, err := hex.DecodeString(body.Sig)
	if err != nil || !ed25519.Verify(claw.pubKey, stored, sig) {
		attestFailed(w)
		return
	}

	var t [32]byte
	if _, err := rand.Read(t[:]); err != nil {
		http.Error(w, "unavailable", http.StatusServiceUnavailable)
		return
	}
	token := hex.EncodeToString(t[:])
	r.mu.Lock()
	r.sessions[token] = id
	r.mu.Unlock()
	log.Printf("attested %s — session issued", id)
	writeJSON(w, http.StatusOK, map[string]any{"session": token, "expires_in": int(sessionTTL.Seconds())})
}

// attestFailed is the single uniform refusal. The body must not tell a prober
// which of the five failure modes it hit.
func attestFailed(w http.ResponseWriter) {
	http.Error(w, "attestation required", http.StatusUnauthorized)
}

// resolve returns the claw id a bearer token is bound to.
func (r *registry) resolve(authorization string) (string, bool) {
	token, ok := strings.CutPrefix(authorization, "Bearer ")
	if !ok {
		return "", false
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	id, known := r.sessions[strings.TrimSpace(token)]
	return id, known
}

// key returns an enrolled claw's device public key.
func (r *registry) key(id string) (ed25519.PublicKey, bool) {
	r.mu.Lock()
	defer r.mu.Unlock()
	c, ok := r.claws[id]
	return c.pubKey, ok
}

// handleBrief serves a brief body for an outward GET.
//
// The brief id and version are ECHOED FROM THE REQUEST, because the claw
// checks them back against the SIGNED frame that announced the brief. A double
// that invented its own would be caught by that check, which is exactly what
// the check is for.
func (f *facade) handleBrief(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	version := uint32(1)
	if v := r.URL.Query().Get("version"); v != "" {
		parsed, err := strconv.ParseUint(v, 10, 32)
		if err != nil {
			http.Error(w, "version is not a number", http.StatusBadRequest)
			return
		}
		version = uint32(parsed)
	}

	items := make([]map[string]any, 0, len(wants))
	for i, want := range wants {
		items = append(items, map[string]any{"id": i + 1, "want": want})
	}
	body := map[string]any{
		"brief_id": id,
		"version":  version,
		"items":    items,
	}

	// ⚠ THE COMPROMISED-FACADE MODES. Each of these adds a field that tries to
	// tell ghillie HOW TO BEHAVE rather than what to ask. The claw's decoder
	// cannot represent any of them and refuses the brief whole.
	switch f.brief {
	case briefConductWait:
		body["wait_time_ms"] = 800
	case briefConductInterrupt:
		body["may_interrupt"] = true
	case briefConductDisclose:
		body["explain_mechanism"] = true
	case briefItems:
	}
	if f.brief != briefItems {
		log.Printf("⚠ serving brief %s with CONDUCT smuggled in (%s) — the claw should refuse it whole", id, f.brief)
	} else {
		log.Printf("serving brief %s revision %d — %d item(s), no conduct", id, version, len(items))
	}

	writeJSON(w, http.StatusOK, body)
}

// handleCredit answers the credit question. THE FACADE'S ANSWER IS THE
// DECISION; whatever the claw displays locally is a courtesy.
func (f *facade) handleCredit(w http.ResponseWriter, r *http.Request) {
	act := r.URL.Query().Get("act")
	if f.credit {
		log.Printf("credit: %s may %s", r.PathValue("id"), act)
		writeJSON(w, http.StatusOK, map[string]any{
			"sufficient": true,
			"balance":    f.balance,
			"note":       "the factory has credit for this",
		})
		return
	}
	log.Printf("credit: %s may NOT %s — the factory says no", r.PathValue("id"), act)
	writeJSON(w, http.StatusPaymentRequired, map[string]any{
		"sufficient": false,
		"balance":    0,
		"note":       "there is no credit on this account for an interview — top up in the companion app and ghillie will pick it up on the next poll",
	})
}

// handleSpecs receives a signed answer submission.
//
// ★ IT VERIFIES THE SIGNATURE OVER SigningBytes, which is the only thing that
// makes a submission a submission. Beyond that it stores nothing and scores
// nothing: the real receiver does not exist yet.
func (f *facade) handleSpecs(w http.ResponseWriter, r *http.Request) {
	var sub protocol.Submission
	if err := json.NewDecoder(io.LimitReader(r.Body, 1<<20)).Decode(&sub); err != nil {
		http.Error(w, "bad submission body", http.StatusBadRequest)
		return
	}
	if !f.receive(sub) {
		http.Error(w, "submission signature does not verify", http.StatusUnauthorized)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"received": sub.SubmissionID})
}

// receive verifies and logs one submission, whichever route it came by. It
// reports whether the submission was accepted.
func (f *facade) receive(sub protocol.Submission) bool {
	pub, known := f.registry.key(sub.ClawID)
	if !known {
		log.Printf("⚠ submission %s names an unenrolled claw %s — rejected", sub.SubmissionID, sub.ClawID)
		return false
	}
	sig, err := hex.DecodeString(sub.Signature)
	if err != nil || !ed25519.Verify(pub, sub.SigningBytes(), sig) {
		log.Printf("⚠ submission %s from %s FAILED SIGNATURE VERIFICATION — rejected", sub.SubmissionID, sub.ClawID)
		return false
	}

	f.mu.Lock()
	if f.seen == nil {
		f.seen = map[string]bool{}
	}
	duplicate := f.seen[sub.SubmissionID]
	f.seen[sub.SubmissionID] = true
	f.mu.Unlock()

	if duplicate {
		log.Printf("submission %s already received — deduped", sub.SubmissionID)
		return true
	}

	log.Printf("submission %s from %s: brief %s revision %d, %d answer(s), signature VERIFIED",
		sub.SubmissionID, sub.ClawID, sub.BriefID, sub.Version, len(sub.Answers))
	for _, a := range sub.Answers {
		verdict := "NOT GOT"
		if a.Answered {
			verdict = "got"
		}
		cut := ""
		if a.CutShort {
			cut = "  [ghillie was cut off putting this one]"
		}
		log.Printf("   item %d  %-12s %-8s attempts=%d%s  %q", a.ItemID, a.State, verdict, a.Attempts, cut, truncate(a.Text, 72))
	}
	log.Printf("   (the factory scores this; ghillie did not and could not)")
	return true
}

// truncate shortens a string for the double's log.
func truncate(s string, n int) string {
	r := []rune(s)
	if len(r) <= n {
		return s
	}
	return string(r[:n]) + "…"
}

// writeJSON writes a JSON body, handling the encode error rather than
// discarding it.
func writeJSON(w http.ResponseWriter, status int, body any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	if err := json.NewEncoder(w).Encode(body); err != nil {
		log.Printf("encode response: %v", err)
	}
}

// errUnauthenticated is what an unattested caller gets when -require-auth is on.
var errUnauthenticated = errors.New("poll requires attestation")

// requireSession wraps a handler so it refuses anything without a session bound
// to the claw named in the path. It is the double growing the ONE piece of
// realism that was missing: v0's mock had no auth at all, which hid the fact
// that the claw sent no Authorization header and would have taken a 401 on
// every poll against the real door.
func (f *facade) requireSession(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if !f.requireAuth {
			next(w, r)
			return
		}
		id := r.PathValue("id")
		bound, ok := f.registry.resolve(r.Header.Get("Authorization"))
		if !ok || bound != id {
			http.Error(w, errUnauthenticated.Error(), http.StatusUnauthorized)
			return
		}
		next(w, r)
	}
}

// requireAnySession is requireSession for the routes whose {id} is NOT a claw
// id — the brief body (whose id is the brief's) and the submission door (which
// has no id at all).
//
// ★ THE SESSION IS NOT WHAT TIES A BRIEF TO A CLAW. The SIGNED FRAME is: it
// named the brief and the revision, the gate admitted it, and the claw checks
// the fetched body back against it. Trying to make the session do that job here
// would be putting authority on the unsigned GET, which is exactly what the
// frame exists to avoid.
func (f *facade) requireAnySession(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if !f.requireAuth {
			next(w, r)
			return
		}
		if _, ok := f.registry.resolve(r.Header.Get("Authorization")); !ok {
			http.Error(w, errUnauthenticated.Error(), http.StatusUnauthorized)
			return
		}
		next(w, r)
	}
}

// sessionTTL is how long the double's sessions last. It matches the real door's
// 30 minutes so a claw's re-attestation behaviour is exercised the same way.
const sessionTTL = 30 * time.Minute
