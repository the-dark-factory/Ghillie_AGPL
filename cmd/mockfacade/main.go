// Command mockfacade is a TEST DOUBLE. IT IS NOT THE FACADE.
//
// ═══════════════════════════════════════════════════════════════════════════
//
//	⚠  TEST DOUBLE — NOT THE REAL FACADE, NOT A PRODUCT, NOT DEPLOYABLE.
//
// It exists so the ghillie terminal can be exercised end-to-end without going
// anywhere near the factory tree. The real facade endpoints belong in
// ada-factory/cmd/specifier beside forge_wiring.go, and building them is a
// separately gated step that this repository deliberately does not take.
//
// What this double fakes, badly and on purpose:
//   - the signing key is derived from a COMMAND-LINE SEED so runs reproduce.
//     The real facade key is a real key.
//   - it runs the enrol/attest ceremony and requires an attested session, but
//     it does NOT check that an enrolment came from an owner (Claw_Enrolment_Pkg,
//     ledger 113) — and neither does the real door yet; that check is recorded
//     as missing. The claw refuses locally instead, which is not the same thing.
//   - the claw registry is in memory and evaporates on restart. The real facade
//     holds declared ceiling, key and last-seen per claw, so a vanished claw is
//     visible.
//   - it will happily issue instructions it knows will be refused, and will
//     happily smuggle CONDUCT into a brief on request. That is the entire point:
//     it plays the compromised facade so the gate and the conduct wall can be
//     watched saying no.
//   - the credit answer is a FLAG (-credit). The real one is a proven balance.
//
// ═══════════════════════════════════════════════════════════════════════════
//
// Usage (zsh):
//
//	mockfacade -addr 127.0.0.1:8787 -pubkey-out /tmp/facade.pub \
//	           -script 'Report_Status:1,Deliver_Artifact:2,Install_Artifact:3'
package main

import (
	"context"
	"crypto/ed25519"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"log"
	"net/http"
	"os"
	"os/signal"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/tonygair/ghillie/internal/frame"
	"github.com/tonygair/ghillie/internal/gate"
	"github.com/tonygair/ghillie/internal/protocol"
)

// defaultScript is the demonstration sequence: one permitted delivery, an
// over-ceiling install, a local-only spec-upload request, a rank-5 run-local,
// a replayed frame and a badly signed frame.
const defaultScript = "Report_Status:1," +
	"Deliver_Artifact:2," +
	"Install_Artifact:3," +
	"Run_Local_Code:4," +
	"Request_Spec_Upload:5," +
	"Deliver_Artifact:2," +
	"Deliver_Artifact:6:badsig"

// settings is every flag of the double.
type settings struct {
	addr        string
	seed        string
	pubkeyOut   string
	script      string
	perPoll     int
	brief       string
	credit      bool
	balance     int64
	requireAuth bool
}

// version is the release this binary was cut from, set at build time by the
// release path (-ldflags "-X main.version=vX.Y.Z").
var version = "dev"

func main() {
	var s settings
	showVersion := flag.Bool("version", false, "print the version and exit")
	flag.StringVar(&s.addr, "addr", "127.0.0.1:8787", "address to listen on (the FACADE listens; the terminal never does)")
	flag.StringVar(&s.seed, "seed", "mockfacade-test-key-do-not-use-anywhere-real", "seed for the test signing key")
	flag.StringVar(&s.pubkeyOut, "pubkey-out", "", "write the test facade public key (hex) to this file")
	flag.StringVar(&s.script, "script", defaultScript, "comma-separated instructions: Name:Seq[:badsig|badproto|unknown]")
	flag.IntVar(&s.perPoll, "per-poll", 1, "instructions to hand out per poll")
	flag.StringVar(&s.brief, "brief", string(briefItems), "what to serve for a brief: items | conduct-wait | conduct-interrupt | conduct-disclose (the conduct modes play the COMPROMISED facade)")
	flag.BoolVar(&s.credit, "credit", true, "whether the factory says there is credit for an interview")
	flag.Int64Var(&s.balance, "balance", 250, "the balance the FACADE reports — this is the number that decides, unlike the claw's courtesy figure")
	flag.BoolVar(&s.requireAuth, "require-auth", true, "refuse poll/brief/specs without an attested session, as the real facade door does")
	flag.Parse()

	if *showVersion {
		fmt.Printf("mockfacade %s\n", version)
		return
	}

	if err := run(s); err != nil {
		fmt.Fprintf(os.Stderr, "mockfacade: %v\n", err)
		os.Exit(1)
	}
}

