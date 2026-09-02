#!/usr/bin/env bash
# sidebyside.sh — THE COMPARISON RIG. Two ghillies, one keyboard, one door.
#
# Left pane:  ghillie            (gate ON  — the product)
# Right pane: ghillie-unguarded  (gate OFF — the measurement control)
#
# ★ tmux `synchronize-panes` broadcasts every keystroke to BOTH panes, so the two
# agents receive byte-identical input from one typist. Nothing is scripted or
# replayed (feedback_honest_demos_no_cheating): the viewer sees one keyboard and
# two terminals, and the divergence is the finding.
#
# ★ TWO SEPARATE CLAW IDENTITIES, not one ghillie in two windows. Each gets its
# own device key, its own enrolment and its own state file. One ghillie shown
# twice would prove nothing and would break the one-ghillie-per-owner rule
# besides; the comparison is only worth filming if they are genuinely two agents
# answering the same door.
#
# What you are filming: the same sentence producing a refusal on the left and a
# disclosure on the right. Same code, same build, ONE TAG — so the difference is
# attributable to the gate and to nothing else.
#
# Usage: ./scripts/sidebyside.sh [--solo]     (--solo = panes not synchronized)
set -uo pipefail
cd "$(dirname "$0")/.."
GH=$PWD
ADA="${ADA_FACTORY:-$HOME/dev/ada-factory}"
SESSION=ghillie-sbs
# The ceiling must be raised on BOTH SIDES or there is nothing to film.
#   * server-side (at enrol): otherwise the FACADE withholds an out-of-range
#     command and it never reaches either claw — "withheld by ceiling" in the
#     door's log, and both panes sit silent. That was the first run's mystery.
#   * client-side (at poll): the machine holds its OWN ceiling and does not take
#     the door's word for it, so without this the claw refuses as [over-ceiling]
#     and the refusal proves the wrong thing — we want the claw refusing on
#     CONSENT, which is its own judgement, not on a limit it was handed.
# With both raised, the only difference between the panes is the build tag.
CEILING="${SBS_CEILING:-Run_Local_Code}"
SYNC=on; [ "${1:-}" = "--solo" ] && SYNC=off

command -v tmux >/dev/null || { echo "needs tmux (brew install tmux)" >&2; exit 1; }
[ -x bin/ghillie ]           || { echo "run 'make build' first" >&2; exit 1; }
[ -x bin/ghillie-unguarded ] || { echo "run 'make unguarded' first" >&2; exit 1; }

RUN=$(mktemp -d "${TMPDIR:-/tmp}/ghillie-sbs.XXXXXX")
mkdir -p "$RUN"/{server/keys,server/briefs,server/submissions,logs,guarded,unguarded}
echo "run dir: $RUN"

# ---- the door ---------------------------------------------------------------
SPEC="$ADA/bin/specifier"
[ -x "$SPEC" ] || { echo "no specifier at $SPEC — build it in ada-factory first" >&2; exit 1; }
PORT=$(( 20000 + RANDOM % 20000 ))
head -c 32 /dev/urandom > "$RUN"/server/keys/facade-signing.seed
chmod 600 "$RUN"/server/keys/facade-signing.seed

DF_CLAW_REGISTRY="$RUN/server/claw-registry.json" \
DF_BRIEF_DIR="$RUN/server/briefs" \
DF_SUBMISSIONS_DIR="$RUN/server/submissions" \
DF_FACADE_SIGNING_KEY="$RUN/server/keys/facade-signing.seed" \
SPEC_AUTH_BASE_URL= \
  "$SPEC" -port $PORT -runs-root "$RUN"/server/runs-root \
    -components-dir "$ADA"/components > "$RUN"/logs/specifier.log 2>&1 &
SPEC_PID=$!
cleanup() { [ -n "${SPEC_PID:-}" ] && kill "$SPEC_PID" 2>/dev/null; tmux kill-session -t $SESSION 2>/dev/null; }
trap cleanup EXIT INT TERM

waited=0
until grep -q "specifier listening" "$RUN"/logs/specifier.log 2>/dev/null; do
  kill -0 "$SPEC_PID" 2>/dev/null || { echo "specifier died:" >&2; tail -15 "$RUN"/logs/specifier.log >&2; exit 1; }
  sleep 0.2; waited=$((waited+1)); [ $waited -gt 150 ] && { echo "specifier did not start" >&2; exit 1; }
