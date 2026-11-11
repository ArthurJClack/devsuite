package main

import (
	"encoding/base64"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
)

func gitBase() string {
	if b := os.Getenv("HQ_GIT_BASE"); b != "" {
		return strings.TrimRight(b, "/") + "/"
	}
	return "https://github.com/"
}

func repoURL(repo string) string { return gitBase() + repo + ".git" }

// gitEnv hands git the token through env-only config, so it never lands in a command line,
// .git/config or the credential store, and git never pops a login prompt.
func gitEnv(token string) []string {
	env := []string{"GIT_TERMINAL_PROMPT=0"}
	if token != "" && strings.HasPrefix(gitBase(), "https://") {
		basic := base64.StdEncoding.EncodeToString([]byte("x-access-token:" + token))
		env = append(env, "GIT_CONFIG_COUNT=1",
			"GIT_CONFIG_KEY_0=http."+gitBase()+".extraheader",
			"GIT_CONFIG_VALUE_0=AUTHORIZATION: basic "+basic)
	}
	return env
}

func gitQuick(dir string, args ...string) string {
	cmd := exec.Command("git", append([]string{"-C", dir}, args...)...)
	hideWindow(cmd)
	out, err := cmd.Output()
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(out))
}

type LocalStatus struct {
	Cloned  bool     `json:"cloned"`
	Path    string   `json:"path"`
	Branch  string   `json:"branch,omitempty"`
	Commit  string   `json:"commit,omitempty"`
	Dirty   int      `json:"dirty"`
	Files   []string `json:"files,omitempty"`
	Ahead   int      `json:"ahead"`
	Behind  int      `json:"behind"`
	HasDev  bool     `json:"hasDev"`
	Tools   []Tool   `json:"tools,omitempty"`
	Project string   `json:"project,omitempty"` // folder holding project.godot
}

func gamePath(ws string, g Game) string { return filepath.Join(ws, g.ID) }

func localStatus(ws string, g Game) LocalStatus {
	dir := gamePath(ws, g)
	st := LocalStatus{Path: dir}
	if _, err := os.Stat(filepath.Join(dir, ".git")); err != nil {
		return st
	}
	st.Cloned = true
	st.Branch = gitQuick(dir, "rev-parse", "--abbrev-ref", "HEAD")
	st.Commit = gitQuick(dir, "rev-parse", "--short", "HEAD")
	if s := gitQuick(dir, "status", "--porcelain"); s != "" {
		lines := strings.Split(s, "\n")
		st.Dirty = len(lines)
		if len(lines) > 12 {
			lines = append(lines[:12], fmt.Sprintf("… +%d more", len(lines)-12))
		}
		st.Files = lines
	}
	dev := "origin/" + g.devBranch()
	if gitQuick(dir, "rev-parse", "--verify", "--quiet", dev) != "" {
		st.HasDev = true
		if lr := strings.Fields(gitQuick(dir, "rev-list", "--left-right", "--count", "HEAD..."+dev)); len(lr) == 2 {
			st.Ahead, _ = strconv.Atoi(lr[0])
			st.Behind, _ = strconv.Atoi(lr[1])
		}
	}
	st.Tools = discoverTools(dir, g)
	st.Project = findGodotProject(dir)
	return st
}

func clone(j *Job, ws string, g Game) error {
	dir := gamePath(ws, g)
	if _, err := os.Stat(filepath.Join(dir, ".git")); err == nil {
		return fmt.Errorf("already downloaded at %s", dir)
	}
	if err := os.MkdirAll(ws, 0o755); err != nil {
		return err
	}
	url := repoURL(g.Repo)
	args := []string{"clone", "--progress"}
	if out, _ := j.Run(ws, "git", "ls-remote", "--heads", url, g.devBranch()); strings.Contains(out, "refs/heads/"+g.devBranch()) {
		args = append(args, "-b", g.devBranch())
	}
	_, err := j.Run(ws, "git", append(args, url, dir)...)
	return err
}

func ensureIdentity(j *Job, dir, login string) {
	if gitQuick(dir, "config", "user.name") == "" && login != "" {
		j.Run(dir, "git", "config", "user.name", login)
		j.Run(dir, "git", "config", "user.email", login+"@users.noreply.github.com")
	}
}

func remoteHas(dir, branch string) bool {
	return gitQuick(dir, "rev-parse", "--verify", "--quiet", "origin/"+branch) != ""
}

// pullDev: get everyone's latest dev. On dev it rebases your local commits on top; on a clean
// tree elsewhere it switches to dev; with unsaved work on another branch it merges dev in.
func pullDev(j *Job, ws string, g Game) error {
	dir := gamePath(ws, g)
	dev := g.devBranch()
	if _, err := j.Run(dir, "git", "fetch", "--prune", "origin"); err != nil {
		return err
	}
	if !remoteHas(dir, dev) {
		return fmt.Errorf("the repo has no %s branch yet: push to dev once to make it", dev)
	}
	branch := gitQuick(dir, "rev-parse", "--abbrev-ref", "HEAD")
	dirty := gitQuick(dir, "status", "--porcelain") != ""
	if branch != dev && !dirty {
		if _, err := j.Run(dir, "git", "checkout", dev); err != nil {
			return err
		}
		branch = dev
	}
	if branch == dev {
		_, err := j.Run(dir, "git", "pull", "--rebase", "--autostash", "origin", dev)
		return err
	}
	j.Printf("you have unsaved work on %s, merging dev into it\n", branch)
	_, err := j.Run(dir, "git", "merge", "--autostash", "--no-edit", "origin/"+dev)
	return err
}