func run(s settings) error {
	steps, err := parseScript(s.script)
	if err != nil {
		return fmt.Errorf("parse script: %w", err)
	}
	if s.perPoll < 1 {
		s.perPoll = 1
	}
	mode, err := parseBriefMode(s.brief)
	if err != nil {
		return err
	}

	signer := keyFromSeed(s.seed)
	impostor := keyFromSeed(s.seed + "/impostor")
	pub, ok := signer.Public().(ed25519.PublicKey)
	if !ok {
		return errors.New("derived signing key has no Ed25519 public half")
	}

	if s.pubkeyOut != "" {
		if err := os.WriteFile(s.pubkeyOut, []byte(hex.EncodeToString(pub)+"\n"), 0o600); err != nil {
			return fmt.Errorf("write public key: %w", err)
		}
	}

	f := &facade{
		signer:      signer,
		impostor:    impostor,
		steps:       steps,
		perPoll:     s.perPoll,
		brief:       mode,
		credit:      s.credit,
		balance:     s.balance,
		requireAuth: s.requireAuth,
		registry:    newRegistry(),
	}

	mux := http.NewServeMux()
	// Enrolment and attestation are SELF-AUTH — a claw presents a device key,
	// not a session — so they are mounted raw, exactly as the real door does.
	mux.HandleFunc("POST /claws/{id}/enrol", f.registry.handleEnrol)
	mux.HandleFunc("POST /claws/{id}/attest", f.registry.handleAttest)
	mux.HandleFunc("POST /claws/{id}/poll", f.requireSession(f.handlePoll))
	mux.HandleFunc("GET /claws/{id}/credit", f.requireSession(f.handleCredit))
	// The brief body and the submission door carry no {id}, so the session need
	// only be a session — which is why the SIGNED FRAME, not this route, is what
	// ties a brief to the claw it was meant for.
	mux.HandleFunc("GET /briefs/{id}", f.requireAnySession(f.handleBrief))
	mux.HandleFunc("POST /specs", f.requireAnySession(f.handleSpecs))

	addr := s.addr
	srv := &http.Server{
		Addr:              addr,
		Handler:           mux,
		ReadHeaderTimeout: 10 * time.Second,
	}

	log.SetFlags(0)
	log.SetPrefix("mockfacade │ ")
	log.Printf("⚠  TEST DOUBLE — NOT THE REAL FACADE")
	log.Printf("listening on http://%s%s", addr, protocol.PollPath("{id}"))
	log.Printf("test signing key (public): %s", hex.EncodeToString(pub))
	if s.pubkeyOut != "" {
		log.Printf("public key written to %s — pin it on the terminal side", s.pubkeyOut)
	}
	log.Printf("script: %d instruction(s), %d per poll", len(steps), s.perPoll)
	log.Printf("brief mode: %s   credit: %v (balance %d)   require-auth: %v", mode, s.credit, s.balance, s.requireAuth)

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	errc := make(chan error, 1)
	go func() {
		if err := srv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			errc <- fmt.Errorf("listen: %w", err)
			return
		}
		errc <- nil
	}()

	select {
	case err := <-errc:
		return err
	case <-ctx.Done():
	}

	shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := srv.Shutdown(shutdownCtx); err != nil {
		return fmt.Errorf("shutdown: %w", err)
	}
	return <-errc
}

// step is one scripted instruction plus whatever the double should do to spoil
// it.
type step struct {
	command  gate.Command
	seq      uint64
	badSig   bool // sign with a key the terminal has not pinned
	badProto bool // claim a protocol version the terminal does not speak
	unknown  bool // a command byte outside the proven enumeration
	raw      string
}

// facade is the in-memory double.
type facade struct {
	signer   ed25519.PrivateKey
	impostor ed25519.PrivateKey
	perPoll  int

	brief       briefMode
	credit      bool
	balance     int64
	requireAuth bool
	registry    *registry

	mu    sync.Mutex
	steps []step
	next  int
	seen  map[string]bool // submission ids already received, for dedupe
}

