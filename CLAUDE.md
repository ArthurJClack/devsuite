# hq

fish market studios' team app. go backend (no deps) + plain js ui in `web/` (embedded). github is the only backend: games = source repos, board + chat = issues in the studio repo, shared config = `hq.json` there.

- files: `main.go` (start/flags), `server.go` (http api), `git.go` (pull/push/merge), `team.go` (studio repo, chat, collaborators, invite zip), `board.go` (cards = issues: labels for list/game/prio/tags, due + checklist in the body; starter boards from `templates/`), `sync.go` (per-game pull/push status, update all / push all, background fetch), `publish.go` (publish via a game's release workflow_dispatch or script, watched step by step), `changes.go` + `activitylog.go` (changes page: commits + releases + the team activity log, an issue in the studio repo that `logEvent` appends to), `github.go` (rest client w/ etag cache), `jobs.go` (slow ops + live logs), `tools.go` (script discovery, godot), `update.go` (auto-update from devsuite releases), `window.go` (app window via edge/chrome --app; each window holds /api/live and hq quits 20 s after the last one closes), `safe.go` (hq.log in the config dir incl. crashes; use goSafe / defer recoverLog in every goroutine), `proc_*.go` / `relaunch_*.go` (per-os launching). board lists are a pipeline (`columns` in team.go, old names mapped by `normColumn`). installing is install.ps1's job: keep the exe from copying itself around or spawning powershell, antivirus flags that
- default games list is `defaultTeam()` in config.go; the live one is `hq.json` in the studio repo
- lowercase, casual copy in the ui
- keep it one exe, no new deps

checks before pushing:

```sh
go vet ./... && GOOS=windows go build -o /dev/null . && node --check web/app.js && tests/e2e.sh && tests/update.sh && tests/life.sh
```

ship a version: push a `v*` tag; every install auto-updates to it.
