package main

// the board: trello-style cards stored as github issues in the studio repo.
//   column   -> label hq:<column> (done = closed issue)
//   game     -> label game:<id>
//   priority -> label prio:p0..p3
//   tags     -> any other label (bug, art, design, …)
//   due date -> a hidden <!-- hq:due=YYYY-MM-DD --> line in the body
//   checklist-> markdown task list lines "- [ ] thing" in the body (github renders them too)
//   comments -> issue comments

import (
	"embed"
	"encoding/json"
	"fmt"
	"os"
	"strconv"
	"regexp"
	"sort"
	"strings"
	"time"
)

//go:embed templates
var templatesFS embed.FS

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
	Name  string `json:"name"`
	Color string `json:"color,omitempty"`
}

type Card struct {
	Number    int       `json:"number"`
	Title     string    `json:"title"`
	Body      string    `json:"body"` // without the hq meta line
	Column    string    `json:"column"`
	Game      string    `json:"game,omitempty"`
	Priority  string    `json:"priority,omitempty"`
	Tags      []string  `json:"tags"`
	Due       string    `json:"due,omitempty"`
	Done      int       `json:"checkDone"`
	Total     int       `json:"checkTotal"`
	Assignees []ghUser  `json:"assignees"`
	Author    string    `json:"author"`
	URL       string    `json:"url"`
	Comments  int       `json:"comments"`
	Updated   time.Time `json:"updated"`
	Created   time.Time `json:"created"`
}

var (
	dueRe   = regexp.MustCompile(`(?m)^\s*<!-- hq:due=(\d{4}-\d{2}-\d{2}) -->\s*$\n?`)
	checkRe = regexp.MustCompile(`(?m)^\s*[-*] \[([ xX])\] `)
)

// tag labels hq manages, with colors (others are fine too, github just picks gray)
var tagColors = map[string]string{
	"bug": "d73a4a", "feature": "0e8a16", "art": "e99695", "design": "c5def5", "audio": "fbca04",
	"writing": "d4c5f9", "tech": "1d76db", "tools": "5319e7", "world": "006b75", "combat": "b60205",
	"ui": "f9d0c4", "steam": "171515", "research": "bfdadc", "blocked": "000000",
	"prio:p0": "b60205", "prio:p1": "d93f0b", "prio:p2": "fbca04", "prio:p3": "c2e0c6",
}

func isMeta(label string) bool {
	return strings.HasPrefix(label, "hq:") || strings.HasPrefix(label, "game:") || strings.HasPrefix(label, "prio:")
}

func toCard(is ghIssue) Card {
	c := Card{Number: is.Number, Title: is.Title, Column: "todo", Assignees: is.Assignees, Tags: []string{},
		Author: is.User.Login, URL: is.HTMLURL, Comments: is.Comments, Updated: is.UpdatedAt, Created: is.CreatedAt}
	if c.Assignees == nil {
		c.Assignees = []ghUser{}
	}
	for _, l := range is.Labels {
		switch {
		case strings.HasPrefix(l.Name, "hq:"):
			c.Column = strings.TrimPrefix(l.Name, "hq:")
		case strings.HasPrefix(l.Name, "game:"):
			c.Game = strings.TrimPrefix(l.Name, "game:")
		case strings.HasPrefix(l.Name, "prio:"):
			c.Priority = strings.TrimPrefix(l.Name, "prio:")
		default:
			c.Tags = append(c.Tags, l.Name)
		}
	}
	if is.State == "closed" {
		c.Column = "done"
	}
	body := strings.ReplaceAll(is.Body, "\r\n", "\n")
	if m := dueRe.FindStringSubmatch(body); m != nil {
		c.Due = m[1]
		body = dueRe.ReplaceAllString(body, "")
	}
	c.Body = strings.TrimRight(body, "\n ")
	for _, m := range checkRe.FindAllStringSubmatch(c.Body, -1) {
		c.Total++
		if m[1] != " " {
			c.Done++
		}
	}
	return c
}

