package main

import (
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"
)

type Server struct {
	store    *Store
	gh       *GH
	jobs     *Jobs
	key      string
	cliToken string

	teamMu sync.Mutex
	teamAt time.Time

	meMu  sync.Mutex
	meVal *ghUser
	meTok string
}

func (s *Server) token() string {
	if t := s.store.Get().Token; t != "" {
		return t
	}
	return s.cliToken
}

func (s *Server) me() (ghUser, error) {
	s.meMu.Lock()
	defer s.meMu.Unlock()
	tok := s.token()
	if tok == "" {
		return ghUser{}, fmt.Errorf("not signed in")
	}
	if s.meVal != nil && s.meTok == tok {
		return *s.meVal, nil
	}
	var u ghUser
	if _, err := s.gh.Do("GET", "/user", nil, &u); err != nil {
		return ghUser{}, err
	}
	s.meVal, s.meTok = &u, tok
	return u, nil
}

func (s *Server) routes() http.Handler {
	mux := http.NewServeMux()
	mux.Handle("GET /", http.FileServerFS(webFS()))
	h := func(pattern string, fn func(w http.ResponseWriter, r *http.Request) (any, error)) {
		mux.HandleFunc(pattern, func(w http.ResponseWriter, r *http.Request) {
			if r.Header.Get("X-HQ-Key") != s.key && r.URL.Query().Get("k") != s.key {
				http.Error(w, "bad key: reopen hq", http.StatusForbidden)
				return
			}
			out, err := fn(w, r)
			if out == nil && err == nil {
				return // handler wrote the response
			}
			w.Header().Set("Content-Type", "application/json")
			if err != nil {
				code := http.StatusBadRequest
				if e, ok := err.(*APIError); ok && e.Status == 401 {
					code = http.StatusUnauthorized
				}
				w.WriteHeader(code)
				json.NewEncoder(w).Encode(map[string]string{"error": err.Error()})
				return
			}
			json.NewEncoder(w).Encode(out)
		})
	}

	h("GET /api/ping", func(w http.ResponseWriter, r *http.Request) (any, error) { return "pong", nil })
	h("GET /api/state", s.apiState)
	h("POST /api/signin", s.apiSignin)
	h("POST /api/signout", func(w http.ResponseWriter, r *http.Request) (any, error) {
		s.cliToken = ""
		return ok(), s.store.Update(func(l *Local) { l.Token = "" })
	})
	h("POST /api/settings", s.apiSettings)
	h("POST /api/studio/setup", func(w http.ResponseWriter, r *http.Request) (any, error) {
		var in struct{ Repo, Name string }
		if err := readJSON(r, &in); err != nil {
			return nil, err
		}
		if strings.TrimSpace(in.Repo) == "" {
			return nil, fmt.Errorf("pick a repo name for the studio")
		}
		return s.jobs.Start("set up studio repo", "", nil, func(j *Job) error { return s.setupStudio(j, strings.TrimSpace(in.Repo), in.Name) }), nil
	})
	h("POST /api/games", func(w http.ResponseWriter, r *http.Request) (any, error) {
		var in struct{ Games []Game }
		if err := readJSON(r, &in); err != nil {
			return nil, err
		}
		t := s.team()
		seen := map[string]bool{}
		for _, g := range in.Games {
			if g.ID == "" || g.Repo == "" || seen[g.ID] || !strings.Contains(g.Repo, "/") {
				return nil, fmt.Errorf("every game needs a unique id and a repo like owner/name")
			}
			seen[g.ID] = true
		}
		t.Games = in.Games
		if s.store.Get().Studio == "" {
			s.setTeam(&t)
			return ok(), nil
		}
		return ok(), s.saveTeam(t, "hq: update games")
	})
	h("POST /api/game/{id}/{op}", s.apiGameOp)
	h("GET /api/remote", s.apiRemote)
	h("GET /api/activity", s.apiActivity)
	h("GET /api/board", func(w http.ResponseWriter, r *http.Request) (any, error) { return s.board() })
	h("POST /api/board", func(w http.ResponseWriter, r *http.Request) (any, error) {
		var e CardEdit
		if err := readJSON(r, &e); err != nil {
			return nil, err
		}
		return s.editCard(0, e)
	})
	h("PATCH /api/board/{n}", func(w http.ResponseWriter, r *http.Request) (any, error) {
		n, _ := strconv.Atoi(r.PathValue("n"))
		var e CardEdit
		if err := readJSON(r, &e); err != nil {
			return nil, err
		}
		return s.editCard(n, e)
	})
	h("GET /api/chat", func(w http.ResponseWriter, r *http.Request) (any, error) { return s.chat() })
	h("POST /api/chat", func(w http.ResponseWriter, r *http.Request) (any, error) {
		var in struct{ Text string }
		if err := readJSON(r, &in); err != nil {
			return nil, err
		}
		if strings.TrimSpace(in.Text) == "" {
			return nil, fmt.Errorf("empty message")
		}
		return ok(), s.say(in.Text)
	})
	h("GET /api/team", func(w http.ResponseWriter, r *http.Request) (any, error) {
		m, errs := s.members()
		return map[string]any{"members": m, "errors": errs, "repos": s.teamRepos()}, nil
	})
	h("POST /api/team/{op}", s.apiTeamOp)
	h("GET /package/{file}", func(w http.ResponseWriter, r *http.Request) (any, error) {
		home, _ := os.UserHomeDir()
		name := filepath.Base(r.PathValue("file"))
		if !strings.HasPrefix(name, "hq-") || !strings.HasSuffix(name, ".zip") {
			return nil, fmt.Errorf("no such package")
		}
		for _, d := range []string{filepath.Join(home, "Downloads"), home} {
			if p := filepath.Join(d, name); fileExists(p) {
				w.Header().Set("Content-Disposition", `attachment; filename="`+name+`"`)
				http.ServeFile(w, r, p)
				return nil, nil
			}
		}
		return nil, fmt.Errorf("no such package")
	})
	h("GET /api/job/{id}", func(w http.ResponseWriter, r *http.Request) (any, error) {
		j := s.jobs.Get(r.PathValue("id"))
		if j == nil {
			return nil, fmt.Errorf("no such job")
		}
		from, _ := strconv.Atoi(r.URL.Query().Get("from"))
		text, n, done, good := j.Snapshot(from)
		return map[string]any{"id": j.ID, "title": j.Title, "game": j.Game, "log": text, "at": n, "done": done, "ok": good}, nil
	})
	return localOnly(mux)
}

