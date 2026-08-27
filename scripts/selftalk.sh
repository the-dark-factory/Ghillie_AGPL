#!/bin/zsh
# selftalk.sh — THE UNATTENDED FULL-LOOP REHEARSAL: ghillie interviews a machine.
#
# Everything realdoor.sh sits you down for, with nobody in the chair: ghillie
# polls the REAL door, verifies the signed frame, fetches the brief, asks its
# questions ALOUD, offers the floor with the audible in-breath — and the
# answers arrive as REAL SOUND IN THE ROOM, spoken by a second process through
# the Kokoro daemon in a DIFFERENT voice, picked up by the REAL microphone and
# transcribed locally. No human, no mocks, nothing typed for the answers
# unless an ear honestly fails (see below).
#
# How the two halves meet: realdoor.sh --ears runs unmodified underneath, its
# console captured. The moment a question's capture window opens, the console
# says so — the "(listening — …)" line printed by the voice surface strictly
# AFTER the question and its offered-floor breath have played. This driver
# watches for that line, waits a beat, renders the scripted answer through the
# warm Kokoro daemon (127.0.0.1:8931) in the interviewee's voice, and afplays
# it into the room. The 1.2 s silence hysteresis closes the turn on its own,
# exactly as it would for a person.
#
# The honest failure path stays honest: if ghillie reprompts for a TYPED
# answer (mic or transcription failure), this driver types the scripted answer
# — and records that it did, loudly. A typed turn is a FINDING, never hidden.
# There is no /cut here: the mic path has no interruption affordance, and that
# is a recorded design fact of tonight, not something this script papers over.
#
# What it refuses: muted or near-silent output. The speakers ARE the
# interviewee's mouth and the mic IS ghillie's ear — a silent room would be a
# rehearsal of nothing, so it fails loudly instead of degrading.
#
# Evidence lands under ~/ObVault/ghillie-voice/wu-runs/ (never /tmp):
#   console.log         the sit, exactly as a person would have seen it
#   console-timed.log   the same lines, wall-clocked as they appeared
#   events.tsv          the driver's own clock: markers, renders, plays, heard
#   answers/            the interviewee's rendered WAVs + daemon responses
#   realdoor-run/       the whole realdoor run dir (door, submission, ears)
#   submissions.jsonl   the stored signed submission, copied out for the record
#   wer-table.txt       scripted answer vs what ghillie put on the record
#   timings.txt         what a sit FEELS like, per turn, in seconds
#
# Env knobs (defaults are tonight's): SELFTALK_EVID, SELFTALK_VOICE,
# SELFTALK_KOKORO_URL, SELFTALK_KOKORO_SH, SELFTALK_WATCHDOG.
#
# Usage: ./scripts/selftalk.sh

set -e -u -o pipefail
zmodload zsh/datetime

