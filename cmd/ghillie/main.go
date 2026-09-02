// Command ghillie is the customer-side terminal of the ghillie⇄facade protocol,
// and — as of v1 — the interviewer that runs on it.
//
// IT POLLS OUTWARD, ALWAYS. This machine opens every connection to the factory
// facade; the facade answers. There is no listener, no open port and no inbound
// path here — NO-REMOTE-REACH is architectural before it is proven. Inside a
// connection this terminal itself opened, every instruction is then put through
// the proven admission gate (Facade_Command_Pkg, ada-factory ledger 112) and
// the proven anti-replay decider (Poll_Freshness_Pkg, ledger 120), and anything
// this machine has not agreed to is refused.
//
// A refused instruction is not silently dropped. It is shown to the user with
// its reason class, and reported to the facade on the next poll.
//
// Nothing this terminal receives is ever executed. Delivered artifacts land in
// a quarantine directory as notices; there is no installer and no executor.
//
// ★ v1 ADDS THE INTERVIEW. A Deliver_Artifact frame whose ArtifactRef names an
// interview brief causes the terminal to fetch that brief by a separate outward
// GET, conduct the interview locally, and submit the answers signed with its
// device key. The brief supplies the QUESTIONS. It cannot supply the CONDUCT:
// how long ghillie waits, whether it may interrupt, what it discloses are
// compiled into this binary, and a brief that tries to carry any of them fails
// to parse and is refused whole.
//
// ★ -glass OPENS ONE LOOPBACK LISTENER, AND ONLY THAT. The interview can be
// carried through an attached browser chat page (the OpenClaw gateway chat
// slice, internal/glass) instead of this console. The page is the person AT
// THIS MACHINE — the keyboard by another door — so the no-remote-reach promise
// above survives it: -glass-addr must be loopback, and anything else is
// refused at startup, before a key or a state file is touched. The facade path
// stays outward-only; nothing the factory sends can reach this listener.
//
// Usage (zsh):
//
//	ghillie -facade http://127.0.0.1:8787 \
//	        -claw-id demo-claw \
//	        -facade-key-file /tmp/facade.pub \
//	        -owner-id acme-it -user-id tony \
//	        -ceiling Deliver_Artifact \
//	        -consent None \
//	        -interview \
//	        -poll 1s -idle-exit 2
package main

import (
	"bufio"
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"flag"
	"fmt"
	"log"
	"net"
	"net/http"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	"github.com/tonygair/ghillie/internal/admit"
	"github.com/tonygair/ghillie/internal/bind"
	"github.com/tonygair/ghillie/internal/bundle"
	"github.com/tonygair/ghillie/internal/credit"
	"github.com/tonygair/ghillie/internal/ears"
	"github.com/tonygair/ghillie/internal/enrol"
	"github.com/tonygair/ghillie/internal/fill"
	"github.com/tonygair/ghillie/internal/gate"
	"github.com/tonygair/ghillie/internal/glass"
	"github.com/tonygair/ghillie/internal/guard"
	"github.com/tonygair/ghillie/internal/identity"
	"github.com/tonygair/ghillie/internal/interview"
	"github.com/tonygair/ghillie/internal/keys"
	"github.com/tonygair/ghillie/internal/locale"
	"github.com/tonygair/ghillie/internal/terminal"
	"github.com/tonygair/ghillie/internal/voice"
)

// glassDeciderEnv names the proven bind-policy front (Glass_Bind_Policy_Pkg,
// ledger 207): the binary that answers whether the glass listener may bind
// where it was asked to. No decider ⇒ no listener, ever — fail closed.
const glassDeciderEnv = "GLASS_BIND_DECIDER"

// options is every flag, gathered so run has one parameter rather than
// fourteen. Named-return discipline applies to the functions below; this is the
// same instinct applied to arguments.
type options struct {
	facadeURL   string
	clawID      string
	keyFile     string
	deviceSeed  string
	deviceKey   string
	encryptKey  string
	ownerID     string
	userID      string
	appleRef    string
	revoked     bool
	ceilingArg  string
	consentArg  string
	quarantine  string
	stateFile   string
	pollEvery   time.Duration
	maxPolls    int
	idleExit    int
	doInterview bool
	doEnrol     bool
	enrolCode   string
	portalURL   string
	courtesy    int64

	voice             bool
	voiceSet          bool // -voice was given explicitly (either way)
	voiceAuto         bool // the voice is on by the Mac default, not by request
	textOnly          bool
	voicePipelineDir  string
	voiceFallbackText bool
	scotsModel        string
	installAbility    string
	admitFloor        string
	admitAdvisory     bool
	reprove           bool
	catalogueBase     string
	listCatalogue     bool
	getAbility        string
	listAbilities     bool
	removeAbility     string

	ears              bool
	earsModel         string
	earsWhisperURL    string
	earsAdoptExternal bool

	talk              bool
	mindURL           string
	mindModel         string
	mindAdoptExternal bool

	glass       bool
	glassAddr   string
	glassOrigin string

	allowFill  string
	fillAnswer string
	revokeFill string
	listFills  bool

	stateSet   bool   // -state was named explicitly; the home notice is then not ours to give
	homeNotice string // the one line resolveStateDir wants said, if any
}

// version is the release this binary was cut from. It is set at build time by
// the release path (-ldflags "-X main.version=vX.Y.Z"); a binary built any other
// way says so rather than claiming a tag it does not have.
var version = "dev"