// localOnly blocks DNS-rebinding: only answer requests addressed to this machine.
func localOnly(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		host, _, err := net.SplitHostPort(r.Host)
		if err != nil {
			host = r.Host
		}
		if host != "127.0.0.1" && host != "localhost" && host != "::1" {
			http.Error(w, "local only", http.StatusForbidden)
			return
		}
		w.Header().Set("Cache-Control", "no-store")
		next.ServeHTTP(w, r)
	})
}

func ok() map[string]bool { return map[string]bool{"ok": true} }

func readJSON(r *http.Request, v any) error {
	if r.Body == nil {
		return nil
	}
	err := json.NewDecoder(http.MaxBytesReader(nil, r.Body, 1<<20)).Decode(v)
	if err != nil && err.Error() == "EOF" {
		return nil
	}
	return err
}

// ---------------------------------------------------------------- handlers

func (s *Server) apiState(w http.ResponseWriter, r *http.Request) (any, error) {
	l := s.store.Get()
	t := s.team()
	out := map[string]any{
		"studio": t.Studio, "studioRepo": l.Studio, "chatIssue": t.ChatIssue, "workspace": l.Workspace,
		"godot": findGodot(l.Godot), "godotSaved": l.Godot, "signedIn": false, "columns": columns,
		"gitOK": hasGit(), "tokenFrom": map[bool]string{true: "pasted", false: "github cli"}[l.Token != ""],
	}
	if u, err := s.me(); err == nil {
		out["signedIn"], out["me"] = true, u
	} else if s.token() != "" {
		out["authError"] = err.Error()
	}
	type gs struct {
		Game
		Local LocalStatus `json:"local"`
	}
	games := make([]gs, len(t.Games))
	var wg sync.WaitGroup
	for i, g := range t.Games {
		wg.Add(1)
		go func(i int, g Game) {
			defer wg.Done()
			games[i] = gs{g, localStatus(l.Workspace, g)}
		}(i, g)
	}
	wg.Wait()
	out["games"] = games
	return out, nil
}

