#!/usr/bin/env bash
# end-to-end: fake github + local bare repos + a real hq binary, driven over its HTTP api.
# usage: tests/e2e.sh   (leaves hq running on :7399 for screenshots when KEEP=1)
set -euo pipefail
cd "$(dirname "$0")/.."
T=$(mktemp -d); R=$T/remotes; export HQ_HOME=$T/home HOME=$T/userhome
mkdir -p "$R/arthur" "$HQ_HOME" "$HOME/Downloads"
G() { git -c user.name=seed -c user.email=s@x "$@"; }

seed() { # repo, with-dev
  local w=$T/seed-$1; mkdir -p "$w/tools"; G init -q -b main "$w"
  printf '[application]\nconfig/name="%s"\n' "$1" > "$w/project.godot"
  printf '#!/bin/sh\necho build\n' > "$w/tools/build_thing.sh"; chmod +x "$w/tools/build_thing.sh"
  G -C "$w" add -A; G -C "$w" commit -qm "first"
  git init -q --bare -b main "$R/arthur/$1.git"; G -C "$w" push -q "$R/arthur/$1.git" main
  if [ "$2" = dev ]; then G -C "$w" checkout -qb dev; echo x > "$w/devfile"; G -C "$w" add -A; G -C "$w" commit -qm "dev work"; G -C "$w" push -q "$R/arthur/$1.git" dev; fi
}
seed secret dev; seed marsprivate nodev; seed shells dev; seed shitbox-rally dev
for r in secret-releases mars shells-release shitboxrally-releases; do git init -q --bare "$R/arthur/$r.git"; done

