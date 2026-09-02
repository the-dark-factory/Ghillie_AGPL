# ghillie — quickstart

Five minutes, from the download to ghillie answering you.

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

## 0. Get the files

Both files come from the same place — the
[latest release](https://github.com/the-dark-factory/Ghillie_AGPL/releases/latest):

| you are on | take this |
|---|---|
| Linux, Intel/AMD | `ghillie-linux-amd64.tar.gz` |
| Linux, ARM | `ghillie-linux-arm64.tar.gz` |
| Mac, Apple silicon | `ghillie-darwin-arm64.tar.gz` |
| Mac, Intel | `ghillie-darwin-amd64.tar.gz` |
| Windows | `ghillie-windows-amd64.tar.gz` |

Take `SHA256SUMS-assets.txt` from that same page as well — §1 checks your
download against it. Put both in the same folder.

---

## 1. Verify what you downloaded

You should now have `SHA256SUMS-assets.txt` beside the tarball from §0. Run **the block for your operating system** — the `grep`
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
ghillie v0.1.4
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

## 6. Give him a mind

**He works without one.** Everything above — the catalogue, installing an
ability, the proofs, the whole facade loop, the interview — needs no model at
all and is untouched by this section. What a mind adds is one thing: free
conversation, in `-talk`.

**The mind is yours.** This ghillie never draws inference from us or from
anybody else. There is no key to paste and no account to make. You run a model
on your own machine and point him at it:

```sh
brew install ollama && ollama serve      # or: curl -fsSL https://ollama.com/install.sh | sh
ollama pull llama3.2
./ghillie -talk
```

That is the whole setup: `-mind-url` already defaults to `http://127.0.0.1:11434`,
which is where ollama listens, and `-mind-model` defaults to empty — meaning
*ask that server what it serves, take the first, and say out loud which one it
was*. The first line he prints names the model, the URL and the API detected,
so you are never guessing whose words you are reading:

```
ghillie │ mind: llama3.2:latest at http://127.0.0.1:11434, speaking ollama /api/chat
```

Already running something else? `vLLM`, `llamafile`, `LM Studio` and
`llama.cpp`'s server all speak the OpenAI-compatible `/v1/chat/completions`, and
he probes for it on the same URL when ollama's own API is not there. Point
`-mind-url` at it and he will say which wire he found. `GHILLIE_MIND_URL` and
`GHILLIE_MIND_MODEL` set the same two things from the environment.

**With no mind, he says so — he does not pretend.** Run `-talk` with nothing
listening and you get this, and nothing else happens:

```
ghillie │ no mind is set on this machine — point -mind-url at your own model (looked at http://127.0.0.1:11434).
```

**The URL must be on this machine.** A non-loopback `-mind-url` is refused
outright, because a mind is shown *everything* you tell him — every question,
every answer, the whole sitting. `-mind-adopt-external` is the eyes-open opt-in
for a remote machine that is genuinely yours; think harder about it than about
`-ears-adopt-external`, which only ever hears one answer.

**The mind decides nothing.** Proofs, refusals, admission decisions and his
conduct in an interview are settled by the proven cores and never see the model
— a build in which they could is a build that fails its own tests.

## 7. Bind him to your membership

If you have an account at the Dark Factory's customer area, you can tie **this
machine** to it, so the factory knows whose ghillie this is. It takes about a
minute and there is no password anywhere in it.

1. Sign in at [customerarea.thedarkfactory.co.uk](https://customerarea.thedarkfactory.co.uk)
   and press **Get a pairing code** on your account page. You are shown a short
   code — something like `K7QM-3XBP` — **once**.
2. Type it here:

```sh
./ghillie -enrol-code K7QM-3XBP
```

He signs that code with his own device key, offers it, and prints what came
back:

```
offering this machine's key d25b-b1e5-7af4-dec3 to https://customerarea.thedarkfactory.co.uk

ADMITTED — admit
key fingerprint : d25b-b1e5-7af4-dec3
bound to        : g-1029384756

You are bound.
```

**Check the fingerprint against your account page.** It is now shown there, and
it should be character-for-character the string he just printed. If the two
differ, the key on your account is not this machine, and we would like to know.

**Nothing secret leaves this machine.** The factory learns your ghillie's
*public* key and nothing else. The private half stays in `~/.ghillie` where it
was generated and has never been anywhere else. You are not sending a password,
because there is not one to send.

**The code is one-shot and lasts ten minutes.** Used, expired, mistyped — press
the button again for a fresh one; nothing is lost. A refusal is printed with the
factory's own word for it, and that word is the thing to quote if you ask why:

```
REFUSED — refuse_code_expired
that code ran out. They last ten minutes; press the button again and come straight here.
```

Those verdicts are not ours to soften. They come from a *proved* core — the one
decision in this whole ceremony, with every refusal reason carried as a theorem
— and ghillie prints its word untouched.

**To unbind**, use the button on your account page. That is host-side only: it
frees the key and stops the portal recognising it, but nothing reaches this
machine, and ghillie keeps its key either way.

> **State of play, 2026-08-30.** The claw side above is real and works today.
> The **Get a pairing code** button is **not on the live account page yet** — the
> proved decider that admits a binding is built and truth-tabled, but its
> compiled front has not shipped into the portal's image, and until it does the
> portal refuses every binding rather than guessing at one. Run `-enrol-code`
> before then and you will get a plain "the enrolment door is not wired on this
> host", nothing will be bound, and your code will not be used up. This note
> goes away when the button appears.

**Not to be confused with `-enrol`.** That flag is a different ceremony
entirely: it submits this machine to a *facade* (who may send it work), and it
is an owner act under its own proved core. Binding to a membership and enrolling
with a facade are separate promises; neither implies the other, and you can have
one without the other.

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
