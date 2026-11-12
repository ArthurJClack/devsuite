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
 {'id':'secret','name':'SECRET','genre':'rpg','repo':'arthur/secret','releases':'arthur/secret-releases','color':'#c77dff','release':{'workflow':'release.yml','ref':'main','update':{'kind':'update','dry_run':'false'},'hotfix':{'kind':'hotfix','dry_run':'false'},'test':{'kind':'update','dry_run':'true'}}},
 {'id':'mars','name':'FOR/AGAINST','genre':'rts','repo':'arthur/marsprivate','releases':'arthur/mars','color':'#ff6b35','publish':'tools/build_thing.sh --yes'},
 {'id':'shells','name':'SHELLS','genre':'fps','repo':'arthur/shells','releases':'arthur/shells-release','color':'#4cc9f0'},
 {'id':'shitbox','name':'SHITBOX RALLY','genre':'racing','repo':'arthur/shitbox-rally','releases':'arthur/shitboxrally-releases','color':'#ffd166'}]}
print(json.dumps(t))")}
EOF
mkdir -p "$T/bin"; printf '#!/bin/sh\necho "$@" >> "%s/godot.log"\n[ "$1" = --headless ] && mkdir -p "$3/.godot"\nexit 0\n' "$T" > "$T/bin/godot"; chmod +x "$T/bin/godot"
export PATH="$T/bin:$PATH"
python3 tests/fakegh.py "$R" 7398 & FAKE=$!
HQ_IMPORT_PACE_MS=0 HQ_API=http://127.0.0.1:7398 HQ_GIT_BASE="file://$R/" "$T/hq" --port 7399 --no-browser > "$T/hq.log" 2>&1 & HQ=$!
trap 'kill $FAKE $HQ 2>/dev/null || true' EXIT
sleep 1
K=$(cat "$HQ_HOME/key")
c() { curl -sf -H "X-HQ-Key: $K" -H 'Content-Type: application/json' "$@"; }
wait_job() { local id; id=$(python3 -c 'import json,sys;print(json.load(sys.stdin)["id"])'); for _ in $(seq 400); do
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

# sync status: the pull above recorded what came in
c http://127.0.0.1:7399/api/state | python3 -c '
import json,sys;d=json.load(sys.stdin);h=d["sync"]["secret"]["history"]
assert h[0]["op"]=="pull" and h[0]["state"]=="ok" and any("mate work" in x for x in h[0]["commits"]), h
assert h[1]["op"]=="push" and h[1]["state"]=="ok" and any("added a level" in x for x in h[1]["commits"]), h
print("✓ sync status: pull + push recorded with their commits")'
# background fetch sees new dev work without touching files
w3=$T/mate3; G clone -q -b dev "$R/arthur/shitbox-rally.git" "$w3"; echo r > "$w3/road.txt"; G -C "$w3" add -A; G -C "$w3" commit -qm "new road"; G -C "$w3" push -q origin dev
c -X POST http://127.0.0.1:7399/api/fetch -d '{}' >/dev/null
c http://127.0.0.1:7399/api/state | python3 -c 'import json,sys;g={x["id"]:x for x in json.load(sys.stdin)["games"]};assert g["shitbox"]["local"]["behind"]==1,g["shitbox"]["local"];print("✓ check github: 1 new on dev")'
[ ! -f "$T/ws/shitbox/road.txt" ] && ok "fetch didn't touch files"
# update all
c -X POST http://127.0.0.1:7399/api/sync-all -d '{"op":"pull"}' > "$T/sa.json"
python3 -c 'import json;r=json.load(open("'"$T"'/sa.json"));assert len([x for x in r if x.get("job")])==4,r' && sleep 3
c http://127.0.0.1:7399/api/state | python3 -c '
import json,sys;d=json.load(sys.stdin);y=d["sync"]
assert y["shitbox"]["history"][0]["summary"]=="got 1 new commit", y["shitbox"]
assert y["secret"]["history"][0]["state"]=="nothing", y["secret"]
print("✓ update all: per-game status (got 1 / up to date)")'
[ -f "$T/ws/shitbox/road.txt" ] && ok "update all pulled the new road"
# push all skips games with nothing to send
echo t > "$T/ws/shitbox/tweak.txt"
c -X POST http://127.0.0.1:7399/api/sync-all -d '{"op":"push","message":"tweaks"}' | python3 -c '
import json,sys;r={x["game"]:x for x in json.load(sys.stdin)};assert r["shitbox"].get("job") and r["secret"].get("skip")=="no changes",r;print("✓ push all: only games with changes")'
sleep 3
git --git-dir "$R/arthur/shitbox-rally.git" log dev -1 --format=%s | grep -q tweaks && ok "push all landed on dev"

# publish: secret via its github release workflow, mars via a local script
c -X POST http://127.0.0.1:7399/api/game/secret/publish -d '{}' | wait_job > "$T/pub.log" && grep -q "released 0.1.0" "$T/pub.log" && grep -q "step 2/4: tests" "$T/pub.log" && ok "publish (workflow): started, watched step by step, released 0.1.0"
c http://127.0.0.1:7399/api/state | python3 -c 'import json,sys;h=json.load(sys.stdin)["sync"]["secret"]["history"][0];assert h["op"]=="update" and h["summary"]=="published 0.1.0",h;print("✓ publish status recorded")'
c http://127.0.0.1:7399/api/chat | grep -q "0.1.0 is out" && ok "publish announced in chat"
c -X POST http://127.0.0.1:7399/api/game/secret/test -d '{}' | wait_job >/dev/null && c http://127.0.0.1:7399/api/state | grep -q "dry run passed" && ok "test build (dry run)"
c -X POST http://127.0.0.1:7399/api/game/mars/publish -d '{}' | wait_job | grep -q "^build" && ok "publish (script): ran with live output"
curl -s -H "X-HQ-Key: $K" -X POST http://127.0.0.1:7399/api/game/shells/publish -d "{}" | grep -q "no update publishing" && ok "games without publishing say so"

# play dev: separate copy of latest dev, launched in godot, your checkout untouched
echo "wip" > "$T/ws/secret/wip.txt"
c -X POST http://127.0.0.1:7399/api/game/secret/playdev -d '{}' | wait_job >/dev/null
[ -f "$T/ws/.dev/secret/mate.txt" ] && [ ! -f "$T/ws/.dev/secret/wip.txt" ] && [ -f "$T/ws/secret/wip.txt" ] && ok "play dev: separate dev copy, your work untouched"
grep -q -- "--headless --path $T/ws/.dev/secret --import" "$T/godot.log" && grep -q -- "^--path $T/ws/.dev/secret$" "$T/godot.log" && ok "play dev: imported once + launched godot"
G -C "$w2" pull -q; echo more > "$w2/more.txt"; G -C "$w2" add -A; G -C "$w2" commit -qm "more"; G -C "$w2" push -q origin dev
c -X POST http://127.0.0.1:7399/api/game/secret/playdev -d '{}' | wait_job >/dev/null
[ -f "$T/ws/.dev/secret/more.txt" ] && [ "$(grep -c -- --import "$T/godot.log")" = 1 ] && ok "play dev again: updated, no re-import"
rm "$T/ws/secret/wip.txt"

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

# board v2: priority, tags, due date, checklist, multiple assignees, comments, starter board
c -X PATCH http://127.0.0.1:7399/api/board/2 -d '{"priority":"p0","tags":["bug","combat"],"due":"2026-12-01","body":"feels like ice\n\n- [x] repro\n- [ ] fix","assignees":["arthur","maya"]}' | python3 -c '
import json,sys;c=json.load(sys.stdin)
assert c["priority"]=="p0" and c["tags"]==["bug","combat"] and c["due"]=="2026-12-01" and (c["checkDone"],c["checkTotal"])==(1,2) and len(c["assignees"])==2 and "hq:due" not in c["body"] and c["column"]=="todo", c
print("✓ card: priority, tags, due, checklist, assignees")'
c -X PATCH http://127.0.0.1:7399/api/board/2 -d '{"column":"doing"}' | python3 -c 'import json,sys;c=json.load(sys.stdin);assert c["due"]=="2026-12-01" and c["priority"]=="p0" and c["column"]=="doing",c;print("✓ moving keeps the rest")'
c -X POST http://127.0.0.1:7399/api/board/2/comments -d '{"text":"on it"}' >/dev/null
c http://127.0.0.1:7399/api/board/2/comments | grep -q '"on it"' && ok "card comments"
c http://127.0.0.1:7399/api/templates | grep -q '"secret"' && ok "secret starter board listed"
c -X POST http://127.0.0.1:7399/api/board/import -d '{"game":"secret"}' | wait_job | tail -1
c -X POST http://127.0.0.1:7399/api/board/import -d '{"game":"secret"}' | wait_job | tail -1
c http://127.0.0.1:7399/api/board | python3 -c '
import json,sys;b=json.load(sys.stdin);s=[c for c in b if c["game"]=="secret"]
assert len(s)==35+1, len(s)
pick=[c for c in s if c["title"].startswith("pick the combat")][0]
assert pick["column"]=="doing" and pick["priority"]=="p0" and pick["checkTotal"]==6, pick
assert sum(1 for c in s if c["column"]=="done")>=7, [c["column"] for c in s]
print("✓ starter board imported once (no dupes on re-import)")'

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
