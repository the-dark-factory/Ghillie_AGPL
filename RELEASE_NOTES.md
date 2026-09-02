# ghillie — first public release (AGPL-3.0)

An open-source assistant that runs on local models, on your own hardware.

## What this is

Most assistants are one-shot: the model writes something, you run it, and you
find out. One-shot output looks the same whether it's right or wrong. ghillie
doesn't ship the one-shot. An extension only runs if it carries a formal proof
that it does what it claims, and that proof is re-checked before anything is let
through.

## What's in this release

- **Proof-gated extensions.** An extension is admitted only if it passes, every
  time:
  - its formal proof (SPARK), re-checked from source;
  - a purpose trial — a local agent drives the extension against a declared,
    machine-checkable acceptance, in a cleanroom with no network;
  - an adversarial pass — a second, different agent tries to make it miss its
    stated purpose.

  Only an all-green run signs the bundle and marks it downloadable.
- **The verdict is itself proven.** The accept/publish decision is a formally
  verified core, not hand-written trust code.
- **Local and sovereign.** The gate runs against your own local model endpoint —
  no cloud, no provider key.
- A worked example is included (`bundles/paid-twice`).

## What this is not, yet

Early. Rough edges. The author UI and the public catalogue aren't wired in this
cut. Tell us where it falls over.

## A note on this repository

This is a fresh repository. ghillie was built in a private working tree; rather
than publish that history, this release starts clean at v0 with the current,
swept source. Nothing here reaches off your machine.

## Licence

AGPL-3.0 — see LICENSE.
