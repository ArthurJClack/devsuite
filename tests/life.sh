#!/usr/bin/env bash
# app mode: hq stays up while a window holds /api/live (also across a reload) and quits ~20 s
# after the last window closes. a background panic is logged, not fatal.
set -euo pipefail
cd "$(dirname "$0")/.."
T=$(mktemp -d); export HQ_HOME=$T/home HOME=$T/userhome; mkdir -p "$HQ_HOME"
go build -o "$T/hq" .
export PATH="/usr/bin:/bin"  # no chrome/xdg-open: the window open fails quietly, app mode stays on
HQ_API=http://127.0.0.1:1 "$T/hq" --port 7395 > "$T/out" 2>&1 & P=$!
sleep 1; K=$(cat "$HQ_HOME/key")
live() { curl -sN -H "X-HQ-Key: $K" http://127.0.0.1:7395/api/live > /dev/null & echo $!; }
W=$(live); sleep 25
kill -0 $P && echo "✓ stays up while a window is open (even with no other requests)" || { echo "✗ quit with a window open"; exit 1; }
kill $W; sleep 0.5; W=$(live)   # a reload
sleep 25; kill -0 $P && echo "✓ survives a reload" || { echo "✗ quit on reload"; exit 1; }
kill $W; sleep 25
kill -0 $P 2>/dev/null && { echo "✗ still running after the window closed"; kill $P; exit 1; } || echo "✓ quits when the last window closes"
grep -q "hq dev starting" "$HQ_HOME/hq.log" && grep -q "window closed, quitting" "$HQ_HOME/hq.log" && echo "✓ hq.log written"
echo "ALL GOOD"
