# The acceptance statement

Every ghillie extension bundle may carry an **acceptance statement** at
`acceptance/acceptance.json` — a small, machine-checkable description of what
the extension's non-visual surface is *supposed* to do. It is the
"here is what done looks like" file: re-runnable, deterministic, and honest
about the surface's behaviour rather than about anyone's intentions.

The statement is **input to a later step**, not the step itself. This document
and the `internal/acceptance` package describe and validate the *shape* of the
statement. Whether the described checks actually hold when the surface runs is
established separately, in the cleanroom, by a proven acceptance core reached
through a thin exec-shim — the same external-front shape `internal/admit` uses
for its own proven core. Nothing in this repo's `internal/acceptance` runs a
surface or compares its output; that work lives behind the shim.

Design: `project_extension_test_gate_and_catalogue`.

## Where it lives

```
bundles/<name>/
  provenance.md            honest proof status
  ghillie.md               when to offer
  human.md                 how a person uses it
  core/ATTESTATION.md      the proven core's attestation
  surface/                 the runnable (e.g. run.sh + sample.csv)
  acceptance/
    acceptance.json        <- the acceptance statement
```

The canonical schema is `acceptance/acceptance.json` at the repository root
(JSON Schema 2020-12). A worked example travels with the paid-twice bundle at
`bundles/paid-twice/acceptance/acceptance.json`.

## The format

A statement is a single JSON object with two required members.

### `purpose`

| field | type | meaning |
| --- | --- | --- |
| `id` | string, kebab-case | a stable identifier for the extension's purpose, e.g. `flag-duplicate-payments` |
| `sentence` | string | one human sentence stating what the surface is meant to do |

### `checks`

A non-empty array. Each check is one re-runnable observation of the surface:
*given* this input, *run* this argv, and the output should meet these
*expectations*.

| field | type | meaning |
| --- | --- | --- |
| `name` | string, kebab-case, unique | identifies the check within the statement |
| `given` | object | the input handed to the surface |
| `run` | array of strings | the argv, relative to the bundle root, that exercises the surface |
| `expect` | object | what the run should produce |

#### `given`

| field | type | meaning |
| --- | --- | --- |
| `file` | string, optional | a **bundle-relative** input path — never absolute, never containing a `..` segment |
| `args` | array of strings, optional | extra arguments appended after the expanded run argv |

#### `run`

An argv, each token relative to the bundle root. The literal token `{given}`
expands to `given.file`; after expansion, `given.args` are appended. No token
may be an absolute path or contain a `..` segment. Ordinary flags (e.g. `-n`)
and the `{given}` placeholder are not paths and are left untouched.

For example, with `given.file` = `surface/sample.csv` and no args:

```
run: ["surface/run.sh", "{given}"]
  expands to  surface/run.sh surface/sample.csv     (from the bundle root)
```

#### `expect`

At least **one** of these must be present. A check that expects nothing is
refused as malformed input.

| field | type | meaning |
| --- | --- | --- |
| `stdout_exact` | string | the exact stdout the run should produce; `""` is meaningful — it means *expect no output at all* |
| `stdout_matches` | string | an RE2 regular expression that stdout should match |
| `exit_code` | integer 0–255 | the exit status the run should end with; when omitted it is taken to be `0` |

`stdout_exact` is stored as a pointer in the Go types so that a present-but-empty
string is distinguishable from an absent one; likewise `exit_code`, so that an
explicit `0` is distinguishable from an omitted field.

## The determinism rule

An acceptance statement describes behaviour that must be **reproducible on any
machine, at any time, with nothing outside the bundle**. A check must not depend
on:

- the **network** — the cleanroom has none, and a check that reaches for it
  simply produces no output there;
- the **wall clock**, the date, timezone, or any source of "now";
- **randomness**, process IDs, temp-file names, or ordering that varies run to
  run;
- anything **outside the bundle** — absolute paths and `..` are refused for
  exactly this reason.

A surface whose output shifts with the clock or the machine cannot be described
by an exact-stdout check; make it deterministic first (fixed inputs, sorted
output, no timestamps in the text), or describe only the stable part with
`stdout_matches`.

## What validation does — and does not — do

`internal/acceptance.Load` / `Parse` read a statement and confirm it is
well-formed and self-consistent:

- bounded size, strict JSON with no unknown fields, well-formed syntax;
- kebab-shaped identifiers, a non-empty and uniquely-named set of checks;
- a non-empty run argv per check, with no path escaping the bundle;
- at least one coherent expectation per check.

Each way a statement can be malformed maps to a named sentinel error
(`ErrTooLarge`, `ErrMalformed`, `ErrUnknownField`, `ErrBadID`, `ErrNoChecks`,
`ErrDuplicateName`, `ErrNoRun`, `ErrBadPath`, `ErrNoExpectation`,
`ErrBadExpectation`), so a caller can say precisely why an input was refused.

That is the whole of it. Validation **runs nothing** and reaches **no
conclusion** about whether the extension does its job. It answers only: *is this
a well-formed acceptance statement?* Establishing whether the checks hold is the
separate proven core's work, and is described where that core lives, not here.
