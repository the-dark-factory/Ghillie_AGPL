#!/bin/zsh
# realdoor.sh — SIT THE INTERVIEW, against the REAL door.
#
# One command, no arguments. This is the handshake wu (2026-07-30) packaged so
# the person in the chair is you: the real specifier (`cmd/specifier` from
# ~/dev/ada-factory), a real Ed25519 facade signing key generated fresh for this
# run, a real generated device key, TOFU pinning — and ghillie asking its
# questions ALOUD through the settled breath pipeline while you type answers.
#
# What happens, in order:
#   1. Both binaries are built: the specifier from ada-factory's WORKING TREE
#      (that repo is read-only to this script — nothing in it is edited,
#      nothing git happens to it), ghillie from this checkout.
#   2. A fresh run directory is made under ~/ObVault/ghillie-voice/demo-runs/
#      — durable and backed up, NEVER /tmp — with a fresh 32-byte signing seed
#      (0600), a fresh claw registry, and a 3-item brief authored below.
#   3. The specifier opens its facade poll door on a free localhost port.
#      SPEC_AUTH_BASE_URL is deliberately unset: unauthenticated dev mode,
#      stated loudly by the server itself — honest for a localhost sit.
#   4. A fresh ghillie enrols: generated device key, facade key pinned trust-
#      on-first-use from the enrol response. No -facade-key-file, no
#      -device-seed — the ceremony is the real one.
#   5. One deliver_artifact instruction is operator-enqueued whose ArtifactRef
#      names the brief. (An interview IS a delivery — "Conduct_Interview" is
#      not a command in the proven vocabulary, and nothing here pretends it is.)
#   6. THE TERMINAL IS YOURS. Ghillie polls, verifies the signed 22-byte frame,
#      fetches the brief, and asks you its three questions — aloud, unless
#      --text. Type answers; prefix a line with /cut to cut it off mid-ask.
#      It stops on its own after three empty polls.
#   7. The script shows where the signed submission and the outcome reports
#      landed SERVER-side, runs the PII confinement grep over the whole run
#      directory, and stops the specifier cleanly.
#
# Voice is the default because the voice is the point. If the pipeline is not
# available this script REFUSES to run rather than silently downgrading — a
# demo that quietly delivers less than it advertised is a dishonest demo. The
# text-only surface is yours on request, out loud: --text.
#
# Flags:
#   --text   text-only surface (no voice preflight, no renders)
#   --ears   ALSO answer by voice: the mic opens after each question's
#            offered-floor breath, transcribed locally by whisper; typing
#            stays available and /cut stays typed. Off by default.
#   --keep   leave the specifier running afterwards for poking at
#
# Usage: ./scripts/realdoor.sh [--text] [--ears] [--keep]

set -e -u -o pipefail

ROOT=${0:a:h}/..
ADA=${REALDOOR_ADA_FACTORY:-$HOME/dev/ada-factory}
PIPE=${REALDOOR_VOICE_PIPELINE:-$HOME/dev/respire}
STAMP=$(date +%Y%m%d-%H%M%S)
RUN=${REALDOOR_RUN_DIR:-$HOME/ObVault/ghillie-voice/demo-runs/$STAMP}

# ⚠ A DELIBERATELY DISTINCTIVE PII VALUE, same one demo.sh uses. It travels in
# the enrolment payload and must appear in NO file the run leaves behind —
# grepped for at the end, server side and client side alike.
APPLE_REF='apple-acct-000111222333-PURCHASER'

# The brief. brief_id is the unpadded lowercase hex of the ArtifactRef the
# operator enqueue names — the claw checks the fetched body back against the
# signed frame on exactly those fields.
BRIEF_REF=cafe01
BRIEF_VERSION=1

CLAW_ID=realdoor-claw
OWNER_ID=tony-owner
USER_ID=tony

TEXT=0
EARS=0
KEEP=0
for arg in "$@"; do
  case $arg in
    --text) TEXT=1 ;;
    --ears) EARS=1 ;;
    --keep) KEEP=1 ;;
    -h|--help)
      sed -n '2,46p' "${0:a}" | sed 's/^# \{0,1\}//'
      exit 0
      ;;
    *)
      print -u2 "realdoor.sh: unknown argument: $arg (flags: --text --ears --keep)"
      exit 1
      ;;
  esac
done

if (( EARS && TEXT )); then
  print -u2 "realdoor.sh: --ears needs the voice surface; it cannot ride --text"
  exit 1
fi

rule() { print -- "────────────────────────────────────────────────────────────────────────"; }

