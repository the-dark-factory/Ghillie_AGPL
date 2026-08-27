# Security review — 2026-08-07 (release-grade: assume the source is public)

Adversarial read of the whole repo against the four central claims:
(a) nothing of the person's leaves the machine, (b) no listener/inbound path,
(c) nothing arriving from outside can make it ACT, (d) abilities arrive
proved and installing is the owner's act.

## HIGH

**H1 — the transcription endpoint is unauthenticated; the person's VOICE can
leave the machine.** `internal/ears/resident.go:142` treats ANY HTTP response
at the whisper URL as the resident server — no loopback check, no identity
check. An unprivileged local process that binds 127.0.0.1:8932 first receives
every captured utterance WAV, in full, for the whole interview. `-ears-whisper-url`
also accepts any scheme/host. Same class, lower value: the GUI's ollama at
127.0.0.1:11434 (`cmd/ghillie-gui/main.go:115`) is equally squattable and
`llm.go` sends typed words verbatim — under a header reading "nothing leaves
the machine". **BREAKS CLAIM (a).**

**H2 — the poll response is the ONE unbounded read in the repo, on the
authority path.** `internal/terminal/terminal.go:370` decodes without an
io.LimitReader; instruction count unbounded. Every other remote read (enrol,
attest, credit, brief, notes, catalogue, whisper) IS bounded. The most
protected endpoint by design is the least protected by code. Trivial DoS from
the facade position the design already models as hostile.

**H3 — the ability supply chain has NO PUBLISHER SIGNATURE.** The digest is
published in the SAME unsigned index as the archive pointer, so it proves only
that the server served what it said. Everything the owner reads before
consenting — `proof: "factory-proved"`, cost, summary — is attacker-authored
text; `open()` accepts plain http. Unpack and copyTree PRESERVE THE ARCHIVE'S
EXEC BIT, and `abilities.go` names `tab.sh` as executed by the out-of-repo
tmux display. Chain: unsigned index → digest-only self-check → exec-bit
preserved → out-of-repo executor. The owner's act survives; the word "proved"
has no cryptographic basis. **AGAINST CLAIM (d).**

## MEDIUM
- **M1** frames carry no claw binding or expiry — a frame observed on claw A
  replays into claw B (ledger 120's theorem is per-device; nothing binds a
  frame to that line). A captured frame is valid forever.
- **M2** `notes.SigningBytes` is NUL-delimited over fields that may contain
  NUL — not injective; two distinct notes can share one signature. The
  submission path length-prefixes and explains why; the notes channel, added
  later, does what that reasoning forbids.
- **M3** quadratic ICS unfold — MEASURED: 400k continuation lines = 3.98s;
  extrapolates to ~50s CPU per gather at the 4 MB cap, every 5 minutes. A
  hostile calendar feed pins a core.
- **M4** tar entry count unbounded (only regular-file BYTES counted) — a
  directory-header bomb inside the 64 MB bound expands to ~10^8 MkdirAll.
- **M5** commissions ledger + day book grow unbounded and re-render
  quadratically (every note re-marshals the whole snapshot).
- **M6** PII (`apple_account_ref`) is POSTed in enrolment BEFORE the facade
  key is verified, and no code path requires https anywhere.
- **M7** `Seq = 2^64-1` permanently wedges the instruction channel; no
  recovery path in code.

## LOW (11 items, see git history of this file / the full report)
Mic recordings + rendered speech written 0644 in an otherwise-0600 home and
never deleted (L1); bundle mode bits honoured (L2); Verify/copyTree TOCTOU so
the ledger hashes what was READ not what was INSTALLED (L3); the never-/tmp
rule misses "/tmp" exactly, $TMPDIR and /var/folders (L4); loose-permission
diary feed ignored SILENTLY (L5); llm history data race (L6); unbounded note
From/Correspondent/Ref and brief item count (L9); enrol swallows a decode
error the comment says it refuses (L10).

## ★ WHAT SURVIVED SCRUTINY (the review's other half)
- **Admission ordering is correct**: signature over the raw 22 bytes BEFORE
  decode; refused before any boundary check; lastSeq advanced before the gate
  runs so a gate-refused frame cannot be re-presented. No verify-after-parse
  error exists.
- **NO COMMAND INJECTION ANYWHERE.** All six exec sites use fixed argv;
  untrusted text travels by stdin or env, never as an argument; the JXA
  source is a compile-time constant.
- **The visual conduct wall holds**: no innerHTML/eval/document.write; every
  wire string reaches the DOM via textContent; every Go→JS crossing marshals
  through encoding/json (verified to escape < > & U+2028/9).
- **Nothing dispatches on a note** — every consumer traced; every branch on
  State selects a sentence or a CSS class. **CLAIM (c) HOLDS.**
- **No listener in ghillie's own code** (net.Listen only in cmd/mockfacade).
  **CLAIM (b) HOLDS** for the Go, with H1's caveat that -ears spawns a child
  that listens.
- Tar unpack refuses absolute/.. paths, multiple roots, and every
  non-file/non-dir typeflag — no traversal constructible.
- `internal/identity` is "the best-built thing in the repo": Format closes the
  %#v route, MarshalJSON redacts, exactly one Reveal() caller.
- Brief-to-frame binding, credit's fail-closed 402, TOFU refusing on ANY
  disagreement, device key 0600 never overwritten on parse failure.

## BEFORE PUBLICATION
1. Bound the poll response (H2) — smallest fix, largest ratio.
2. Constrain whisper/ollama to verified loopback or authenticate them (H1);
   until then the "nothing leaves the machine" wording is not supported.
3. Sign the catalogue index with a pinned publisher key, refuse non-https,
   STRIP mode bits on unpack (H3).
4. Require https for -facade/-catalogue; move PII out of the
   pre-verification enrolment POST (M6).
5. Bind frames/notes to the claw id + expiry, or state in the README that the
   freshness theorem is per-device (M1).
6. Length-prefix notes.SigningBytes (M2); reject control characters.
7. Bounds pass: ICS unfold, tar entries, ledgers, note fields, brief items.
8. **Remove hardcoded personal paths** (personal home paths in main.go — FIXED 2026-08-26, resolving defaults,
   breath.go) — they disclose the author's layout and work for nobody else.
9. LICENSE + SECURITY.md + a stated threat model; .gitignore the 9 MB binary
   at the repo root.
10. CI with -race (catches L6), gosec, govulncheck.
11. **Reconcile every comment-says-X-code-does-Y** (resident.go:140,
    catalogue.go:17, enrol.go:188, bundle.go:22, pipeline.go:114,
    diary.go:288). In a repo whose comments ARE the specification, each is a
    defect in the spec as much as the code.
