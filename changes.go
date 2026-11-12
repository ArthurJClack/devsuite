package main

// the changes log: every recent commit across the games (dev or main), plus published releases,
// newest first. click a commit for its full message and the files it touched.

import (
	"fmt"
	"net/http"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"
)

type Change struct {
	Kind   string    `json:"kind"` // commit | release
	Game   string    `json:"game"`
	SHA    string    `json:"sha,omitempty"`
	Msg    string    `json:"msg"`
	Body   string    `json:"body,omitempty"`
	Who    string    `json:"who"`
	Avatar string    `json:"avatar,omitempty"`
	At     time.Time `json:"at"`
	URL    string    `json:"url"`
	Tag    string    `json:"tag,omitempty"`
	Pre    bool      `json:"pre,omitempty"`
	Merge  bool      `json:"merge,omitempty"`
}

func (s *Server) apiChanges(w http.ResponseWriter, r *http.Request) (any, error) {
	q := r.URL.Query()
	branch := q.Get("branch") // "" = each game's dev
	page, _ := strconv.Atoi(q.Get("page"))
	if page < 1 {
		page = 1
	}
	per := 25
	var games []Game
	for _, g := range s.team().Games {
		if q.Get("game") == "" || q.Get("game") == g.ID {
			games = append(games, g)
		}
	}
	var mu sync.Mutex
	var out []Change
	var errs []string
	var wg sync.WaitGroup
	for _, g := range games {
		wg.Add(1)
		go func(g Game) {
			defer wg.Done()
			b := branch
			if b == "" || b == "dev" {
				b = g.devBranch()
			} else if b == "main" {
				b = g.mainBranch()
			}
			var cs []struct {
				ghCommit
				Parents []struct{} `json:"parents"`
			}
			_, err := s.gh.Do("GET", fmt.Sprintf("/repos/%s/commits?sha=%s&per_page=%d&page=%d", g.Repo, esc(b), per, page), nil, &cs)
			var rels []ghRelease
			if page == 1 && q.Get("releases") != "0" {
				_, _ = s.gh.Do("GET", "/repos/"+orDefault(g.Releases, g.Repo)+"/releases?per_page=10", nil, &rels)
			}
			mu.Lock()
			defer mu.Unlock()
			if err != nil {
				errs = append(errs, g.Name+": "+err.Error())
			}
			for _, c := range cs {
				a := toActivity(g.ID, c.ghCommit)
				parts := strings.SplitN(c.Commit.Message, "\n", 2)
				ch := Change{Kind: "commit", Game: g.ID, SHA: a.SHA, Msg: a.Msg, Who: a.Who, Avatar: a.Avatar, At: a.At, URL: a.URL, Merge: len(c.Parents) > 1}
				if len(parts) > 1 {
					ch.Body = strings.TrimSpace(parts[1])
				}
				out = append(out, ch)
			}
			for _, rl := range rels {
				out = append(out, Change{Kind: "release", Game: g.ID, Tag: rl.TagName, Msg: orDefault(rl.Name, rl.TagName), Body: rl.Body,
					At: rl.PublishedAt, URL: rl.HTMLURL, Pre: rl.Prerelease, Who: rl.Author.Login})
			}
		}(g)
	}
	wg.Wait()
	sort.Slice(out, func(a, b int) bool { return out[a].At.After(out[b].At) })
	return map[string]any{"changes": out, "errors": errs, "more": len(out) >= per}, nil
}

type commitDetail struct {
	SHA     string `json:"sha"`
	HTMLURL string `json:"html_url"`
	Commit  struct {
		Message string `json:"message"`
	} `json:"commit"`
	Stats struct {
		Additions int `json:"additions"`
		Deletions int `json:"deletions"`
	} `json:"stats"`
	Files []struct {
		Filename  string `json:"filename"`
		Status    string `json:"status"`
		Additions int    `json:"additions"`
		Deletions int    `json:"deletions"`
	} `json:"files"`
}

func (s *Server) apiChange(w http.ResponseWriter, r *http.Request) (any, error) {
	g, ok := s.game(r.PathValue("game"))
	if !ok {
		return nil, fmt.Errorf("unknown game")
	}
	var d commitDetail
	if _, err := s.gh.Do("GET", "/repos/"+g.Repo+"/commits/"+esc(r.PathValue("sha")), nil, &d); err != nil {
		return nil, err
	}
	if len(d.Files) > 80 {
		d.Files = d.Files[:80]
	}
	return d, nil
}
