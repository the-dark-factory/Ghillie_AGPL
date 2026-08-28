# ghillie — the customer-side terminal and interviewer (v1)

The thing that runs on a customer's own computer, asks the factory facade
whether there is anything for it, **refuses anything it has not agreed to**, and
— as of v1 — **conducts the interview** the factory asks for.

    enrol ──► attest ──► poll loop ──► per-instruction gate ──► act / refuse ──► report
                                              │
                                              └─ Deliver_Artifact naming a brief
                                                     │
                                     outward GET ────┴──► conduct ──► sign ──► submit

Built from the factory's internal facade-protocol design (§2–§6) and the v1
interview-loop plan. Nothing in this repository redesigns any part of it.
*(Those design documents are internal and are not part of this repository.)*

---

## The three rules v1 is built on

**1. Ghillie judges nothing.** It is an interviewing agent working through the
questions a brief supplies. It may **recall, present and ask**. The factory alone
**judges, scores and decides what is missing**. Ghillie does not know whether a
spec is complete and **cannot tell the client it is ready** — asked directly, it
says so plainly and truthfully.

**2. ★ Conduct is compiled in, never delivered — and the wall is structural.**
*Content* (which questions, in what order) comes from the brief. *Conduct* (wait
time, whether it may interrupt, what it discloses) is compiled into the binary.
The brief type carries **items only** and decodes with `DisallowUnknownFields()`,
so a brief carrying `wait_time_ms`, `may_interrupt` or `explain_mechanism`
**fails to parse and is refused wholesale**. This is a safety property, not
tidiness: the promise is *"this agent will not interrupt you, and here is the
proof"*, and conduct delivered over the wire is conduct an attacker can rewrite.

**3. Disclosure: explain outcome, decline mechanism.** Ghillie explains the
guarantee side fully — what gets built, what is guaranteed, every refusal it
made. It declines mechanism, and can do so *honestly*, because it genuinely does
not hold the scoring. Warm and matter-of-fact, never evasive.

---

## The two claims, and which layer each rests on

**1. Transport: the facade physically cannot reach in.**

ghillie **polls outward, always**. The customer's machine opens every
connection. There is no listener, no open port and no inbound path on the
customer side — no `http.Server` exists anywhere on the ghillie side of this
repository and none should ever be added. NO-REMOTE-REACH is architectural
*before* it is proven.

**2. Decision: even inside a connection ghillie opened, an instruction this
machine has not agreed to is refused.**

Every received instruction — however delivered, however authentic — passes
through the proven admission gate before anything acts. The gate is the
protocol; the wire is packaging.

---

## What is proven, what is cross-checked, what is unproven shim

This section is the honest one. Read it before quoting anything from this repo.

### PROVEN — Ada/SPARK cores forged, admitted and captured in the factory's internal Ada estate (not part of this repository)

| Ledger | Core | What it decides |
|---|---|---|
| 112 | `Facade_Command_Pkg` | may this command be admitted, at this ceiling, with this consent, given authenticity |
| 113 | `Claw_Enrolment_Pkg` | who may enrol, revoke and command this machine (**ENROLMENT-IS-AN-OWNER-ACT**, **NO-TWO-MASTERS**) |
| 115 | `User_Access_Pkg` | may this person still act, and who may take that away (presence and consent are **proved irrelevant**) |
| 119 | `Facade_Instruction_Codec_Pkg` | the 22-byte frame layout and its round-trip |
| 120 | `Poll_Freshness_Pkg` | is this frame's sequence strictly newer than the last accepted one |
| 122 | `Turn_State_Pkg` | whose turn it is (**INTERRUPTION-ALWAYS-YIELDS**) |
| 123 | `Question_Ledger_Pkg` | what happened to each question (**INTERRUPTED-IS-NOT-ANSWERED**, **NEVER-MOVE-ON-FROM-A-MERE-MENTION**) |

**No Ada is called at run time.** The proven cores are the *authority for what
this Go does*, not a library it links. See "cross-checked" below for how that gap
is closed, and "what is stubbed" for how it stops being a gap.

### CROSS-CHECKED — Go that is unproven, but shown equivalent to a proven core

- **`internal/frame`** — the 22-byte marshaller. `golden_test.go` asserts it is
  **byte-identical to the proven Ada `Encode`** over 31 vectors (field extremes,
  zero, all-ones, each field isolated at max/MSB/LSB/pattern/word-boundary,
  every command rank, alternating patterns across every boundary). Those vectors
  were produced by **building and running the real core**, from a copy whose
  sha256 matches the ledger record — see
  [`internal/frame/testdata/README.md`](internal/frame/testdata/README.md).
  Honest label: **cross-checked against ledger 119, not itself proven.**