go build -o "$T/hq" .
cat > "$HQ_HOME/config.json" <<EOF
{"workspace":"$T/ws","team":$(python3 -c "
import json,subprocess
t={'studio':'fish market studios','games':[
 {'id':'secret','name':'SECRET','genre':'rpg','repo':'arthur/secret','releases':'arthur/secret-releases','color':'#c77dff','publish':'tools/build_thing.sh'},
 {'id':'mars','name':'FOR/AGAINST','genre':'rts','repo':'arthur/marsprivate','releases':'arthur/mars','color':'#ff6b35'},
 {'id':'shells','name':'SHELLS','genre':'fps','repo':'arthur/shells','releases':'arthur/shells-release','color':'#4cc9f0'},
 {'id':'shitbox','name':'SHITBOX RALLY','genre':'racing','repo':'arthur/shitbox-rally','releases':'arthur/shitboxrally-releases','color':'#ffd166'}]}
print(json.dumps(t))")}
EOF
python3 tests/fakegh.py "$R" 7398 & FAKE=$!
HQ_API=http://127.0.0.1:7398 HQ_GIT_BASE="file://$R/" "$T/hq" --port 7399 --no-browser > "$T/hq.log" 2>&1 & HQ=$!
trap 'kill $FAKE $HQ 2>/dev/null || true' EXIT
sleep 1
K=$(cat "$HQ_HOME/key")
c() { curl -sf -H "X-HQ-Key: $K" -H 'Content-Type: application/json' "$@"; }
wait_job() { local id; id=$(python3 -c 'import json,sys;print(json.load(sys.stdin)["id"])'); for _ in $(seq 100); do
  r=$(c "http://127.0.0.1:7399/api/job/$id"); if python3 -c 'import json,sys;d=json.loads(sys.argv[1]);sys.exit(0 if d["done"] else 1)' "$r"; then
  python3 -c 'import json,sys;d=json.loads(sys.argv[1]);print(d["log"]);sys.exit(0 if d["ok"] else 1)' "$r"; return; fi; sleep 0.2; done; echo TIMEOUT; return 1; }
ok() { echo "✓ $*"; }

curl -s -o /dev/null -w '%{http_code}' http://127.0.0.1:7399/api/state | grep -q 403 && ok "no key → 403"
c -X POST http://127.0.0.1:7399/api/signin -d '{"token":"bad"}' >/dev/null && { echo "bad token accepted"; exit 1; } || ok "bad token rejected"
c -X POST http://127.0.0.1:7399/api/signin -d '{"token":"good-token"}' | grep -q arthur && ok "signed in"
c -X POST http://127.0.0.1:7399/api/studio/setup -d '{"repo":"fishmarket-hq","name":"fish market studios"}' | wait_job >/dev/null && ok "studio set up"
c http://127.0.0.1:7399/api/state | python3 -c 'import json,sys;d=json.load(sys.stdin);assert d["studioRepo"]=="arthur/fishmarket-hq" and d["chatIssue"]==1,d;print("✓ studio repo + chat issue saved")'

for g in secret mars shells shitbox; do c -X POST http://127.0.0.1:7399/api/game/$g/clone -d '{}' | wait_job >/dev/null; done && ok "downloaded 4 games"
[ "$(git -C "$T/ws/secret" branch --show-current)" = dev ] && ok "secret cloned on dev"
[ "$(git -C "$T/ws/mars" branch --show-current)" = main ] && ok "mars (no dev) cloned on main"
c http://127.0.0.1:7399/api/state | python3 -c '
import json,sys;d=json.load(sys.stdin);g={x["id"]:x for x in d["games"]}
assert g["secret"]["local"]["cloned"] and g["secret"]["local"]["hasDev"], g["secret"]
assert any(t["path"]=="tools/build_thing.sh" for t in g["secret"]["local"]["tools"]), g["secret"]["local"]["tools"]
assert g["secret"]["local"]["project"], "project.godot not found"
print("✓ local status + tools discovered")'

echo "new level" > "$T/ws/secret/level.txt"
c -X POST http://127.0.0.1:7399/api/game/secret/push -d '{"message":"added a level"}' | wait_job >/dev/null && ok "push to dev"
git --git-dir "$R/arthur/secret.git" log dev -1 --format=%s | grep -q "added a level" && ok "commit landed on remote dev"

# a teammate pushes to dev; we pull
w2=$T/mate; G clone -q -b dev "$R/arthur/secret.git" "$w2"; echo hi > "$w2/mate.txt"; G -C "$w2" add -A; G -C "$w2" commit -qm "mate work"; G -C "$w2" push -q origin dev
c -X POST http://127.0.0.1:7399/api/game/secret/pull -d '{}' | wait_job >/dev/null && [ -f "$T/ws/secret/mate.txt" ] && ok "pull dev got teammate's work"

# side branch → push merges into dev on github
G -C "$T/ws/shells" checkout -qb arthur/gun; echo bang > "$T/ws/shells/gun.txt"
c -X POST http://127.0.0.1:7399/api/game/shells/push -d '{"message":"new gun"}' | wait_job >/dev/null
git --git-dir "$R/arthur/shells.git" show dev:gun.txt | grep -q bang && ok "side branch merged into dev"

c -X POST http://127.0.0.1:7399/api/game/secret/tomain -d '{}' | wait_job >/dev/null
git --git-dir "$R/arthur/secret.git" show main:level.txt | grep -q level && ok "dev → main merged"

# mars has no dev: push creates it
echo u > "$T/ws/mars/unit.txt"
c -X POST http://127.0.0.1:7399/api/game/mars/push -d '{"message":"units"}' | wait_job >/dev/null
git --git-dir "$R/arthur/marsprivate.git" show dev:unit.txt | grep -q u && ok "push created dev on mars"

# board
c -X POST http://127.0.0.1:7399/api/board -d '{"title":"car handling feels floaty","game":"shitbox","column":"todo","assignee":"arthur"}' >/dev/null
c -X POST http://127.0.0.1:7399/api/board -d '{"title":"shell head models","game":"shells","column":"doing"}' >/dev/null
c -X POST http://127.0.0.1:7399/api/board -d '{"title":"city bake from OSM","game":"secret","column":"backlog"}' >/dev/null
c -X POST http://127.0.0.1:7399/api/board -d '{"title":"tank pathing","game":"mars","column":"review","assignee":"arthur"}' >/dev/null
c -X PATCH http://127.0.0.1:7399/api/board/3 -d '{"column":"done"}' >/dev/null
c http://127.0.0.1:7399/api/board | python3 -c '
import json,sys;b=json.load(sys.stdin);cols={c["number"]:c["column"] for c in b}
assert len(b)==4 and cols[3]=="done" and cols[2]=="todo" and cols[5]=="review", b
print("✓ board: create + move (chat issue hidden)")'

# chat
c -X POST http://127.0.0.1:7399/api/chat -d '{"text":"yo who broke the wrx"}' >/dev/null
c http://127.0.0.1:7399/api/chat | python3 -c 'import json,sys;m=json.load(sys.stdin);assert any("wrx" in x["text"] for x in m) and any("pushed" in x["text"] for x in m),m;print("✓ chat + push announcements")'

# team
c -X POST http://127.0.0.1:7399/api/team/add -d '{"user":"maya","repos":["secret","shells","studio"]}' | wait_job >/dev/null && ok "invited maya"
c http://127.0.0.1:7399/api/team | python3 -c 'import json,sys;t=json.load(sys.stdin);m={x["login"]:x for x in t["members"]};assert m["maya"]["pending"]["secret"],t;print("✓ team shows pending invite")'
c -X POST http://127.0.0.1:7399/api/team/package -d '{"user":"maya"}' | python3 -c 'import json,sys;print(json.load(sys.stdin)["path"])' > "$T/pkg"
unzip -l "$(cat "$T/pkg")" | grep -q team.json && unzip -p "$(cat "$T/pkg")" '*/team.json' | grep -q fishmarket-hq && ok "invite package has hq + team.json"
c -o /dev/null "http://127.0.0.1:7399/package/$(basename "$(cat "$T/pkg")")" && ok "package downloads"

c http://127.0.0.1:7399/api/remote | python3 -c 'import json,sys;r={x["id"]:x for x in json.load(sys.stdin)};assert r["shitbox"]["downloads"]==852 and r["shells"]["last"],r;print("✓ remote stats")'
c http://127.0.0.1:7399/api/activity | python3 -c 'import json,sys;a=json.load(sys.stdin);assert len(a)>=4;print("✓ activity feed")'
curl -s -o /dev/null -w '%{http_code}' -H "Host: evil.com" -H "X-HQ-Key: $K" http://127.0.0.1:7399/api/state | grep -q 403 && ok "dns rebinding blocked"
echo "ALL GOOD"
if [ "${KEEP:-}" = 1 ]; then echo "hq: http://127.0.0.1:7399/#k=$K"; echo "$K" > /tmp/hq-key; trap - EXIT; fi
