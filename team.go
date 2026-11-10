package main

import (
	"archive/zip"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
	"sync"
	"time"
)

var columns = []string{"backlog", "todo", "doing", "review", "done"}

const chatLabel = "hq:chat"

// ---------------------------------------------------------------- studio repo + shared team file

type ghRepo struct {
	FullName string `json:"full_name"`
	Private  bool   `json:"private"`
	Owner    struct {
		Login string `json:"login"`
		Type  string `json:"type"`
	} `json:"owner"`
	Permissions map[string]bool `json:"permissions"`
}

type ghContent struct {
	SHA     string `json:"sha"`
	Content string `json:"content"`
}

func (s *Server) fetchTeam(studio string) (*Team, string, error) {
	var c ghContent
	if _, err := s.gh.Do("GET", "/repos/"+studio+"/contents/hq.json", nil, &c); err != nil {
		return nil, "", err
	}
	raw, err := base64.StdEncoding.DecodeString(strings.ReplaceAll(c.Content, "\n", ""))
	if err != nil {
		return nil, "", err
	}
	var t Team
	if err := json.Unmarshal(raw, &t); err != nil {
		return nil, "", fmt.Errorf("hq.json in %s is broken: %v", studio, err)
	}
	t.Repo = studio
	return &t, c.SHA, nil
}

func (s *Server) saveTeam(t Team, msg string) error {
	studio := s.store.Get().Studio
	if studio == "" {
		return fmt.Errorf("set up the studio repo first (settings)")
	}
	t.Repo = studio
	_, sha, _ := s.fetchTeam(studio)
	b, _ := json.MarshalIndent(t, "", "  ")
	body := map[string]any{"message": msg, "content": base64.StdEncoding.EncodeToString(append(b, '\n'))}
	if sha != "" {
		body["sha"] = sha
	}
	if _, err := s.gh.Do("PUT", "/repos/"+studio+"/contents/hq.json", body, nil); err != nil {
		return err
	}
	s.setTeam(&t)
	return nil
}

// team returns the shared team file: studio repo (cached ~30 s), then last known copy, then defaults.
func (s *Server) team() Team {
	s.teamMu.Lock()
	defer s.teamMu.Unlock()
	l := s.store.Get()
	if l.Studio != "" && time.Since(s.teamAt) > 30*time.Second && l.Token+s.cliToken != "" {
		s.teamAt = time.Now()
		if t, _, err := s.fetchTeam(l.Studio); err == nil {
			_ = s.store.Update(func(x *Local) { x.Team = t })
			return *t
		}
	}
	if l.Team != nil && len(l.Team.Games) > 0 {
		return *l.Team
	}
	return defaultTeam()
}

func (s *Server) setTeam(t *Team) {
	s.teamMu.Lock()
	s.teamAt = time.Now()
	s.teamMu.Unlock()
	_ = s.store.Update(func(x *Local) { x.Team = t })
}

func (s *Server) game(id string) (Game, bool) {
	for _, g := range s.team().Games {
		if g.ID == id {
			return g, true
		}
	}
	return Game{}, false
}