func main() {
	var o options

	// ★ THE STATE LIVES IN A PLACE, NOT IN A CWD (v0.1.2). Resolved before the
	// flags are declared so `-h` prints the real paths this run would use, and
	// so an operator naming -state still wins over all of it.
	cwd, cwdErr := os.Getwd()
	if cwdErr != nil {
		cwd = ""
	}
	stateDir, homeNotice := resolveStateDir(ghillieHome(), cwd, pathExists)
	o.homeNotice = homeNotice

	showVersion := flag.Bool("version", false, "print the version and exit")
	flag.StringVar(&o.facadeURL, "facade", "", "facade base URL, e.g. https://facade.example (required)")
	flag.StringVar(&o.clawID, "claw-id", "", "this CLAW's id — the device, not the person (required)")
	flag.StringVar(&o.keyFile, "facade-key-file", "", "file holding the facade signing public key, hex — an EXPLICIT OVERRIDE; when absent the key pinned at enrolment (trust on first use) is used")
	flag.StringVar(&o.deviceSeed, "device-seed", "", "derive the device key from a string — a DEMO AFFORDANCE so scripted runs reproduce byte for byte; a real install omits this and gets a generated key kept in -device-key-file")
	flag.StringVar(&o.deviceKey, "device-key-file", "", "file holding this claw's Ed25519 device key seed, hex — generated once (0600) when absent (default: ghillie-device.key beside the state file)")
	flag.StringVar(&o.encryptKey, "encrypt-key-file", "", "file holding this claw's X25519 encryption identity (age format) — generated once (0600) when absent (default: ghillie-encrypt.key beside the state file); confidential deliveries are sealed to it, and its public half is published signed by the device key")
	flag.StringVar(&o.ownerID, "owner-id", "", "the ENROLLING OWNER — sets the ceiling, holds enrolment and revocation (ledger 113)")
	flag.StringVar(&o.userID, "user-id", "", "the PERSON AT THE KEYBOARD — supplies consent, revocable in absentia (ledger 115)")
	flag.StringVar(&o.appleRef, "apple-account-ref", "", "the Apple Account that bought the credits — PII: bound at enrolment, never logged, never reported, never quarantined")
	flag.BoolVar(&o.revoked, "user-revoked", false, "treat the person at this keyboard as REVOKED (ledger 115) — for demonstrating that a revocation bites without anything from them")
	flag.StringVar(&o.ceilingArg, "ceiling", gate.DefaultCeiling.String(), "the most this machine will ever permit: an Ada command name")
	flag.StringVar(&o.consentArg, "consent", gate.NoConsent.String(), "human consent present at this machine: None, Session or Fresh_Explicit")
	flag.StringVar(&o.quarantine, "quarantine", filepath.Join(stateDir, "quarantine"), "directory for delivered artifacts — nothing here is ever executed")
	flag.StringVar(&o.stateFile, "state", filepath.Join(stateDir, "ghillie-state.json"), "where the last-seen sequence number is kept, and the directory the device key, encryption key and abilities live beside (default: the ghillie home, $GHILLIE_HOME or ~/.ghillie)")
	flag.DurationVar(&o.pollEvery, "poll", 5*time.Minute, "idle poll interval")
	flag.IntVar(&o.maxPolls, "max-polls", 0, "stop after this many polls (0 = run until interrupted)")
	flag.IntVar(&o.idleExit, "idle-exit", 0, "stop after this many consecutive empty polls (0 = never)")
	flag.BoolVar(&o.doInterview, "interview", false, "conduct the interview a delivered brief describes, on this terminal")
	flag.BoolVar(&o.doEnrol, "enrol", false, "enrol this claw before polling — AN OWNER ACT (ledger 113); requires -owner-id")
	flag.StringVar(&o.enrolCode, "enrol-code", "", "BIND THIS MACHINE TO YOUR MEMBERSHIP: give the pairing code from your account page and this ghillie signs it with its own device key and offers it to the portal — a standalone verb, it binds and exits. Nothing secret is sent: the factory learns this machine's PUBLIC key and nothing else. Not to be confused with -enrol, which is the separate facade ceremony (ledger 113)")
	flag.StringVar(&o.portalURL, "portal", bind.DefaultPortal, "the customer portal the pairing code came from (used by -enrol-code)")
	flag.Int64Var(&o.courtesy, "courtesy-credit", 0, "the balance to DISPLAY to the client. Advisory only: it authorises nothing, ever")
	flag.BoolVar(&o.voice, "voice", false, "speak ghillie's lines ALOUD through the settled breath pipeline (textplan + local playback); replies are still typed. Needs -interview. On a Mac with the pipeline present this is ON by default — -text-only is the off switch")
	flag.BoolVar(&o.textOnly, "text-only", false, "THE VOICE OFF SWITCH: ghillie never speaks — the whole interview in text alone. His lines render as text in every mode anyway; this switch chooses silence, not less information")
	flag.StringVar(&o.voicePipelineDir, "voice-pipeline-dir", defaultVoicePipeline(), "the respire checkout holding .venv-kokoro and demo/textplan.py (default: $GHILLIE_VOICE_PIPELINE, else <ghillie home>/respire)")
	flag.BoolVar(&o.voiceFallbackText, "voice-fallback-text", false, "if the voice pipeline fails mid-interview, carry on in text instead of stopping — OFF by default so a voiceless run is loud")
	flag.StringVar(&o.scotsModel, "scots-model", defaultScotsModel(), "Piper fine-tune for ghillie's Scots voice (empty = the pipeline's stock voice, honestly a placeholder)")
	flag.StringVar(&o.installAbility, "install-ability", "", "install a downloaded ability bundle (.tar.gz or directory) into this home's abilities — AN OWNER ACT, run by hand; verifies the five-part contract and records the ledger, then exits")
	flag.StringVar(&o.admitFloor, "admit-floor", "none", "the proof floor THIS machine demands of an ability before it may install: none | carried | reproved. It is your policy, not the ability's property — none is an honest answer and is the default, and raising it later needs no new decider and nobody's permission")
	flag.BoolVar(&o.admitAdvisory, "admit-advisory", false, "ADVISORY MODE: report what the admission gate would refuse, and install anyway. For seeing what a raised floor would cost before it costs it. Every advisory install is stamped into the ledger as an overridden refusal — permanently, and on purpose")
	flag.BoolVar(&o.reprove, "reprove", false, "RUNG B for a bundle that ships its proof: re-derive the proof from the shipped source with YOUR prover (zero unproved or no install), build the front with your toolchain, and check it against the shipped truth table. Without this, a proof-shipping bundle installs on RUNG A and the ledger says plainly: proofs carried, not re-derived here")
	flag.StringVar(&o.catalogueBase, "catalogue", defaultCatalogue(), "the catalogue to read: an https base or a local directory holding index.json")
	flag.BoolVar(&o.listCatalogue, "abilities-available", false, "list what the catalogue offers — name, what it needs, its HONEST proof status and cost; installs nothing")
	flag.StringVar(&o.getAbility, "get-ability", "", "fetch AND install one ability from the catalogue by name — AN OWNER ACT: the digest is checked against the index, the five-part contract verified, the ledger written; then exits")
	flag.BoolVar(&o.listAbilities, "abilities", false, "list what is installed here, with provenance from the ledger; anything installed outside the ledger is NAMED as such")
	flag.StringVar(&o.removeAbility, "remove-ability", "", "uninstall an installed ability by name — the owner's act, recorded in the ledger; this is also how an upgrade is done, visibly")
	flag.BoolVar(&o.ears, "ears", false, "capture ANSWERS from the microphone, transcribed locally by whisper. The mic opens only after a question's offered-floor breath; typed input remains the fallback and /cut stays typed. Needs -voice")
	flag.StringVar(&o.earsModel, "ears-model", defaultEarsModel(), "ggml whisper model file for local transcription (durable path, never /tmp; default: $GHILLIE_EARS_MODEL, else <ghillie home>/models/whisper/ggml-base.en.bin)")
	flag.StringVar(&o.earsWhisperURL, "ears-whisper-url", "http://127.0.0.1:8932", "resident whisper-server URL; when nothing answers there, ghillie starts one itself and stops it on exit; when that too is impossible it falls back to per-utterance whisper-cli, stating the latency cost")
	flag.StringVar(&o.allowFill, "allow-fill", "", "grant a STANDING RULE: brief items of this class are answered from -fill-answer without asking — AN OWNER ACT, run by hand at this machine, never settable over the wire; recorded, revocable, then exits")
	flag.StringVar(&o.fillAnswer, "fill-answer", "", "the standing answer -allow-fill licenses (required with it)")
	flag.StringVar(&o.revokeFill, "revoke-fill", "", "withdraw the licence for this item class — the answer is KEPT (he still knows; later briefs get it offered, not filled); an owner act, then exits")
	flag.BoolVar(&o.listFills, "fill-rules", false, "list the standing fill rules and their licence state; changes nothing, then exits")
	flag.BoolVar(&o.glass, "glass", false, "conduct the interview through an attached browser chat page (OpenClaw gateway chat slice) instead of this console. Needs -interview. The page is the person at THIS machine: the listener is loopback-only and a non-loopback -glass-addr is refused")
	flag.StringVar(&o.glassAddr, "glass-addr", "127.0.0.1:8788", "loopback address the chat page attaches to (ws://<addr>/ws)")
	flag.StringVar(&o.glassOrigin, "glass-origin", "", "host:port of ONE browser origin additionally allowed to open the glass socket (e.g. localhost:5173 for a dev page). Empty = same-host only — the ClawJacked defence: without this, no other page in the browser can drive the glass")
	flag.BoolVar(&o.earsAdoptExternal, "ears-adopt-external", false, "USE a whisper-server this process did not start. OFF by default: whatever holds that port is handed every word you speak, and it is not known to be whisper. Turn this on only when you know what is listening there. The URL must be loopback either way — your voice does not leave this machine.")
	flag.BoolVar(&o.talk, "talk", false, "free conversation with ghillie, using YOUR mind (see -mind-url) — a standalone verb: it talks, then exits. He decides nothing here: proofs, refusals, admissions and the conduct wall stay deterministic and never see the model. With no mind set he says so plainly and does nothing else")
	flag.StringVar(&o.mindURL, "mind-url", defaultMindURL(), "THE MIND IS YOURS: the inference endpoint on YOUR machine, e.g. the ollama you installed (default: $GHILLIE_MIND_URL, else http://127.0.0.1:11434). LOOPBACK-ONLY unless -mind-adopt-external. A downloaded ghillie never draws anyone else's inference; when nothing answers here he says so and everything else still works")
	flag.StringVar(&o.mindModel, "mind-model", defaultMindModel(), "which model answers (default: $GHILLIE_MIND_MODEL, else EMPTY — ask the server what it serves, take the first, and say out loud which one that was)")
	flag.BoolVar(&o.mindAdoptExternal, "mind-adopt-external", false, "SEND YOUR SITTING OFF THIS MACHINE. OFF by default, and think harder about this one than about -ears-adopt-external: the ears hear one answer, the MIND IS TOLD EVERYTHING — every question, every answer, the whole conversation, for as long as it lasts, in plain text to whatever holds that address. Turn this on only for a machine that is yours and that you would be content to have read the lot.")
	installUsage()
	flag.Parse()
	flag.Visit(func(f *flag.Flag) {
		switch f.Name {
		case "voice":
			o.voiceSet = true
		case "state":
			o.stateSet = true
		}
	})

	if *showVersion {
		fmt.Println(versionLine(version))
		return
	}

	if err := run(o); err != nil {
		fmt.Fprintf(os.Stderr, "ghillie: %v\n", err)
		os.Exit(1)
	}
}

