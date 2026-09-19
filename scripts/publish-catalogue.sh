#!/bin/sh
# publish-catalogue.sh — pack every bundle in bundles/ and write index.json
# with real digests. The catalogue's primary consumer is GHILLIE, so each
# entry carries what he needs to decide whether an ability FITS a moment:
# when to offer it, what it needs from the person, its honest proof status,
# and its cost. Run from the repo root.
set -eu
DEST="${1:-$HOME/ObVault/ghillie-home/catalogue}"
mkdir -p "$DEST"
cd bundles

# offer/needs/proof/cost per bundle, kept HERE rather than in the bundle so a
# publisher's claims and an author's documents stay separable.
meta() {
  case "$1" in
    paid-twice)
      OFFER="when they wonder aloud about a payment they may have made twice, or ask about checking their spending"
      NEEDS="a payments CSV they export themselves: date,payee,amount"
      PROOF="prototype"; COST="free"
      SUMMARY="flags lookalike double payments — same payee, same amount, close together" ;;
    the-old-words)
      OFFER="when they ask about the old language, his people, or how a Gaelic word is said"
      NEEDS="nothing but a line of Irish"
      PROOF="prototype"; COST="free"
      SUMMARY="speaks a line of Gaeilge in the household's own Gaelic voice" ;;
    *)
      OFFER="when asked"; NEEDS=""; PROOF="prototype"; COST="free"
      SUMMARY="$1" ;;
  esac
}

printf '{\n  "catalogue": "The Dark Factory — ghillie abilities",\n  "published": "%s",\n  "abilities": [\n' \
  "$(date -u +%Y-%m-%dT%H:%M:%SZ)" > "$DEST/index.json"

first=1
for b in */; do
  b="${b%/}"
  [ -f "$b/human.md" ] || continue
  COPYFILE_DISABLE=1 tar -czf "$DEST/$b.tar.gz" "$b"
  DIGEST=$(shasum -a 256 "$DEST/$b.tar.gz" | cut -d' ' -f1)
  BYTES=$(wc -c < "$DEST/$b.tar.gz" | tr -d ' ')
  meta "$b"
  [ $first -eq 1 ] || printf ',\n' >> "$DEST/index.json"
  first=0
  printf '    {\n      "name": "%s",\n      "summary": "%s",\n      "offer": "%s",\n      "needs": "%s",\n      "proof": "%s",\n      "cost": "%s",\n      "archive": "%s.tar.gz",\n      "digest": "%s",\n      "bytes": %s\n    }' \
    "$b" "$SUMMARY" "$OFFER" "$NEEDS" "$PROOF" "$COST" "$b" "$DIGEST" "$BYTES" >> "$DEST/index.json"
  echo "packed $b ($BYTES bytes, $DIGEST)"
done
printf '\n  ]\n}\n' >> "$DEST/index.json"

# PRUNE WHAT WAS OURS AND IS NO LONGER — AND NOTHING ELSE.
#
# This script only ever ADDED. It packs every bundle in bundles/ and rewrites
# index.json, but a .tar.gz already in $DEST for a bundle since removed was
# never touched — so it stayed staged, stayed on the host, and stayed fetchable
# by direct URL long after it left the index.
#
# Not hypothetical: on 2026-09-02 commit 861c5a8 removed bundles/the-old-words
# after df-opsec flagged its provenance for naming never-public codenames. Nine
# days later the identical archive was still served at 200, byte-for-byte the
# flagged one, because removing a bundle from the tree is not a recall.
#
# ★ IT MUST NOT PRUNE WHAT WAS NEVER OURS. Three policy abilities —
# brief-fill-policy, mouth-policy, guard-nudge-policy — are forged and packed
# ELSEWHERE and have never been in this repo's history. Their archives in $DEST
# may be the only copies on this machine. A prune that deletes every archive
# without a bundles/ directory destroys them. Git history is the discriminator:
# prune only what this tree once held and no longer does.
for f in "$DEST"/*.tar.gz; do
  [ -e "$f" ] || continue
  n=$(basename "$f" .tar.gz)
  [ -d "$n" ] && continue
  if git -C .. log --oneline --all -- "bundles/$n" 2>/dev/null | head -1 | grep -q .; then
    echo "PRUNED   $n.tar.gz — was in this tree, was removed; it is no longer ours to serve"
    rm -f "$f"
  else
    echo "kept     $n.tar.gz — never in this repo's history; built elsewhere, not ours to delete"
  fi
done

echo "wrote $DEST/index.json"
echo
echo "NOTE: this updates the STAGING directory only ($DEST)."
echo "      Whatever uploads it must also DELETE remotely what was pruned here;"
echo "      an upload that only copies leaves the withdrawn archive live."