- **`internal/gate`** — a line-for-line transliteration of
  `facade_command_pkg.ads` and `poll_freshness_pkg.ads`. `gate_test.go` walks
  **all 216** (command × ceiling × consent × authentic) combinations and asserts
  the four proved postconditions of `May_Command` individually, plus the three
  theorems of `Is_Fresh`. Honest label: **cross-checked against ledgers 112 and
  120, not itself proven. Where this Go and the Ada spec disagree, the Ada is
  right.**

- **`internal/gate` (the identity cores)** — `claw_enrolment_pkg.ads` (113) and
  `user_access_pkg.ads` (115), transliterated. `identity_cores_test.go` walks
  **all 32** (state × actor × authentic × serving) and **all 128** (requester ×
  authentic × enrolled × serving × present × consents) combinations. The 115
  sweep additionally proves the property that is an *absence*: flipping
  `User_Present` or `User_Consents` anywhere in the space never moves the
  verdict.

- **`internal/conduct`** — `turn_state_pkg.ads` (122) and
  `question_ledger_pkg.ads` (123), transliterated. Each is walked over its full
  **12**-combination domain against every proved postcondition. These are the
  cores that make the interview's promises theorems rather than intentions.

### UNPROVEN SHIM — plumbing, no decisions in it

- **`internal/terminal`** — poll loop, signature verification, decode ordering,
  quarantine, brief fetch, answer queue, submission, refusal reporting, durable
  last-sequence.
- **`internal/brief`** — the brief type, the strict decoder (**the conduct
  wall**), and the live ledger state, which routes every transition through 123.
- **`internal/interview`** — the text front end. Asks, records, never judges.
- **`internal/identity`** — the four identities as four distinct types, and the
  PII confinement of the Apple account reference.
- **`internal/credit`** — the courtesy/authority split.
- **`internal/enrol`** — the enrol/attest client and the device key.
- **`internal/protocol`** — the JSON poll shapes. Deliberately unproven: none of
  it is load-bearing, because the only thing carrying authority is the 22 signed
  bytes inside it.
- **`cmd/ghillie`** — flags and wiring.

The pattern is `provenLaneGate` at the bottom of the factory's internal
`specifier` wiring (not in this repository): *"There is no logic here by
design."* Every verdict the terminal appears to reach is a call into
`internal/gate`.

### TEST DOUBLE — not a product

- **`cmd/mockfacade`** — **a test double. It is not the facade.** It fakes the
  signing key from a command-line seed, keeps its claw registry in memory, does
  **not** check that an enrolment came from an owner (neither does the real door
  yet — that check is recorded as missing), serves a hard-coded brief, and will
  happily issue instructions it knows will bounce *and smuggle conduct into a
  brief on request*. Those last parts are its purpose: it plays the compromised
  facade so the gate and the conduct wall can be watched saying no. The real
  facade endpoints belong in the factory's internal `specifier`, and building
  them is a **separately gated step this repository does not take**.

  v1 did close one honesty gap in it: the double now runs the **enrol/attest
  ceremony and requires an attested session**, because v0's mock had no auth at
  all, which hid the fact that the terminal sent no `Authorization` header and
  would have taken a 401 on every poll against the real door.

---

## The command vocabulary IS the proven enumeration

Ordered by **blast radius, not convenience**. No protocol command exists that
the gate has no rank for.

| Rank | Command | v0 meaning | Gate conditions |
|---|---|---|---|
| 0 | `Report_Status` | answer "how are you" | authentic, ≤ ceiling |
| 1 | `Offer_Catalogue` | catalogue index available; display only | authentic, ≤ ceiling |
| 2 | `Deliver_Artifact` | delivery notice → **quarantine**, never installed | authentic, ≤ ceiling |
| 3 | `Request_Spec_Upload` | **refused at every ceiling and every consent level** | never |
| 4 | `Install_Artifact` | install a quarantined artifact | authentic, ≤ ceiling, `Fresh_Explicit` consent |
| 5 | `Run_Local_Code` | total-compromise rank | authentic, ≤ ceiling, `Fresh_Explicit` consent |