func run(o options) error {
	log.SetFlags(0)
	log.SetPrefix("ghillie │ ")

	// ★ SAY WHERE THE IDENTITY IS, WHENEVER IT IS NOT THE OBVIOUS PLACE. An
	// operator who named -state gets no notice: they know where their state is,
	// and the resolution did not decide anything for them.
	if o.homeNotice != "" && !o.stateSet {
		log.Printf("%s", o.homeNotice)
	}

	// The state directory is made once, here, rather than by whichever writer
	// happens to run first — a claw whose home is created as a side effect of
	// key generation is a claw whose home depends on the order of the flags.
	if dir := filepath.Dir(o.stateFile); dir != "" && dir != "." {
		if err := os.MkdirAll(dir, 0o700); err != nil {
			return fmt.Errorf("create the ghillie home %s: %w", dir, err)
		}
	}

	// THE CATALOGUE'S OWNER-SIDE VERBS — each a standalone command that does
	// its one thing and exits. Reading the catalogue installs nothing;
	// installing and removing are the OWNER'S acts and are both recorded.
	if o.listCatalogue || o.getAbility != "" || o.listAbilities || o.removeAbility != "" {
		return runCatalogue(o)
	}

	// ★ THE MIND IS THE OWNER'S, AND SO IS THE VERB. -talk is standalone like
	// the catalogue verbs: it opens the endpoint the owner named at their own
	// machine, converses, and exits. It runs BEFORE the facade is required
	// because a downloaded ghillie with a mind and no facade is a complete
	// product — the ruling's whole point (Tony 2026-08-28).
	if o.talk {
		return runTalk(context.Background(), o, os.Stdin, os.Stdout)
	}

	// ★ BINDING TO A MEMBERSHIP IS AN OWNER ACT, and a standalone verb:
	// take the pairing code, sign it with this machine's device key, offer
	// it, print what the factory said, exit. It runs BEFORE the facade is
	// required because binding is between this machine and the PORTAL —
	// a ghillie with a membership and no facade is a perfectly good state,
	// and demanding -facade here would be demanding an unrelated thing.
	if o.enrolCode != "" {
		return runEnrolCode(context.Background(), o)
	}

	// ★ FILL RULES ARE OWNER ACTS — standalone verbs like enrolment and
	// ability-install: grant, revoke, or list, then exit. A rule arriving any
	// other way (a brief, a page, the mouth) does not exist.
	if o.allowFill != "" || o.revokeFill != "" || o.listFills {
		return runFillRules(o)
	}

	// ★ INSTALLING AN ABILITY IS AN OWNER ACT — a standalone command, like
	// enrolment: verify, install, record, exit. Download and install are two
	// events with the owner between them (internal/bundle).
	if o.installAbility != "" {
		abilities := filepath.Join(filepath.Dir(o.stateFile), "abilities")
		if err := os.MkdirAll(abilities, 0o755); err != nil {
			return fmt.Errorf("install-ability: %w", err)
		}
		src := o.installAbility
		if strings.HasSuffix(src, ".tar.gz") || strings.HasSuffix(src, ".tgz") {
			staging := filepath.Join(filepath.Dir(o.stateFile), "quarantine", "bundle-unpack")
			if err := os.MkdirAll(staging, 0o755); err != nil {
				return fmt.Errorf("install-ability: %w", err)
			}
			dir, err := bundle.Unpack(src, staging)
			if err != nil {
				return err
			}
			defer os.RemoveAll(staging)
			src = dir
		}
		proofNote, err := settleBundleProof(src, o.reprove)
		if err != nil {
			return err
		}
		floor, err := admit.ParseFloor(o.admitFloor)
		if err != nil {
			return err
		}
		// ★ THE OPERATOR'S OWN HAND IS THE ATTESTATION on this path. A person
		// reached into their own filesystem and named a directory; no index
		// vouches for it and none needs to. What changes is that the digest of
		// what was installed is now computed and recorded, so a developer
		// install stops being the one install nobody can reconstruct.
		gate := admit.GateFor(context.Background(), admit.Candidate{
			SourceDir:         src,
			OperatorRequested: true,
			ProverRanClean:    proofNote == bundle.ProofReproved,
			RunsAtHostTrust:   proofNote == bundle.ProofReproved,
			Floor:             floor,
		}, "", o.admitAdvisory)
		entry, err := bundle.Install(src, abilities, o.installAbility, proofNote, gate)
		if err != nil {
			return err
		}
		log.Printf("installed %s (hash %s…) — recorded in the ledger; its tab forms when a display next looks", entry.Name, entry.Hash[:12])
		logAdmission(entry)
		if entry.Proof != "" {
			log.Printf("  proof: %s", entry.Proof)
		}
		return nil
	}

	// ★ THE MAC SPEAKS BY DEFAULT (Tony, 2026-08-26) — and the off switch is
	// prominent: -text-only always wins. An explicit -voice keeps its old loud
	// contract; a defaulted voice must never cost the sitting, so it carries
	// text fallback with it. Text renders in every mode regardless.
	{
		platformOK, _ := voice.Platform()
		speak, fallback, auto, verr := settleVoice(o.voiceSet, o.voice, o.textOnly,
			o.doInterview, o.glass, o.ears, platformOK,
			voicePipelinePresent(o.voicePipelineDir))
		if verr != nil {
			return verr
		}
		o.voice, o.voiceAuto = speak, auto
		if auto {
			o.voiceFallbackText = fallback
		}
	}

	// ★ PLATFORM CHECK FIRST — before keys, before enrolment, before anything
	// is written to disk. Whether this build can speak is a STATIC FACT about
	// the binary; making somebody sort out a signing key and only then
	// discovering the voice surface was never available on their machine wastes
	// their time and reads like a fault in the product. Refuse loudly, and say
	// what still works.
	if o.voice || o.ears {
		if ok, why := voice.Platform(); !ok {
			return fmt.Errorf("-voice is not available here: %s", why)
		}
	}

	if o.facadeURL == "" || o.clawID == "" {
		return fmt.Errorf("-facade and -claw-id are required")
	}
	o.facadeURL = strings.TrimRight(o.facadeURL, "/")

	ceiling, ok := gate.ParseCommand(o.ceilingArg)
	if !ok {
		return fmt.Errorf("-ceiling %q is not a command in the proven enumeration (Report_Status, Offer_Catalogue, Deliver_Artifact, Request_Spec_Upload, Install_Artifact, Run_Local_Code)", o.ceilingArg)
	}
	consentLevel, ok := gate.ParseConsent(o.consentArg)
	if !ok {
		return fmt.Errorf("-consent %q is not a consent level (None, Session, Fresh_Explicit)", o.consentArg)
	}

	// ⚠ CONSENT FROM A FLAG IS A STUB, NOT THE DESIGN. Fresh_Explicit consent
	// means a human at this machine was asked about THIS act and said yes; a
	// flag is a standing answer to a question nobody asked. What v1 changed is
	// that the SOURCE is now an interface called on the gate path, so the real
	// per-act prompt drops in without the gate path moving. The value is at
	// least still LOCAL — never taken from the wire.
	if consentLevel == gate.FreshExplicit {
		log.Printf("⚠ consent is Fresh_Explicit from a COMMAND-LINE FLAG — still a stub; the real thing is a per-act local prompt")
	}

	// The EXPLICIT facade key, when the operator gave one. It is the authority
	// when present; when absent, the key pinned at enrolment (trust on first
	// use) carries the trust instead — see resolveFacadeKey below, which runs
	// AFTER enrolment because that is when a door presents its key.
	var explicitKey ed25519.PublicKey
	if o.keyFile != "" {
		var err error
		explicitKey, err = loadFacadeKey(o.keyFile)
		if err != nil {
			return err
		}
	}

	// ★ FOUR IDENTITIES, KEPT APART. The claw is this device. The owner enrolled
	// it. The user is at the keyboard. The Apple account bought the credits and
	// is PII — it is bound at enrolment and appears in no log, no report and no
	// quarantine file.
	binding := identity.Binding{
		Claw:  identity.ClawID(o.clawID),
		Owner: identity.OwnerID(defaultTo(o.ownerID, o.clawID+"-owner")),
		User:  identity.UserID(defaultTo(o.userID, "user-at-"+o.clawID)),
		Apple: identity.NewAppleAccountRef(o.appleRef),
	}

	// The DEVICE KEY is the claw's identity. The real path generates it once,
	// keeps it 0600 beside the state file, and never derives it from anything —
	// that is what makes the claw an identity rather than a name. -device-seed
	// remains as the demo affordance it always was, now opt-in and said out
	// loud when used.
	var deviceKey ed25519.PrivateKey
	if o.deviceSeed != "" {
		log.Printf("⚠ device key derived from -device-seed — a DEMO AFFORDANCE so scripted runs reproduce; a real install keeps a generated key")
		deviceKey = deviceKeyFromSeed(o.deviceSeed)
	} else {
		path := defaultTo(o.deviceKey, filepath.Join(filepath.Dir(o.stateFile), "ghillie-device.key"))
		key, created, err := loadOrCreateDeviceKey(path)
		if err != nil {
			return err
		}
		if created {
			log.Printf("device key generated and kept at %s (0600) — the claw's identity now lives on this device and nowhere else", path)
		}
		deviceKey = key
	}

	// The ENCRYPTION KEY is deliberately a second key: confidential deliveries
	// are sealed to it, and it cannot sign — the device key signs its public
	// half so nobody can be handed an impostor recipient. Generated once,
	// 0600, beside the device key, same lifecycle.
	encPath := defaultTo(o.encryptKey, filepath.Join(filepath.Dir(o.stateFile), "ghillie-encrypt.key"))
	encID, encCreated, err := keys.LoadOrCreate(encPath)
	if err != nil {
		return err
	}
	if encCreated {
		log.Printf("encryption key generated and kept at %s (0600) — confidential deliveries seal to this device and open nowhere else", encPath)
	}
	encBinding := keys.Bind(encID, deviceKey)
	_ = encBinding // published at enrolment; the install path opens with encID.

	enrolState := gate.Unenrolled
	if !o.doEnrol {
		// Not being asked to enrol means this machine believes it already is —
		// which is also what stops a second enrolment (NO-TWO-MASTERS).
		enrolState = gate.Enrolled
	}
	client, err := enrol.New(o.facadeURL, binding, deviceKey, enrolState, nil)
	if err != nil {
		return err
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	if o.doEnrol {
		if o.ownerID == "" {
			return fmt.Errorf("-enrol needs -owner-id: ENROLMENT IS AN OWNER ACT (Claw_Enrolment_Pkg, ledger 113) and this machine will not pretend an owner was present")
		}
		// The requester is the enrolling owner and the claim is authentic
		// because the operator ran this command at this machine. Both are
		// handed to the proven core rather than assumed by it.
		if err := client.Enrol(ctx, gate.ActorEnrollingOwner, true, ceiling); err != nil {
			return err
		}
		log.Printf("enrolled — %s at ceiling %s (an owner act; the purchaser reference travelled in the enrolment payload and nowhere else)", binding, ceiling)
	}

	// Resolve which facade key this terminal trusts, now that enrolment — the
	// moment a door presents its key — has had its chance to happen.
	facadeKey, note, err := resolveFacadeKey(explicitKey, client.FacadeKey(), o.keyFile, facadePinPath(o.stateFile))
	if err != nil {
		return err
	}
	if note != "" {
		log.Printf("%s", note)
	}

	standing := gate.InGoodStanding
	if o.revoked {
		standing = gate.RevokedStanding
	}

	cfg := terminal.Config{
		FacadeURL:     o.facadeURL,
		ClawID:        o.clawID,
		Ceiling:       ceiling,
		Consent:       terminal.StaticConsent(consentLevel),
		QuarantineDir: o.quarantine,
		StateFile:     o.stateFile,
		PollInterval:  o.pollEvery,
		MaxPolls:      o.maxPolls,
		IdleExit:      o.idleExit,
		Identity:      binding,
		UserStanding:  standing,
		Auth:          client,
		Signer:        client,
		Credit:        credit.NewHTTPAuthority(o.facadeURL, o.clawID, client, nil),
	}

	if o.glass && !o.doInterview {
		return fmt.Errorf("-glass needs -interview: the glass is an interview surface, and without an interview nothing on it would ever speak")
	}

	if o.doInterview {
		courtesy := credit.Courtesy{
			Units: o.courtesy,
			AsOf:  time.Now().UTC().Format(time.RFC3339),
			Stale: true, // this machine has not refreshed it; the factory holds the real one
		}
		var surface interview.Surface = interview.NewConsoleSurface(os.Stdin, os.Stdout)
		if o.ears && !o.voice {
			return fmt.Errorf("-ears needs -voice: the microphone opens only after a question's offered-floor breath, and only the voice surface plays one")
		}
		if o.glass && (o.voice || o.ears) {
			return fmt.Errorf("-glass and -voice are two mouths for one interview — pick one (-ears rides -voice)")
		}
		if o.glass {
			// The glass surface REPLACES the console: ghillie's lines become
			// chat events on the attached page, replies come from its composer,
			// and /cut works from the keyboard there exactly as it does here.
			// Conduct is untouched — interview.go cannot tell which surface it
			// holds, and the glass does not get a vote.
			srv, stopGlass, gerr := startGlass(o.glassAddr, o.glassOrigin)
			if gerr != nil {
				return gerr
			}
			defer stopGlass()
			surface = interview.NewGlassSurface(srv)
			// The startup banner must not claim "no open port" over an open
			// port: name the one loopback door honestly.
			cfg.LocalDoor = o.glassAddr
		}
		if o.voice {
			// The voice surface COMPOSES the console: ghillie's lines are
			// spoken through the settled pipeline (internal/voice), replies
			// are typed exactly as before, and the /cut affordance and the
			// no-deadline promise ride along untouched. Renders land under
			// the state dir — never /tmp.
			player, perr := voice.NewAFPlay()
			if perr != nil {
				return fmt.Errorf("-voice: %w", perr)
			}
			renderer := &voice.TextPlan{
				PythonPath: filepath.Join(o.voicePipelineDir, ".venv-kokoro", "bin", "python"),
				ScriptPath: filepath.Join(o.voicePipelineDir, "demo", "textplan.py"),
				RenderDir:  filepath.Join(filepath.Dir(o.stateFile), "ghillie-voice"),
			}
			if o.scotsModel != "" {
				// GHILLIE SPEAKS SCOTS — the bespoke Piper fine-tune in front
				// of the harness, breath and all
				// (feedback_ghillie_voice_must_be_scottish; the stock RP was
				// the placeholder, never the voice).
				renderer.PiperPath = filepath.Join(o.voicePipelineDir, ".venv-piper", "bin", "piper")
				renderer.PiperModel = o.scotsModel
			}
			vs := interview.NewVoiceSurface(renderer, player, voice.NewBreathBank(),
				interview.NewConsoleSurface(os.Stdin, os.Stdout), o.voiceFallbackText, log.Default())
			if o.ears {
				listener, stopEars, eerr := buildEars(ctx, o)
				if eerr != nil {
					return fmt.Errorf("-ears: %w", eerr)
				}
				defer stopEars()
				vs = vs.WithEars(listener)
			}
			surface = vs
		}
		// THE PROMINENT SWITCH — the voice state and how to flip it, stated at
		// every sitting, in whichever direction it currently points. The glass
		// says nothing here: it is another mouth, not a silenced one.
		if !o.glass {
			switch {
			case o.voice:
				what := "placeholder RP — honestly not yet his voice"
				if o.scotsModel != "" {
					what = "the Scots voice"
				}
				how := "asked for with -voice"
				if o.voiceAuto {
					how = "on by default on this Mac"
				}
				log.Printf("voice ON (%s; %s) — his lines render as text here regardless. THE SWITCH: -text-only turns the voice off", what, how)
			case o.textOnly:
				log.Printf("text only — the voice is off at your switch (-text-only)")
			default:
				if ok, _ := voice.Platform(); ok {
					log.Printf("text only — the voice pipeline is not on this machine (%s); text is the whole interview (a gap, not a fault)", o.voicePipelineDir)
				}
			}
		}
		// The attempt bound is COMPILED-IN CONDUCT, decided by the proven
		// attempt-bound core (internal/conduct/attempt_bound.go, admission
		// pending): ghillie puts an outstanding item at most
		// conduct.MaxAttempts times and then lets it lie, out loud. It is not
		// a parameter here and not a brief field, on purpose.
		// THE GUARD, in its settled posture: nudge them to be better humans,
		// if they can. The owner's term store decides what is bang-to-rights;
		// a trip earns one humane line and a quiet count, and service simply
		// continues. Active whenever a term store exists beside the state.
		{
			termsPath := filepath.Join(filepath.Dir(o.stateFile), "guard-terms.tsv")
			store, serr := guard.LoadStore(termsPath)
			if serr != nil {
				log.Printf("⚠ %v — the guard cannot detect this sitting (gap)", serr)
			}
			recordPath := filepath.Join(filepath.Dir(o.stateFile), "guard-record.json")
			record, rerr := guard.LoadRecord(recordPath)
			if rerr != nil {
				return fmt.Errorf("guard: %w", rerr)
			}
			surface = guard.WrapSurface(surface, guard.New(store, serr, recordPath, record, log.Default()))
		}
		cfg.Interview = interview.New(surface, courtesy, log.Default())

		// A ghillie fills in only the bits he does not know are his to say:
		// with the proven fill decider wired, every brief item is put to
		// Brief_Fill_Policy_Pkg first, and only the remainder is asked.
		// Unwired means everything is asked — said out loud as the gap it is.
		if decider := os.Getenv(fill.DeciderEnv); decider != "" {
			store, serr := fill.Load(fillRulesPath(o.stateFile))
			if serr != nil {
				log.Printf("⚠ fill rules unreadable: %v — the fill machinery will refuse every item and ask unaided", serr)
				store = &fill.Store{Path: fillRulesPath(o.stateFile)}
			}
			cfg.Interview = fill.Wrap(cfg.Interview, store, serr, decider, log.Default())
			log.Printf("fill: standing rules in force — each brief item decided by the proven Brief_Fill_Policy_Pkg (%s)", decider)
		} else {
			log.Printf("fill: %s unwired — every brief item will be asked (a gap, not a fault)", fill.DeciderEnv)
		}
	}

	t, err := terminal.New(cfg, facadeKey, log.Default())
	if err != nil {
		return err
	}
	return t.Run(ctx)
}

// buildEars assembles the listening path for -ears: microphone capture into
// the state dir (never /tmp) and a local whisper transcriber — the RESIDENT
// whisper-server for preference (model loads once, stays warm), whisper-cli
// per utterance as the stated-cost fallback. The returned stop function
// releases whatever was started; calling it is safe in every case.
func buildEars(ctx context.Context, o options) (listener *ears.Ears, stop func(), err error) {
	stop = func() {}
	if _, statErr := os.Stat(o.earsModel); statErr != nil {
		return nil, stop, fmt.Errorf("whisper model: %w (a small honest model such as ggml-base.en.bin belongs at a durable path — never /tmp)", statErr)
	}

	var transcriber ears.Transcriber
	resident, resErr := ears.StartResident(ctx, ears.ResidentConfig{
		URL:           o.earsWhisperURL,
		ModelPath:     o.earsModel,
		AdoptExternal: o.earsAdoptExternal,
	})
	if resErr == nil {
		if resident.External {
			log.Printf("ears: using the whisper-server already resident at %s", resident.URL)
		} else {
			log.Printf("ears: started a resident whisper-server at %s (model %s, warm for the whole interview; stopped on exit)", resident.URL, o.earsModel)
		}
		transcriber = &ears.WhisperServer{URL: resident.URL}
		stop = func() {
			if cerr := resident.Close(); cerr != nil {
				log.Printf("ears: %v", cerr)
			}
		}
	} else {
		cliPath, lookErr := exec.LookPath("whisper-cli")
		if lookErr != nil {
			return nil, stop, fmt.Errorf("no resident whisper-server (%v) and no whisper-cli on PATH (%v) — brew install whisper-cpp provides both", resErr, lookErr)
		}
		log.Printf("⚠ ears: no resident whisper-server (%v) — falling back to PER-UTTERANCE whisper-cli, which reloads the model every answer: expect roughly an extra half-second or more per reply", resErr)
		transcriber = &ears.WhisperCLI{BinPath: cliPath, ModelPath: o.earsModel}
	}

	return &ears.Ears{
		Capture: &ears.Capture{
			CaptureDir: filepath.Join(filepath.Dir(o.stateFile), "ghillie-ears"),
		},
		Transcriber: transcriber,
		Log:         log.Default(),
	}, stop, nil
}

// startGlass stands up the ONE listener ghillie ever opens: the chat page the
// person at this machine attaches to. Whether it MAY bind where it was asked
// is not decided here — the proven Glass_Bind_Policy_Pkg decides, through its
// front named by GLASS_BIND_DECIDER, and any answer other than a definite
// permit_loopback (including no decider, or a decider that fails) means no
// listener. This glue only classifies the parsed address, asks, and obeys.
func startGlass(addr, origin string) (srv *glass.Server, stop func(), err error) {
	stop = func() {}
	host, _, err := net.SplitHostPort(addr)
	if err != nil {
		return nil, stop, fmt.Errorf("-glass-addr %q: %w", addr, err)
	}
	class := glassHostClass(host)
	verdict, err := glassBindVerdict(class)
	if err != nil {
		return nil, stop, err
	}
	if verdict != "permit_loopback" {
		return nil, stop, fmt.Errorf("-glass-addr %q refused by the proven bind policy: %s — the glass is the person AT THIS MACHINE, and this terminal opens no listener a network can reach (give a loopback address such as 127.0.0.1:8788)", addr, verdict)
	}

	ln, err := net.Listen("tcp", addr)
	if err != nil {
		return nil, stop, fmt.Errorf("-glass listen %s: %w", addr, err)
	}
	srv = glass.NewServer("ghillie")
	if os.Getenv("GLASS_DEBUG") != "" {
		srv.Debug = log.Default()
	}
	if origin != "" {
		// The OPERATOR names the one extra page origin that may attach. The
		// default (same-host only) is the ClawJacked defence; this widens it
		// by exactly one stated origin, never by a wildcard.
		srv.OriginPatterns = []string{origin}
		log.Printf("glass: origin %q may attach, by your say-so", origin)
	}
	mux := http.NewServeMux()
	mux.Handle("/ws", srv)
	mux.HandleFunc("/healthz", func(w http.ResponseWriter, _ *http.Request) {
		if _, werr := w.Write([]byte("ok\n")); werr != nil {
			log.Printf("glass: healthz write: %v", werr)
		}
	})
	httpSrv := &http.Server{Handler: mux, ReadHeaderTimeout: 10 * time.Second}
	go func() {
		if serr := httpSrv.Serve(ln); serr != nil && !errors.Is(serr, http.ErrServerClosed) {
			log.Printf("glass: %v", serr)
		}
	}()
	stop = func() {
		if cerr := httpSrv.Close(); cerr != nil {
			log.Printf("glass: close: %v", cerr)
		}
	}
	log.Printf("glass: chat page may attach at ws://%s/ws — loopback only, permitted by the proven bind policy (%s)", addr, class)
	return srv, stop, nil
}

// glassHostClass establishes which Host_Class_Type word describes the PARSED
// bind host. This is the mechanical half the proven core's scope boundary
// assigns to glue: no policy lives here — a class nobody recognises, and any
// name this glue cannot parse as an address, goes to the decider as
// named_remote, where the proven default is refusal.
func glassHostClass(host string) string {
	if host == "" {
		return "unspecified_all"
	}
	ip := net.ParseIP(host)
	switch {
	case ip == nil:
		return "named_remote"
	case ip.IsUnspecified():
		return "unspecified_all"
	case ip.IsLoopback() && ip.To4() != nil:
		return "loopback_v4"
	case ip.IsLoopback():
		return "loopback_v6"
	default:
		return "named_remote"
	}
}

// glassBindVerdict asks the proven front. The front's exit contract is the
// deciders': 0 with one word on stdout is an answer; anything else means the
// decider could not be asked, and the caller must fail closed.
func glassBindVerdict(class string) (verdict string, err error) {
	decider := os.Getenv(glassDeciderEnv)
	if decider == "" {
		return "", fmt.Errorf("-glass needs %s: the bind decision belongs to the proven Glass_Bind_Policy_Pkg front, and without it this terminal opens no listener", glassDeciderEnv)
	}
	out, err := exec.Command(decider, "verdict", class).Output()
	if err != nil {
		return "", fmt.Errorf("glass bind decider would not answer for %q: %w — failing closed, no listener", class, err)
	}
	return strings.TrimSpace(string(out)), nil
}

// fillRulesPath is where the owner's standing fill rules live: beside the
// state file, like the facade pin and the device key — durable local facts.
func fillRulesPath(stateFile string) string {
	return filepath.Join(filepath.Dir(stateFile), "fill-rules.json")
}

// runFillRules serves the three fill-rule owner verbs: grant, revoke, list.
func runFillRules(o options) error {
	store, err := fill.Load(fillRulesPath(o.stateFile))
	if err != nil {
		return err
	}
	switch {
	case o.allowFill != "":
		if strings.TrimSpace(o.fillAnswer) == "" {
			return fmt.Errorf("-allow-fill needs -fill-answer: a rule licenses a KNOWN answer; without one there is nothing to fill")
		}
		if err := store.Allow(o.allowFill, o.fillAnswer, time.Now()); err != nil {
			return err
		}
		if err := store.Save(); err != nil {
			return err
		}
		log.Printf("standing rule granted: items of class %q are answered %q without asking — revoke with -revoke-fill %s", o.allowFill, o.fillAnswer, o.allowFill)
		return nil
	case o.revokeFill != "":
		if err := store.Revoke(o.revokeFill, time.Now()); err != nil {
			return err
		}
		if err := store.Save(); err != nil {
			return err
		}
		log.Printf("licence withdrawn for %q — the answer is kept: later briefs get it OFFERED, never filled", o.revokeFill)
		return nil
	default:
		if len(store.Rules) == 0 {
			log.Printf("no standing fill rules — every brief item is asked")
			return nil
		}
		for _, r := range store.Rules {
			state := "licensed " + r.GrantedAt
			if !r.Licensed {
				state = "REVOKED " + r.RevokedAt + " (answer kept; offered, not filled)"
			}
			log.Printf("%-24s %-40q %s", r.ItemClass, r.Answer, state)
		}
		return nil
	}
}

// defaultTo returns v, or fallback when v is empty.
func defaultTo(v, fallback string) string {
	if strings.TrimSpace(v) == "" {
		return fallback
	}
	return v
}

// deviceKeyFromSeed derives this claw's device signing key.
//
// ⚠ DERIVING A DEVICE KEY FROM A STRING IS A DEMO AFFORDANCE, NOT THE DESIGN. A
// real device key is generated once at install, kept on the device, and never
// leaves it — that is what makes the claw an identity rather than a name. This
// exists so a demo run reproduces byte for byte.
func deviceKeyFromSeed(seed string) ed25519.PrivateKey {
	sum := sha256.Sum256([]byte(seed))
	return ed25519.NewKeyFromSeed(sum[:])
}

// facadePinPath is where the facade key pinned at enrolment lives: beside the
// state file, because both are the same kind of thing — durable local facts
// this machine holds about its relationship with one facade.
func facadePinPath(stateFile string) string {
	return filepath.Join(filepath.Dir(stateFile), "ghillie-facade.pin")
}

// resolveFacadeKey decides which facade signing key this terminal trusts, and
// is the whole trust-on-first-use policy in one place.
//
// The order of authority:
//
//  1. An EXPLICIT -facade-key-file is the operator speaking, and it wins. But
//     if the door's enrol response presented a DIFFERENT key, that is not a
//     configuration nuance — it is two parties claiming to be the same facade —
//     and the resolution is a hard refusal, never a quiet preference.
//  2. A PINNED key (written at first enrolment) carries the trust thereafter.
//     A door that later presents a different key gets the same hard refusal:
//     re-pinning is an OPERATOR act (deliberately remove the pin file), never
//     something this code does on a door's say-so. Silent re-pinning would
//     make the pin theatre.
//  3. No explicit key, no pin, and a door presenting one: TRUST ON FIRST USE.
//     The key is pinned (0600) and every later run holds the door to it. This
//     is honest about what it is — the first contact is trusted because there
//     is nothing yet to check it against.
//
// A key from NOWHERE — no flag, no pin, no enrolling door — is an error, not a
// default: a terminal that verifies signatures against no particular key would
// be theatre of a different kind.
func resolveFacadeKey(explicit, enrolled ed25519.PublicKey, explicitPath, pinPath string) (key ed25519.PublicKey, note string, err error) {
	pinned, err := loadPinnedFacadeKey(pinPath)
	if err != nil {
		return nil, "", err
	}

	if enrolled != nil {
		if explicit != nil && !explicit.Equal(enrolled) {
			return nil, "", fmt.Errorf(
				"facade key mismatch: the door's enrol response presented key %x, but -facade-key-file %s holds %x — REFUSING: the explicit key is the operator's word and the door disagrees with it; one of them is not the facade it claims to be",
				enrolled, explicitPath, explicit)
		}
		if pinned != nil && !pinned.Equal(enrolled) {
			return nil, "", fmt.Errorf(
				"facade key mismatch: the door's enrol response presented key %x, but the key pinned at first use in %s is %x — REFUSING, never silently re-pinning: if the facade's key legitimately rotated, removing the pin file is a deliberate operator act; otherwise something is standing where the door used to be",
				enrolled, pinPath, pinned)
		}
		if explicit == nil && pinned == nil {
			if err := writePinnedFacadeKey(pinPath, enrolled); err != nil {
				return nil, "", err
			}
			return enrolled, fmt.Sprintf("facade key pinned at %s (0600) — TRUST ON FIRST USE: this run trusted the door's word because there was nothing yet to check it against; every later run holds the door to this key", pinPath), nil
		}
	}

	switch {
	case explicit != nil:
		return explicit, "", nil
	case pinned != nil:
		return pinned, "", nil
	case enrolled != nil:
		// Unreachable today — an enrolled key with no explicit and no pin was
		// pinned above — kept so a future re-ordering fails safe, not blind.
		return enrolled, "", nil
	}
	return nil, "", errors.New("no facade key: give -facade-key-file, or enrol (-enrol) against a door that presents facade_pubkey so it can be pinned at first use — a terminal with no key to verify against would be verifying nothing")
}

// loadPinnedFacadeKey reads the key pinned at first use. A missing file is not
// an error — it is the "first" in trust-on-first-use.
func loadPinnedFacadeKey(path string) (ed25519.PublicKey, error) {
	body, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("read pinned facade key %s: %w", path, err)
	}
	raw, err := hex.DecodeString(strings.TrimSpace(string(body)))
	if err != nil {
		return nil, fmt.Errorf("pinned facade key %s is not hex: %w — refusing to guess; if the pin is corrupt, removing it is a deliberate operator act", path, err)
	}
	if len(raw) != ed25519.PublicKeySize {
		return nil, fmt.Errorf("pinned facade key %s is %d bytes, want %d — refusing to guess; if the pin is corrupt, removing it is a deliberate operator act", path, len(raw), ed25519.PublicKeySize)
	}
	return ed25519.PublicKey(raw), nil
}

