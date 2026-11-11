#!/usr/bin/env bash
# auto-update: an old installed build sees a newer release on the fake github, downloads it, swaps itself and restarts on the same port.
set -euo pipefail
cd "$(dirname "$0")/.."
T=$(mktemp -d); export HQ_HOME=$T/home HOME=$T/userhome HQ_INSTALL_DIR=$T/installed
mkdir -p "$HOME/Downloads" "$T/dl" "$HQ_HOME"
mkdir -p "$T/installed"; go build -ldflags "-X main.version=v0.1.0" -o "$T/installed/hq" .
go build -ldflags "-X main.version=v0.2.0" -o "$T/dl/hq-linux" .
echo '{"studio":"fish market studios","games":[{"id":"x","name":"X","genre":"rpg","repo":"arthur/x"}]}' > "$T/installed/team.json"
FAKE_DL=$T/dl FAKE_TAG=v0.2.0 python3 tests/fakegh.py "$T" 7396 & FAKE=$!
trap 'kill $FAKE 2>/dev/null; cat "$HQ_HOME/hq.log" 2>/dev/null; pkill -f "$T/installed/hq" 2>/dev/null || true' EXIT
sleep 0.5
HQ_API=http://127.0.0.1:7396 HQ_UPDATE_REPO=arthur/devsuite "$T/installed/hq" --port 7397 --no-browser >> "$HQ_HOME/hq.log" 2>&1 &
for i in $(seq 40); do [ -f "$HQ_HOME/key" ] && break; sleep 0.25; done; K=$(cat "$HQ_HOME/key")
v() { curl -sf -H "X-HQ-Key: $K" http://127.0.0.1:7397/api/update | python3 -c 'import json,sys;print(json.load(sys.stdin)["current"])' 2>/dev/null || true; }
for i in $(seq 60); do [ "$(v)" = v0.2.0 ] && break; sleep 0.5; done
[ "$(v)" = v0.2.0 ] && echo "✓ auto-updated to v0.2.0 and back up on the same port" || { echo "✗ still on $(v)"; cat "$HQ_HOME/hq.log" 2>/dev/null; exit 1; }
[ "$("$T/installed/hq" --version)" = "hq v0.2.0" ] && echo "✓ installed exe is v0.2.0"
sleep 1; [ ! -e "$T/installed/hq.old" ] && echo "✓ old exe cleaned up"
curl -sf -H "X-HQ-Key: $K" http://127.0.0.1:7397/api/state | grep -q '"studio":"fish market studios"' && echo "✓ studio kept"
echo "ALL GOOD"