// setupStudio makes (or adopts) the studio repo: labels, the chat thread and hq.json.
func (s *Server) setupStudio(j *Job, studio, name string) error {
	me, err := s.me()
	if err != nil {
		return err
	}
	if !strings.Contains(studio, "/") {
		studio = me.Login + "/" + studio
	}
	var r ghRepo
	_, err = s.gh.Do("GET", "/repos/"+studio, nil, &r)
	if isStatus(err, 404) {
		owner, repo, _ := strings.Cut(studio, "/")
		j.Printf("creating private repo %s…\n", studio)
		body := map[string]any{"name": repo, "private": true, "has_issues": true, "auto_init": true,
			"description": "studio hq: board, chat and team config (managed by hq)"}
		path := "/user/repos"
		if !strings.EqualFold(owner, me.Login) {
			path = "/orgs/" + owner + "/repos"
		}
		if _, err = s.gh.Do("POST", path, body, &r); err != nil {
			return err
		}
	} else if err != nil {
		return err
	} else {
		j.Printf("using existing repo %s\n", studio)
	}
	_ = s.store.Update(func(x *Local) { x.Studio = studio })

	t := s.team()
	if t2, _, err := s.fetchTeam(studio); err == nil {
		t = *t2
	}
	if name != "" {
		t.Studio = name
	}
	j.Printf("making labels…\n")
	colors := map[string]string{"backlog": "6e7681", "todo": "1f6feb", "doing": "d29922", "review": "a371f7", "done": "2ea043"}
	for _, c := range columns {
		s.ensureLabel(studio, "hq:"+c, colors[c])
	}
	s.ensureLabel(studio, chatLabel, "f778ba")
	for _, g := range t.Games {
		s.ensureLabel(studio, "game:"+g.ID, strings.TrimPrefix(orDefault(g.Color, "#888888"), "#"))
	}
	if t.ChatIssue == 0 {
		j.Printf("starting the team chat thread…\n")
		var is ghIssue
		if _, err := s.gh.Do("POST", "/repos/"+studio+"/issues", map[string]any{
			"title": "💬 team chat", "labels": []string{chatLabel},
			"body": "hq's team chat lives in the comments of this issue. chat from the hq app, or right here.",
		}, &is); err != nil {
			return err
		}
		t.ChatIssue = is.Number
	}
	j.Printf("saving hq.json…\n")
	return s.saveTeam(t, "hq: studio setup")
}

func (s *Server) ensureLabel(repo, name, color string) {
	// a 422 means it already exists, which is fine
	_, _ = s.gh.Do("POST", "/repos/"+repo+"/labels", map[string]string{"name": name, "color": color}, nil)
}

func orDefault(s, d string) string {
	if s == "" {
		return d
	}
	return s
}

// ---------------------------------------------------------------- board (issues in the studio repo)

type ghUser struct {
	Login     string `json:"login"`
	AvatarURL string `json:"avatar_url"`
	Name      string `json:"name,omitempty"`
}

type ghIssue struct {
	Number    int       `json:"number"`
	Title     string    `json:"title"`
	Body      string    `json:"body"`
	State     string    `json:"state"`
	HTMLURL   string    `json:"html_url"`
	User      ghUser    `json:"user"`
	Assignees []ghUser  `json:"assignees"`
	Labels    []ghLabel `json:"labels"`
	Comments  int       `json:"comments"`
	UpdatedAt time.Time `json:"updated_at"`
	CreatedAt time.Time `json:"created_at"`
	PR        *struct{} `json:"pull_request,omitempty"`
}

type ghLabel struct {
	Name string `json:"name"`
}

type Card struct {
	Number    int       `json:"number"`
	Title     string    `json:"title"`
	Body      string    `json:"body"`
	Column    string    `json:"column"`
	Game      string    `json:"game,omitempty"`
	Assignees []ghUser  `json:"assignees"`
	Author    string    `json:"author"`
	URL       string    `json:"url"`
	Comments  int       `json:"comments"`
	Updated   time.Time `json:"updated"`
}

func toCard(is ghIssue) Card {
	c := Card{Number: is.Number, Title: is.Title, Body: is.Body, Column: "todo", Assignees: is.Assignees,
		Author: is.User.Login, URL: is.HTMLURL, Comments: is.Comments, Updated: is.UpdatedAt}
	if c.Assignees == nil {
		c.Assignees = []ghUser{}
	}
	for _, l := range is.Labels {
		if col, ok := strings.CutPrefix(l.Name, "hq:"); ok {
			c.Column = col
		}
		if g, ok := strings.CutPrefix(l.Name, "game:"); ok {
			c.Game = g
		}
	}
	if is.State == "closed" {
		c.Column = "done"
	}
	return c
}

