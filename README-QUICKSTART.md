# ghillie — quickstart

Five minutes, from a downloaded tarball to ghillie answering you.

This tarball holds four binaries, the language packs, and the licence:

| | |
|---|---|
| `ghillie` | **the product.** The customer-side terminal and interviewer. |
| `mockfacade` | a **test double** — not the real facade. It is here so you have something to point `ghillie` at on your own machine, today, with no account. |
| `ghillie-post` | **optional.** Reads the owner's Gmail and presents arrivals under the owner's own grants instead of a provider's notification regime. Read-only; needs the owner's own OAuth client. |
| `ghillie-wa` | **optional.** The same idea for WhatsApp, as a linked device on the owner's personal account. Read-only; no send path. It prints its own terms-of-service warning at every start, and you should read it. |
| `locales/` | interface language packs — `GHILLIE_LANG=fr` (or `de`, `es`, `id`, `cy`) with the pack copied into `~/.ghillie/locales/`. Refusals stay English until a pack is human-reviewed. |
| `LICENSE` | AGPL-3.0. The program is free software and this is the licence you receive it under. |

---

## 1. Verify what you downloaded

Download `SHA256SUMS-assets.txt` from the same release page, put it beside the
tarball, and check **your** platform's line — the `grep` filter is what keeps
this from failing on the four archives you did not download:

```sh
grep ghillie-darwin-arm64 SHA256SUMS-assets.txt | shasum -a 256 -c -
```

Swap `darwin-arm64` for your platform: `darwin-amd64`, `linux-amd64`,
`linux-arm64`, `windows-amd64`.

On Linux, `sha256sum` is the usual name for the same tool:

```sh
grep ghillie-linux-amd64 SHA256SUMS-assets.txt | sha256sum -c -
```

You want `OK`. Anything else means stop and download again.

Then unpack:

```sh
tar xzf ghillie-darwin-arm64.tar.gz
```

## 2. macOS only: clear the Gatekeeper quarantine

These binaries are not notarised. macOS attaches a quarantine attribute to
anything downloaded from a browser and will refuse to run them until it is
removed. Do this only after step 1 has said `OK`:

```sh
xattr -d com.apple.quarantine ghillie ghillie-post ghillie-wa mockfacade
```

If a binary was fetched with `curl` rather than a browser, there may be no
attribute to remove and `xattr` will say so — that is fine.

## 3. Meet him — the catalogue, out of the box

Nothing to configure. This talks to the public ability catalogue over plain
HTTPS and installs nothing:

```sh
./ghillie -catalogue https://thereef.ink/catalogue -abilities-available
```

You get the list of abilities, each with what it needs, its **honest** proof
status, and its cost. Then take one:

```sh
./ghillie -catalogue https://thereef.ink/catalogue -get-ability brief-fill-policy
```

The digest is checked against the index, the five-part contract is verified, and
the install is written to the ledger before it is called done. Installing is an
**owner act** — it happens because you typed it, never because a facade asked.

See what is installed, with its provenance:

```sh
./ghillie -abilities
```

## 4. Try him locally — the full loop, three lines

`mockfacade` plays the factory door so you can watch the whole thing without an
account. It is a test double and will happily play the *compromised* facade on
request; that is the point.

```sh
./mockfacade -addr 127.0.0.1:8787 -pubkey-out /tmp/facade.pub -script 'Deliver_Artifact:1' -brief items -credit &
./ghillie -facade http://127.0.0.1:8787 -claw-id my-claw -facade-key-file /tmp/facade.pub -owner-id me -user-id me -enrol -interview -ceiling Deliver_Artifact -consent None -poll 1s -idle-exit 3
kill %1
```

Type your answers when he asks. Prefix a line with `/cut` to cut him off
mid-question and watch the interrupted-question path run.

## Where his state lives

One place: **`~/.ghillie`** — the state file, the device key that *is* this
claw's identity, the encryption key, the installed abilities, the quarantine.
Set `GHILLIE_HOME` to put it somewhere else, or name `-state` per run.

If you have state from an older build sitting in a working directory, ghillie
uses it where it sits and says so on the way past, rather than quietly minting a
second identity in the home and orphaning your enrolment.

## What he never does

Nothing delivered is ever executed. Deliveries land in the quarantine directory
as notices; there is no installer and no executor on that path. The terminal
polls **outward** only — no listener, no open port, no inbound reach. And every
instruction that arrives inside a connection this machine opened still goes
through the proven admission gate before anything happens.

---

`ghillie -version`, `ghillie -h` — the flags are documented in the binary, and
there are a lot of them. The full README lives with the source:
<https://github.com/the-dark-factory/Ghillie_AGPL>
