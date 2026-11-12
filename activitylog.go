package main

// the team activity log: everything people do through hq (cards, merges, publishes, invites,
// games list edits…) is written as a comment on one "📜 activity log" issue in the studio repo.
// shared, permanent, readable on github too. the changes page merges it with commits + releases.

import (
	"encoding/json"
	"fmt"
	"regexp"
	"strings"
	"sync"
)

const logLabel = "hq:log"

type LogEntry struct {
	Kind string `json:"k"`           // card | merge | publish | push | team | games | studio
	Game string `json:"g,omitempty"` // game id, if any
}

var logMetaRe = regexp.MustCompile(`\n?<!-- hq:(\{.*\}) -->\s*$`)

var logMu sync.Mutex

// ensureLogIssue returns the activity log issue number, making the issue the first time.
func (s *Server) ensureLogIssue() (int, error) {
	logMu.Lock()
	defer logMu.Unlock()
	t := s.team()
	if t.LogIssue != 0 {
		return t.LogIssue, nil
	}
	studio := s.store.Get().Studio
	if studio == "" {
		return 0, fmt.Errorf("no studio repo yet")
	}
	s.ensureLabel(studio, logLabel, "8b949e")
	var is ghIssue
	if _, err := s.gh.Do("POST", "/repos/"+studio+"/issues", map[string]any{
		"title": "📜 activity log", "labels": []string{logLabel},
		"body": "hq writes everything the team does here: card changes, merges, publishes, invites. read it in hq's changes page, or right here.",
	}, &is); err != nil {
		return 0, err
	}
	t.LogIssue = is.Number
	if err := s.saveTeam(t, "hq: activity log"); err != nil {
		return 0, err
	}
	return is.Number, nil
}

// logEvent records something in the background; a failed write never blocks the action.
func (s *Server) logEvent(kind, game, text string) {
	go func() {
		n, err := s.ensureLogIssue()
		if err != nil {
			return
		}
		meta, _ := json.Marshal(LogEntry{Kind: kind, Game: game})
		studio := s.store.Get().Studio
		_, _ = s.gh.Do("POST", fmt.Sprintf("/repos/%s/issues/%d/comments", studio, n),
			map[string]string{"body": text + "\n<!-- hq:" + string(meta) + " -->"}, nil)
	}()
}

func (s *Server) readLog() ([]Change, error) {
	t := s.team()
	studio := s.store.Get().Studio
	if studio == "" || t.LogIssue == 0 {
		return []Change{}, nil
	}
	var is ghIssue
	if _, err := s.gh.Do("GET", fmt.Sprintf("/repos/%s/issues/%d", studio, t.LogIssue), nil, &is); err != nil {
		return nil, err
	}
	last := max(1, (is.Comments+99)/100)
	var out []Change
	for p := max(1, last-1); p <= last; p++ {
		var cs []ghComment
		if _, err := s.gh.Do("GET", fmt.Sprintf("/repos/%s/issues/%d/comments?per_page=100&page=%d", studio, t.LogIssue, p), nil, &cs); err != nil {
			return nil, err
		}
		for _, c := range cs {
			e := LogEntry{Kind: "note"}
			text := c.Body
			if m := logMetaRe.FindStringSubmatch(text); m != nil {
				_ = json.Unmarshal([]byte(m[1]), &e)
				text = logMetaRe.ReplaceAllString(text, "")
			}
			out = append(out, Change{Kind: "event", Game: e.Game, Msg: strings.TrimSpace(text), Body: e.Kind,
				Who: c.User.Login, Avatar: c.User.AvatarURL, At: c.CreatedAt})
		}
	}
	return out, nil
}