func (s *Server) board() ([]Card, error) {
	studio := s.store.Get().Studio
	if studio == "" {
		return nil, fmt.Errorf("no studio repo yet")
	}
	var cards []Card
	add := func(list []ghIssue) {
		for _, is := range list {
			if is.PR != nil || hasLabel(is, chatLabel) || hasLabel(is, "hq:deleted") {
				continue
			}
			cards = append(cards, toCard(is))
		}
	}
	for page := 1; page <= 3; page++ {
		var list []ghIssue
		if _, err := s.gh.Do("GET", fmt.Sprintf("/repos/%s/issues?state=open&per_page=100&page=%d", studio, page), nil, &list); err != nil {
			return nil, err
		}
		add(list)
		if len(list) < 100 {
			break
		}
	}
	// recently finished cards (2 weeks); the day stamp keeps the URL (and ETag cache) stable
	since := time.Now().AddDate(0, 0, -14).UTC().Format("2006-01-02") + "T00:00:00Z"
	var closed []ghIssue
	if _, err := s.gh.Do("GET", "/repos/"+studio+"/issues?state=closed&per_page=60&since="+since, nil, &closed); err == nil {
		add(closed)
	}
	sort.Slice(cards, func(a, b int) bool { return cards[a].Updated.After(cards[b].Updated) })
	return cards, nil
}

func hasLabel(is ghIssue, name string) bool {
	for _, l := range is.Labels {
		if l.Name == name {
			return true
		}
	}
	return false
}

type CardEdit struct {
	Title    *string `json:"title"`
	Body     *string `json:"body"`
	Column   *string `json:"column"`
	Game     *string `json:"game"`
	Assignee *string `json:"assignee"` // "" = unassign
	Delete   bool    `json:"delete"`
}

func (s *Server) editCard(num int, e CardEdit) (Card, error) {
	studio := s.store.Get().Studio
	var is ghIssue
	if num > 0 {
		if _, err := s.gh.Do("GET", fmt.Sprintf("/repos/%s/issues/%d", studio, num), nil, &is); err != nil {
			return Card{}, err
		}
	}
	cur := toCard(is)
	col, game := cur.Column, cur.Game
	if e.Column != nil {
		col = *e.Column
	}
	if e.Game != nil {
		game = *e.Game
	}
	labels := []string{}
	for _, l := range is.Labels {
		if !strings.HasPrefix(l.Name, "hq:") && !strings.HasPrefix(l.Name, "game:") {
			labels = append(labels, l.Name)
		}
	}
	if col != "done" {
		labels = append(labels, "hq:"+col)
	}
	if game != "" {
		labels = append(labels, "game:"+game)
	}
	body := map[string]any{"labels": labels}
	if e.Title != nil {
		body["title"] = *e.Title
	}
	if e.Body != nil {
		body["body"] = *e.Body
	}
	if e.Assignee != nil {
		if *e.Assignee == "" {
			body["assignees"] = []string{}
		} else {
			body["assignees"] = []string{*e.Assignee}
		}
	}
	if num == 0 {
		if e.Title == nil || strings.TrimSpace(*e.Title) == "" {
			return Card{}, fmt.Errorf("a card needs a title")
		}
		var out ghIssue
		if _, err := s.gh.Do("POST", "/repos/"+studio+"/issues", body, &out); err != nil {
			return Card{}, err
		}
		return toCard(out), nil
	}
	switch {
	case e.Delete:
		body["state"], body["state_reason"] = "closed", "not_planned"
		body["labels"] = append(labels, "hq:deleted")
	case col == "done":
		body["state"] = "closed"
	default:
		body["state"] = "open"
	}
	var out ghIssue
	if _, err := s.gh.Do("PATCH", fmt.Sprintf("/repos/%s/issues/%d", studio, num), body, &out); err != nil {
		return Card{}, err
	}
	return toCard(out), nil
}

// ---------------------------------------------------------------- chat (comments on one issue)

type ghComment struct {
	ID        int64     `json:"id"`
	Body      string    `json:"body"`
	User      ghUser    `json:"user"`
	CreatedAt time.Time `json:"created_at"`
}