// writePinnedFacadeKey pins a facade key, 0600, beside the state file.
func writePinnedFacadeKey(path string, key ed25519.PublicKey) error {
	if dir := filepath.Dir(path); dir != "." {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			return fmt.Errorf("create pin dir: %w", err)
		}
	}
	if err := os.WriteFile(path, []byte(hex.EncodeToString(key)+"\n"), 0o600); err != nil {
		return fmt.Errorf("write pinned facade key %s: %w", path, err)
	}
	return nil
}

// loadOrCreateDeviceKey loads this claw's device key seed, generating it ONCE
// when the file does not exist yet.
//
// The file holds the 32-byte Ed25519 seed as one line of hex, 0600, and it is
// the claw's identity: generated from the system's entropy at first run, kept
// on the device, never derived from anything an operator typed. (Deriving a
// key from a string is what -device-seed does, and that is a demo affordance —
// see deviceKeyFromSeed.)
func loadOrCreateDeviceKey(path string) (key ed25519.PrivateKey, created bool, err error) {
	body, err := os.ReadFile(path)
	switch {
	case err == nil:
		raw, derr := hex.DecodeString(strings.TrimSpace(string(body)))
		if derr != nil {
			return nil, false, fmt.Errorf("device key %s is not hex: %w — refusing to guess and refusing to overwrite: a device key is an identity, and clobbering one that might be recoverable would orphan every enrolment made under it", path, derr)
		}
		if len(raw) != ed25519.SeedSize {
			return nil, false, fmt.Errorf("device key %s is %d bytes, want %d — refusing to guess and refusing to overwrite", path, len(raw), ed25519.SeedSize)
		}
		return ed25519.NewKeyFromSeed(raw), false, nil

	case errors.Is(err, os.ErrNotExist):
		seed := make([]byte, ed25519.SeedSize)
		if _, rerr := rand.Read(seed); rerr != nil {
			return nil, false, fmt.Errorf("generate device key: %w", rerr)
		}
		if dir := filepath.Dir(path); dir != "." {
			if merr := os.MkdirAll(dir, 0o755); merr != nil {
				return nil, false, fmt.Errorf("create device key dir: %w", merr)
			}
		}
		if werr := os.WriteFile(path, []byte(hex.EncodeToString(seed)+"\n"), 0o600); werr != nil {
			return nil, false, fmt.Errorf("write device key %s: %w", path, werr)
		}
		return ed25519.NewKeyFromSeed(seed), true, nil

	default:
		return nil, false, fmt.Errorf("read device key %s: %w", path, err)
	}
}