# ---------------------------------------------------------------- voice preflight
# Checked BEFORE anything is built or started, so a missing pipeline fails in
# the first second, loudly, with the honest alternative spelled out.
if (( ! TEXT )); then
  missing=()
  [[ -x $PIPE/.venv-kokoro/bin/python ]] || missing+=("$PIPE/.venv-kokoro/bin/python (the pipeline's own venv interpreter)")
  [[ -f $PIPE/demo/textplan.py       ]] || missing+=("$PIPE/demo/textplan.py (the reconstructed textplan harness)")
  command -v afplay >/dev/null 2>&1     || missing+=("afplay (macOS local playback)")
  if (( EARS )); then
    # The ears want a local whisper (resident server for preference, CLI as
    # the stated-cost fallback), a model at a durable path, and ffmpeg on
    # the mic side.
    command -v ffmpeg >/dev/null 2>&1 || missing+=("ffmpeg (microphone capture; brew install ffmpeg)")
    if ! command -v whisper-server >/dev/null 2>&1 && ! command -v whisper-cli >/dev/null 2>&1; then
      missing+=("whisper-server or whisper-cli (local transcription; brew install whisper-cpp)")
    fi
    [[ -f ${EARS_MODEL:-$HOME/models/whisper/ggml-base.en.bin} ]] || missing+=("${EARS_MODEL:-$HOME/models/whisper/ggml-base.en.bin} (the ggml whisper model)")
  fi
  if (( ${#missing} > 0 )); then
    rule
    print -u2 -- "★ VOICE PIPELINE UNAVAILABLE — refusing to run"
    rule
    for m in "${missing[@]}"; do print -u2 -- "  missing: $m"; done
    print -u2 -- ""
    print -u2 -- "Voice is this demo's default and a silent fall-back to text would be"
    print -u2 -- "a dishonest demo. If the text-only surface is what you want, say so:"
    print -u2 -- ""
    print -u2 -- "    ./scripts/realdoor.sh --text"
    exit 1
  fi
fi

# ---------------------------------------------------------------------- layout
mkdir -p "$RUN"/bin "$RUN"/logs "$RUN"/client \
         "$RUN"/server/briefs "$RUN"/server/keys "$RUN"/server/submissions "$RUN"/server/runs-root

# ---------------------------------------------------------------------- builds
rule
print -- "BUILDING — specifier from $ADA (working tree, read-only), ghillie from $ROOT"
rule
{
  print -- "run: $STAMP"
  print -- "specifier: $ADA @ $(git -C "$ADA" rev-parse HEAD) (branch $(git -C "$ADA" branch --show-current))"
  if [[ -n $(git -C "$ADA" status --porcelain) ]]; then
    print -- "specifier tree: DIRTY — built from the working tree as found, exactly as the wu proof run was"
  else
    print -- "specifier tree: clean"
  fi
  print -- "ghillie: $ROOT @ $(git -C "$ROOT" rev-parse HEAD) (branch $(git -C "$ROOT" branch --show-current))"
  if [[ -n $(git -C "$ROOT" status --porcelain) ]]; then
    print -- "ghillie tree: DIRTY"
  else
    print -- "ghillie tree: clean"
  fi
} > "$RUN"/provenance.txt

( cd "$ADA"  && go build -o "$RUN"/bin/specifier ./cmd/specifier )
( cd "$ROOT" && go build -o "$RUN"/bin/ghillie   ./cmd/ghillie   )
print -- "built: $RUN/bin/specifier + $RUN/bin/ghillie (provenance: $RUN/provenance.txt)"
print -- ""

# ------------------------------------------------------------------ keys + brief
head -c 32 /dev/urandom > "$RUN"/server/keys/facade-signing.seed
chmod 600 "$RUN"/server/keys/facade-signing.seed
seedsize=$(stat -f%z "$RUN"/server/keys/facade-signing.seed)
if [[ $seedsize != 32 ]]; then
  print -u2 "signing seed is $seedsize bytes, not 32 — refusing"
  exit 1
fi

cat > "$RUN"/server/briefs/brief-$BRIEF_REF.json <<EOF
{
  "brief_id": "$BRIEF_REF",
  "version": $BRIEF_VERSION,
  "items": [
    {"id": 1, "want": "What would you like built? Describe it the way you would to a colleague, not to a computer."},
    {"id": 2, "want": "Who is it for, and what does their day look like when it is working?"},
    {"id": 3, "want": "What must it never do — the failure that would make you switch it off?"}
  ]
}
EOF

# ------------------------------------------------------------------- free port
PORT=
for p in {8797..8896}; do
  if ! nc -z 127.0.0.1 $p >/dev/null 2>&1; then
    PORT=$p
    break
  fi
done
if [[ -z $PORT ]]; then
  print -u2 "no free localhost port in 8797..8896"
  exit 1
fi

# ------------------------------------------------------------------- the door
spec_pid=""
cleanup() {
  if [[ -n "$spec_pid" ]] && kill -0 "$spec_pid" 2>/dev/null; then
    if (( KEEP )); then
      rule
      print -- "--keep: specifier LEFT RUNNING — pid $spec_pid, http://127.0.0.1:$PORT"
      print -- "  poke it:  curl -s http://127.0.0.1:$PORT/claws/$CLAW_ID/credit?act=deliver_artifact"
      print -- "  stop it:  kill $spec_pid"
      rule
    else
      kill "$spec_pid" 2>/dev/null || true
      wait "$spec_pid" 2>/dev/null || true
      print -- "specifier stopped cleanly (pid $spec_pid)"
    fi
    spec_pid=""
  fi
}
trap cleanup EXIT INT TERM

rule
print -- "OPENING THE REAL DOOR — specifier on 127.0.0.1:$PORT"
rule
DF_CLAW_REGISTRY="$RUN/server/claw-registry.json" \
DF_BRIEF_DIR="$RUN/server/briefs" \
DF_SUBMISSIONS_DIR="$RUN/server/submissions" \
DF_FACADE_SIGNING_KEY="$RUN/server/keys/facade-signing.seed" \
SPEC_AUTH_BASE_URL= \
  "$RUN"/bin/specifier -port $PORT \
    -runs-root "$RUN"/server/runs-root \
    -components-dir "$ADA"/components \
    > "$RUN"/logs/specifier.log 2>&1 &
spec_pid=$!

waited=0
until grep -q "specifier listening" "$RUN"/logs/specifier.log 2>/dev/null; do
  if ! kill -0 "$spec_pid" 2>/dev/null; then
    print -u2 "specifier died on startup — last lines of $RUN/logs/specifier.log:"
    tail -20 "$RUN"/logs/specifier.log >&2
    exit 1
  fi
  sleep 0.2
  waited=$(( waited + 1 ))
  if (( waited > 150 )); then
    print -u2 "specifier did not come up in 30s — see $RUN/logs/specifier.log"
    exit 1
  fi
done

# Every line printed below is checked against the server's own log first —
# nothing is claimed that the door did not say itself.
grep -q "facade poll door OPEN" "$RUN"/logs/specifier.log || {
  print -u2 "the facade poll door did not open — see $RUN/logs/specifier.log"; exit 1; }
grep -q "facade signing key loaded" "$RUN"/logs/specifier.log || {
  print -u2 "the signing key did not load — instruction issuance would refuse; see $RUN/logs/specifier.log"; exit 1; }
grep -q "auth DISABLED" "$RUN"/logs/specifier.log || {
  print -u2 "expected unauthenticated dev mode and the server did not state it — see $RUN/logs/specifier.log"; exit 1; }
FACADE_PUB=$(grep -o 'pubkey [0-9a-f]*' "$RUN"/logs/specifier.log | head -1 | cut -d' ' -f2)

print -- "door open: poll + briefs + submissions, signing key loaded (pubkey $FACADE_PUB)"
print -- "auth: unauthenticated dev mode (SPEC_AUTH_BASE_URL unset) — stated by the server, honest for localhost"
print -- ""

# ---------------------------------------------------------------------- enrol
rule
print -- "ENROLLING — fresh device key, facade key pinned trust-on-first-use"
rule
"$RUN"/bin/ghillie -facade "http://127.0.0.1:$PORT" \
                   -claw-id "$CLAW_ID" \
                   -owner-id "$OWNER_ID" \
                   -user-id "$USER_ID" \
                   -apple-account-ref "$APPLE_REF" \
                   -enrol \
                   -ceiling Deliver_Artifact \
                   -consent None \
                   -state "$RUN"/client/ghillie-state.json \
                   -quarantine "$RUN"/client/quarantine \
                   -poll 1s -max-polls 1 2>&1 | tee "$RUN"/logs/enrol.log

# The pin the client wrote must BE the key the server said it was signing with.
PINNED=$(cat "$RUN"/client/ghillie-facade.pin)
if [[ $PINNED != "$FACADE_PUB" ]]; then
  print -u2 "pinned facade key ($PINNED) is not the server's stated signing key ($FACADE_PUB)"
  exit 1
fi
[[ -s "$RUN"/client/ghillie-device.key ]] || { print -u2 "no device key was generated"; exit 1; }
print -- ""
print -- "verified: pinned key == the server's stated signing key ($FACADE_PUB)"
print -- ""

# -------------------------------------------------------------------- enqueue
rule
print -- "OPERATOR ENQUEUE — one deliver_artifact naming brief $BRIEF_REF"
rule
ENQ=$(curl -sS -X POST "http://127.0.0.1:$PORT/claws/$CLAW_ID/instructions" \
        -H 'Content-Type: application/json' \
        -d "{\"command\":\"deliver_artifact\",\"artifact_ref\":\"$BRIEF_REF\",\"version\":$BRIEF_VERSION}")
print -- "$ENQ" > "$RUN"/server/enqueue.json
print -- "$ENQ" | grep -q '"command":"deliver_artifact"' || {
  print -u2 "enqueue refused: $ENQ"; exit 1; }
print -- "$ENQ" | grep -q '"seq":' || {
  print -u2 "enqueue carried no sequence number: $ENQ"; exit 1; }
print -- "enqueued, signed by the facade key: $ENQ"
print -- ""

# -------------------------------------------------------------- the interview
rule
print -- "★ THE CHAIR IS YOURS"
rule
print -- "Ghillie polls the door, verifies the signed frame, fetches brief $BRIEF_REF,"
if (( TEXT )); then
  print -- "and asks you three questions on the text surface (--text)."
else
  print -- "and asks you three questions ALOUD (first render takes a few seconds — Kokoro"
  print -- "is not a daemon; the wait is real and unclaimed)."
fi
if (( EARS )); then
  print -- "SPEAK your answers: the mic opens after each question's offered-floor"
  print -- "breath and closes when you pause. Typing still works; /cut stays typed."
else
  print -- "Type your answers. Prefix a line with /cut to cut a question off mid-ask."
fi
print -- "It will stop on its own after three empty polls."
print -- ""

voice_flags=()
if (( TEXT )); then
  # The binary's Mac default now SPEAKS; a text run must say so, not rely on
  # the absence of -voice.
  voice_flags=(-text-only)
else
  voice_flags=(-voice -voice-pipeline-dir "$PIPE")
  if (( EARS )); then
    voice_flags+=(-ears)
  fi
fi

"$RUN"/bin/ghillie -facade "http://127.0.0.1:$PORT" \
                   -claw-id "$CLAW_ID" \
                   -owner-id "$OWNER_ID" \
                   -user-id "$USER_ID" \
                   -apple-account-ref "$APPLE_REF" \
                   -ceiling Deliver_Artifact \
                   -consent None \
                   -interview \
                   -courtesy-credit 120 \
                   "${voice_flags[@]}" \
                   -state "$RUN"/client/ghillie-state.json \
                   -quarantine "$RUN"/client/quarantine \
                   -poll 1s -idle-exit 3

# ------------------------------------------------------------ where it landed
rule
print -- "WHERE IT LANDED — server side"
rule
SUBS="$RUN"/server/submissions/submissions.jsonl
REPS="$RUN"/server/submissions/reports.jsonl
[[ -s $SUBS ]] || { print -u2 "no submission arrived server-side ($SUBS is missing or empty)"; exit 1; }
grep -q "\"brief_id\":\"$BRIEF_REF\"" "$SUBS" || {
  print -u2 "the stored submission does not name brief $BRIEF_REF — see $SUBS"; exit 1; }
[[ -s $REPS ]] || { print -u2 "no outcome reports arrived server-side ($REPS is missing or empty)"; exit 1; }
print -- "signed submission ($(wc -l < "$SUBS" | tr -d ' ') record(s), device-signature verified by the door before storing):"
print -- "    $SUBS"
print -- "outcome reports ($(wc -l < "$REPS" | tr -d ' ') record(s)):"
print -- "    $REPS"
print -- "claw registry (durable Seq lives here):"
print -- "    $RUN/server/claw-registry.json"
print -- "client state after the run: $(cat "$RUN"/client/ghillie-state.json)"
if (( ! TEXT )); then
  wavs=$(ls "$RUN"/client/ghillie-voice/*.wav 2>/dev/null | wc -l | tr -d ' ')
  print -- "voice renders ($wavs WAV(s) + plan sidecars): $RUN/client/ghillie-voice/"
fi
print -- ""

# --------------------------------------------------------------- PII confinement
rule
print -- "PII CONFINEMENT CHECK — the Apple account reference must appear nowhere"
rule
if grep -r --binary-files=without-match "$APPLE_REF" "$RUN" 2>/dev/null; then
  print -u2 "★ THE APPLE ACCOUNT REFERENCE LEAKED into the files above"
  exit 1
fi
print -- "clean: the purchaser reference is in no server file, no log, no report,"
print -- "no quarantine file and no render — checked over the whole run directory"
print -- ""

rule
print -- "sat and done — everything from this run is under $RUN"
rule
