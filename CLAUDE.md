# hq

fish market studios' team app. go backend (no deps) + plain js ui in `web/` (embedded). github is the only backend: games = source repos, board + chat = issues in the studio repo, shared config = `hq.json` there.

- files: `main.go` (start/flags), `server.go` (http api), `git.go` (pull/push/merge), `team.go` (studio repo, board, chat, collaborators, invite zip), `github.go` (rest client w/ etag cache), `jobs.go` (slow ops + live logs), `tools.go` (script discovery, godot), `proc_*.go` (per-os launching)
- default games list is `defaultTeam()` in config.go; the live one is `hq.json` in the studio repo
- lowercase, casual copy in the ui
- keep it one exe, no new deps

checks before pushing:

```sh
go vet ./... && GOOS=windows go build -o /dev/null . && node --check web/app.js && tests/e2e.sh
```