// loadFacadeKey reads an EXPLICITLY GIVEN facade signing public key
// (-facade-key-file). When the flag is given it is the operator speaking and it
// is the authority; when it is absent, the key pinned at enrolment — trust on
// first use, see resolveFacadeKey — carries the trust instead.
func loadFacadeKey(path string) (ed25519.PublicKey, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, fmt.Errorf("read facade key %s: %w", path, err)
	}
	defer func() {
		if cerr := f.Close(); cerr != nil {
			log.Printf("close facade key file: %v", cerr)
		}
	}()

	line, err := bufio.NewReader(f).ReadString('\n')
	if err != nil && line == "" {
		return nil, fmt.Errorf("read facade key %s: %w", path, err)
	}
	raw, err := hex.DecodeString(strings.TrimSpace(line))
	if err != nil {
		return nil, fmt.Errorf("facade key %s is not hex: %w", path, err)
	}
	if len(raw) != ed25519.PublicKeySize {
		return nil, fmt.Errorf("facade key %s is %d bytes, want %d", path, len(raw), ed25519.PublicKeySize)
	}
	return ed25519.PublicKey(raw), nil
}

// settleBundleProof decides an install's proof rung and returns the ledger
// note. Manifest first (an altered delivery installs nowhere); then, for a
// bundle that ships its proof project: -reprove runs the whole Rung B —
// re-derive with the owner's prover (zero unproved or refuse), build the
// front here, check every shipped truth-table row against the built binary,
// and mark the LOCALLY BUILT front executable (delivered bytes never gain
// the execute bit; bytes we compiled from proven source do). Without
// -reprove the bundle installs on Rung A and the ledger says so plainly.
// Asking to re-prove a bundle that ships no proof is a refusal, not a shrug.
func settleBundleProof(dir string, reprove bool) (string, error) {
	if n, err := bundle.VerifyManifest(dir); err != nil {
		return "", err
	} else if n > 0 {
		log.Printf("manifest: %d files verified against their recorded digests", n)
	}
	if !bundle.ClaimsProof(dir) {
		if reprove {
			return "", fmt.Errorf("-reprove: this bundle ships no proof project (core/src/proof.gpr) — there is nothing to re-derive")
		}
		return "", nil
	}
	if !reprove {
		return bundle.ProofCarried, nil
	}
	prover, err := bundle.FindProver()
	if err != nil {
		return "", fmt.Errorf("-reprove: %w", err)
	}
	log.Printf("re-proving with %s — zero unproved or no install…", prover)
	if err := bundle.Reprove(dir, prover); err != nil {
		return "", err
	}
	log.Printf("proof discharged here. building the front and checking its table…")
	frontPath, err := bundle.BuildAndTable(dir, prover)
	if err != nil {
		return "", err
	}
	if err := os.Chmod(frontPath, 0o700); err != nil {
		return "", fmt.Errorf("-reprove: the built front would not take the execute bit: %w", err)
	}
	log.Printf("front built and truth-tabled clean: %s", filepath.Base(frontPath))
	return bundle.ProofReproved, nil
}