// pushDev: commit everything and land it on dev. From a side branch it pushes the branch and
// merges it into dev on GitHub.
func pushDev(j *Job, gh *GH, ws, login string, g Game, msg string) error {
	dir := gamePath(ws, g)
	dev := g.devBranch()
	ensureIdentity(j, dir, login)
	if gitQuick(dir, "status", "--porcelain") != "" {
		if strings.TrimSpace(msg) == "" {
			msg = "work from " + login
		}
		if _, err := j.Run(dir, "git", "add", "-A"); err != nil {
			return err
		}
		if _, err := j.Run(dir, "git", "commit", "-m", msg); err != nil {
			return err
		}
	}
	if _, err := j.Run(dir, "git", "fetch", "origin"); err != nil {
		return err
	}
	branch := gitQuick(dir, "rev-parse", "--abbrev-ref", "HEAD")
	if branch == dev || branch == "HEAD" {
		if remoteHas(dir, dev) {
			if _, err := j.Run(dir, "git", "pull", "--rebase", "--autostash", "origin", dev); err != nil {
				j.Printf("\nsomeone else changed the same lines. fix the conflicts (or run `git rebase --abort`) then push again\n")
				return err
			}
		}
		_, err := j.Run(dir, "git", "push", "origin", "HEAD:refs/heads/"+dev)
		return err
	}
	if _, err := j.Run(dir, "git", "push", "-u", "origin", branch); err != nil {
		return err
	}
	if !remoteHas(dir, dev) {
		_, err := j.Run(dir, "git", "push", "origin", "HEAD:refs/heads/"+dev)
		return err
	}
	return ghMerge(j, gh, g.Repo, dev, branch)
}

// ghMerge merges head into base on GitHub (no local checkout needed).
func ghMerge(j *Job, gh *GH, repo, base, head string) error {
	j.Printf("merging %s → %s on github…\n", head, base)
	var res struct {
		SHA string `json:"sha"`
	}
	_, err := gh.Do("POST", "/repos/"+repo+"/merges", map[string]string{
		"base": base, "head": head, "commit_message": fmt.Sprintf("merge %s into %s (hq)", head, base),
	}, &res)
	switch {
	case err == nil && res.SHA == "":
		j.Printf("%s already has everything from %s\n", base, head)
		return nil
	case err == nil:
		j.Printf("merged: %s\n", res.SHA[:min(7, len(res.SHA))])
		return nil
	case isStatus(err, 409):
		return fmt.Errorf("%s and %s conflict: open a pull request on github to sort it out (%s/%s/compare/%s...%s)", base, head, strings.TrimSuffix(gitBase(), "/"), repo, base, head)
	case isStatus(err, 404):
		return fmt.Errorf("branch %s or %s doesn't exist on github", base, head)
	}
	return err
}

// devCopy keeps a separate, read-only copy of the latest dev at <workspace>/.dev/<game> so you can
// play dev without touching your own checkout. Godot's import cache (.godot/) is gitignored and
// survives between updates, so only the first launch imports everything.
func devCopy(j *Job, ws string, g Game) (string, error) {
	dev := g.devBranch()
	dir := filepath.Join(ws, ".dev", g.ID)
	main := gamePath(ws, g)
	if err := os.MkdirAll(filepath.Join(ws, ".dev"), 0o755); err != nil {
		return "", err
	}
	if fileExists(filepath.Join(dir, ".git")) || dirExists(filepath.Join(dir, ".git")) {
		if _, err := j.Run(dir, "git", "fetch", "--depth", "1", "origin", dev); err != nil {
			return "", err
		}
		if _, err := j.Run(dir, "git", "checkout", "--detach", "-f", "FETCH_HEAD"); err != nil {
			return "", err
		}
		_, err := j.Run(dir, "git", "clean", "-fdq", "-e", ".godot", "-e", "*/.godot")
		return dir, err
	}
	if fileExists(filepath.Join(main, ".git", "HEAD")) {
		// share objects with your clone: fast, no second download
		if _, err := j.Run(main, "git", "fetch", "origin", dev); err != nil {
			return "", err
		}
		j.Run(main, "git", "worktree", "prune")
		_, err := j.Run(main, "git", "worktree", "add", "--detach", "-f", dir, "origin/"+dev)
		return dir, err
	}
	_, err := j.Run(ws, "git", "clone", "--progress", "--depth", "1", "-b", dev, repoURL(g.Repo), dir)
	return dir, err
}

func dirExists(p string) bool {
	st, err := os.Stat(p)
	return err == nil && st.IsDir()
}
