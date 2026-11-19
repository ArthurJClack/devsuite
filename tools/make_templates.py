# rebuilds templates/*.json from each game repo's docs (FOR/AGAINST docs/BACKLOG.md, SHITBOX RALLY docs/AUDIT_2026-11.md + docs/CODEBASE_PLAN.md);
# expects those files dumped to /tmp first (see the paths below). SECRET's is hand-made from its docs.
import json, re
def clean(t):
    t = re.sub(r'\*\*', '', t).strip()
    return t[:180] + ('…' if len(t) > 180 else '')
OUT = '/home/claude/hq/templates/'

# ---------- FOR/AGAINST: one card per backlog section, items as a checklist; ★ items get their own card
md = open('/tmp/mars_backlog.md').read()
tagmap = {'Vehicles': ['feature'], 'Soldiers and AI': ['tech'], 'CQB': ['combat'], 'Weapons and ballistics': ['combat'],
          'Armor and medical': ['combat'], 'Environment and destruction': ['world'], 'Stealth and intel': ['design'],
          'Player control and UI': ['ui'], 'Co-op and meta': ['feature'], 'Realism pass (next)': ['combat'], 'Vehicle roadmap': ['feature']}
cards = []; sec = None; items = {}
for line in md.splitlines():
    if line.startswith('## '):
        sec = line[3:].strip(); items[sec] = []
    m = re.match(r'^\d+\.\s+(.*)$', line)
    if m and sec:
        t = m.group(1); done = t.startswith('✓'); star = t.startswith('★')
        t = clean(t.lstrip('✓★ '))
        items[sec].append((done, star, t))
for sec, its in items.items():
    if not its: continue
    done = sum(1 for d, s, t in its if d)
    open_ = [x for x in its if not x[0]]
    if not open_: continue
    body = f"from docs/BACKLOG.md ({sec}). {done} of {len(its)} already in the game.\n\n" + "\n".join(f"- [{'x' if d else ' '}] {t}" for d, s, t in its)
    cards.append({"title": f"backlog: {sec.lower()}", "column": "planning", "priority": "p1" if 'next' in sec.lower() else "p2", "tags": tagmap.get(sec, []), "body": body})
    for d, s, t in its:
        if s: cards.append({"title": t[:90], "column": "implementation", "priority": "p1", "tags": tagmap.get(sec, []), "body": f"marked ★ (being built) in docs/BACKLOG.md, {sec}."})
for v, what in [("v0.1.9.1", "hotfix: hold orders keep the picked cover, WITNESS line-of-fire fixes"), ("v0.1.9.0", "combat, world fixes, rebalance, enemy ai"), ("v0.1.8.0", "story jobs, dialogue editor, co-op SERVER FARM + OPEN SEASON")]:
    cards.append({"title": f"release {v}: {what}", "column": "shipped", "tags": ["feature"]})
json.dump({"game": "mars", "name": "FOR/AGAINST", "cards": cards}, open(OUT + 'mars.json', 'w'), indent=1)
print('mars', len(cards))

# ---------- SHITBOX RALLY: audit sections (open items) + codebase plan items
md = open('/tmp/sb_audit.md').read()
cards = []; sec = None; sub = None; groups = {}
for line in md.splitlines():
    if line.startswith('## '): sec = re.sub(r'^[A-Z]\.\s*', '', line[3:].strip()); sub = None
    elif line.startswith('### '): sub = line[4:].strip()
    m = re.match(r'^\d+\.\s+\[(.)\]\s*(?:\([^)]*\)\s*)?(.*)$', line)
    if m and sec:
        key = sec + (' / ' + sub if sub else '')
        groups.setdefault(key, []).append((m.group(1) in 'x-', clean(m.group(2))))
def tags_for(k):
    k = k.lower(); t = []
    if 'perf' in k: t.append('tech')
    if 'ux' in k or 'ui' in k: t.append('ui')
    if 'text' in k: t.append('writing')
    if 'test' in k or 'tool' in k: t.append('tools')
    if 'bug' in k or 'broken' in k or 'found' in k or 'finds' in k: t.append('bug')
    return t or ['bug']