// settleVoice decides whether ghillie speaks this run. The rule (Tony,
// 2026-08-26): the voice is INCLUDED on a Mac — a console interview speaks by
// default when the pipeline is present — and the off switch is prominent:
// -text-only, which always wins and contradicts an explicit -voice out loud
// rather than quietly picking a winner. An explicit -voice keeps its loud
// contract (a missing pipeline is an error, not a shrug into text); a
// DEFAULTED voice must never cost the sitting, so it returns fallback=true.
// The glass is another mouth and never speaks by default. -ears stays opt-in
// (the microphone is never a default), but once asked for it may ride the
// defaulted voice exactly as it rides an asked-for one.
func settleVoice(voiceSet, voiceAsked, textOnly, doInterview, glass, ears,
	platformOK, pipelinePresent bool) (speak, fallback, auto bool, err error) {
	if textOnly {
		if voiceSet && voiceAsked {
			return false, false, false, fmt.Errorf("-voice and -text-only contradict each other — say which you mean")
		}
		if ears {
			return false, false, false, fmt.Errorf("-ears rides the voice, and -text-only switches the voice off — drop one")
		}
		return false, false, false, nil
	}
	if voiceSet {
		return voiceAsked, false, false, nil
	}
	if doInterview && !glass && platformOK && pipelinePresent {
		return true, true, true, nil
	}
	return false, false, false, nil
}

