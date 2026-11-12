package main

// publishing a game update from hq, with live status. two ways, per game:
//   release workflow: start the game's github actions release (workflow_dispatch) and watch it
//                     step by step until the release is out. works without the game downloaded.
//   local script:     run the game's publish script (non-interactive, e.g. --yes) inside hq and
//                     stream its output.
// every run is recorded in the game's sync history (op "update" / "hotfix" / "test").

import (
	"fmt"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"time"
)

type ghRun struct {
	ID         int64     `json:"id"`
	Status     string    `json:"status"`     // queued | in_progress | completed
	Conclusion string    `json:"conclusion"` // success | failure | cancelled …
	HTMLURL    string    `json:"html_url"`
	CreatedAt  time.Time `json:"created_at"`
}

type ghJobs struct {
	Jobs []struct {
		Name   string `json:"name"`
		Status string `json:"status"`
		Steps  []struct {
			Name       string `json:"name"`
			Status     string `json:"status"`
			Conclusion string `json:"conclusion"`
			Number     int    `json:"number"`
		} `json:"steps"`
	} `json:"jobs"`
}

func publishKinds(g Game) []string {
	var k []string
	if g.Release != nil && g.Release.Workflow != "" {
		k = append(k, "update", "hotfix")
		if g.Release.Test != nil {
			k = append(k, "test")
		}
		return k
	}
	if g.Publish != "" {
		k = append(k, "update")
	}
	if g.Hotfix != "" {
		k = append(k, "hotfix")
	}
	return k
}

func (s *Server) startPublish(g Game, kind string) (*Job, error) {
	ok := false
	for _, k := range publishKinds(g) {
		ok = ok || k == kind
	}
	if !ok {
		return nil, fmt.Errorf("%s has no %s publishing set up (games → edit games list)", g.Name, kind)
	}
	label := map[string]string{"update": "publish update", "hotfix": "publish hotfix", "test": "test build (dry run)"}[kind]
	login := ""
	if u, err := s.me(); err == nil {
		login = u.Login
	}
	j := s.jobs.Start(label+" · "+g.Name, "publish:"+g.ID, gitEnv(s.token()), func(j *Job) error {
		set := func(state, summary string) {
			s.syncs.set(g.ID, SyncStatus{Op: kind, State: state, Summary: summary, At: time.Now(), Job: j.ID})
		}
		set("running", "starting…")
		var tag string
		var err error
		if g.Release != nil && g.Release.Workflow != "" {
			tag, err = s.publishWorkflow(j, g, kind, set)
		} else {
			err = s.publishScript(j, g, kind, set)
		}
		if err != nil {
			set("failed", firstLine(err.Error()))
			s.logEvent("publish", g.ID, fmt.Sprintf("❌ %s of **%s** failed: %s", label, g.Name, firstLine(err.Error())))
			return err
		}
		if kind != "test" {
			s.logEvent("publish", g.ID, strings.Join(strings.Fields(fmt.Sprintf("🚀 published **%s** %s (%s)", g.Name, tag, kind)), " "))
		}
		switch {
		case kind == "test":
			set("ok", "dry run passed (nothing published)")
		case tag != "":
			set("ok", "published "+tag)
			_ = s.say(fmt.Sprintf("🚀 **%s** %s is out (%s by @%s)", g.Name, tag, kind, login))
		default:
			set("ok", "published")
			_ = s.say(fmt.Sprintf("🚀 **%s**: %s published by @%s", g.Name, kind, login))
		}
		return nil
	})
	return j, nil
}