func withDue(body, due string) string {
	body = strings.TrimRight(dueRe.ReplaceAllString(strings.ReplaceAll(body, "\r\n", "\n"), ""), "\n ")
	if due != "" {
		body += "\n\n<!-- hq:due=" + due + " -->"
	}
	return body
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
	for page := 1; page <= 5; page++ {
		var list []ghIssue
		if _, err := s.gh.Do("GET", fmt.Sprintf("/repos/%s/issues?state=open&per_page=100&page=%d", studio, page), nil, &list); err != nil {
			return nil, err
		}
		add(list)
		if len(list) < 100 {
			break
		}
	}
	// recently finished cards (3 weeks); the day stamp keeps the URL (and ETag cache) stable
	since := time.Now().AddDate(0, 0, -21).UTC().Format("2006-01-02") + "T00:00:00Z"
	var closed []ghIssue
	if _, err := s.gh.Do("GET", "/repos/"+studio+"/issues?state=closed&per_page=100&since="+since, nil, &closed); err == nil {
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
	Title     *string   `json:"title"`
	Body      *string   `json:"body"`
	Column    *string   `json:"column"`
	Game      *string   `json:"game"`
	Priority  *string   `json:"priority"`
	Tags      *[]string `json:"tags"`
	Due       *string   `json:"due"`
	Assignee  *string   `json:"assignee"` // single (older ui); "" = nobody
	Assignees *[]string `json:"assignees"`
	Delete    bool      `json:"delete"`
}

func (s *Server) editCard(num int, e CardEdit) (Card, error) {
	studio := s.store.Get().Studio
	if studio == "" {
		return Card{}, fmt.Errorf("set up the studio repo first")
	}
	var is ghIssue
	if num > 0 {
		if _, err := s.gh.Do("GET", fmt.Sprintf("/repos/%s/issues/%d", studio, num), nil, &is); err != nil {
			return Card{}, err
		}
	}
	cur := toCard(is)
	col, game, prio, tags, due, body := cur.Column, cur.Game, cur.Priority, cur.Tags, cur.Due, cur.Body
	if e.Column != nil {
		col = *e.Column
	}
	if e.Game != nil {
		game = *e.Game
	}
	if e.Priority != nil {
		prio = *e.Priority
	}
	if e.Tags != nil {
		tags = *e.Tags
	}
	if e.Due != nil {
		due = *e.Due
	}
	if e.Body != nil {
		body = *e.Body
	}
	if due != "" {
		if _, err := time.Parse("2006-01-02", due); err != nil {
			return Card{}, fmt.Errorf("due date should look like 2026-12-01")
		}
	}
	labels := []string{}
	seen := map[string]bool{}
	addL := func(l string) {
		l = strings.TrimSpace(l)
		if l != "" && !seen[strings.ToLower(l)] {
			seen[strings.ToLower(l)] = true
			labels = append(labels, l)
		}
	}
	for _, t := range tags {
		if !isMeta(t) {
			addL(t)
		}
	}
	if col != "done" {
		addL("hq:" + col)
	}
	if game != "" {
		addL("game:" + game)
	}
	if prio != "" {
		addL("prio:" + prio)
	}
	out := map[string]any{"labels": labels}
	if e.Title != nil {
		out["title"] = *e.Title
	}
	if e.Body != nil || e.Due != nil || num == 0 {
		out["body"] = withDue(body, due)
	}
	if e.Assignees != nil {
		out["assignees"] = *e.Assignees
	} else if e.Assignee != nil {
		if *e.Assignee == "" {
			out["assignees"] = []string{}
		} else {
			out["assignees"] = []string{*e.Assignee}
		}
	}
	if num == 0 {
		if e.Title == nil || strings.TrimSpace(*e.Title) == "" {
			return Card{}, fmt.Errorf("a card needs a title")
		}
		var created ghIssue
		if _, err := s.gh.Do("POST", "/repos/"+studio+"/issues", out, &created); err != nil {
			return Card{}, err
		}
		if col == "done" { // new issues are always open: close it to land in done
			if _, err := s.gh.Do("PATCH", fmt.Sprintf("/repos/%s/issues/%d", studio, created.Number), map[string]any{"state": "closed"}, &created); err != nil {
				return Card{}, err
			}
		}
		return toCard(created), nil
	}
	switch {
	case e.Delete:
		out["state"], out["state_reason"] = "closed", "not_planned"
		out["labels"] = append(labels, "hq:deleted")
	case col == "done":
		out["state"] = "closed"
	default:
		out["state"] = "open"
	}
	var updated ghIssue
	if _, err := s.gh.Do("PATCH", fmt.Sprintf("/repos/%s/issues/%d", studio, num), out, &updated); err != nil {
		return Card{}, err
	}
	return toCard(updated), nil
}

// ---------------------------------------------------------------- card comments

func (s *Server) cardComments(num int) ([]Msg, error) {
	studio := s.store.Get().Studio
	var cs []ghComment
	if _, err := s.gh.Do("GET", fmt.Sprintf("/repos/%s/issues/%d/comments?per_page=100", studio, num), nil, &cs); err != nil {
		return nil, err
	}
	out := make([]Msg, 0, len(cs))
	for _, c := range cs {
		out = append(out, Msg{ID: c.ID, Text: c.Body, Who: c.User.Login, Avatar: c.User.AvatarURL, At: c.CreatedAt})
	}
	return out, nil
}

func (s *Server) addCardComment(num int, text string) error {
	studio := s.store.Get().Studio
	_, err := s.gh.Do("POST", fmt.Sprintf("/repos/%s/issues/%d/comments", studio, num), map[string]string{"body": text}, nil)
	return err
}

// ---------------------------------------------------------------- starter boards

type TemplateCard struct {
	Title    string   `json:"title"`
	Body     string   `json:"body,omitempty"`
	Column   string   `json:"column"`
	Priority string   `json:"priority,omitempty"`
	Tags     []string `json:"tags,omitempty"`
}

type Template struct {
	Game  string         `json:"game"`
	Name  string         `json:"name"`
	Cards []TemplateCard `json:"cards"`
}

func loadTemplates() map[string]Template {
	out := map[string]Template{}
	ents, _ := templatesFS.ReadDir("templates")
	for _, e := range ents {
		b, err := templatesFS.ReadFile("templates/" + e.Name())
		if err != nil {
			continue
		}
		var t Template
		if json.Unmarshal(b, &t) == nil && t.Game != "" {
			out[t.Game] = t
		}
	}
	return out
}

// importTemplate adds a game's starter cards, skipping titles already on the board. Paced so
// github's anti-spam limit on creating issues doesn't kick in.
func (s *Server) importTemplate(j *Job, game string) error {
	t, ok := loadTemplates()[game]
	if !ok {
		return fmt.Errorf("no starter board for %s", game)
	}
	studio := s.store.Get().Studio
	for name, color := range tagColors {
		s.ensureLabel(studio, name, color)
	}
	have := map[string]bool{}
	if cards, err := s.board(); err == nil {
		for _, c := range cards {
			have[strings.ToLower(strings.TrimSpace(c.Title))] = true
		}
	}
	made := 0
	for i, tc := range t.Cards {
		if have[strings.ToLower(strings.TrimSpace(tc.Title))] {
			j.Printf("· already there: %s\n", tc.Title)
			continue
		}
		title, body, col, prio, g := tc.Title, tc.Body, tc.Column, tc.Priority, game
		tags := tc.Tags
		if tags == nil {
			tags = []string{}
		}
		if _, err := s.editCard(0, CardEdit{Title: &title, Body: &body, Column: &col, Priority: &prio, Game: &g, Tags: &tags}); err != nil {
			return fmt.Errorf("card %d (%s): %v", i+1, tc.Title, err)
		}
		made++
		j.Printf("+ [%s] %s\n", col, tc.Title)
		time.Sleep(importPace())
	}
	j.Printf("\nadded %d cards to the %s board\n", made, t.Name)
	return nil
}

func importPace() time.Duration {
	if v := os.Getenv("HQ_IMPORT_PACE_MS"); v != "" {
		if n, err := strconv.Atoi(v); err == nil {
			return time.Duration(n) * time.Millisecond
		}
	}
	return 1200 * time.Millisecond
}
