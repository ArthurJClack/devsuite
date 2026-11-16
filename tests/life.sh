#!/usr/bin/env bash
# app mode: hq quits a few seconds after its window says bye, and stays through a reload.
set -euo pipefail
cd "$(dirname "$0")/.."
T=$(mktemp -d); export HQ_HOME=$T/home HOME=$T/userhome; mkdir -p "$HQ_HOME"
go build -o "$T/hq" .
export PATH="/usr/bin:/bin"  # no chrome/xdg-open: the window open fails quietly, app mode stays on
HQ_API=http://127.0.0.1:1 "$T/hq" --port 7395 > "$T/log" 2>&1 & P=$!
sleep 1; K=$(cat "$HQ_HOME/key")
c() { curl -sf -H "X-HQ-Key: $K" "$@" >/dev/null; }
c http://127.0.0.1:7395/api/ping
c -X POST "http://127.0.0.1:7395/api/bye?k=$K"; sleep 0.5; c http://127.0.0.1:7395/api/ping   # a reload
sleep 9; kill -0 $P && echo "✓ survives a reload" || { echo "✗ quit on reload"; exit 1; }
c -X POST "http://127.0.0.1:7395/api/bye?k=$K"; sleep 10
kill -0 $P 2>/dev/null && { echo "✗ still running after window closed"; kill $P; exit 1; } || echo "✓ quits when the window closes"
echo "ALL GOOD"
