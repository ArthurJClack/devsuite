# hq · fish market studios

the all-in-one team app for fish market studios. one exe, opens in your browser, no server to run: github does all the syncing.

works with every game we have in dev: **SECRET** (rpg), **FOR/AGAINST** (rts), **SHELLS** (fps), **SHITBOX RALLY** (racing). adding another is a line in the games list.

## what it does

| page | what |
| --- | --- |
| dashboard | every game at a glance: latest build + downloads, how much of dev isn't on main yet, open PRs, where you are locally. recent work feed, your cards, mini chat |
| games | download the source, **pull dev**, **push to dev**, **dev → main**, open in godot, play, open the folder, run any of the game's tools (`*.bat` in the root and `tools/`), publish an update or hotfix |
| board | trello-style board: backlog / todo / doing / review / done. drag cards, tag a game, assign someone |
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

## run it

needs [git](https://git-scm.com/downloads). godot for opening games.

1. download `hq.exe` from releases (or get a package from someone on the team)
2. double-click it, it opens in your browser
3. sign in: "use my github cli login" if you have `gh`, or paste a token (the link opens github with the right boxes ticked)
4. first person only: hit **set it up** on the banner to make the studio repo

```sh
go build -o hq .            # or: GOOS=windows go build -o hq.exe .
./hq                        # --port 7313 --no-browser --version
```

## dev

- go 1.24, no dependencies. ui is plain html/css/js in `web/`, embedded in the exe.
- `tests/e2e.sh` runs the real binary against a fake github (`tests/fakegh.py`) and local bare repos: sign in, studio setup, download all 4 games, push/pull dev, side branch merge, dev → main, board, chat, team, packages.
- releases: push a tag `v*` and actions builds windows / mac / linux.