ROOT=${0:a:h}/..
DAEMON=${SELFTALK_KOKORO_URL:-http://127.0.0.1:8931}
DAEMON_SH=${SELFTALK_KOKORO_SH:-$HOME/dev/respire/demo/kokoro_daemon.sh}
# ghillie speaks bm_george; the interviewee must be unmistakably someone else.
VOICE=${SELFTALK_VOICE:-bf_emma}
EVID=${SELFTALK_EVID:-$HOME/ObVault/ghillie-voice/wu-runs/selftalk-$(date +%Y-%m-%d)}
WATCHDOG=${SELFTALK_WATCHDOG:-900}

# The console lines this driver keys on. All three are printed atomically by
# the voice surface (internal/interview/console.go) — the first strictly after
# the offered-floor breath, i.e. the moment the microphone window opens.
LISTEN_MARK='(listening'
HEARD_MARK='(heard by ear)'
REPROMPT_MARK='I did not catch that by ear'

# The interviewee's three answers — short, distinct, content-ful, and in the
# register of a person who actually wants the thing built.
ANSWERS=(
  # The first utterance is the PERSON'S REQUEST for the interview — consumed
  # by invite(), content discarded (nothing is conducted until asked for).
  "Aye, go on then."
  "A greenhouse minder that watches temperature and humidity and opens the vents before the midday heat builds."
  "It is for the head grower, whose day starts with a walk down every row and should end with no scorched seedlings."
  "It must never open the vents during a frost, because one cold night would kill the whole tomato crop."
)

rule() { print -- "────────────────────────────────────────────────────────────────────────" }

# ---------------------------------------------------------------- preflight
for tool in curl jq afplay osascript; do
  command -v $tool >/dev/null 2>&1 || { print -u2 "selftalk.sh: missing $tool"; exit 1 }
done

muted=$(osascript -e 'output muted of (get volume settings)')
volume=$(osascript -e 'output volume of (get volume settings)')
if [[ $muted == true ]] || (( volume < 25 )); then
  rule
  print -u2 -- "★ THE ROOM IS SILENT — refusing to run"
  rule
  print -u2 -- "Output is ${muted:+muted=}$muted at volume $volume. The speakers are the"
  print -u2 -- "interviewee's mouth: a muted rehearsal would prove nothing and claim"
  print -u2 -- "otherwise. Turn the volume up (>= 25) and run it again."
  exit 1
fi

mkdir -p "$EVID"/logs "$EVID"/answers

KOKORO_STARTED=0
kokoro_pid=""
if ! curl -sf -m 3 "$DAEMON/health" >/dev/null 2>&1; then
  print -- "kokoro daemon not answering at $DAEMON — starting it ($DAEMON_SH)"
  "$DAEMON_SH" > "$EVID"/logs/kokoro-daemon.log 2>&1 &
  kokoro_pid=$!
  KOKORO_STARTED=1
  waited=0
  until curl -sf -m 3 "$DAEMON/health" >/dev/null 2>&1; do
    kill -0 $kokoro_pid 2>/dev/null || { print -u2 "kokoro daemon died on startup — see $EVID/logs/kokoro-daemon.log"; exit 1 }
    sleep 1; waited=$(( waited + 1 ))
    (( waited > 120 )) && { print -u2 "kokoro daemon not up in 120s"; exit 1 }
  done
fi
print -r -- "$(curl -sf -m 3 "$DAEMON/health")" > "$EVID"/logs/kokoro-health.json

# ------------------------------------------------------------------- launch
FIFO=$EVID/stdin.fifo
rm -f "$FIFO"; mkfifo "$FIFO"
: > "$EVID"/console.log
: > "$EVID"/events.tsv

demo_pid=""
tail_pid=""
cleanup() {
  [[ -n $demo_pid ]] && kill -0 $demo_pid 2>/dev/null && { kill $demo_pid 2>/dev/null || true; wait $demo_pid 2>/dev/null || true }
  [[ -n $tail_pid ]] && kill $tail_pid 2>/dev/null || true
  # $! named the while-loop end of the mirror pipeline; the tail itself is a
  # sibling and must be reaped by name or it outlives the run (observed).
  pkill -f "tail -f $EVID/console.log" 2>/dev/null || true
  exec 3>&- 2>/dev/null || true
  rm -f "$FIFO"
  if (( KOKORO_STARTED )) && [[ -n $kokoro_pid ]] && kill -0 $kokoro_pid 2>/dev/null; then
    kill $kokoro_pid 2>/dev/null || true
    print -- "kokoro daemon stopped (this run started it)"
  fi
}
trap cleanup EXIT INT TERM

rule
print -- "★ SELFTALK — realdoor.sh --ears with a MACHINE in the chair"
rule
print -- "interviewee voice: $VOICE (ghillie itself speaks bm_george)"
print -- "output volume: $volume, not muted — the room is live"
print -- "evidence: $EVID"
print -- ""

REALDOOR_RUN_DIR=$EVID/realdoor-run "$ROOT"/scripts/realdoor.sh --ears \
  < "$FIFO" > "$EVID"/console.log 2>&1 &
demo_pid=$!
exec 3>"$FIFO"   # held open for the whole run so ghillie's stdin never EOFs

# Wall-clocked mirror of the console, for the timings table.
tail -f "$EVID"/console.log | while IFS= read -r line; do
  printf '%s  %s\n' "$EPOCHREALTIME" "$line"
done > "$EVID"/console-timed.log &
tail_pid=$!

ev() { printf '%s\t%s\t%s\t%s\n' "$EPOCHREALTIME" "$1" "${2:-}" "${3:-}" >> "$EVID"/events.tsv }

# -------------------------------------------------------------- the sit itself
played=0 typed=0 heard=0 chair=0
start=$EPOCHREALTIME
while kill -0 $demo_pid 2>/dev/null; do
  if (( EPOCHREALTIME - start > WATCHDOG )); then
    print -u2 "★ WATCHDOG (${WATCHDOG}s) — the sit did not finish; killing it. See $EVID/console.log"
    exit 1
  fi

  L=$(grep -cF "$LISTEN_MARK" "$EVID"/console.log || true)
  H=$(grep -cF "$HEARD_MARK" "$EVID"/console.log || true)
  R=$(grep -cF "$REPROMPT_MARK" "$EVID"/console.log || true)

  if (( chair == 0 )) && grep -qF "THE CHAIR IS YOURS" "$EVID"/console.log; then
    chair=1; ev chair_seen
  fi

  # Honest fallback first: ghillie asked for a TYPED answer. Give it the same
  # scripted sentence — and record the turn as a FINDING, not a success.
  if (( R > typed )); then
    typed=$(( typed + 1 ))
    ev typed_fallback $L "${ANSWERS[$L]}"
    print -r -- "${ANSWERS[$L]}" >&3
    print -- "  driver  ▸ turn $L fell back to TYPED (finding, recorded)"
  fi

  # A capture window opened: wait a beat, render the answer warm, speak it.
  if (( L > played && L <= ${#ANSWERS} )); then
    turn=$L
    ev listening_seen $turn
    sleep 1
    ev render_start $turn
    jq -n --arg t "${ANSWERS[$turn]}" --arg v "$VOICE" '{text:$t, voice:$v, speed:1.0}' \
      | curl -sS -X POST "$DAEMON/render" -H 'Content-Type: application/json' -d @- \
      > "$EVID"/answers/render-$turn.json
    jq -r '.wav_b64' "$EVID"/answers/render-$turn.json | base64 -d > "$EVID"/answers/answer-$turn.wav
    ev render_done $turn "render_s=$(jq -r '.render_s' "$EVID"/answers/render-$turn.json)"
    ev play_start $turn
    afplay "$EVID"/answers/answer-$turn.wav
    ev play_end $turn
    played=$turn
    print -- "  driver  ▸ spoke answer $turn into the room ($VOICE)"
  fi

  if (( H > heard )); then
    heard=$H
    txt=$(grep -F "$HEARD_MARK" "$EVID"/console.log | sed -n "${H}p" \
      | sed -E 's/^  you     ▸ (.*)   \(heard by ear\)$/\1/')
    ev heard_seen $L "$txt"
  fi

  sleep 0.2
done

rc=0
wait $demo_pid || rc=$?
kill $tail_pid 2>/dev/null || true
pkill -f "tail -f $EVID/console.log" 2>/dev/null || true
tail_pid=""
if (( rc != 0 )); then
  print -u2 "★ realdoor.sh exited $rc — see $EVID/console.log"
  exit $rc
fi

# ------------------------------------------------------------ the record
RUN=$EVID/realdoor-run
SUBS=$RUN/server/submissions/submissions.jsonl
[[ -s $SUBS ]] || { print -u2 "no stored submission at $SUBS"; exit 1 }
cp "$SUBS" "$EVID"/submissions.jsonl
cp "$RUN"/server/submissions/reports.jsonl "$EVID"/reports.jsonl 2>/dev/null || true

answered=$(jq -r '[.submission.answers[] | select(.answered)] | length' "$EVID"/submissions.jsonl)
if [[ $answered != ${#ANSWERS} ]]; then
  print -u2 "★ submission shows $answered/${#ANSWERS} answered — see $EVID/submissions.jsonl"
  exit 1
fi

# WER per item: word-level Levenshtein over normalised text (lowercased,
# punctuation stripped). Through-air transcription will not be verbatim and
# the table says exactly how far off it was — honesty over prettiness.
wer_line() {
  awk -v ref="$1" -v hyp="$2" '
    function norm(s,  t) {
      t = tolower(s); gsub("\047", "", t); gsub(/[^a-z0-9 ]/, " ", t)
      gsub(/  +/, " ", t); sub(/^ /, "", t); sub(/ $/, "", t); return t
    }
    BEGIN {
      n = split(norm(ref), Rw, " "); m = split(norm(hyp), Hw, " ")
      for (i = 0; i <= n; i++) d[i,0] = i
      for (j = 0; j <= m; j++) d[0,j] = j
      for (i = 1; i <= n; i++) for (j = 1; j <= m; j++) {
        c = (Rw[i] == Hw[j]) ? 0 : 1
        x = d[i-1,j] + 1; y = d[i,j-1] + 1; z = d[i-1,j-1] + c
        d[i,j] = (x < y ? (x < z ? x : z) : (y < z ? y : z))
      }
      e = d[n,m]
      printf "%.3f (%d edits / %d ref words)", (n > 0 ? e / n : 0), e, n
    }'
}

{
  print -- "scripted answer vs what ghillie put on the record (normalised word-level WER)"
  print -- ""
  for i in {1..${#ANSWERS}}; do
    stored=$(jq -r --argjson i $i '.submission.answers[] | select(.item_id == $i) | .text' "$EVID"/submissions.jsonl)
    attempts=$(jq -r --argjson i $i '.submission.answers[] | select(.item_id == $i) | .attempts' "$EVID"/submissions.jsonl)
    print -- "item $i  (attempts: $attempts)"
    print -- "  scripted: ${ANSWERS[$i]}"
    print -- "  recorded: $stored"
    print -- "  wer:      $(wer_line "${ANSWERS[$i]}" "$stored")"
    print -- ""
  done
  (( typed > 0 )) && print -- "★ FINDING: $typed turn(s) fell back to the TYPED reprompt path — see events.tsv"
} > "$EVID"/wer-table.txt

# Per-turn wall clock, from the driver's own event clock: what a sit feels like.
awk -F'\t' '
  $2 == "chair_seen"     { chair = $1 }
  $2 == "listening_seen" { tl[$3] = $1; turns = $3 }
  $2 == "play_start"     { tp[$3] = $1 }
  $2 == "play_end"       { te[$3] = $1 }
  $2 == "heard_seen"     { if (!($3 in th)) th[$3] = $1 }
  $2 == "typed_fallback" { typed[$3] = 1; if (!($3 in th)) th[$3] = $1 }
  END {
    print "per-turn wall clock (question render + play + offered breath | answer playback | capture close + transcribe | total)"
    print ""
    prev = chair; total = 0
    for (i = 1; i <= turns; i++) {
      q = tl[i] - prev; a = te[i] - tp[i]; c = th[i] - te[i]; t = th[i] - prev
      printf "turn %d:  question %.1fs  |  answer %.1fs  |  close+transcribe %.1fs  |  total %.1fs%s\n", \
        i, q, a, c, t, (typed[i] ? "   [TYPED FALLBACK]" : "")
      prev = th[i]; total += t
    }
    printf "\nthree turns, first question to last answer on the record: %.1fs\n", total
  }' "$EVID"/events.tsv > "$EVID"/timings.txt

rule
print -- "SAT, UNATTENDED, AND ON THE RECORD — $answered/${#ANSWERS} answered"
rule
print -- "submission: $EVID/submissions.jsonl"
cat "$EVID"/wer-table.txt
cat "$EVID"/timings.txt
print -- ""
grep -A2 "PII CONFINEMENT" "$EVID"/console.log | tail -1 || true
print -- "everything from this run is under $EVID"
