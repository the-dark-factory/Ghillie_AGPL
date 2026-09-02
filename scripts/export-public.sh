#!/bin/zsh
# export-public.sh — produce the PUBLIC release tree, per the approved
# release proposal (proposal_ghillie_release_2026-08-26) and the 08-26 sweep.
#
# THE EXPORT IS A CLEAN TREE, NEVER THE REPOSITORY: no .git travels — the
# history is presumed to contain internal material many times over and is
# not shipped, scanned, or argued about. The public repo's first commit is
# this export, made by the OWNER by hand.
#
# Exclusions are POLICY, listed here so changing the policy is editing this
# list in the open:
#   - internal/gate/unguarded.go + the Makefile's `unguarded` target: the
#     measurement control arm does not ship (Tony's gate; recommendation
#     recorded 2026-08-26).
#   - docs/KEY_POLICY.md: internal key/backup policy.
#   - verification/: recorded verifier evidence with machine paths in its
#     verbatim inputs. EXCLUDED BY DEFAULT pending Tony's ruling — flip
#     SHIP_VERIFICATION=1 to include it unchanged, with its README caveat.
#   - bin/ dist/ .demo/ quarantine/ *.log: build products and run leavings.
#
# Usage: ./scripts/export-public.sh <dest-dir>

set -e -u -o pipefail

(( $# == 1 )) || { print -u2 "usage: $0 <dest-dir>"; exit 2; }
DEST=$1
ROOT=${0:a:h}/..
SHIP_VERIFICATION=${SHIP_VERIFICATION:-0}

[[ -e $DEST ]] && { print -u2 "export-public: $DEST already exists — refusing to overwrite an export"; exit 1; }
mkdir -p "$DEST"

# ONLY GIT-TRACKED FILES CAN SHIP. The working tree accumulates run
# leavings — keys, state files, built binaries, test bundles — and a copy
# of the directory would take them all. git archive of HEAD takes exactly
# what is committed, with no history attached.
( cd "$ROOT" && git archive HEAD ) | tar -x -C "$DEST"

# Policy exclusions, applied to the archived tree:
rm -f  "$DEST"/internal/gate/unguarded.go
rm -f  "$DEST"/docs/KEY_POLICY.md
(( SHIP_VERIFICATION )) || rm -rf "$DEST"/verification
# A WORKFLOW WHOSE SOURCE DOES NOT SHIP MUST NOT SIT IN THE PUBLIC REPO EITHER.
# Same rule as the unguarded Make target below. gobra.yml runs in
# verification/gobra, which the line above removes, so on the public mirror it
# failed on every push from 2026-08-27 onward — emailing a failure notice each
# time for a directory that was never going to be there. Deleted with its
# subject.
(( SHIP_VERIFICATION )) || rm -f "$DEST"/.github/workflows/gobra.yml

# Belt over braces: no key, state, or database file ships whatever happens.
find "$DEST" \( -name '*.key' -o -name '*.pin' -o -name 'ghillie-state*.json' -o -name '*.db' \) -delete

# The unguarded Make target dies with its file — a target whose source does
# not ship must not sit in the public Makefile looking buildable.
python3 - "$DEST/Makefile" << 'PYEOF'
import re, sys
p = sys.argv[1]
s = open(p).read()
s = re.sub(r"\n# THE CONTROL ARM.*?built \$\(BIN\)/ghillie-unguarded[^\n]*\n", "\n", s, flags=re.S)
s = s.replace(" unguarded dist", " dist")
open(p, 'w').write(s)
PYEOF

# Prove the export builds and its tests pass, from the export itself.
( cd "$DEST" && go build ./... && go vet ./... && go test ./... > /dev/null && echo "export: build+vet+test clean" )

# Last look: nothing internal survives.
if grep -rniE "steelclaw|spec-home|gertrude|/Users/tony|ObVault/df-" \
    --include='*.go' --include='*.sh' --include='*.md' --include='Makefile' "$DEST" \
    | grep -v "scripts/export-public.sh" | grep -v "keeps working" | grep .; then
  print -u2 "export-public: INTERNAL RESIDUE ABOVE — the export is not clean; fix the tree, not the export"
  exit 1
fi
print -- "export ready at $DEST — the owner reads it, then pushes it by hand"