func (s *Server) apiSignin(w http.ResponseWriter, r *http.Request) (any, error) {
	var in struct{ Token string }
	if err := readJSON(r, &in); err != nil {
		return nil, err
	}
	tok := strings.TrimSpace(in.Token)
	if tok == "" {
		s.cliToken = ghCLIToken()
		if s.cliToken == "" {
			return nil, fmt.Errorf("github cli isn't signed in here: paste a token instead")
		}
		_ = s.store.Update(func(l *Local) { l.Token = "" })
	} else {
		old := s.store.Get().Token
		_ = s.store.Update(func(l *Local) { l.Token = tok })
		if _, err := s.me(); err != nil {
			_ = s.store.Update(func(l *Local) { l.Token = old })
			return nil, fmt.Errorf("that token didn't work: %v", err)
		}
	}
	u, err := s.me()
	if err != nil {
		return nil, err
	}
	s.teamMu.Lock()
	s.teamAt = time.Time{}
	s.teamMu.Unlock()
	return u, nil
}

func (s *Server) apiSettings(w http.ResponseWriter, r *http.Request) (any, error) {
	var in struct {
		Workspace *string
		Godot     *string
		Studio    *string
	}
	if err := readJSON(r, &in); err != nil {
		return nil, err
	}
	err := s.store.Update(func(l *Local) {
		if in.Workspace != nil && strings.TrimSpace(*in.Workspace) != "" {
			l.Workspace = strings.TrimSpace(*in.Workspace)
		}
		if in.Godot != nil {
			l.Godot = strings.Trim(strings.TrimSpace(*in.Godot), `"`)
		}
		if in.Studio != nil {
			l.Studio = strings.TrimSpace(*in.Studio)
		}
	})
	s.teamMu.Lock()
	s.teamAt = time.Time{}
	s.teamMu.Unlock()
	return ok(), err
}

