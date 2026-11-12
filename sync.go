package main

// per-game sync status: every pull / push records what happened (state, commits moved, when),
// "update all" / "push all" run every game at once, and a background fetch keeps the
// "N new on dev" counts honest without touching anyone's files.

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

type SyncStatus struct {
	Op      string    `json:"op"`    // pull | push
	State   string    `json:"state"` // running | ok | nothing | failed
	Summary string    `json:"summary"`
	Commits []string  `json:"commits,omitempty"`
	At      time.Time `json:"at"`
	Job     string    `json:"job,omitempty"`
}

type Syncs struct {
	mu      sync.Mutex
	hist    map[string][]SyncStatus // newest first, per game
	fetched map[string]time.Time
}

func newSyncs() *Syncs {
	return &Syncs{hist: map[string][]SyncStatus{}, fetched: map[string]time.Time{}}
}

func (y *Syncs) set(game string, st SyncStatus) {
	y.mu.Lock()
	defer y.mu.Unlock()
	h := y.hist[game]
	if len(h) > 0 && h[0].Job != "" && h[0].Job == st.Job {
		h[0] = st // same job finishing: replace the running entry
	} else {
		h = append([]SyncStatus{st}, h...)
	}
	if len(h) > 10 {
		h = h[:10]
	}
	y.hist[game] = h
}

func (y *Syncs) Snapshot() map[string]any {
	y.mu.Lock()
	defer y.mu.Unlock()
	out := map[string]any{}
	for g, h := range y.hist {
		out[g] = map[string]any{"history": append([]SyncStatus(nil), h...), "fetched": y.fetched[g]}
	}
	for g, t := range y.fetched {
		if _, ok := out[g]; !ok {
			out[g] = map[string]any{"history": []SyncStatus{}, "fetched": t}
		}
	}
	return out
}

func plural(n int, one string) string {
	if n == 1 {
		return "1 " + one
	}
	return fmt.Sprintf("%d %ss", n, one)
}

// startSync runs a pull or push for one game as a job and records its status.
func (s *Server) startSync(g Game, op, msg string) (*Job, error) {
	l := s.store.Get()
	if !fileExists(filepath.Join(gamePath(l.Workspace, g), ".git", "HEAD")) {
		return nil, fmt.Errorf("download %s first", g.Name)
	}
	login := ""
	if u, err := s.me(); err == nil {
		login = u.Login
	}
	title := map[string]string{"pull": "update (pull dev) · ", "push": "push to dev · "}[op] + g.Name
	var j *Job
	j = s.jobs.Start(title, g.ID, gitEnv(s.token()), func(j *Job) error {
		s.syncs.set(g.ID, SyncStatus{Op: op, State: "running", Summary: map[string]string{"pull": "updating…", "push": "pushing…"}[op], At: time.Now(), Job: j.ID})
		var commits []string
		var err error
		if op == "pull" {
			commits, err = pullDev(j, l.Workspace, g)
		} else {
			commits, err = pushDev(j, s.gh, l.Workspace, login, g, msg)
		}
		st := SyncStatus{Op: op, At: time.Now(), Job: j.ID, Commits: commits}
		switch {
		case err != nil:
			st.State, st.Summary = "failed", firstLine(err.Error())
		case len(commits) == 0 && op == "pull":
			st.State, st.Summary = "nothing", "already up to date"
		case len(commits) == 0:
			st.State, st.Summary = "nothing", "nothing new to push"
		case op == "pull":
			st.State, st.Summary = "ok", "got "+plural(len(commits), "new commit")
		default:
			st.State, st.Summary = "ok", "pushed "+plural(len(commits), "commit")
		}
		s.syncs.set(g.ID, st)
		if err == nil {
			s.syncs.mu.Lock()
			s.syncs.fetched[g.ID] = time.Now()
			s.syncs.mu.Unlock()
		}
		if err == nil && op == "push" && len(commits) > 0 {
			note := msg
			if strings.TrimSpace(note) == "" {
				note = strings.SplitN(commits[0], " ", 2)[len(strings.SplitN(commits[0], " ", 2))-1]
			}
			_ = s.say(fmt.Sprintf("🟢 pushed %s to **%s** dev: %s", plural(len(commits), "commit"), g.Name, note))
		}
		return err
	})
	return j, nil
}

func firstLine(s string) string {
	s = strings.TrimSpace(s)
	if i := strings.IndexByte(s, '\n'); i > 0 {
		s = s[:i]
	}
	if len(s) > 160 {
		s = s[:160] + "…"
	}
	return s
}

// syncAll starts op on every downloaded game (push only where there's something to send).
func (s *Server) syncAll(op, msg string) []map[string]string {
	l := s.store.Get()
	var out []map[string]string
	for _, g := range s.team().Games {
		row := map[string]string{"game": g.ID, "name": g.Name}
		st := localStatus(l.Workspace, g)
		switch {
		case !st.Cloned:
			row["skip"] = "not downloaded"
		case op == "push" && st.Dirty == 0 && st.Ahead == 0 && st.HasDev:
			row["skip"] = "no changes"
		default:
			if j, err := s.startSync(g, op, msg); err != nil {
				row["skip"] = err.Error()
			} else {
				row["job"] = j.ID
			}
		}
		out = append(out, row)
	}
	return out
}

// fetchLoop: every 5 minutes, quietly `git fetch` each downloaded game so the "new on dev"
// numbers are real. skips a game while something else is running on it.
func (s *Server) fetchLoop() {
	time.Sleep(20 * time.Second)
	for {
		s.fetchAll()
		time.Sleep(5 * time.Minute)
	}
}

func (s *Server) fetchAll() {
	l := s.store.Get()
	if s.token() == "" && os.Getenv("HQ_GIT_BASE") == "" {
		return
	}
	for _, g := range s.team().Games {
		dir := gamePath(l.Workspace, g)
		if !fileExists(filepath.Join(dir, ".git", "HEAD")) {
			continue
		}
		lk, _ := s.jobs.locks.LoadOrStore(g.ID, &sync.Mutex{})
		m := lk.(*sync.Mutex)
		if !m.TryLock() {
			continue
		}
		j := &Job{Env: gitEnv(s.token())}
		_, err := j.Run(dir, "git", "fetch", "--quiet", "--prune", "origin")
		m.Unlock()
		if err == nil {
			s.syncs.mu.Lock()
			s.syncs.fetched[g.ID] = time.Now()
			s.syncs.mu.Unlock()
		}
	}
}
