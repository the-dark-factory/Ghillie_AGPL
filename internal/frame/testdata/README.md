# Golden vectors — how `ledger119_vectors.txt` was produced

These bytes were **not written by hand and not reasoned about**. They are the
output of building and running the proven Ada core.

## Source of truth

`Facade_Instruction_Codec_Pkg` — ada-factory ledger **119**, forged and admitted
2026-07-29, captured at
`~/dev/ada-factory/components/Facade_Instruction_Codec_Pkg/`.

    source_sha256  bb82366ddef3211ff7e56998d9a0f841a2815c75eca64d5e9249a14db7bc6405
                   facade_instruction_codec_pkg.ads

That hash is the one recorded in `VERIFIED_CORES.jsonl` and reproduced in the
component's `CAPTURE.md`. The copy used to generate these vectors was hash-checked
against it before the build.

## Procedure (zsh, 2026-07-30, on Bill)

`~/dev/ada-factory` is READ ONLY. Nothing was built inside it; the spec was
copied out to a scratch directory first.

```zsh
SC=$(mktemp -d)                      # scratch — NOT inside the factory tree
cp ~/dev/ada-factory/components/Facade_Instruction_Codec_Pkg/facade_instruction_codec_pkg.ads "$SC/"
shasum -a 256 "$SC/facade_instruction_codec_pkg.ads"
# => bb82366ddef3211ff7e56998d9a0f841a2815c75eca64d5e9249a14db7bc6405   (matches the ledger)

cp gen_vectors.adb "$SC/"            # the harness beside this README
mkdir -p "$SC/obj"
cd "$SC" && gnatmake -q -gnat2022 -gnata -D obj gen_vectors.adb -o gen_vectors
./gen_vectors > ledger119_vectors.txt
```

Toolchain: `GNATMAKE 15.0.1 20250418` from `~/.alire/bin`, darwin/arm64.

`gen_vectors.adb` is kept beside this README so the file can be regenerated and
audited. It is a **scratch test harness, not a factory component**, is explicitly
`SPARK_Mode (Off)`, and contains no protocol decisions — it only calls the proven
`Encode` and prints the bytes. `-gnata` is on, so its per-vector round-trip
assertions are live during generation.

## File format

One vector per line, `|`-separated:

    name|command|protocol|artifact_ref|version|seq|wire_hex

Numeric fields are decimal; `wire_hex` is 44 lowercase hex characters (22 bytes).

## Coverage

31 vectors: zero, all-ones, each of the five fields isolated at max / MSB / LSB /
bit-pattern / word-boundary values, every command rank 0–5 of the proven
enumeration, and two mixed alternating-bit cases that cross every field boundary.

## What the vectors prove, and what they do not

`golden_test.go` asserts the Go marshaller is byte-identical to the proven Ada
`Encode` **on these vectors**. That is equivalence on a sample, not a proof of
equivalence — the Go remains unproven shim. The production fix is a C-ABI front
over ledger 119 (the 105/108 flattened-signature pattern), for which these
vectors become the acceptance test.
