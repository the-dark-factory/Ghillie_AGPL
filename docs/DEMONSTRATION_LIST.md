# DEMONSTRATION LIST

*Moved here from ObVault 2026-08-23. It lived in the notes vault, which is not
version-controlled — its only protection was the Time Machine backup on vault,
and that had a twelve-day hole in it until this morning. It lives in the ghillie
repo because that is where the two working demos are; it also references
ada-factory and ObVault, so those paths are given in full.*

What we can show, on camera, that is REAL. Opened 2026-08-23. Ordered by watchability × how
little must be built. House rules: never staged ([[feedback_honest_demos_no_cheating]]), screen
recordings, no face, no script, and **the most interesting footage is a machine refusing
something**. Failures are episodes, not embarrassments.

Sources: Tony; Muse (muse-glimmer, local) and Gemma 4 both briefed 2026-08-23 — their raw lists
in the session scratchpad. Convergence worth noting: both models independently put *refusal*
footage first, unprompted by each other.

## Tier 1 — exists now, film it this week

| # | Demo | On screen | Effort |
|---|------|-----------|--------|
| 1 | **Ada/SPARK GUARD** *(Tony, added 2026-08-23)* | A guard core refusing a call that would leave its proven domain — the seam returning **403 DF_FORBIDDEN**, nothing written, nothing changed. — **BUILT 2026-08-23, now demo C in Tier 0.** | low |
| 2 | **ghillie refuses** | ghillie on a laptop declining an instruction it never agreed to, saying why, and recording the refusal. | low |
| 3 | **The gate strikes a hollow proof** | The proof gate rejecting a body that COMPILED but did not discharge — the machine refusing our own work. | low |
| 4 | **Aunty Prudence vs floating point** | The same money sum in a spreadsheet and in proven whole-penny arithmetic, diverging. 628 obligations, 0 unproved. | low |
| 5 | **Python calls a proven core** | Three lines of Python through the C seam into Ada; then feeding it rubbish and getting 400/403 back. | low |

## Tier 0 — BUILT AND RUNNING TODAY (2026-08-23)

Both of these exist, run, and have been verified end to end. Point a camera and go.

| # | Demo | Command | What the viewer sees |
|---|------|---------|----------------------|
| **A** | **Gated vs ungated, one keyboard** | `scripts/sidebyside.sh` (this repo) | tmux synchronize-panes drives TWO separately-enrolled claws with byte-identical input. Same door, same signed frame, one build tag. LEFT: `REFUSED — needs Fresh_Explicit consent from a human at this machine`. RIGHT: `ADMITTED`. |
| **B** | **The door is the attacker** | `scripts/hostile-door.sh` (this repo) | The facade records a ceiling of rank 5 as though an attacker reached it, then sends a correctly-signed `run_local_code` it believes it is entitled to send. The claw refuses: `rank 5 is above this machine's ceiling of Deliver_Artifact (rank 2)`. |

| **C** | **The seam refuses a foreign caller** | `~/dev/ada-factory/wu-seam-guard/edge/demo/run.sh` | A plain **C** program calls a proven Ada core. Contract holds → `0 DF_OK`. A negative value in a well-formed buffer → `403 DF_FORBIDDEN`. Null pointer or negative length → `400 DF_MALFORMED`. The caller writes a sentinel before every call: on every refusal the result is **UNTOUCHED**, observed from the C side rather than asserted. |

**C is the one to show a sceptical engineer.** It is the only demo where the
audience's own language is doing the calling, and the `UNTOUCHED` column is
evidence rather than a claim — a sentinel that survives, not a comment that
promises. The unproven part is ~40 lines that overlay the caller's bytes and do
nothing else; the whole of it can be read on camera in a minute, which is the
argument. One assumption, stated: the pointer is valid. Every C library on earth
makes it; almost none writes it down.

**B was found by accident** — raising the ceiling
to fix demo A produced a refusal that proved something better than the thing being
fixed. It answers the question a sceptic actually asks about trust-on-first-use:
what happens when the thing you pinned turns hostile? The line to end on:
**authority cannot be granted inward — a door can only ever send LESS than the
machine permits.**

⚠ Scope both honestly. B shows the CEILING cannot be raised remotely. It does not
show the pinned key cannot be swapped, nor that a hostile door can do no damage
*within* rank 2. Separate claims; do not let the episode blur them.

Supporting kit, also built: the canary identity + leak scanner
(`~/ObVault/experiments/canary-identity`), the proven disclosure core
(`~/dev/ada-factory/wu-cv-disclosure`, gnatprove-discharged and independently
re-proved), and `cvgate` wired to it with no local fallback. Measured: a cold
enquirer gets **0 of 7** protected fields from the gated build and **7 of 7** from
the control arm.

## Tier 2 — a day's work

| # | Demo | On screen | Effort |
|---|------|-----------|--------|
| 6 | **Multics PL/I → proven Ada** | 1970s PL/I in, Ada out, gnatprove discharging it. The oldest code anyone has, proved. | medium |
| 7 | **We were wrong: 94 → 2** | Our own published map claimed 94 memory-safe modules; only 2 had every check discharged. The correction, in public. | medium |
| 8 | **The blind detector** | A checker that reported RED for a healthy forge for a week — and the moral: test BOTH polarities. | medium |
| 9 | **Twelve days with no backup** | vault died in a power cut, nothing noticed for 12 days; building the heartbeat whose silence is the alarm. | medium |
| 10 | **Sudoku, proved** | A solver that cannot print a wrong grid — proven checker over unproven search. Tamper with the answer, watch it refuse. | medium |

## Tier 3 — needs building; widest potential audience, unproven

**11 — THE GHILLIE THAT LOOKS FOR WORK** (Tony, 2026-08-23). A ghillie holding its owner's CV,
talking to recruiters and job boards, **releasing nothing personal until stated conditions are
met** — a named end client, a real vacancy, a salary figure, no CV-farming. Free to the jobseeker;
the agencies pay for access. See [[project_ghillie_cv_agent]]. Effort: medium — the refusal
machinery, the conditions engine and the ledger all exist; this is a skill on top.
**Untested claim, flagged as such:** the other demos need a viewer who already cares about proof;
this one needs no prior interest, so its ceiling is larger. Against it: a proof company leading
with a recruitment gadget may muddy what it sells. Settle it by showing two one-line descriptions
to someone who does not know the company and seeing which needs explaining. Not yet done.

## Open-source candidates (Tony's standing aim: less to maintain alone)
The seam + taxonomy, the guard pattern, ghillie itself (pending its H3 signing work), and the
CV-agent skill. Anything whose value is the PROOF rather than the code can be opened without
loss — the moat is the guarantee, not the source ([[project_model_agnostic_factory]]).
