# The acceptance front — wire contract

`internal/acceptance/run.go` is the acceptance **shim**: the Go glue that runs a
bundle's surface, establishes facts about how it behaved, and asks a proven core
whether those facts add up to an accepted surface. The shim decides nothing. The
proven core — an Ada main named `acceptance_verdict_front` over
`Acceptance_Verdict_Pkg` (ledger core `acceptance_verdict_pkg`) — decides.

This file is the contract between the two, so the front can be forged to match a
shim that already exists. It is the same shape as the admission front described
by `internal/admit` (six-fact tuple in, one answer word out); the difference is
that acceptance sends **one tuple per check** and the core folds them.

## How the front is found

Same resolution as the admission front:

1. the path in `GHILLIE_ACCEPTANCE_FRONT`, if set (must be an executable file);
2. a file named `acceptance_verdict_front` beside the ghillie binary;
3. `acceptance_verdict_front` on `PATH`.

If none is found the shim publishes nothing and says so. A missing decider is
never a silent acceptance.

## Input — stdin

The shim writes **one line per check**, in the bundle statement's own check
order, and then closes stdin. Each line is **six space-separated tokens**, in
this fixed order:

| Position | Fact              | Token when true    | Token when false |
|----------|-------------------|--------------------|------------------|
| 1        | exact arm asked   | `exact_required`   | `exact_absent`   |
| 2        | exact arm met     | `exact_met`        | `exact_unmet`    |
| 3        | pattern arm asked | `pattern_required` | `pattern_absent` |
| 4        | pattern arm met   | `pattern_met`      | `pattern_unmet`  |
| 5        | exit arm asked    | `exit_required`    | `exit_absent`    |
| 6        | exit arm met      | `exit_met`         | `exit_unmet`     |

The tokens are **position-distinct on purpose**: no token is legal in more than
one column. A line with two columns transposed therefore names a token the front
must reject, so an argument-order mistake fails loud (exit 2) rather than
silently answering a different question. This mirrors the `floor_`-prefixing in
the admission tuple.

Each `*_met` token is only ever `*_met` when the matching arm was `*_required`
**and** the observed run satisfied it. A required arm the run did not satisfy —
and every arm of a check whose surface could not be run, timed out, or was
signalled — arrives as `*_unmet`. The front never has to reason about run
errors; the shim has already folded every doubt down to `*_unmet`.

### What the shim establishes (for reference)

For each check the shim runs the surface (argv = the check's `run` with
`{given}` expanded to `given.file`, then `given.args` appended), inside the
bundle root, under a scrubbed environment (`PATH`, `LC_ALL=C`, `LANG=C` only),
with a per-run timeout and a capped stdout. From that run and the declared
`expect`:

- `exact_required` = statement set `stdout_exact`; `exact_met` = observed stdout
  equals it (and was not truncated);
- `pattern_required` = statement set `stdout_matches`; `pattern_met` = that RE2
  matched observed stdout;
- `exit_required` = statement set `exit_code`; `exit_met` = observed exit equals
  it.

## The core's job

For each line the core computes **Check_Satisfied** — its own rule for whether
one check's tuple counts as satisfied (e.g. every *required* arm is *met*) — and
then **Combine**-folds the per-check results into one answer for the bundle. All
of that lives in the proven core; none of it is in Go.

## Output — stdout and exit status

The front prints exactly one answer word and exits:

| Exit | stdout (trimmed) | Meaning                                             |
|------|------------------|-----------------------------------------------------|
| 0    | `accepted`       | the fold accepts the surface — it may be published  |
| 1    | `refused`        | the fold refuses the surface — publish nothing      |
| 2    | (diagnostic on stderr) | a line was malformed — the tuples were rejected |

Anything else — exit 0 with any other word, an empty stdout, extra words, a
signal, any other status — is **not an answer**. The shim treats every such case
as "cannot decide — nothing published", which is never an acceptance.

## The one rule that matters

Only `accepted` on exit 0 lets a bundle publish. Everything else, including the
front being absent, publishes nothing. Forge the front so that its silence, its
crashes and its rejections all land on the safe side of that line.