// voicePipelinePresent reports whether the respire checkout this build would
// speak through actually exists here. The Mac default consults it so that a
// defaulted courtesy never turns into a path error on a machine that was
// never set up to speak; an EXPLICIT -voice deliberately skips this check and
// fails loudly instead.
func voicePipelinePresent(dir string) bool {
	if _, err := os.Stat(filepath.Join(dir, ".venv-kokoro", "bin", "python")); err != nil {
		return false
	}
	if _, err := os.Stat(filepath.Join(dir, "demo", "textplan.py")); err != nil {
		return false
	}
	return true
}

// defaultScotsModel returns the trained Scots fine-tune when it is present on
// this machine — ghillie's voice by default wherever it exists
// (feedback_ghillie_voice_must_be_scottish: it was always a wiring choice,
// not a missing asset). Empty when absent: the stock voice then runs and is
// named a placeholder, never presented as ghillie's.
func defaultScotsModel() string {
	if p := os.Getenv("GHILLIE_SCOTS_MODEL"); p != "" {
		return p
	}
	p := homeSub("voices", "scottish.onnx")
	if _, err := os.Stat(p); err != nil {
		return ""
	}
	return p
}

// defaultVoicePipeline resolves the respire checkout: the owner's explicit
// choice first (GHILLIE_VOICE_PIPELINE), then the ghillie home. A machine with
// the checkout somewhere of its own names it in the environment — the binary
// no longer carries anybody's private layout as a guess.
func defaultVoicePipeline() string {
	if p := os.Getenv("GHILLIE_VOICE_PIPELINE"); p != "" {
		return p
	}
	return homeSub("respire")
}