type Msg struct {
	ID     int64     `json:"id"`
	Text   string    `json:"text"`
	Who    string    `json:"who"`
	Avatar string    `json:"avatar"`
	At     time.Time `json:"at"`
}

func (s *Server) chat() ([]Msg, error) {
	t := s.team()
	studio := s.store.Get().Studio
	if studio == "" || t.ChatIssue == 0 {
		return nil, fmt.Errorf("no team chat yet: set up the studio in settings")
	}
	var is ghIssue
	if _, err := s.gh.Do("GET", fmt.Sprintf("/repos/%s/issues/%d", studio, t.ChatIssue), nil, &is); err != nil {
		return nil, err
	}
	last := max(1, (is.Comments+99)/100)
	var all []ghComment
	for p := max(1, last-1); p <= last; p++ {
		var page []ghComment
		if _, err := s.gh.Do("GET", fmt.Sprintf("/repos/%s/issues/%d/comments?per_page=100&page=%d", studio, t.ChatIssue, p), nil, &page); err != nil {
			return nil, err
		}
		all = append(all, page...)
	}
	if len(all) > 120 {
		all = all[len(all)-120:]
	}
	msgs := make([]Msg, 0, len(all))
	for _, c := range all {
		msgs = append(msgs, Msg{ID: c.ID, Text: c.Body, Who: c.User.Login, Avatar: c.User.AvatarURL, At: c.CreatedAt})
	}
	return msgs, nil
}

func (s *Server) say(text string) error {
	t := s.team()
	studio := s.store.Get().Studio
	if studio == "" || t.ChatIssue == 0 {
		return fmt.Errorf("no team chat yet")
	}
	_, err := s.gh.Do("POST", fmt.Sprintf("/repos/%s/issues/%d/comments", studio, t.ChatIssue), map[string]string{"body": text}, nil)
	return err
}

// ---------------------------------------------------------------- collaborators

type Member struct {
	Login   string          `json:"login"`
	Avatar  string          `json:"avatar"`
	Repos   map[string]bool `json:"repos"`   // game id (or "studio") -> has access
	Pending map[string]bool `json:"pending"` // invite not accepted yet
}

type ghInvite struct {
	ID      int64  `json:"id"`
	Invitee ghUser `json:"invitee"`
}

func (s *Server) teamRepos() map[string]string {
	m := map[string]string{}
	for _, g := range s.team().Games {
		m[g.ID] = g.Repo
	}
	if st := s.store.Get().Studio; st != "" {
		m["studio"] = st
	}
	return m
}

func (s *Server) members() ([]Member, []string) {
	byLogin := map[string]*Member{}
	get := func(u ghUser) *Member {
		k := strings.ToLower(u.Login)
		if byLogin[k] == nil {
			byLogin[k] = &Member{Login: u.Login, Avatar: u.AvatarURL, Repos: map[string]bool{}, Pending: map[string]bool{}}
		}
		return byLogin[k]
	}
	var mu sync.Mutex
	var wg sync.WaitGroup
	var errs []string
	for id, repo := range s.teamRepos() {
		wg.Add(1)
		go func(id, repo string) {
			defer wg.Done()
			var users []ghUser
			var inv []ghInvite
			_, err := s.gh.Do("GET", "/repos/"+repo+"/collaborators?per_page=100", nil, &users)
			if err == nil {
				_, _ = s.gh.Do("GET", "/repos/"+repo+"/invitations?per_page=100", nil, &inv)
			}
			mu.Lock()
			defer mu.Unlock()
			if err != nil {
				errs = append(errs, fmt.Sprintf("%s: %v", repo, err))
				return
			}
			for _, u := range users {
				get(u).Repos[id] = true
			}
			for _, i := range inv {
				get(i.Invitee).Pending[id] = true
			}
		}(id, repo)
	}
	wg.Wait()
	out := make([]Member, 0, len(byLogin))
	for _, m := range byLogin {
		out = append(out, *m)
	}
	sort.Slice(out, func(a, b int) bool { return strings.ToLower(out[a].Login) < strings.ToLower(out[b].Login) })
	return out, errs
}

