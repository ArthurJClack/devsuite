# hq · fish market studios

the all-in-one team app for fish market studios. one exe, opens in your browser, no server to run: github does all the syncing.

works with every game we have in dev: **SECRET** (rpg), **FOR/AGAINST** (rts), **SHELLS** (fps), **SHITBOX RALLY** (racing). adding another is a line in the games list.

## what it does

| page | what |
| --- | --- |
| dashboard | every game at a glance: latest build + downloads, how much of dev isn't on main yet, open PRs, where you are locally. recent work feed, your cards, mini chat |
| games | **⟳ update / ↑ push** per game or **update all / push all** at once, each with live status (what came in, what went out, history). a quiet `git fetch` every 5 minutes keeps "N new on dev" honest. **🚀 publish** an update, hotfix or test build: starts the game's github release workflow and shows every step until the release is out (or runs its publish script with live output). **▶ play dev**: runs the latest dev from github in its own copy (`.dev/<game>`), even if you haven't downloaded the game, without touching your work. download the source, **pull dev**, **push to dev**, **dev → main**, open in godot, play, open the folder, run any of the game's tools (`*.bat` in the root and `tools/`), publish an update or hotfix |
| board | trello-style: a board per game (or everything), lists backlog / todo / doing / review / done, drag cards between them, quick-add, priorities, colored tags, due dates (overdue goes red), checklists with progress, several people per card, comments, search + filters. each game can ship a starter board (`templates/<game>.json`, SECRET's comes from its docs) |
| chat | small team chat. pushes and merges post in it automatically |
| team | add someone as a collaborator to any of the repos, see who has access / pending invites, and **make their package**: a zip with hq + the studio settings. they unzip, double-click, sign in, done |
| settings | where games download to, godot path, which studio repo, sign out |

## how it works

- **games** are their source repos (private). hq runs plain `git` in `~/hq-games/<game>`; your github token is passed to git through env-only config, never written into `.git/config`.
- **push to dev** commits everything, rebases onto everyone else's dev, and pushes. from a side branch it pushes the branch and merges it into dev on github. if the game has no dev branch yet, the first push makes it.
- **dev → main** merges on github (no local checkout). a conflict tells you to open a PR.
- **the studio repo** (private, e.g. `ArthurJClack/fishmarket-hq`) holds `hq.json` (the shared games list), the board (its issues, labelled `hq:todo` etc + `game:<id>`) and the chat (comments on one pinned issue). everyone who has access sees the same thing. it all still works straight from github.com too.
- polling is cheap: github answers unchanged data with a 304 that doesn't count against the rate limit.
- the page only answers on `127.0.0.1`, and only with the key hq opens it with.

## install

**windows:** paste this in powershell (installs to `%LOCALAPPDATA%\Programs\hq`, adds start menu + desktop shortcuts, installs git if missing):
```powershell
irm https://raw.githubusercontent.com/ArthurJClack/devsuite/main/install.ps1 | iex
```
or grab [`hq.exe`](https://github.com/ArthurJClack/devsuite/releases/latest/download/hq.exe) and run it from any folder.

**mac / linux:** `curl -fsSL https://raw.githubusercontent.com/ArthurJClack/devsuite/main/install.sh | sh`

an invite package from a teammate is hq + the studio settings: unzip anywhere and run.

the exe isn't code-signed, so windows defender can false-flag it. if it does: windows security → virus & threat protection → manage settings → exclusions → add folder `%LOCALAPPDATA%\Programs\hq`, then rerun the installer. (hq deliberately doesn't install itself or touch powershell, which keeps antivirus calmer.)

then: sign in ("use my github cli login" if you have `gh`, or paste a token: the link opens github with the right boxes ticked). first person only: hit **set it up** on the banner to make the studio repo.

needs [git](https://git-scm.com/downloads). godot for opening games.

## updates

hq checks this repo's releases on start and every 3 hours. when there's a new one it downloads it, swaps itself and restarts on the same port once nothing is running. your open tab keeps working. turn it off or update by hand in settings → updates. 

to ship a new version: `git tag v0.2.0 && git push origin v0.2.0`. actions tests it, builds windows/mac/linux and makes the release; everyone's hq picks it up.

```sh
go build -o hq .            # or: GOOS=windows go build -o hq.exe .
./hq                        # --port 7313 --no-browser --version
```

## dev

- go 1.24, no dependencies. ui is plain html/css/js in `web/`, embedded in the exe.
- `tests/e2e.sh` runs the real binary against a fake github (`tests/fakegh.py`) and local bare repos: sign in, studio setup, download all 4 games, push/pull dev, side branch merge, dev → main, board, chat, team, packages.
- `tests/update.sh`: an old build finds a newer fake release, updates itself and comes back on the same port.
- releases: push a tag `v*` and actions builds windows / mac / linux.
