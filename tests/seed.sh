#!/usr/bin/env bash
# a fresh studio whose board was never set up: opening the board loads every starter board by
# itself, once. emptying it later doesn't bring them back.
set -euo pipefail
cd "$(dirname "$0")/.."
T=$(mktemp -d); R=$T/remotes; export HQ_HOME=$T/home HOME=$T/userhome
mkdir -p "$R/arthur" "$HQ_HOME"
for r in secret marsprivate shells shitbox-rally; do git init -q --bare -b main "$R/arthur/$r.git"; done
go build -o "$T/hq" .
cat > "$HQ_HOME/config.json" <<J
{"workspace":"$T/ws","team":{"studio":"fish market studios","games":[
 {"id":"secret","name":"SECRET","genre":"rpg","repo":"arthur/secret"},{"id":"mars","name":"FOR/AGAINST","genre":"rts","repo":"arthur/marsprivate"},
 {"id":"shells","name":"SHELLS","genre":"fps","repo":"arthur/shells"},{"id":"shitbox","name":"SHITBOX RALLY","genre":"racing","repo":"arthur/shitbox-rally"}]}}
J
python3 tests/fakegh.py "$R" 7394 & FAKE=$!
HQ_IMPORT_PACE_MS=0 HQ_API=http://127.0.0.1:7394 HQ_GIT_BASE="file://$R/" "$T/hq" --port 7393 --no-browser > "$T/hq.log" 2>&1 & HQ=$!
trap 'kill $FAKE $HQ 2>/dev/null || true' EXIT
sleep 1; K=$(cat "$HQ_HOME/key")
c() { curl -sf -H "X-HQ-Key: $K" -H 'Content-Type: application/json' "$@"; }
c -X POST http://127.0.0.1:7393/api/signin -d '{"token":"good-token"}' >/dev/null
J=$(c -X POST http://127.0.0.1:7393/api/studio/setup -d '{"repo":"fishmarket-hq"}' | python3 -c 'import json,sys;print(json.load(sys.stdin)["id"])')
for i in $(seq 50); do c http://127.0.0.1:7393/api/job/$J | grep -q '"done":true' && break; sleep 0.2; done
echo "$K" > /tmp/hq-seed-key
python3 - <<'PY'
from playwright.sync_api import sync_playwright
import json, urllib.request
k=open('/tmp/hq-seed-key').read().strip()
get=lambda p: json.load(urllib.request.urlopen(urllib.request.Request('http://127.0.0.1:7393'+p, headers={'X-HQ-Key':k})))
with sync_playwright() as p:
    b=p.chromium.launch(); pg=b.new_page(viewport={'width':1400,'height':900})
    pg.goto(f'http://127.0.0.1:7393/#k={k}'); pg.wait_for_timeout(2500)
    pg.evaluate("show('board')"); pg.wait_for_timeout(15000)
    n=len(get('/api/board')); print('✓ board filled itself:', n, 'cards') if n > 50 else print('✗ only', n)
    print('✓ studio marked seeded' if get('/api/state')['seeded'] else '✗ not marked')
    pg.reload(); pg.wait_for_timeout(2500); pg.evaluate("show('board')"); pg.wait_for_timeout(1500)
    print('cards shown:', pg.locator('#page-board .card').count())
    b.close()
PY
c -X POST http://127.0.0.1:7393/api/board/seed -d '{}' >/dev/null 2>&1 && echo "✗ seeded twice" || echo "✓ won't seed a second time"
