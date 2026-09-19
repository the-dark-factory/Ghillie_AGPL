# The proven cores — an adoption guide

This terminal holds no rules of its own. Every decision it reports comes from
a separate, machine-proved decision core, called as an ordinary executable.
This page documents those cores well enough that **anyone — including other
agent ecosystems — can lift them out and put them in their own offering.**
That is not an oversight; it is the point. A proven decider is better in your
product than absent from it, and every adoption spreads the guarantee tier.

## What a core IS, mechanically

- A SPARK Ada package specification (`.ads`), spec-only: one expression
  function whose contract IS its definition. `SPARK_Mode => On, Pure` — it
  imports nothing, holds no state, performs no I/O.
- Its postcondition is a conjunction of **named theorems** — the design's
  promises, discharged by `gnatprove` over the whole input domain, not
  sampled by tests.
- A **front**: a small CLI that decodes words, calls the proven function,
  prints one lower-case word, and honours a strict exit contract —
  `0` answer on stdout · `2` wrong shape · `3` unrecognised word ·
  `4` anything else. **Any non-zero exit means "decider unavailable":
  callers fail closed, never guess.**
- A **truth table** (`TABLE.tsv`) covering the entire input domain, checked
  against the built front.

## How to adopt one (no trust required)

Each core ships as a bundle carrying source, proof project, the originating
prover output, the front, and the table. From the bundle:

    cd core/src && gnatprove -P proof.gpr -f -U --level=2   # 0 unproved, or refuse it
    cd ../../front && gprbuild -P edge.gpr                  # your own toolchain
    # then run every TABLE.tsv row against the built binary

The free GNAT FSF `gnatprove` package alone suffices — it carries the
compiler and `gprbuild` in its own `libexec`. A tampered or defective core
**fails to discharge on your machine**; that refusal is the security model.
Full walk-through: any bundle's `core/REPROVE.md`.

Call the front from any language via `exec` with an environment variable
naming its path (that is all this terminal does). If the variable is unset
or the exit is non-zero, do the safe thing for your domain and record a gap.

## The cores this terminal consults

Interface details, provenance, and admission receipts travel in each
bundle's `provenance.md`; the theorems below are quoted from the proofs.

### Interview & brief handling
| core | decides | named theorems (the postcondition, verbatim) |
|---|---|---|
| **Brief_Fill_Policy** | may a known answer be filled without asking? `Decide(Known, Rule_Class) → fill_silently \| ask_with_offer \| ask_plain \| refuse_item` | NEVER-FILLED-WITHOUT-A-LICENSING-RULE · AN-ABSENT-STORE-NEVER-LETS-A-FILL-THROUGH · NOTHING-INVENTED-EVER · NOTHING-ASKED-THAT-A-RULE-ALREADY-ANSWERS |
| **Mouth_Policy** | may a fluency layer re-voice this line? `Decide(Line_Class, Mouth_Ready) → speak_rephrased \| speak_verbatim` | ONLY-A-PLAIN-LINE-IS-EVER-REPHRASED · REFUSALS-ARE-NEVER-REPHRASED · AN-ABSENT-MOUTH-CHANGES-NOTHING · THE-GUARD-SPEAKS-IN-HIS-OWN-WORDS · A-READY-MOUTH-IS-USED-WHERE-IT-MAY-BE |
| **Guard_Nudge_Policy** | what does the guard do about one utterance? `Decide(Trip_State) → stay_silent \| nudge_once \| silent_with_gap` | THE-INNOCENT-ARE-NEVER-SPOKEN-ABOUT · ONLY-A-TRIP-EARNS-THE-NUDGE · CLOSED-EYES-ARE-DECLARED-NOT-DENIED · THE-GAP-IS-NEVER-INVENTED · A-TRIP-EARNS-EXACTLY-THE-NUDGE — and structurally: no verdict value can withhold service |

### Household conduct
| core | decides | named theorems |
|---|---|---|
| **Quiet_Hours_Policy** | when is one piece of news presented? `Decide(Hour_Class, Urgency) → present_now \| hold_till_waking \| wake_the_owner` | THE-WAKING-HOUSE-HEARS-EVERYTHING · ONLY-BREAK-THROUGH-WAKES · ROUTINE-WAITS-FOR-MORNING · URGENT-QUEUES-AND-NEVER-WAKES · NOTHING-HELD-IN-DAYLIGHT |
| **Digest_Frequency_Policy** | does the assistant speak at this moment? `Decide(News_Class, Elapsed) → stay_silent \| speak_batched \| speak_now` | HE-NEVER-SPEAKS-FIRST-WITHOUT-NEWS · AN-ANSWER-IS-NEVER-BATCHED · THE-DIGEST-WAITS-FOR-ITS-WINDOW · ROUTINE-NEWS-ACCUMULATES-QUIETLY · ONLY-AN-ANSWER-INTERRUPTS |
| **Contact_Grant_Policy** | is one arriving contact presented, asked about, or refused? `Decide(Sender_Class, First_Occasion) → present \| ask_owner \| refuse_silently` | ONLY-A-GRANT-REACHES-THE-HOUSE · THE-STRANGER-IS-NEVER-ANNOUNCED · A-GRANT-IS-HONOURED · THE-ONE-QUESTION-IS-SPENT-ONCE · NOBODY-IS-ASKED-TWICE |
| **Spend_Ceiling_Policy** | may one costed act proceed? `Decide(Cost_Class, Fresh_Consent) → permit \| ask_first \| refuse` | THE-HARD-CAP-HAS-NO-KEY · NOTHING-PERMITTED-PAST-AN-UNASKED-CEILING · THE-EVERYDAY-IS-NEVER-NAGGED · THE-CEILING-ASKS-FIRST · ASKING-MEANS-NOBODY-HAS-CONSENTED-YET |

