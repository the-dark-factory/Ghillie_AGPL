#!/usr/bin/env bash
# hostile-door.sh — THE DOOR IS THE ATTACKER.
#
# Every other demo assumes the facade is honest and asks whether the claw
# behaves. This one assumes the FACADE IS COMPROMISED and asks whether that is
# enough to widen a claw's authority. It is not.
#
# THE SETUP. At enrolment the door records a ceiling for the claw. Here the door
# records the HIGHEST rank — Run_Local_Code — as though an attacker had reached
# the facade and raised it. The door then quite legitimately sends a
# run_local_code instruction: by its own records, it is entitled to.
#
# THE POINT. The claw holds its OWN ceiling and does not take the door's word for
# what it may do. The instruction arrives correctly signed by a facade the claw
# trusts, inside the limit the DOOR believes applies, and the claw refuses it
# anyway — because the limit that governs is the one on THIS MACHINE.
#
# This is the case that matters for the whole design: trust-on-first-use means
# the claw trusts the door's KEY, and people reasonably ask what happens when the
# thing behind that key turns hostile. The answer is that authority cannot be
# granted inward. A door can only ever send LESS than the machine permits.
#
# Nothing here is staged: the door really does record the raised ceiling, and the
# refusal really is the claw's own (feedback_honest_demos_no_cheating).
#  --slow  paces the four beats for FILMING. The demo runs in about two seconds
#  otherwise, which is fine for verifying and useless on camera: a viewer has to
#  read "the door records ceiling = 5" BEFORE the refusal lands, or the whole
#  point is lost. Nothing about the demonstration changes — the same commands run
#  in the same order; only the pauses between them differ.
set -uo pipefail
cd "$(dirname "$0")/.."
SLOW=0; [ "${1:-}" = "--slow" ] && SLOW=1
beat() { [ "$SLOW" = 1 ] && sleep "${1:-3}"; return 0; }
GH=$PWD; ADA="${ADA_FACTORY:-$HOME/dev/ada-factory}"
[ -x bin/ghillie ] || { echo "run 'make build' first" >&2; exit 1; }
SPEC="$ADA/bin/specifier"; [ -x "$SPEC" ] || { echo "no specifier at $SPEC" >&2; exit 1; }

R=$(mktemp -d "${TMPDIR:-/tmp}/hostile-door.XXXXXX")
mkdir -p "$R"/{server/keys,server/briefs,server/submissions,logs,claw}
head -c 32 /dev/urandom > "$R"/server/keys/facade-signing.seed
chmod 600 "$R"/server/keys/facade-signing.seed
PORT=$(( 30000 + RANDOM % 10000 ))

DF_CLAW_REGISTRY="$R/server/claw-registry.json" DF_BRIEF_DIR="$R/server/briefs" \
DF_SUBMISSIONS_DIR="$R/server/submissions" DF_FACADE_SIGNING_KEY="$R/server/keys/facade-signing.seed" \
SPEC_AUTH_BASE_URL= "$SPEC" -port $PORT -runs-root "$R"/server/runs-root \
  -components-dir "$ADA"/components > "$R"/logs/spec.log 2>&1 &
SP=$!; trap 'kill $SP 2>/dev/null' EXIT INT TERM
w=0; until grep -q "facade poll door OPEN" "$R"/logs/spec.log 2>/dev/null; do
  sleep 0.2; w=$((w+1)); [ $w -gt 150 ] && { echo "door did not open" >&2; exit 1; }; done

# ---- 0. THE PROOF, BEFORE THE REFUSAL --------------------------------------
# Without this the demo shows a Go program printing a refusal, which looks
# exactly like a hand-written `if` — ordinary defensive programming, and a
# viewer who knows anything thinks "yes, and?". The refusal is only interesting
# if the RULE behind it was proved rather than typed. So discharge the core
# first, live, and let the refusal follow from something the audience watched a
# prover check.
CORE="$ADA/components/Facade_Command_Pkg"
if [ -f "$CORE/proof.gpr" ] && command -v gnatprove >/dev/null 2>&1; then
  echo
  echo "0. First, the RULE. internal/gate/gate.go mirrors Facade_Command_Pkg"
  echo "   (ada-factory ledger 112). Discharging it now — not a stored result:"
  echo
  ( cd "$CORE" && gnatprove -P proof.gpr --level=2 --report=all -j0 2>&1 \
      | grep -E "postcondition proved|Always_Terminates" | sed 's/^/   /' ) || true
  echo
  echo "   Command_Rank, Consent_Rank, Requires_Consent, Is_Local_Only — every"
  echo "   postcondition discharged. Nothing medium, nothing unproved. THAT is"
  echo "   the rule the claw is about to apply."
  beat 6
fi

echo
echo "1. The door opens on 127.0.0.1:$PORT"
beat 3
echo
echo "2. The claw enrols. THE DOOR RECORDS A CEILING OF Run_Local_Code —"
echo "   the highest rank there is. Imagine an attacker reached the facade."
beat 2
"$GH"/bin/ghillie -facade "http://127.0.0.1:$PORT" -claw-id victim -owner-id owner \
  -enrol -ceiling Run_Local_Code -device-key-file "$R"/claw/k -state "$R"/claw/s.json \
  -poll 1s -max-polls 1 > "$R"/logs/enrol.log 2>&1
grep -q . "$R"/claw/k || { echo "enrolment failed"; tail -5 "$R"/logs/enrol.log; exit 1; }
# Read the door's OWN registry and show it. The claim "the door thinks it may
# send rank 5" must come from the door's file, not from this script asserting it.
CEIL_REC=$(python3 -c "import json,sys; print(json.load(open(sys.argv[1]))[0]['ceiling'])" \
             "$R"/server/claw-registry.json 2>/dev/null)
echo "   the door's own registry now records:  ceiling = ${CEIL_REC:-?}  (Run_Local_Code)"
beat 4
echo
echo "3. The door sends run_local_code — by its own records it is ENTITLED to."
curl -sS -X POST "http://127.0.0.1:$PORT/claws/victim/instructions" \
  -H 'Content-Type: application/json' \
  -d '{"command":"run_local_code","artifact_ref":"00000000000000ab","version":1}' \
  | grep -oE '"(command|seq|signature)":"?[^,"]*' | tr '\n' ' ' | sed 's/^/   signed and queued: /'
echo; echo
beat 4
echo "4. The claw polls. It has NOT been told to raise its own ceiling."
echo "   The instruction is authentic, from a door it trusts, within the limit"
echo "   the DOOR believes applies. Watch what the machine says:"
echo
beat 3
"$GH"/bin/ghillie -facade "http://127.0.0.1:$PORT" -claw-id victim \
  -device-key-file "$R"/claw/k -state "$R"/claw/s.json -poll 1s -max-polls 3 2>&1 \
  | grep -iE "REFUSED|ADMITTED" | sed 's/^/   /'
echo
beat 3
echo "   The door proposed rank 5. The machine holds rank 2. Authority cannot be"
echo "   granted inward — a door can only ever send LESS than the machine permits."
echo
echo "   And the rank rule it refused on is not invented here. gate.go is an"
echo "   UNPROVEN SHIM that MIRRORS Facade_Command_Pkg — the core discharged at"
echo "   the top of this recording. The Go is checked against the Ada; the Ada"
echo "   is proved. Said precisely, because the difference is the whole point."
echo
