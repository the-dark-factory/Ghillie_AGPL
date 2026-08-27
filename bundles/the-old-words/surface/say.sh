#!/bin/sh
# say.sh — speak a line of Gaeilge in the old voice, full pipeline, local only.
LINE="$1"
[ -n "$LINE" ] || { echo 'usage: say.sh "<Gaeilge line>"' >&2; exit 1; }
MODEL="$HOME/ObVault/voice-assets/kenny/kenny_4744.onnx"
PIPER="$HOME/dev/respire/.venv-piper/bin/piper"
HARNESS="$HOME/dev/respire/demo/textplan.py"
PY="$HOME/dev/respire/.venv-kokoro/bin/python"
for p in "$MODEL" "$PIPER" "$HARNESS" "$PY"; do
  [ -e "$p" ] || { echo "missing: $p — cannot speak without it" >&2; exit 1; }
done
OUT="$HOME/ObVault/ghillie-voice/old-words"
mkdir -p "$OUT"
STAMP=$(date +%Y%m%d-%H%M%S)
echo "$LINE" | "$PIPER" --model "$MODEL" --output_file "$OUT/$STAMP.speech.wav" 2>/dev/null || exit 1
TP_TEXT="$LINE" TP_SRC_WAV="$OUT/$STAMP.speech.wav" "$PY" "$HARNESS" "$OUT/$STAMP.wav" >/dev/null 2>&1 || exit 1
rm -f "$OUT/$STAMP.speech.wav"
exec afplay "$OUT/$STAMP.wav"
