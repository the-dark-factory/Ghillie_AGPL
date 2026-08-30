# ghillie — quickstart

Five minutes, from a downloaded tarball to ghillie answering you.

This tarball holds four binaries, the language packs, and the licence:

| | |
|---|---|
| `ghillie` | **the product.** The customer-side terminal and interviewer. |
| `mockfacade` | a **test double** — not the real facade. It is here so you have something to point `ghillie` at on your own machine, today, with no account. |
| `ghillie-post` | **optional.** Reads the owner's Gmail and presents arrivals under the owner's own grants instead of a provider's notification regime. Read-only; needs the owner's own OAuth client. |
| `ghillie-wa` | **optional.** The same idea for WhatsApp, as a linked device on the owner's personal account. Read-only; no send path. It prints its own terms-of-service warning at every start, and you should read it. |
| `locales/` | interface language packs — `GHILLIE_LANG=de` (or `fr`, `es`, `id`, `cy`). **Read from right here, beside the binary**; nothing to copy. Refusals stay English until a pack is human-reviewed. |
| `LICENSE` | AGPL-3.0. The program is free software and this is the licence you receive it under. |

> **On a bare container** (`ubuntu:24.04`, `debian:*` and friends) install two
> things first, or the download step has nothing to download with:
> `apt-get update && apt-get install -y curl ca-certificates`

> **On Windows** the binaries are `ghillie.exe`, `mockfacade.exe` and so on, and
> the commands below are written for a Unix shell. Every step has a labelled
> Windows form: the verify block in §1, and the whole local loop in §4b. In
> PowerShell run them as `.\ghillie.exe`; in `cmd` the leading `./` is not a
> thing and you type `ghillie.exe` alone.

---

## 1. Verify what you downloaded

Download `SHA256SUMS-assets.txt` from the same release page and put it beside
the tarball. Then run **the block for your operating system** — the `grep`
filter is what keeps this from failing on the four archives you did not
download.

**Linux** — the tool is called `sha256sum`:

```sh
grep ghillie-linux-amd64 SHA256SUMS-assets.txt | sha256sum -c -
```

**macOS** — the same job, a different name (`sha256sum` does not exist there):

```sh
grep ghillie-darwin-arm64 SHA256SUMS-assets.txt | shasum -a 256 -c -
```

**Windows** — no pipe, no `-c`: Windows prints the hash and **you** compare it
to the line for your file in `SHA256SUMS-assets.txt`. Either shell:

```
certutil -hashfile ghillie-windows-amd64.tar.gz SHA256
```

In PowerShell you can use the native cmdlet instead, which is easier to read:

```powershell
Get-FileHash ghillie-windows-amd64.tar.gz -Algorithm SHA256 | Format-List
```

Both print one hex string. It must match, character for character, the hash
beside `ghillie-windows-amd64.tar.gz` in `SHA256SUMS-assets.txt` — case does
not matter, anything else does.

Swap the platform for yours: `linux-amd64`, `linux-arm64`, `darwin-arm64`
(Apple silicon), `darwin-amd64` (Intel Macs), `windows-amd64`.

On Linux and macOS you want `OK`; on Windows you want two identical hashes.
Anything else means stop and download again.

Then unpack. `tar` ships with Windows 10 1803 and later, so this one line is
the same everywhere:

```sh
tar xzf ghillie-linux-amd64.tar.gz
```

## 2. macOS: nothing to do

The macOS binaries are **signed and notarised by The Dark Factory Ltd**
(Developer ID `85L96KL9LX`). Downloaded in a browser, they run: Gatekeeper
checks the notarisation with Apple the first time — so let the machine reach
the network on that first run — and then leaves you alone.

Earlier releases asked you to strip the quarantine attribute by hand. That
instruction is gone, and good riddance: it read as "turn off the safety check",
which is not a thing anyone should be told to do by a stranger's README.

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

## 4. Try him locally — the full loop, three lines (Linux and macOS)

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

## 4b. The same loop on Windows — PowerShell

Not a translation for tidiness: the block above fails on **every line** in
PowerShell 5.1. A trailing `&` is a hard parse error there, not a request to
background something; `/tmp` does not exist; and `kill %1` means nothing outside
a Unix job table. Use this instead:

```powershell
$pub  = "$env:TEMP\facade.pub"
$mock = Start-Process -PassThru -NoNewWindow .\mockfacade.exe `
  -ArgumentList '-addr','127.0.0.1:8787','-pubkey-out',$pub,'-script','Deliver_Artifact:1','-brief','items','-credit'

.\ghillie.exe -facade http://127.0.0.1:8787 -claw-id my-claw -facade-key-file $pub `
  -owner-id me -user-id me -enrol -interview -ceiling Deliver_Artifact -consent None -poll 1s -idle-exit 3

Stop-Process -Id $mock.Id
```

The backtick is PowerShell's line continuation, as the backslash is bash's. If
you would rather not deal with it, put each command on one long line.

His state lands in `C:\Users\<you>\.ghillie`, which is what `~/.ghillie` means
on Windows; everything else behaves exactly as it does on Linux and macOS.

## 5. In your own language — one variable, nothing to install

The packs sit in `locales/` beside the binary and are read from there. Set
`GHILLIE_LANG` and run any command:

```sh
GHILLIE_LANG=de ./ghillie -h | head -3
```

```
ghillie v0.1.3
Oberflächensprache: de (Paket aus …/locales/de.json; Ablehnungen bleiben
Englisch, bis ein Mensch das Paket geprüft hat)
ghillie — das kundenseitige Terminal. Optionen:
```

Try `fr`, `es`, `id` or `cy` the same way; the catalogue listing speaks it too:

```sh
GHILLIE_LANG=fr ./ghillie -catalogue https://thereef.ink/catalogue -abilities-available
```

**Refusals stay in English, deliberately.** Every pack shipped here is a machine
draft (`"reviewed": false`), and the words ghillie refuses in are the exact
words his behaviour was proved over — a machine translation of a refusal is a
refusal quietly softened. Chrome renders from a draft; refusals wait for a human
reviewer. Edit any string in `locales/<lang>.json`, set `"reviewed": true`, and
that pack's refusals go live on your machine.

To keep your own corrected copy across upgrades, put it in the ghillie home —
`~/.ghillie/locales/<lang>.json` — which is searched first and always wins over
what the tarball shipped.

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