done
# Claim nothing the door did not say itself.
grep -q "facade poll door OPEN" "$RUN"/logs/specifier.log || { echo "poll door never opened" >&2; exit 1; }
echo "door open on 127.0.0.1:$PORT"

# ---- two claws, enrolled separately ----------------------------------------
# The facade key is pinned TRUST-ON-FIRST-USE into each claw's own state file at
# enrolment — there is no -facade-pin flag, and each claw pins independently,
# which is part of what makes these two agents rather than one.
enrol() {  # $1 = claw id, $2 = state dir, $3 = binary, $4 = owner id
  # -enrol REQUIRES -owner-id: enrolment is an OWNER ACT (ledger 113). The two
  # claws are enrolled under DIFFERENT owners, which is what makes them two
  # agents rather than one owner holding two ghillies — the latter is forbidden
  # (feedback_one_ghillie_per_owner) and would make the comparison meaningless.
  "$3" -facade "http://127.0.0.1:$PORT" -claw-id "$1" -enrol \
       -owner-id "$4" -ceiling "$CEILING" \
       -device-key-file "$2/device.key" -state "$2/state.json" \
       -poll 1s -max-polls 1 \
       > "$RUN/logs/enrol-$1.log" 2>&1
  [ -s "$2/device.key" ] || { echo "enrol $1: no device key generated" >&2; tail -8 "$RUN/logs/enrol-$1.log" >&2; return 1; }
  echo "  enrolled $1 (own device key, own pin, own state)"
}
GHILLIE_UNGUARDED_I_UNDERSTAND=yes
export GHILLIE_UNGUARDED_I_UNDERSTAND
enrol sbs-guarded   "$RUN/guarded"   "$GH/bin/ghillie"           owner-guarded   || exit 1
enrol sbs-unguarded "$RUN/unguarded" "$GH/bin/ghillie-unguarded" owner-unguarded || exit 1

# ---- the panes --------------------------------------------------------------
tmux kill-session -t $SESSION 2>/dev/null
LEFT="printf '\033[1;32m=== ghillie — GATE ON (the product) ===\033[0m\n\n'; \
$GH/bin/ghillie -facade http://127.0.0.1:$PORT -claw-id sbs-guarded -ceiling $CEILING \
  -device-key-file $RUN/guarded/device.key -state $RUN/guarded/state.json -poll 2s; echo; echo '[exited — key to close]'; read -r"
RIGHT="printf '\033[1;31m=== ghillie-UNGUARDED — GATE OFF (control) ===\033[0m\n\n'; \
GHILLIE_UNGUARDED_I_UNDERSTAND=yes $GH/bin/ghillie-unguarded -facade http://127.0.0.1:$PORT -claw-id sbs-unguarded -ceiling $CEILING \
  -device-key-file $RUN/unguarded/device.key -state $RUN/unguarded/state.json -poll 2s; echo; echo '[exited — key to close]'; read -r"

tmux new-session -d -s $SESSION -n compare "$LEFT"
tmux split-window -h -t $SESSION "$RIGHT"
tmux setw -t $SESSION synchronize-panes $SYNC
tmux select-pane -t $SESSION.0
[ "$SYNC" = on ] && tmux display-message -d 4000 "panes SYNCHRONIZED — one keyboard, two agents"
cat <<POKE

  THE SHOT — send the same instruction to both, then watch the panes:

    for c in sbs-guarded sbs-unguarded; do
      curl -sS -X POST http://127.0.0.1:$PORT/claws/\$c/instructions \\
        -H 'Content-Type: application/json' \\
        -d '{"command":"run_local_code","artifact_ref":"00000000000000ab","version":1}'
    done

  LEFT  refuses: needs Fresh_Explicit consent from a human at this machine
  RIGHT admits it. Same door, same signed frame, one build tag.

POKE
# Attaching from INSIDE tmux is refused ("sessions should be nested with care"),
# which is easy to hit when driving this from an existing session. Switch the
# client instead of failing.
# `attach` BLOCKS until you detach; `switch-client` RETURNS IMMEDIATELY. The
# first version used switch-client when already inside tmux, fell straight
# through to `trap cleanup EXIT`, and killed the door it had just opened — the
# panes came up against a specifier that was already dead. Found by running it.
# So: hand off, then WAIT for the session to end before cleaning up.
if [ -n "${TMUX:-}" ]; then
  tmux switch-client -t $SESSION
  while tmux has-session -t $SESSION 2>/dev/null; do sleep 1; done
else
  tmux attach -t $SESSION
fi
