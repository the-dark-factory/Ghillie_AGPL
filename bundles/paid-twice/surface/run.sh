#!/bin/sh
# non-visual surface: CSV in, flags out, nothing changed, nothing sent.
[ -f "$1" ] || { echo "usage: run.sh payments.csv  (date,payee,amount)" >&2; exit 1; }
exec awk -f "$(dirname "$0")/paid-twice.awk" "$1"
