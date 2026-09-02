#!/bin/zsh
# demo.sh — end-to-end run of the ghillie terminal against the mockfacade TEST
# DOUBLE. Nothing here touches the factory tree, and nothing is ever executed by
# the terminal.
#
# Five phases:
#   A. the fresh install — ceiling Deliver_Artifact (rank 2), no consent. Shows
#      a permitted delivery and four refusal classes.
#   B. the ceiling raised to the maximum with fresh explicit consent. Shows that
#      Request_Spec_Upload is STILL refused — the local-only theorem — and that
#      even a gate-admitted install or run performs no act.
#   C. ★ THE INTERVIEW LOOP. A brief is delivered as Deliver_Artifact, fetched
#      by a separate outward GET, conducted, and the answers submitted signed
#      with the device key. One question is INTERRUPTED mid-ask; it lands
#      Interrupted, is never reported answered, and is not restarted.
#   D. ★ THE CONDUCT WALL. The double smuggles wait_time_ms into the brief. It
#      is refused at parse, wholesale, and the refusal is reported.
#   E. ★ CREDIT: COURTESY vs AUTHORITY. The local balance says 9,999 and the
#      factory says no. The act is refused.
#
# Every phase now ENROLS AND ATTESTS first. v0's demo did not, because the old
# double had no auth — which hid the fact that the terminal sent no Authorization
# header and would have taken a 401 on every poll against the real facade door.
#
# Usage: make demo   (or ./scripts/demo.sh)

set -e -u -o pipefail

ROOT=${0:a:h}/..
BIN=$ROOT/bin
RUN=${GHILLIE_DEMO_DIR:-$ROOT/.demo}

PORT_A=${GHILLIE_DEMO_PORT_A:-8787}
PORT_B=${GHILLIE_DEMO_PORT_B:-8788}
PORT_C=${GHILLIE_DEMO_PORT_C:-8789}
PORT_D=${GHILLIE_DEMO_PORT_D:-8790}
PORT_E=${GHILLIE_DEMO_PORT_E:-8791}

# ⚠ A DELIBERATELY DISTINCTIVE PII VALUE. It is grepped for at the end of the
# run: the Apple account reference must appear in no log line, no outcome report
# and no quarantine file.
APPLE_REF='apple-acct-000111222333-PURCHASER'

rm -rf "$RUN"
mkdir -p "$RUN"

facade_pid=""
cleanup() {
  if [[ -n "$facade_pid" ]] && kill -0 "$facade_pid" 2>/dev/null; then
    kill "$facade_pid" 2>/dev/null || true
    wait "$facade_pid" 2>/dev/null || true
  fi
}
trap cleanup EXIT INT TERM

rule() { print -- "────────────────────────────────────────────────────────────────────────"; }

# start_facade launches the double and waits for it to write its public key.
start_facade() {
  local dir=$1 port=$2; shift 2
  mkdir -p "$dir"
  "$BIN/mockfacade" -addr "127.0.0.1:$port" -pubkey-out "$dir/facade.pub" "$@" &
  facade_pid=$!

  local waited=0
  while [[ ! -s "$dir/facade.pub" ]]; do
    sleep 0.1
    (( waited += 1 ))
    if (( waited > 50 )); then
      print -u2 "mockfacade did not start"
      exit 1
    fi
  done
  sleep 0.2
}

phase() {
  local name=$1 port=$2 script=$3 ceiling=$4 consent=$5 dir=$RUN/$1

  rule
  print -- "PHASE $name — ceiling $ceiling, consent $consent"
  rule

  start_facade "$dir" "$port" -script "$script" -per-poll 1

  "$BIN/ghillie" -facade "http://127.0.0.1:$port" \
                 -claw-id "demo-claw-$name" \
                 -facade-key-file "$dir/facade.pub" \
                 -owner-id "acme-it" \
                 -user-id "tony" \
                 -apple-account-ref "$APPLE_REF" \
                 -enrol \
                 -ceiling "$ceiling" \
                 -consent "$consent" \
                 -quarantine "$dir/quarantine" \
                 -state "$dir/state.json" \
                 -poll 300ms \
                 -idle-exit 3

  cleanup
  facade_pid=""

  print -- ""
  print -- "quarantine after phase $name:"
  if [[ -d "$dir/quarantine" ]]; then
    ls -l "$dir/quarantine" | sed 's/^/    /'
  else
    print -- "    (empty — nothing was delivered)"
  fi
  print -- ""
}

# interview_phase runs a phase that fetches a brief and conducts it. The client's
# side of the conversation is piped in; a line beginning /cut means "you were
# cutting me off mid-sentence and said this instead".
interview_phase() {
  local name=$1 port=$2 courtesy=$3 dir=$RUN/$1; shift 3

  rule
  print -- "PHASE $name"
  rule

  start_facade "$dir" "$port" -script 'Deliver_Artifact:1' -per-poll 1 "$@"

  print -- 'aye, go on then.
Booking vans across three depots, so the yard manager stops keeping it in his head.
/cut sorry - it is him and two dispatchers, and they are usually on the phone when they need it.
An old stock system, a label printer, and a spreadsheet nobody will give up.
A van goes out unbooked and a customer waits half a day. About four hundred pounds each time.
How do you score this?
If the yard manager stops keeping the list on paper.' \
  | "$BIN/ghillie" -facade "http://127.0.0.1:$port" \
                   -claw-id "demo-claw-$name" \
                   -facade-key-file "$dir/facade.pub" \
                   -owner-id "acme-it" \
                   -user-id "tony" \
                   -apple-account-ref "$APPLE_REF" \
                   -enrol \
                   -interview \
                   -text-only \
                   -courtesy-credit "$courtesy" \
                   -ceiling Deliver_Artifact \
                   -consent None \
                   -quarantine "$dir/quarantine" \
                   -state "$dir/state.json" \
                   -poll 300ms \
                   -idle-exit 3

  cleanup
  facade_pid=""
  print -- ""
}

phase A "$PORT_A" \
  'Report_Status:1,Deliver_Artifact:2,Install_Artifact:3,Run_Local_Code:4,Request_Spec_Upload:5,Deliver_Artifact:2,Deliver_Artifact:6:badsig' \
  Deliver_Artifact None

phase B "$PORT_B" \
  'Request_Spec_Upload:1,Install_Artifact:2,Run_Local_Code:3' \
  Run_Local_Code Fresh_Explicit

interview_phase C "$PORT_C" 120 -brief items -credit -balance 250
interview_phase D "$PORT_D" 120 -brief conduct-wait -credit -balance 250
interview_phase E "$PORT_E" 9999 -brief items -credit=false

rule
print -- "PII CONFINEMENT CHECK — the Apple account reference must appear nowhere"
rule
if grep -r --binary-files=without-match "$APPLE_REF" "$RUN" 2>/dev/null; then
  print -u2 "★ THE APPLE ACCOUNT REFERENCE LEAKED into the files above"
  exit 1
fi
print -- "clean: the purchaser reference is in no log, no outcome report and no quarantine file"
print -- ""

rule
print -- "demo complete — artefacts under $RUN"
rule
