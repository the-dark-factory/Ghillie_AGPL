# Golden vectors — how `ledger112_vectors.txt` was produced

These bytes were **not written by hand and not reasoned about**. They are the
output of building and running the proven Ada core.

## Source of truth

`Facade_Command_Pkg` — ada-factory ledger **112**, captured at
`~/dev/ada-factory/components/Facade_Command_Pkg/`.

    source_sha256  8fe2d8b6b82db8b843dcd32cba4150f4a99190f7151cec21ea37302f5b7bce55
                   facade_command_pkg.ads

That hash is the one recorded in `VERIFIED_CORES.jsonl`, in the component's
`CAPTURE.md`, and on the copy used to generate these vectors — all three agree.
The copy was hash-checked before the build.

## What makes these vectors different from ordinary golden files

The domain is **finite, small, and completely covered**:

    Command_Type (6) x ceiling (6) x Consent_Type (3) x Authentic (2) = 216

216 lines is not a sample of `May_Command`'s behaviour. It **is** `May_Command`'s
behaviour. There is no input these vectors omit, so a Go mirror that matches this
table matches the proven core everywhere, not merely on the cases someone thought
to write down.

Sanity: 48 of the 216 are allows (22%). The gate refuses 78% of its own domain,
so agreement is not the trivial "both sides always say no".

## Procedure (zsh, 2026-08-25, on Bill)

`~/dev/ada-factory` is READ ONLY. Nothing was built inside it; the spec was
copied out to a scratch directory first.

```zsh
SC=$(mktemp -d)                      # scratch — NOT inside the factory tree
cp ~/dev/ada-factory/components/Facade_Command_Pkg/facade_command_pkg.ads "$SC/"
shasum -a 256 "$SC/facade_command_pkg.ads"
# => 8fe2d8b6b82db8b843dcd32cba4150f4a99190f7151cec21ea37302f5b7bce55  (matches the ledger)

cp gen_vectors.adb "$SC/"            # the harness beside this README
mkdir -p "$SC/obj"
cd "$SC" && gnatmake -q -gnat2022 -gnata -D obj gen_vectors.adb -o gen_vectors
./gen_vectors > ledger112_vectors.txt
```

Toolchain: `~/.alire/bin/gnatmake`, darwin/arm64.

`gen_vectors.adb` is kept beside this README so the file can be regenerated and
audited. It is a **scratch test harness, not a factory component**, is explicitly
`SPARK_Mode (Off)`, and contains no protocol decisions — it only enumerates the
domain and asks the proven core.

## Format

One line per case:

    c ceiling k authentic verdict

`c`, `ceiling` are `Command_Type'Pos` (0 = Report_Status … 5 = Run_Local_Code);
`k` is `Consent_Type'Pos` (0 = None, 1 = Session, 2 = Fresh_Explicit);
`authentic` and `verdict` are 0/1 for False/True.

## Who consumes them

`internal/gate/ledger112_golden_test.go` — it replays all 216 through the Go
`MayCommand` and fails on any disagreement. That test is the CI gate standing
between the Go mirror and silent drift from the core it claims to mirror.

## When the core changes

If `Facade_Command_Pkg` is ever re-forged, this table is stale and the recorded
hash will no longer match. Regenerate by the procedure above and update the hash
in this README **in the same commit** — a stale table that still passes is worse
than no table, because it certifies the mirror against a core that no longer exists.