**Default ceiling for a fresh unenrolled install: rank 2, `Deliver_Artifact`.**
Notifications and deliveries flow; nothing installs or executes without the
local machine raising the ceiling *and* a human consenting to the specific act.

Rank 3 is in the vocabulary precisely so its refusal is a **theorem rather than
an absence**: `Is_Local_Only` is true for it, and `May_Command` proves a
local-only command is refused whatever the ceiling and whatever the consent.

## Refusal reason classes

A refused instruction is **not silently dropped**. It is shown to the user and
reported to the facade on the next poll.

`over-ceiling` · `local-only` · `no-consent` · `bad-signature` · `stale-seq`

Three more exist only because Go's type system is weaker than SPARK's: in Ada,
`Command_Type` cannot hold a value outside the enumeration, so the core never
has to refuse one. Go's `uint8` can, so the shim refuses at the decode boundary
what Ada refuses at the type boundary:

`unknown-command` · `unsupported-protocol` · `malformed-frame`

## Order of checks

1. **signature** over the exact 22 bytes → `Authentic` — before anything is
   decoded, so no unauthenticated byte influences any later step;
2. **decode** (ledger 119 layout);
3. **protocol version** and **command byte** boundary checks;
4. **freshness** (ledger 120) — only authentic frames reach it, so a forged
   frame cannot move the stored sequence number. An authentic, fresh frame
   advances the stored sequence *whether or not the gate admits it*: a frame
   seen once must never be accepted twice;
5. **the gate** (ledger 112);
6. act, or refuse with a reason class.

## The empty response is the non-disclosure identity

"Nothing for you" and "instructions above your ceiling exist" must be
indistinguishable on the wire. Both are `204 No Content` with no body.

An **HTTP 401 is not an empty poll** and is never treated as one: it means the
session this claw believed in is not one the facade honours, so the session is
dropped, the next poll re-attests, and the poll is reported as failed.

---

## ★ The interview loop (v1)

**No new command and no re-forge of the gate.** The vocabulary is proven and
closed, and `Command_Type'Pos` **is** the wire byte, so inserting a member
anywhere but the end would renumber the wire. The brief therefore arrives as an
ordinary **`Deliver_Artifact`** — rank 2, the default ceiling — with the brief id
in `ArtifactRef` and the revision in `Version`. All 31 golden vectors stay valid.

    signed frame  ──►  gate admits  ──►  user standing (115)  ──►  credit (facade)
                                                                        │
        outward GET /briefs/{id}?version=N  ◄───────────────────────────┘
                    │
                    ├─ strict decode ── CONDUCT WALL ── refuse wholesale
                    │
                    └─ check body against the SIGNED FRAME's ref + version
                                    │
                          conduct ──┴──► sign with device key ──► POST /specs
                                                                   (or ride the poll)

**The 22-byte signed frame remains the only authority-bearing object.** The brief
body comes down an unsigned GET, which is safe *because it carries no authority*:
the signed frame said which brief and which revision, and the fetched body is
checked back against it. A facade that signs for one brief and serves another is
caught.

**Answers leave by ghillie's own initiative.** They are signed with the device
key over a length-prefixed byte string (not canonicalised JSON — the same swamp
the 22-byte frame declines to enter) and posted to `/specs`. `Is_Local_Only
(Request_Spec_Upload)` is proven, so **the facade can never pull them** at any
ceiling or consent level. If the `/specs` door is shut, the queued submission
rides the next poll — same signed object, different route, and nothing is lost.

### What an interrupted question does

Cut off mid-ask, an item goes to `Interrupted` through ledger 123 and **is never
reported answered**. Three things make that true rather than intended:

1. the turn yields to `Listening` through ledger 122, from any state,
   unconditionally;
2. `Is_Got(Interrupted)` is false, and the report asserts that nothing is sent as
   answered unless `May_Move_On` says it may be;
3. **only what actually reached the client** goes in the transcript, cut at a
   `--(cut off here)` marker. Ghillie cannot resume a sentence it has no record
   of saying — the remainder was never written down. The closing board shows the
   item without its wording for the same reason.

## ★ Four identities, kept apart

| Identity | What it is | Governed by |
|---|---|---|
| the **claw** | this installation; holds the device key, has a ceiling | 113 |
| the **owner** | sets the ceiling, holds enrolment and revocation | 113 |
| the **user** at the keyboard | supplies `Fresh_Explicit` consent; revocable *in absentia* | 115 |
| ★ the **Apple Account** | who actually **bought the credits** | ⛔ **nothing — this is the gap** |