// addMember invites a GitHub user to the chosen repos (push = can push to dev and main).
func (s *Server) addMember(j *Job, user string, ids []string, remove bool) error {
	repos := s.teamRepos()
	fails := 0
	for _, id := range ids {
		repo, ok := repos[id]
		if !ok {
			continue
		}
		var err error
		if remove {
			_, err = s.gh.Do("DELETE", "/repos/"+repo+"/collaborators/"+esc(user), nil, nil)
		} else {
			_, err = s.gh.Do("PUT", "/repos/"+repo+"/collaborators/"+esc(user), map[string]string{"permission": "push"}, nil)
		}
		if err != nil {
			fails++
			j.Printf("✗ %s: %v\n", repo, err)
		} else if remove {
			j.Printf("removed from %s\n", repo)
		} else {
			j.Printf("invited to %s (they accept from the email or github.com/notifications)\n", repo)
		}
	}
	if fails > 0 {
		return fmt.Errorf("%d repo(s) failed", fails)
	}
	return nil
}

// ---------------------------------------------------------------- invite package

// makePackage zips this exe + team.json + a readme. The teammate unzips, double-clicks, signs in.
func (s *Server) makePackage(user string) (string, error) {
	exe, err := os.Executable()
	if err != nil {
		return "", err
	}
	home, _ := os.UserHomeDir()
	dir := filepath.Join(home, "Downloads")
	if _, err := os.Stat(dir); err != nil {
		dir = home
	}
	t := s.team()
	t.Repo = s.store.Get().Studio
	safe := strings.Map(func(r rune) rune {
		if r == '-' || r == '_' || (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9') {
			return r
		}
		return -1
	}, user)
	if safe == "" {
		safe = "teammate"
	}
	out := filepath.Join(dir, "hq-"+slug(t.Studio)+"-"+safe+".zip")
	f, err := os.Create(out)
	if err != nil {
		return "", err
	}
	defer f.Close()
	zw := zip.NewWriter(f)
	folder := "hq-" + slug(t.Studio) + "/"
	exeName := "hq"
	if runtime.GOOS == "windows" {
		exeName = "hq.exe"
	}
	src, err := os.Open(exe)
	if err != nil {
		return "", err
	}
	defer src.Close()
	h := &zip.FileHeader{Name: folder + exeName, Method: zip.Deflate}
	h.SetMode(0o755)
	w, err := zw.CreateHeader(h)
	if err != nil {
		return "", err
	}
	if _, err := io.Copy(w, src); err != nil {
		return "", err
	}
	tb, _ := json.MarshalIndent(t, "", "  ")
	w, _ = zw.Create(folder + "team.json")
	w.Write(tb)
	w, _ = zw.Create(folder + "README.txt")
	fmt.Fprintf(w, "welcome to %s%s\r\n\r\n", t.Studio, map[bool]string{true: ", " + user, false: ""}[user != ""])
	fmt.Fprintf(w, "1. unzip this folder anywhere (desktop is fine)\r\n2. double-click %s, it opens in your browser\r\n", exeName)
	fmt.Fprintf(w, "3. sign in with github (accept the repo invites in your email first)\r\n4. hit download on the games you work on. that's it\r\n\r\n")
	fmt.Fprintf(w, "you need git installed: https://git-scm.com/downloads\r\ngodot for opening the games: https://godotengine.org/download\r\n")
	if err := zw.Close(); err != nil {
		return "", err
	}
	return out, nil
}

func slug(s string) string {
	s = strings.ToLower(strings.TrimSpace(s))
	var b strings.Builder
	for _, r := range s {
		switch {
		case r >= 'a' && r <= 'z', r >= '0' && r <= '9':
			b.WriteRune(r)
		case r == ' ' || r == '-' || r == '_':
			b.WriteByte('-')
		}
	}
	return strings.Trim(b.String(), "-")
}
