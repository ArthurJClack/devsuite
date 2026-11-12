package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"strings"
	"sync"
	"time"
)

// GH is a tiny GitHub REST client. GETs are cached by ETag: a 304 costs no rate limit, so the
// board and chat can poll every few seconds.
type GH struct {
	Base  string
	Token func() string
	hc    *http.Client
	mu    sync.Mutex
	cache map[string]cached
}

type cached struct {
	etag string
	body []byte
	link string
}

type APIError struct {
	Status int
	Msg    string
}

func (e *APIError) Error() string { return fmt.Sprintf("github %d: %s", e.Status, e.Msg) }

func newGH(token func() string) *GH {
	base := os.Getenv("HQ_API")
	if base == "" {
		base = "https://api.github.com"
	}
	return &GH{Base: strings.TrimRight(base, "/"), Token: token, hc: &http.Client{Timeout: 30 * time.Second}, cache: map[string]cached{}}
}

// Do sends a request; out may be nil. Returns the Link header (pagination).
func (g *GH) Do(method, path string, body, out any) (string, error) {
	var rd io.Reader
	if body != nil {
		b, _ := json.Marshal(body)
		rd = bytes.NewReader(b)
	}
	req, err := http.NewRequest(method, g.Base+path, rd)
	if err != nil {
		return "", err
	}
	req.Header.Set("Accept", "application/vnd.github+json")
	req.Header.Set("X-GitHub-Api-Version", "2022-11-28")
	req.Header.Set("User-Agent", "hq")
	if t := g.Token(); t != "" {
		req.Header.Set("Authorization", "Bearer "+t)
	}
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	key := path
	var sent cached // the copy a 304 refers to, even if a write drops the cache meanwhile
	if method == "GET" {
		g.mu.Lock()
		if c, ok := g.cache[key]; ok {
			sent = c
			req.Header.Set("If-None-Match", c.etag)
		}
		g.mu.Unlock()
	} else {
		g.invalidate(path)
	}
	resp, err := g.hc.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	data, _ := io.ReadAll(resp.Body)
	link := resp.Header.Get("Link")
	if resp.StatusCode == http.StatusNotModified {
		data, link = sent.body, sent.link
	} else if resp.StatusCode >= 300 {
		var m struct {
			Message string `json:"message"`
		}
		_ = json.Unmarshal(data, &m)
		if m.Message == "" {
			m.Message = strings.TrimSpace(string(data))
		}
		return link, &APIError{resp.StatusCode, m.Message}
	} else if method == "GET" {
		if et := resp.Header.Get("ETag"); et != "" {
			g.mu.Lock()
			g.cache[key] = cached{et, data, link}
			g.mu.Unlock()
		}
	}
	if out != nil && len(data) > 0 {
		return link, json.Unmarshal(data, out)
	}
	return link, nil
}

// invalidate drops cached GETs under the same repo so a write shows up on the next poll.
func (g *GH) invalidate(path string) {
	parts := strings.SplitN(strings.TrimPrefix(path, "/"), "/", 4)
	prefix := path
	if len(parts) >= 3 && parts[0] == "repos" {
		prefix = "/repos/" + parts[1] + "/" + parts[2]
	}
	g.mu.Lock()
	for k := range g.cache {
		if strings.HasPrefix(k, prefix) {
			delete(g.cache, k)
		}
	}
	g.mu.Unlock()
}

func isStatus(err error, code int) bool {
	e, ok := err.(*APIError)
	return ok && e.Status == code
}

func esc(s string) string { return url.PathEscape(s) }

// ghCLIToken borrows the token from the GitHub CLI when it's installed and signed in.
func ghCLIToken() string {
	out, err := exec.Command("gh", "auth", "token").Output()
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(out))
}