// defaultEarsModel resolves the whisper model under the ghillie home.
func defaultEarsModel() string {
	if p := os.Getenv("GHILLIE_EARS_MODEL"); p != "" {
		return p
	}
	return homeSub("models", "whisper", "ggml-base.en.bin")
}

// defaultCatalogue is the catalogue under the ghillie home. A local directory
// today, an https base tomorrow: internal/bundle reads both through one path,
// so pointing this at https://thereef.ink/catalogue is a flag, not a rewrite.
func defaultCatalogue() string {
	if p := os.Getenv("GHILLIE_CATALOGUE"); p != "" {
		return p
	}
	return homeSub("catalogue")
}

// runCatalogue serves the four owner-side verbs. Reading offers nothing;
// getting and removing are acts, and both leave a record.
func runCatalogue(o options) error {
	home := filepath.Dir(o.stateFile)
	abilities := filepath.Join(home, "abilities")

	switch {
	case o.listAbilities:
		list, err := bundle.Installed(abilities)
		if err != nil {
			return err
		}
		if len(list) == 0 {
			log.Printf("%s", locale.T("abilities.none"))
			return nil
		}
		for _, e := range list {
			hash := e.Hash
			if len(hash) > 12 {
				hash = hash[:12]
			}
			// The proof rung travels with the listing — what the ledger
			// recorded at install is what the owner sees, in the same words.
			if e.Proof != "" {
				log.Printf("%-18s %s  %s  [%s]", e.Name, hash, e.Source, e.Proof)
			} else {
				log.Printf("%-18s %s  %s", e.Name, hash, e.Source)
			}
		}
		return nil

	case o.removeAbility != "":
		if err := bundle.Uninstall(o.removeAbility, abilities); err != nil {
			return err
		}
		log.Printf("%s", fmt.Sprintf(locale.T("abilities.removed"), o.removeAbility))
		return nil
	}

	// Both remaining verbs read the catalogue.
	if o.catalogueBase == "" {
		return fmt.Errorf("no catalogue configured — pass -catalogue")
	}
	f := bundle.NewFetcher(o.catalogueBase)
	idx, err := f.Index()
	if err != nil {
		return err
	}

	if o.listCatalogue {
		// ★ The listing HEADER and the needs label are chrome and render from
		// the pack; the ability names, proof states and costs are DATA from the
		// catalogue and are printed exactly as published — a proof state
		// translated locally is a proof state nobody can check against the
		// index it came from.
		if l := langLine(); l != "" {
			log.Printf("%s", l)
		}
		log.Printf("%s", fmt.Sprintf(locale.T("catalogue.header"), idx.Catalogue, idx.Published, len(idx.Abilities)))
		installed := map[string]bool{}
		if list, _ := bundle.Installed(abilities); list != nil {
			for _, e := range list {
				installed[e.Name] = true
			}
		}
		for _, e := range idx.Abilities {
			mark := " "
			if installed[e.Name] {
				mark = "✓"
			}
			log.Printf("%s %-16s %-9s %-6s %s", mark, e.Name, e.Proof, e.Cost, e.Summary)
			if e.Needs != "" {
				log.Printf("%s", fmt.Sprintf(locale.T("catalogue.needs"), e.Needs))
			}
		}
		return nil
	}

	// -get-ability: fetch (digest-checked), verify, install, record.
	var want bundle.Entry
	for _, e := range idx.Abilities {
		if e.Name == o.getAbility {
			want = e
			break
		}
	}
	if want.Name == "" {
		return fmt.Errorf("the catalogue holds no ability called %q — try -abilities-available", o.getAbility)
	}
	staging := filepath.Join(home, "quarantine", "catalogue")
	archive, err := f.Fetch(want, staging)
	if err != nil {
		return err
	}
	var dir string
	if want.Delivery == "encrypted" {
		// A confidential delivery: sealed to THIS claw's encryption key,
		// decrypted as a stream (the plaintext archive never becomes a
		// file), and the unpacked source scrubbed after the install
		// settles — pass or fail.
		encPath := defaultTo(o.encryptKey, filepath.Join(filepath.Dir(o.stateFile), "ghillie-encrypt.key"))
		encID, _, kerr := keys.LoadOrCreate(encPath)
		if kerr != nil {
			return kerr
		}
		dir, err = bundle.UnpackEncrypted(archive, staging, encID)
		if err != nil {
			return err
		}
		defer func() {
			if derr := bundle.Dispose(staging); derr != nil {
				log.Printf("⚠ confidential source disposal left residue under %s: %v — remove it by hand", staging, derr)
			}
		}()
	} else {
		dir, err = bundle.Unpack(archive, staging)
		if err != nil {
			return err
		}
		defer os.RemoveAll(staging)
	}
	if err := os.MkdirAll(abilities, 0o755); err != nil {
		return err
	}
	proofNote, err := settleBundleProof(dir, o.reprove)
	if err != nil {
		return err
	}
	floor, err := admit.ParseFloor(o.admitFloor)
	if err != nil {
		return err
	}
	gate := admit.GateFor(context.Background(), admit.Candidate{
		SourceDir:         dir,
		ArchivePath:       archive,
		WantDigest:        want.Digest,
		OperatorRequested: true,
		ProverRanClean:    proofNote == bundle.ProofReproved,
		RunsAtHostTrust:   proofNote == bundle.ProofReproved,
		Floor:             floor,
	}, o.catalogueBase, o.admitAdvisory)
	entry, err := bundle.Install(dir, abilities, "catalogue:"+o.catalogueBase, proofNote, gate)
	if err != nil {
		return err
	}
	log.Printf("installed %s (%s, %s) — digest verified, hash %s…, recorded in the ledger",
		entry.Name, want.Proof, want.Cost, entry.Hash[:12])
	logAdmission(entry)
	if entry.Proof != "" {
		log.Printf("  proof: %s", entry.Proof)
	}
	if want.Needs != "" {
		log.Printf("it needs from you: %s", want.Needs)
	}
	return nil
}

// logAdmission reports what the proven decider said about an install, in the
// words the ledger will keep. It prints on every install, admitted or
// advisory: a gate whose verdict is only visible when it refuses is a gate
// nobody learns to read.
func logAdmission(entry bundle.LedgerEntry) {
	if entry.Verdict == "" {
		return
	}
	if entry.Advisory {
		log.Printf("  ⚠ ADMISSION OVERRIDDEN: the decider said %s and -admit-advisory let it through", entry.Verdict)
		log.Printf("    this is recorded in the ledger permanently — facts: %s", entry.Facts)
		return
	}
	log.Printf("  admission: %s — facts: %s", entry.Verdict, entry.Facts)
	if entry.Authority != "" {
		log.Printf("    attested by: %s", entry.Authority)
	}
}