func (s *Server) apiGameOp(w http.ResponseWriter, r *http.Request) (any, error) {
	g, found := s.game(r.PathValue("id"))
	if !found {
		return nil, fmt.Errorf("unknown game")
	}
	var in struct{ Message, Path string }
	if err := readJSON(r, &in); err != nil {
		return nil, err
	}
	l := s.store.Get()
	dir := gamePath(l.Workspace, g)
	env := gitEnv(s.token())
	login := ""
	if u, err := s.me(); err == nil {
		login = u.Login
	}
	needClone := func() error {
		if !fileExists(filepath.Join(dir, ".git", "HEAD")) {
			return fmt.Errorf("download %s first", g.Name)
		}
		return nil
	}
	switch op := r.PathValue("op"); op {
	case "clone":
		return s.jobs.Start("download "+g.Name, g.ID, env, func(j *Job) error { return clone(j, l.Workspace, g) }), nil
	case "pull":
		if err := needClone(); err != nil {
			return nil, err
		}
		return s.jobs.Start("pull dev · "+g.Name, g.ID, env, func(j *Job) error { return pullDev(j, l.Workspace, g) }), nil
	case "push":
		if err := needClone(); err != nil {
			return nil, err
		}
		return s.jobs.Start("push to dev · "+g.Name, g.ID, env, func(j *Job) error {
			if err := pushDev(j, s.gh, l.Workspace, login, g, in.Message); err != nil {
				return err
			}
			if strings.TrimSpace(in.Message) != "" {
				_ = s.say(fmt.Sprintf("🟢 pushed to **%s** dev: %s", g.Name, in.Message))
			}
			return nil
		}), nil
	case "tomain":
		return s.jobs.Start("dev → main · "+g.Name, g.ID, env, func(j *Job) error {
			if err := ghMerge(j, s.gh, g.Repo, g.mainBranch(), g.devBranch()); err != nil {
				return err
			}
			_ = s.say(fmt.Sprintf("🚀 **%s**: dev merged into main by @%s", g.Name, login))
			if fileExists(filepath.Join(dir, ".git", "HEAD")) {
				j.Run(dir, "git", "fetch", "origin")
			}
			return nil
		}), nil
	case "folder":
		if err := needClone(); err != nil {
			return nil, err
		}
		return ok(), openPath(dir)
	case "editor", "play":
		if err := needClone(); err != nil {
			return nil, err
		}
		godot := findGodot(l.Godot)
		if godot == "" {
			return nil, fmt.Errorf("couldn't find godot: set its path in settings")
		}
		proj := findGodotProject(dir)
		if proj == "" {
			return nil, fmt.Errorf("no project.godot in %s", dir)
		}
		args := []string{"--path", proj}
		if op == "editor" {
			args = append([]string{"-e"}, args...)
		}
		return ok(), startDetached(godot, args...)
	case "tool", "publish", "hotfix":
		if err := needClone(); err != nil {
			return nil, err
		}
		p := in.Path
		if op == "publish" {
			p = g.Publish
		} else if op == "hotfix" {
			p = g.Hotfix
		}
		if p == "" {
			return nil, fmt.Errorf("%s has no %s script set (games tab → edit)", g.Name, op)
		}
		script := filepath.Join(dir, filepath.FromSlash(scriptOf(p)))
		if rel, err := filepath.Rel(dir, script); err != nil || strings.HasPrefix(rel, "..") || !fileExists(script) {
			return nil, fmt.Errorf("script not found: %s", scriptOf(p))
		}
		return ok(), launchTool(filepath.Dir(script), script, argsOf(p))
	case "releases":
		return ok(), openURL("https://github.com/" + orDefault(g.Releases, g.Repo) + "/releases")
	case "github":
		return ok(), openURL("https://github.com/" + g.Repo + "/tree/" + g.devBranch())
	}
	return nil, fmt.Errorf("unknown action")
}

func (s *Server) apiTeamOp(w http.ResponseWriter, r *http.Request) (any, error) {
	var in struct {
		User   string
		Repos  []string
		Invite bool
	}
	if err := readJSON(r, &in); err != nil {
		return nil, err
	}
	in.User = strings.TrimPrefix(strings.TrimSpace(in.User), "@")
	switch r.PathValue("op") {
	case "add", "remove":
		if in.User == "" || len(in.Repos) == 0 {
			return nil, fmt.Errorf("need a github username and at least one game")
		}
		rm := r.PathValue("op") == "remove"
		verb := map[bool]string{true: "remove", false: "add"}[rm]
		return s.jobs.Start(verb+" @"+in.User, "", nil, func(j *Job) error {
			err := s.addMember(j, in.User, in.Repos, rm)
			if err == nil && !rm {
				_ = s.say("👋 invited @" + in.User + " to the team")
			}
			return err
		}), nil
	case "package":
		if in.Invite && in.User != "" && len(in.Repos) > 0 {
			j := s.jobs.Start("invite @"+in.User, "", nil, func(j *Job) error { return s.addMember(j, in.User, in.Repos, false) })
			_ = j
		}
		p, err := s.makePackage(in.User)
		if err != nil {
			return nil, err
		}
		return map[string]string{"path": p, "file": filepath.Base(p)}, nil
	}
	return nil, fmt.Errorf("unknown action")
}

// ---------------------------------------------------------------- remote (github) info for the dashboard

type ghCommit struct {
	SHA     string `json:"sha"`
	HTMLURL string `json:"html_url"`
	Commit  struct {
		Message string `json:"message"`
		Author  struct {
			Name string    `json:"name"`
			Date time.Time `json:"date"`
		} `json:"author"`
	} `json:"commit"`
	Author *ghUser `json:"author"`
}