func (s *Server) publishWorkflow(j *Job, g Game, kind string, set func(string, string)) (string, error) {
	rc := g.Release
	var in map[string]string
	switch kind {
	case "update":
		in = rc.Update
	case "hotfix":
		in = rc.Hotfix
	case "test":
		in = rc.Test
	}
	if in == nil {
		in = map[string]string{}
	}
	ref := orDefault(rc.Ref, g.mainBranch())
	wf := "/repos/" + g.Repo + "/actions/workflows/" + esc(rc.Workflow)
	relRepo := orDefault(g.Releases, g.Repo)
	var before []ghRelease
	_, _ = s.gh.Do("GET", "/repos/"+relRepo+"/releases?per_page=1", nil, &before)

	started := time.Now().Add(-10 * time.Second)
	j.Printf("starting %s on %s (%s) with %v\n", rc.Workflow, g.Repo, ref, in)
	if _, err := s.gh.Do("POST", wf+"/dispatches", map[string]any{"ref": ref, "inputs": in}, nil); err != nil {
		if isStatus(err, 404) {
			return "", fmt.Errorf("can't start %s: the workflow isn't there or you don't have write access", rc.Workflow)
		}
		return "", err
	}
	set("running", "waiting for github to pick it up…")
	var run ghRun
	for i := 0; i < 40 && run.ID == 0; i++ {
		time.Sleep(3 * time.Second)
		var list struct {
			Runs []ghRun `json:"workflow_runs"`
		}
		s.gh.invalidate(wf)
		if _, err := s.gh.Do("GET", wf+"/runs?event=workflow_dispatch&per_page=5", nil, &list); err != nil {
			continue
		}
		for _, r := range list.Runs {
			if r.CreatedAt.After(started) {
				run = r
				break
			}
		}
	}
	if run.ID == 0 {
		return "", fmt.Errorf("the release never started: look at the actions tab on github")
	}
	j.Printf("running: %s\n", run.HTMLURL)
	lastStep := ""
	for i := 0; i < 360; i++ { // up to an hour
		time.Sleep(8 * time.Second)
		runPath := fmt.Sprintf("/repos/%s/actions/runs/%d", g.Repo, run.ID)
		if _, err := s.gh.Do("GET", runPath, nil, &run); err != nil {
			continue
		}
		var jobs ghJobs
		if _, err := s.gh.Do("GET", runPath+"/jobs", nil, &jobs); err == nil {
			for _, jb := range jobs.Jobs {
				total, cur := len(jb.Steps), ""
				done := 0
				for _, st := range jb.Steps {
					if st.Status == "completed" {
						done++
					} else if st.Status == "in_progress" && cur == "" {
						cur = st.Name
					}
				}
				if cur != "" {
					step := fmt.Sprintf("%s · step %d/%d: %s", jb.Name, done+1, total, cur)
					if step != lastStep {
						j.Printf("  %s\n", step)
						set("running", step)
						lastStep = step
					}
				}
			}
		}
		if run.Status == "completed" {
			break
		}
	}
	if run.Status != "completed" {
		return "", fmt.Errorf("still running after an hour: %s", run.HTMLURL)
	}
	if run.Conclusion != "success" {
		return "", fmt.Errorf("release %s (%s): %s", run.Conclusion, lastStep, run.HTMLURL)
	}
	if kind == "test" {
		return "", nil
	}
	var after []ghRelease
	s.gh.invalidate("/repos/" + relRepo)
	if _, err := s.gh.Do("GET", "/repos/"+relRepo+"/releases?per_page=1", nil, &after); err == nil && len(after) > 0 {
		if len(before) == 0 || after[0].TagName != before[0].TagName {
			j.Printf("released %s: %s\n", after[0].TagName, after[0].HTMLURL)
			return after[0].TagName, nil
		}
	}
	return "", nil
}

func (s *Server) publishScript(j *Job, g Game, kind string, set func(string, string)) error {
	l := s.store.Get()
	dir := gamePath(l.Workspace, g)
	if !fileExists(filepath.Join(dir, ".git", "HEAD")) {
		return fmt.Errorf("download %s first: its publish script runs from your copy", g.Name)
	}
	p := g.Publish
	if kind == "hotfix" {
		p = g.Hotfix
	}
	script := filepath.Join(dir, filepath.FromSlash(scriptOf(p)))
	if !fileExists(script) {
		return fmt.Errorf("publish script not found: %s", scriptOf(p))
	}
	args := strings.Fields(argsOf(p))
	set("running", "running "+filepath.Base(script)+"…")
	var err error
	if runtime.GOOS == "windows" {
		_, err = j.Run(dir, "cmd", append([]string{"/c", script}, args...)...)
	} else if _, e := exec.LookPath("bash"); e == nil {
		_, err = j.Run(dir, "bash", append([]string{script}, args...)...)
	} else {
		_, err = j.Run(dir, script, args...)
	}
	return err
}