### Protocol spine (wired throughout this terminal)
| core | decides |
|---|---|
| **Facade_Command** (the admission gate) | whether an instruction arriving inside a connection this machine opened is within what this machine agreed to — ceiling, consent, and command class; anything else refused with a reason class |
| **Poll_Freshness** (anti-replay) | whether a signed frame is fresh against the last-seen sequence — a replayed or stale frame is refused |
| **Glass_Bind_Policy** | whether the one optional loopback listener may bind where asked — ONLY-THE-LOOPBACK-FAMILY-EVER-BINDS; the wildcard refused by its own name; no decider ⇒ no listener |
| **Delivery / disclosure / credit family** | arrival presentation, CV-style disclosure (no rule, no disclosure — absence of a rule is a recorded refusal, never a guess), and credit (a locally-displayed balance never authorises an act; the factory holds the decision) |
| **Attempt_Bound** (compiled-in conduct) | an outstanding interview item is put at most a fixed number of times, then let lie, out loud — not a parameter, not a brief field, on purpose |

### Install admission (KingKlaw, member one)
| core | decides | named theorems |
|---|---|---|
| **Extension_Admission_Pkg** | may this artefact be admitted at all? `Decide(Operator, Origin, Integrity, Proof, Floor, Containment) → admit \| one of eight named refusals` | ADMIT-IFF-ADMISSIBLE (a biconditional, both directions) · NOTHING-ADMITTED-OVER-A-REVOKED-ORIGIN · NOTHING-ADMITTED-OVER-AN-UNATTESTED-ORIGIN · NOTHING-ADMITTED-UNLESS-INTEGRITY-VERIFIED · NOTHING-ADMITTED-BELOW-THE-DECLARED-FLOOR · NOTHING-UNPROVEN-ADMITTED-UNCONTAINED — plus one theorem naming the EXACT refusal for each of the eight refusing states |
| **Extension_Admission_Abi_Pkg** | how does an untrusted integer decode? | EVERY-UNRECOGNISED-CODE-FALLS-TO-THE-LEAST-PERMISSIVE-VALUE — including the floor, where least permissive means the STRICTEST floor, not the weakest |

Two differences from the cores above, both deliberate:

- Its exit contract is **0 admit · 1 refuse · 2 malformed**, not the 0/2/3/4
  above. The gate has a two-valued answer and a malformed case, and nothing
  else; a third success code would be a third verdict.
- Its truth table is **exhaustive at 324 rows** — the entire state space
  (2 × 3 × 3 × 3 × 3 × 2) — checked by two independent implementations, one
  through the command-line front and one through the C ABI.

This terminal consults it on **both** install paths (`-install-ability` and
`-get-ability`), over the DESTINATION COPY, after the bytes are in place and
before the ledger entry is written. The ledger records the verdict, the six
facts as given, the floor in force and the attesting authority, so any install
can be re-decided from its own record. `internal/admit` is the only thing in
this estate permitted to construct the tuple, and its fields are unexported so
that no other caller CAN assert a fact it did not establish.

Source, proofs, the complete table and a fitting guide live in the KingKlaw
library, AGPL-3.0-or-later — the same licence as this terminal — and kept in
its own repository so that anyone can take the gate without taking anything
else. The separation is for reuse, not for licence compatibility.

> ⚠ Until 2026-09-09 this paragraph said KingKlaw was "MIT-licensed and
> deliberately separate from this AGPL terminal". Both halves were wrong.
> KingKlaw is AGPL-3.0-or-later — its `LICENSE`, its `alire.toml`
> `licenses =` field, and the SPDX header in every source file agree — and
> there was never a licence reason to keep AGPL source out of an AGPL tree.
> The correction is recorded here rather than made silently, because an
> adopter may have relied on the wrong claim when deciding what they could
> lawfully do with a core.

## The adoption invitations, explicitly

1. **Take a core as-is**: bundle → re-prove → build → table → wire the env
   var. Your runtime keeps its own architecture; only the decision moves.
2. **Port the CONTRACT, keep the proof**: the `.ads` is the specification;
   if you re-implement the front in your own stack, run OUR table against
   YOUR implementation — the table is the portable conformance suite.
3. **Write your own cores in this shape**: spec-only expression functions,
   named-theorem postconditions, total (no preconditions), a word-in/word-out
   front with the 0/2/3/4 contract. The shape is the contribution; nothing
   about it is ours to keep.

What does NOT travel: the machinery that authored these cores. You receive
source, proofs, and evidence — everything needed to verify and adopt, and
nothing needed to trust.