They are four distinct Go types, so one cannot be passed where another is wanted.
The Apple account reference is **PII**: it is held in an unexported field, redacts
itself through `fmt` (including `%#v`) and `encoding/json`, and the *only* way out
is `Reveal()` — which has exactly one caller, the enrolment payload. It appears in
**no log line, no outcome report and no quarantine file**, and `make demo` greps
the whole run to prove it.

## ★ Credit: the local balance is a COURTESY, the facade's is the DECISION

*Never trust a declaration computed on a machine we do not control.* So
`credit.Courtesy` has **no method that returns a permission** — there is nothing
to do with it but render it, and a test asserts that absence. The only verdict
comes from `credit.Authorise`, which asks the facade. An unreachable facade has
not said yes; the failure is closed.

Ghillie is **required to be open about cost** and does so unprompted: it shows
the courtesy figure clearly labelled, and says that a precise description is both
a better build and a cheaper one. That is the honest incentive alignment, not a
sales line — and it never quotes a price, because pricing is not its to do.

---

## Build and run

```zsh
make check          # gofmt + go vet + go test
make build          # ./bin/ghillie and ./bin/mockfacade
make demo           # end-to-end, all five phases
make cores          # the proven-core sweeps, with their combination counts
make opsec          # the OPSEC gate over every client-visible string
make golden         # how the golden vectors are regenerated
```

Running an interview by hand:

```zsh
./bin/mockfacade -addr 127.0.0.1:8787 -pubkey-out /tmp/facade.pub \
                 -script 'Deliver_Artifact:1' -brief items -credit &

./bin/ghillie -facade http://127.0.0.1:8787 \
              -claw-id my-claw \
              -facade-key-file /tmp/facade.pub \
              -owner-id acme-it -user-id tony \
              -enrol -interview -courtesy-credit 120 \
              -ceiling Deliver_Artifact \
              -consent None \
              -poll 1s -idle-exit 3
```

Type answers when asked. **Prefix a line with `/cut`** to say "you were cutting
me off mid-sentence and I said this instead" — that is how the interrupted
question is exercised from a keyboard, where there is no microphone to cut in on.
In v2 the identical `Reply` comes from the voice pipeline noticing someone start
speaking; the state machine on the other side does not know or care which.

`make demo` runs five phases: the fresh install (ceiling rank 2, no consent); the
ceiling raised to maximum with fresh explicit consent — which shows
`Request_Spec_Upload` **still** refused and a gate-admitted install performing no
act; **the interview loop with a question interrupted mid-ask**; **the conduct
wall** refusing a brief carrying `wait_time_ms`; and **credit refused by the
factory while the local courtesy figure says 9,999**. It finishes by grepping the
whole run for the purchaser reference.

Replace `-brief items` with `conduct-wait`, `conduct-interrupt` or
`conduct-disclose` to watch the wall fire on each field.

### ★ Abilities — browse the catalogue, install, re-derive the proof

An **ability** is an extension bundle. Installing one is an **owner act**: each
of these flags does its work at your keyboard and then exits — none of it can be
driven over the wire, and none of it happens during a poll.

```zsh
make build

# 1. See what a catalogue offers — name, what it needs, its HONEST proof
#    status, and cost. Installs nothing.
./bin/ghillie -catalogue https://thereef.ink/catalogue -abilities-available

# 2. Install one by name. The archive's digest is checked against the index,
#    the five-part contract is verified, and the ledger records the install.
./bin/ghillie -catalogue https://thereef.ink/catalogue \
              -get-ability brief-fill-policy

# 3. RUNG B — re-derive the proof yourself instead of taking it on trust.
./bin/ghillie -catalogue https://thereef.ink/catalogue \
              -get-ability brief-fill-policy -reprove

# 4. What is installed here, with provenance from the ledger.
./bin/ghillie -abilities
```

`-catalogue` takes **either** an `https` base **or** a local directory holding
an `index.json`. With the flag omitted it defaults to a local catalogue
directory under your home.

**What `-reprove` changes.** Without it, a bundle that ships a proof installs on
**Rung A**, and the ledger says plainly: *proofs carried, not re-derived here*.
With it, ghillie runs **Rung B** — it re-derives the proof from the shipped
source using **your** prover (zero unproved, or no install), builds the front
with **your** toolchain, and checks it against the shipped truth table. Rung B
needs a working SPARK/GNAT toolchain on your machine. Asking for `-reprove` on a
bundle that ships no proof project is an **error, not a silent downgrade**.

