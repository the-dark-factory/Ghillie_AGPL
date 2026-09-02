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
echo "wrote $DEST/index.json"