for k, its in groups.items():
    open_ = [t for d, t in its if not d]
    if not open_: continue
    done = len(its) - len(open_)
    body = f"open items from docs/AUDIT_2026-11.md ({k}): {len(open_)} open, {done} fixed.\n\n" + "\n".join(f"- [{'x' if d else ' '}] {t}" for d, t in its)
    k = re.sub(r'\s*\(.*?\)', '', k)
    cards.append({"title": f"audit: {k.lower()}", "column": "implementation" if done else "planning", "priority": "p2", "tags": tags_for(k), "body": body})
plan = open('/tmp/sb_plan.md').read()
for sec, chunk in re.findall(r'^## (\d\. [^\n]+)\n(.*?)(?=^## |\Z)', plan, flags=re.M | re.S):
    if sec.startswith('5'): continue
    for m in re.finditer(r'(?:^\d+\.|^-)\s+\*\*([^*]+)\*\*\s*(.*?)(?=^\d+\.|^- |\Z)', chunk, flags=re.M | re.S):
        title = m.group(1).strip().rstrip('.')
        if 'done in' in (title + m.group(2)).lower(): continue
        body = re.sub(r'\s+', ' ', m.group(2)).strip()
        cards.append({"title": f"codebase: {title}", "column": "planning", "priority": "p2", "tags": ["tech"],
                      "body": f"from docs/CODEBASE_PLAN.md (Oct 22), {sec}. may already be partly done: check before starting.\n\n{body}"})
json.dump({"game": "shitbox", "name": "SHITBOX RALLY", "cards": cards}, open(OUT + 'shitbox.json', 'w'), indent=1)
print('shitbox', len(cards))

# ---------- SHELLS: the fresh gunplay demo (what the README says is in it)
feat = ["the Mk 23 standard issue, built off FOR/AGAINST's Glock frame", "ammo types in mags: FMJ, AP, hollow point, frangible, incendiary, shock, subsonic, tracer",
        "found guns from the FOR/AGAINST armory: M4A1, AK-74M, SIG MPX, MWS 6.5, Benelli M4", "simulated rounds: muzzle speed, drop, drag",
        "recoil: Tarkov pull-down + Halo recovery", "ADS onto the real optic, proper 4x scope picture", "procedural viewmodel: sway, bob, springs, reloads, casings",
        "diegetic UI: ammo on the gun, stats board over the lanes", "targets: zoned mannequins, armored, mover, gongs, security drone"]
cards = [{"title": "gunplay demo: range on security deck C-2", "column": "implementation", "priority": "p1", "tags": ["combat"],
          "body": "the first slice from the README: nail how the guns feel before anything else gets built on top.\n\n" + "\n".join(f"- [x] {f}" for f in feat)},
         {"title": "decide the slice after the gunplay demo", "column": "planning", "priority": "p2", "tags": ["design"],
          "body": "SHELLS is now a tactical FPS survival horror on a dead colony ship (you're a security guard). the README stops at the gun range."},
         {"title": "old pvp shooter wiped for the fresh start", "column": "shipped", "tags": ["tech"]}]
json.dump({"game": "shells", "name": "SHELLS", "cards": cards}, open(OUT + 'shells.json', 'w'), indent=1)

# ---------- IGL: from the README + this week's work
cards = [{"title": "balance pass: headless sim-test win rates per mode", "column": "planning", "priority": "p2", "tags": ["design", "tools"],
          "body": "the README's headless balance test plays every stratbook execute 6x per mode and prints win rates (--sim-test, IGL_TEST_MODES=exec)."},
         {"title": "more maps through the marker editor", "column": "planning", "priority": "p2", "tags": ["world"],
          "body": "README flow: drop <name>.glb in assets/cs2/maps, place spawns, sites, ct holds and rotate spots with F2, save with F3."},
         {"title": "real cs2 animations on agents", "column": "qa", "priority": "p1", "tags": ["art"],
          "body": "62 agent clips on a layered AnimationTree, additive shot/flinch, gun on weapon_hand_R (Nov 15). check it in matches."},
         {"title": "dust2 playable", "column": "qa", "priority": "p1", "tags": ["world"],
          "body": "markers from the map's buyzone/bombsite brushes, auto rush/exec strats from the navmesh, ct holds aimed at t routes (Nov 15)."},
         {"title": "prototype: sim, 4 modes (rts, execute, exec + timeouts, hybrid), caster, cs2 export", "column": "shipped", "tags": ["feature"]}]
json.dump({"game": "igl", "name": "IGL", "cards": cards}, open(OUT + 'igl.json', 'w'), indent=1)
print('done')