// handlePoll answers one outward poll: log any reported outcomes, then hand out
// the next slice of the script.
//
// The 204 on an exhausted script is the NON-DISCLOSURE IDENTITY: "nothing for
// you" and "there are instructions you may not have" must look identical on the
// wire, so both are an empty 204 with no body.
func (f *facade) handlePoll(w http.ResponseWriter, r *http.Request) {
	clawID := r.PathValue("id")

	var req protocol.PollRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "bad poll body", http.StatusBadRequest)
		return
	}
	for _, o := range req.Reports {
		if o.Admitted {
			log.Printf("report from %s: seq %d %s ADMITTED — %s", clawID, o.Seq, o.CommandName, o.Note)
			continue
		}
		log.Printf("report from %s: seq %d %s REFUSED [%s] — %s", clawID, o.Seq, o.CommandName, o.Reason, o.Note)
	}
	// Submissions that rode the poll because the direct /specs post could not
	// land. Same signed object, same verification — only the route differs.
	for _, sub := range req.Answers {
		log.Printf("submission %s arrived ON THE POLL (the direct post had not landed)", sub.SubmissionID)
		f.receive(sub)
	}

	batch := f.take()
	if len(batch) == 0 {
		w.WriteHeader(http.StatusNoContent)
		return
	}

	resp := protocol.PollResponse{Instructions: make([]protocol.Envelope, 0, len(batch))}
	for _, s := range batch {
		env, err := f.issue(s)
		if err != nil {
			log.Printf("issue %s: %v", s.raw, err)
			continue
		}
		log.Printf("issuing to %s: %s seq %d%s", clawID, s.command, s.seq, spoilLabel(s))
		resp.Instructions = append(resp.Instructions, env)
	}

	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(resp); err != nil {
		log.Printf("encode poll response: %v", err)
	}
}

// take pops the next slice of the script.
func (f *facade) take() []step {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.next >= len(f.steps) {
		return nil
	}
	end := min(f.next+f.perPoll, len(f.steps))
	batch := f.steps[f.next:end]
	f.next = end
	return batch
}

// issue builds and signs one instruction frame. The signature covers the exact
// 22 bytes and nothing else.
func (f *facade) issue(s step) (protocol.Envelope, error) {
	protoVersion := uint8(frame.ProtocolV0)
	if s.badProto {
		protoVersion = 99
	}
	commandByte := uint8(s.command)
	if s.unknown {
		commandByte = 200
	}

	instr := frame.Instruction{
		Command:     commandByte,
		Protocol:    protoVersion,
		ArtifactRef: 0xDEADBEEFCAFEF00D,
		Version:     7,
		Seq:         s.seq,
	}
	wire := instr.Marshal()

	key := f.signer
	if s.badSig {
		// Signed with a perfectly valid Ed25519 key that this terminal has
		// simply never pinned — the realistic shape of the attack, rather than
		// random bytes in the signature field.
		key = f.impostor
	}
	sig := ed25519.Sign(key, wire[:])

	return protocol.Envelope{
		Frame:     hex.EncodeToString(wire[:]),
		Signature: hex.EncodeToString(sig),
	}, nil
}

// keyFromSeed derives a deterministic test key. A REAL facade key is generated,
// not derived from a string on a command line; this exists only so a demo run
// reproduces byte for byte.
func keyFromSeed(seed string) ed25519.PrivateKey {
	sum := sha256.Sum256([]byte(seed))
	return ed25519.NewKeyFromSeed(sum[:])
}

// parseScript reads the instruction script: comma-separated Name:Seq[:spoiler].
func parseScript(s string) ([]step, error) {
	var out []step
	for _, tok := range strings.Split(s, ",") {
		tok = strings.TrimSpace(tok)
		if tok == "" {
			continue
		}
		parts := strings.Split(tok, ":")
		if len(parts) < 2 || len(parts) > 3 {
			return nil, fmt.Errorf("%q: want Name:Seq[:spoiler]", tok)
		}
		command, ok := gate.ParseCommand(parts[0])
		if !ok {
			return nil, fmt.Errorf("%q: unknown command %q (use the Ada enumeration names)", tok, parts[0])
		}
		seq, err := strconv.ParseUint(parts[1], 10, 64)
		if err != nil {
			return nil, fmt.Errorf("%q: sequence: %w", tok, err)
		}
		st := step{command: command, seq: seq, raw: tok}
		if len(parts) == 3 {
			switch strings.ToLower(parts[2]) {
			case "badsig":
				st.badSig = true
			case "badproto":
				st.badProto = true
			case "unknown":
				st.unknown = true
			default:
				return nil, fmt.Errorf("%q: unknown spoiler %q (badsig, badproto, unknown)", tok, parts[2])
			}
		}
		out = append(out, st)
	}
	if len(out) == 0 {
		return nil, errors.New("empty script")
	}
	return out, nil
}

// spoilLabel names what the double did to an instruction, for its own log.
func spoilLabel(s step) string {
	switch {
	case s.badSig:
		return "  [signed with an UNPINNED key]"
	case s.badProto:
		return "  [protocol version 99]"
	case s.unknown:
		return "  [command byte 200 — outside the enumeration]"
	default:
		return ""
	}
}