Proof status is stated honestly per ability in the index — at the time of
writing the reef catalogue lists both `prototype` and `re-provable` bundles, and
only the latter can be taken to Rung B.

To remove one: `./bin/ghillie -remove-ability <name>` (also an owner act, and
also how an upgrade is done — visibly). A bundle you already downloaded can be
installed from disk with `-install-ability <path>`, which honours `-reprove` the
same way.

### ★ Sit the interview — the real door, out loud

> **INTERNAL ONLY.** This script builds against working trees that are **not
> part of this repository** (the factory's Ada estate and the voice pipeline)
> and writes into internal directories. It will not run from a clean public
> clone. It is documented here for completeness, not as a step you can follow.

```zsh
./scripts/realdoor.sh          # one command; ghillie asks ALOUD, you type answers
./scripts/realdoor.sh --text   # the text-only surface — an explicit choice
./scripts/realdoor.sh --keep   # leave the specifier running afterwards for poking
```

No mockfacade anywhere in this one. The script builds the **real specifier**
from the factory's internal Ada working tree (read-only — nothing there is touched),
generates a fresh 32-byte facade signing seed, enrols a fresh ghillie with a
real generated device key and a trust-on-first-use pin, operator-enqueues one
`deliver_artifact` naming a 3-item brief, and then the chair is yours: ghillie
polls, verifies the signed frame, fetches the brief, and asks you what you want
built — **spoken through the settled breath pipeline** (an internal checkout; the
first render takes a few seconds, honestly unclaimed). You type; `/cut` cuts a
question off mid-ask. If the voice pipeline is missing the script **refuses to
run** rather than silently downgrading — `--text` is the fallback, and you have
to say it.

Everything lands under an internal `ghillie-voice/demo-runs/<timestamp>/`
directory (never `/tmp`): the signed submission and outcome reports **server-side**
(`server/submissions/*.jsonl`), the claw registry with the durable Seq, the
quarantined delivery notice, the voice renders with their plan sidecars, and
`provenance.txt` recording both build trees. The run ends with the same PII
confinement grep the demo uses, over the whole run directory, and a clean
specifier shutdown.

---

## What is stubbed, and deliberately so

- **No installer, no executor.** `Install_Artifact` and `Run_Local_Code` can be
  *admitted* by the gate, and then nothing happens, loudly. Installing would
  require `Update_Admission_Pkg` (ledger 114: well-formed / signature valid /
  key known / version **strictly newer**), which is not wired — and installing
  without it would be exactly the unproven-decision-on-the-security-path this
  design exists to avoid.
- **The attempt-bound core is PROVEN, ADMITTED (ledger 128) and wired.**
  `Attempt_Bound_Pkg` (proven 2026-07-30, level 2, 14 checks, 0 unproved,
  solver grade 3/3; admitted 2026-07-30 via the off-seat admission chain,
  ledger entry 128) replaced the `SingleSweep` seam placeholder.
  `internal/conduct/attempt_bound.go` is now its transliteration: ghillie puts
  an outstanding item at most `Max_Attempts = 3` times — every put, including
  the first, licensed by `Decide` — and then **lets it lie, out loud**. The
  core `with`s ledger 123 rather than forking it, so *a let-lie item is never
  reported as answered* bites against the real `Is_Got`/`May_Move_On`. The
  bound is **compiled-in conduct**: not a parameter, not a brief field.
  Nothing is dropped silently and nothing is claimed that was not received.
  **Ledger 123 still has no `Dropped` state, so this build reports none.**
- **Consent is a command-line flag — but the SOURCE is now an interface.**
  `Fresh_Explicit` means a human at this machine was asked about *this* act and
  said yes; a flag is a standing answer to a question nobody asked. v1 replaced
  the stored `Config.Consent` **value** with a `ConsentSource` called on the gate
  path per act, so the real per-act local prompt drops in without the gate path
  moving. The value is at least still **local** — never taken from the wire.
- **The enrolment ceremony exists; the owner check on the facade side does not.**
  ghillie now enrols and attests, and refuses locally through
  `Claw_Enrolment_Pkg.May_Enrol` before anything reaches the wire. The facade —
  real and mock — does **not** verify that an enrolment came from an owner; that
  check is recorded as missing there. A claw declining to participate in an
  unlawful enrolment is the right thing for the claw and is **not a substitute**
  for the facade having its own conscience.
- **The facade's public key now travels at enrolment, pinned trust-on-first-use.**
  A door that presents `facade_pubkey` in its enrol response gets that key pinned
  to `ghillie-facade.pin` (0600, beside the state file) on first contact, and
  every later run holds the door to it. A door that later presents a *different*
  key is refused **hard** — re-pinning is a deliberate operator act (remove the
  pin file), never something the code does on a door's say-so. `-facade-key-file`
  remains as the explicit override and is the authority when given; a door whose
  enrol response disagrees with it is refused the same way. A door that presents
  no key at all is an older door and stays legal — the key then comes from the
  flag or the pin. TOFU is honest about what it is: the *first* contact is
  trusted because there is nothing yet to check it against.
- **The device key is now generated once and kept** — `ghillie-device.key`
  (0600, beside the state file, or wherever `-device-key-file` points): born
  from the system's entropy at first run, loaded ever after, never clobbered
  even when corrupt. `-device-seed` survives as a **demo affordance only**: it
  derives the key from a string so scripted runs reproduce byte for byte, says
  so out loud when used, and is not how a real install gets an identity.
- **Voice has its first honest slice — output only.** `-voice` (with
  `-interview`) speaks ghillie's lines ALOUD through the settled breath
  pipeline (`internal/voice` → the settled harness `textplan.py` → local
  playback on this machine); replies are still typed, and `/cut` remains the keyboard's
  stand-in for cutting ghillie off. Say is NON-OFFERING; only Ask ends on an
  audible offered-floor in-breath and then immediately opens the reply capture
  — the hand-over signal and the listening window are one call
  (`internal/interview/voice.go` documents the reasoning). A pipeline failure
  stops the run loudly unless `-voice-fallback-text` is explicitly set.
  Renders are seconds each — Kokoro is not a daemon yet, and no latency is
  claimed. Deliberately NOT here: microphone, relay, barge-in, whisper,
  streaming. `Vad_Pkg` (121) and `Clause_Split_Pkg` (125) remain proven,
  captured and consumer-less; they are the microphone side of the voice path,
  still v2. Lung state resets per utterance — continuity across an utterance
  sequence is a recorded refinement.
- **No C-ABI into the proven cores.** v1 is the sanctioned golden-vector
  cross-check. Production is a `Facade_Instruction_Codec_Abi_Pkg` with a
  flattened signature (the 105/108 pattern), foreman-exported and cgo-linked, at
  which point the bytes *are* the proven core's output rather than a
  cross-checked copy — and these golden vectors become its acceptance test.
  The same applies to the gate.
- **Refusal reasons are reported in full to the facade.** Open decision 7 is
  unresolved: full reasons help operations and honesty, but also tell a
  compromised facade which probe bounced off which ceiling. If it lands the
  other way, send only "refused" outward and keep the reason local. The
  user-visible refusal stays either way — that one is the point.

## Repository layout

    cmd/ghillie/         the terminal + interviewer (client only, never a server)
    cmd/mockfacade/      TEST DOUBLE — not the facade
    internal/frame/      22-byte codec, cross-checked against ledger 119
      testdata/          golden vectors + the Ada generator + provenance
    internal/gate/       ledgers 112, 113, 115 and 120, transliterated
    internal/conduct/    ledgers 122, 123 and 128 (attempt bound), transliterated
    internal/brief/      the brief type, the CONDUCT WALL, the live ledger state
    internal/interview/  the text front end — asks, records, never judges
    internal/identity/   four identities, four types; Apple account = PII
    internal/credit/     courtesy (display) vs authority (decision)
    internal/enrol/      enrol/attest client, device key, identity binding
    internal/protocol/   JSON poll shapes (nothing load-bearing)
    internal/terminal/   poll loop, brief fetch, answer queue, submission, refusals
    scripts/demo.sh      end-to-end demonstration, five phases

## Constraints honoured

*(Internal development notes, kept for the record. The trees named here are the
factory's own and are not part of this repository.)*

The factory's Ada estate was read only — v1 read four more `.ads` files
(`claw_enrolment_pkg`, `user_access_pkg`, `turn_state_pkg`, `question_ledger_pkg`)
and transliterated them here. The golden vectors were generated by copying
`facade_instruction_codec_pkg.ads` **out** to a scratch directory, hash-checking
it against the ledger record, and building there. Nothing was built inside,
modified in, or committed to the factory tree. No facade endpoint was added to
`specifier`.