type ghRelease struct {
	TagName     string    `json:"tag_name"`
	Name        string    `json:"name"`
	HTMLURL     string    `json:"html_url"`
	Prerelease  bool      `json:"prerelease"`
	PublishedAt time.Time `json:"published_at"`
	Assets      []struct {
		DownloadCount int `json:"download_count"`
	} `json:"assets"`
}

type Remote struct {
	ID       string     `json:"id"`
	Release  *ghRelease `json:"release,omitempty"`
	Total    int        `json:"downloads"`
	DevAhead int        `json:"devAhead"`
	PRs      int        `json:"prs"`
	Last     *Activity  `json:"last,omitempty"`
	Error    string     `json:"error,omitempty"`
}

type Activity struct {
	Game   string    `json:"game"`
	SHA    string    `json:"sha"`
	Msg    string    `json:"msg"`
	Who    string    `json:"who"`
	Avatar string    `json:"avatar,omitempty"`
	At     time.Time `json:"at"`
	URL    string    `json:"url"`
}

func toActivity(game string, c ghCommit) Activity {
	a := Activity{Game: game, SHA: c.SHA[:min(7, len(c.SHA))], Msg: strings.SplitN(c.Commit.Message, "\n", 2)[0],
		Who: c.Commit.Author.Name, At: c.Commit.Author.Date, URL: c.HTMLURL}
	if c.Author != nil {
		a.Who, a.Avatar = c.Author.Login, c.Author.AvatarURL
	}
	return a
}

func (s *Server) apiRemote(w http.ResponseWriter, r *http.Request) (any, error) {
	games := s.team().Games
	out := make([]Remote, len(games))
	var wg sync.WaitGroup
	for i, g := range games {
		wg.Add(1)
		go func(i int, g Game) {
			defer wg.Done()
			rm := Remote{ID: g.ID}
			var rels []ghRelease
			if _, err := s.gh.Do("GET", "/repos/"+orDefault(g.Releases, g.Repo)+"/releases?per_page=30", nil, &rels); err == nil && len(rels) > 0 {
				rm.Release = &rels[0]
				for _, rel := range rels {
					for _, a := range rel.Assets {
						rm.Total += a.DownloadCount
					}
				}
			}
			var cmp struct {
				AheadBy int `json:"ahead_by"`
			}
			if _, err := s.gh.Do("GET", "/repos/"+g.Repo+"/compare/"+esc(g.mainBranch())+"..."+esc(g.devBranch()), nil, &cmp); err == nil {
				rm.DevAhead = cmp.AheadBy
			}
			var prs []struct{}
			if _, err := s.gh.Do("GET", "/repos/"+g.Repo+"/pulls?state=open&per_page=50", nil, &prs); err == nil {
				rm.PRs = len(prs)
			}
			var cs []ghCommit
			if _, err := s.gh.Do("GET", "/repos/"+g.Repo+"/commits?per_page=1&sha="+esc(g.devBranch()), nil, &cs); err != nil {
				rm.Error = err.Error()
			} else if len(cs) > 0 {
				a := toActivity(g.ID, cs[0])
				rm.Last = &a
			}
			out[i] = rm
		}(i, g)
	}
	wg.Wait()
	return out, nil
}

func (s *Server) apiActivity(w http.ResponseWriter, r *http.Request) (any, error) {
	var mu sync.Mutex
	var all []Activity
	var wg sync.WaitGroup
	for _, g := range s.team().Games {
		wg.Add(1)
		go func(g Game) {
			defer wg.Done()
			var cs []ghCommit
			if _, err := s.gh.Do("GET", "/repos/"+g.Repo+"/commits?per_page=10&sha="+esc(g.devBranch()), nil, &cs); err != nil {
				return
			}
			mu.Lock()
			for _, c := range cs {
				all = append(all, toActivity(g.ID, c))
			}
			mu.Unlock()
		}(g)
	}
	wg.Wait()
	sort.Slice(all, func(a, b int) bool { return all[a].At.After(all[b].At) })
	if len(all) > 30 {
		all = all[:30]
	}
	return all, nil
}
