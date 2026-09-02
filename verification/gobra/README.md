# Gobra experiment — ghillie @ c4440f4

Worked entirely on a scratch clone. `~/dev/ghillie` and `~/dev/ada-factory` untouched.

## Provenance

Session of 2026-07-30. Gobra (ETH Zurich; Viper/Z3 backend) run via the podman
image `ghcr.io/viperproject/gobra` — amd64-only, so under Rosetta on Apple
silicon at roughly 40 s per package.

Result: Gobra discharged the complete postcondition sets of ledger cores
**112 (Facade_Command_Pkg)** and **120 (Poll_Freshness_Pkg)** on ghillie's Go
transliteration — every conjunct of `May_Command` and all three freshness
theorems. Mutation testing: 4 mutants per proof set, all caught (`mut_M1..M4`,
`n_N1..N4`).

**Verdict: adopt for `internal/gate` ONLY.**

### Known limits

- Shift operators are uninterpreted in Gobra's encoding (no axioms, no
  `--bitvector` option), so the 22-byte codec is closed to Gobra. Mitigation
  remains the golden vectors cross-checked against ledger 119.
- `seq` is a reserved Gobra keyword (see `kw/`).
- Verification needed hand-written stubs for 8 stdlib packages.

## Install recipe (macOS arm64, no sudo)

    brew install openjdk z3          # only needed if running the jar natively; the container carries its own
    podman pull --platform linux/amd64 ghcr.io/viperproject/gobra:latest

The `latest` image is self-contained (gobra.jar + MS OpenJDK 21 + Z3 4.x) and was
built 2026-07-29 from master (the "1.1-SNAPSHOT / 2024" banner is a stale constant).
It is amd64-only; on Apple silicon it runs under Rosetta at roughly 40 s per package.

Driver script `./gobra` (mounts $PWD at /work):

    podman run --rm --platform linux/amd64 -v "$(pwd)":/work:z -w /work \
      --entrypoint java ghcr.io/viperproject/gobra:latest \
      -Xss128m -jar /gobra/gobra.jar "$@"

Note the `--entrypoint java` override: the image's own entrypoint uses a relative
`-jar gobra.jar` and breaks whenever `-w` is set.

## Layout

| dir | what | result |
|---|---|---|
| `negctl/` | checker sanity: one true, one false postcondition | correctly 1 error |
| `gate/` | REAL internal/gate, unmodified | 4 front-end type errors |
| `gateA/` | Ada postconditions literal, no preconditions | 2 errors (`res<=5`, `res<=2`) |
| `gateB/` | + domain preconditions | **0 errors** |
| `gateFull/` | ledger 112 + 120 + Evaluate theorems | **0 errors** |
| `mut_M1..M4/` | mutants of gateFull | all 4 caught |
| `frameReal/` | REAL internal/frame, unmodified | 4 errors (array slice, fmt.Errorf) |
| `frameRT/` | codec round-trip attempt | byte contracts OK, reassembly fails |
| `bits/`, `bits2/`, `shift/` | why: shifts are uninterpreted | see below |
| `arith/`, `arith32/` | codec in pure arithmetic | non-termination (>11 min) |
| `ordering/` | terminal.go admission path, trusted stubs | **0 errors**, T1–T6 |
| `n_N1..N4/` | mutants of ordering | all 4 caught |

## The bit-level wall

`shift/shift.go.vpr` line 271 is the whole story:

    function intShiftRight(left: Int, right: Int): Int
      requires right >= 0
      decreases _

Uninterpreted, no axioms. So `v >> 8 == v / 256` is unprovable, and every
byte-level codec is out of reach. Confirmed by probe: `byte(x) == x%256`,
`x<<8 == x*256`, `v>>8 == v/256` all fail; pure integer `(v/256)*256 + v%256 == v`
verifies. There is no `--bitvector` option.

## Reproduce

    cd <this dir> && ./gobra -i gateFull/gate.go
    cd <this dir> && ./gobra -i ordering/ordering.go
