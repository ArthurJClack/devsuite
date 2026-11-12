package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"sync"
)

// Tool is a button on a game card: a script in the game's folder (relative path, may carry args).
type Tool struct {
	Name string `json:"name"`
	Path string `json:"path"`
}

// Game is one project hq manages. Source lives in Repo (private), player builds in Releases (public).
type Game struct {
	ID       string      `json:"id"`
	Name     string      `json:"name"`
	Genre    string      `json:"genre"`
	Repo     string      `json:"repo"`
	Releases string      `json:"releases,omitempty"`
	Dev      string      `json:"dev,omitempty"`
	Main     string      `json:"main,omitempty"`
	Engine   string      `json:"engine,omitempty"`
	Color    string      `json:"color,omitempty"`
	Tools    []Tool      `json:"tools,omitempty"`
	Publish  string      `json:"publish,omitempty"` // local script, run in hq with live output
	Hotfix   string      `json:"hotfix,omitempty"`
	Release  *ReleaseCfg `json:"release,omitempty"` // or: a github actions workflow does the release
}

// ReleaseCfg: publishing = starting a workflow_dispatch on github and watching it.
type ReleaseCfg struct {
	Workflow string            `json:"workflow"`         // e.g. release.yml
	Ref      string            `json:"ref,omitempty"`    // branch the workflow runs on (main)
	Update   map[string]string `json:"update,omitempty"` // inputs for an update
	Hotfix   map[string]string `json:"hotfix,omitempty"` // inputs for a hotfix
	Test     map[string]string `json:"test,omitempty"`   // inputs for a dry run (optional)
}

func (g Game) devBranch() string {
	if g.Dev == "" {
		return "dev"
	}
	return g.Dev
}

func (g Game) mainBranch() string {
	if g.Main == "" {
		return "main"
	}
	return g.Main
}

// Team is the shared studio file (hq.json in the studio repo, team.json in an invite package).
type Team struct {
	Studio    string `json:"studio"`
	Repo      string `json:"repo,omitempty"` // studio repo: board + chat live in its issues
	ChatIssue int    `json:"chatIssue,omitempty"`
	Games     []Game `json:"games"`
}

func defaultTeam() Team {
	return Team{
		Studio: "fish market studios",
		Games: []Game{
			{ID: "secret", Name: "SECRET", Genre: "rpg", Repo: "ArthurJClack/secret", Releases: "ArthurJClack/secret-releases",
				Engine: "godot", Color: "#c77dff", Release: &ReleaseCfg{Workflow: "release.yml", Ref: "main",
					Update: map[string]string{"kind": "update", "dry_run": "false"},
					Hotfix: map[string]string{"kind": "hotfix", "dry_run": "false"},
					Test:   map[string]string{"kind": "update", "dry_run": "true"}}},
			{ID: "mars", Name: "FOR/AGAINST", Genre: "rts", Repo: "ArthurJClack/marsprivate", Releases: "ArthurJClack/mars",
				Engine: "godot", Color: "#ff6b35"},
			{ID: "shells", Name: "SHELLS", Genre: "fps", Repo: "ArthurJClack/shells", Releases: "ArthurJClack/shells-release",
				Engine: "godot", Color: "#4cc9f0"},
			{ID: "shitbox", Name: "SHITBOX RALLY", Genre: "racing", Repo: "ArthurJClack/shitbox-rally", Releases: "ArthurJClack/shitboxrally-releases",
				Engine: "godot", Color: "#ffd166", Publish: "tools/publish_update.bat --yes", Hotfix: "tools/publish_update.bat --hotfix --yes",
				Tools: []Tool{{Name: "shitbox dashboard", Path: "Shitbox Dashboard.bat"}}},
		},
	}
}

// Local is this machine's settings, never shared.
type Local struct {
	Token     string `json:"token,omitempty"`
	Workspace string `json:"workspace,omitempty"`
	Godot     string `json:"godot,omitempty"`
	Studio    string `json:"studio,omitempty"` // owner/repo of the studio repo
	Team      *Team  `json:"team,omitempty"`   // last known team file (offline fallback)

	NoAutoUpdate bool `json:"noAutoUpdate,omitempty"`
}

type Store struct {
	mu   sync.Mutex
	path string
	L    Local
}

func configDir() string {
	if d := os.Getenv("HQ_HOME"); d != "" {
		return d
	}
	d, err := os.UserConfigDir()
	if err != nil {
		d = "."
	}
	return filepath.Join(d, "hq")
}

func loadStore() *Store {
	s := &Store{path: filepath.Join(configDir(), "config.json")}
	if b, err := os.ReadFile(s.path); err == nil {
		_ = json.Unmarshal(b, &s.L)
	}
	if s.L.Workspace == "" {
		home, _ := os.UserHomeDir()
		s.L.Workspace = filepath.Join(home, "hq-games")
	}
	// an invite package ships team.json next to the exe: adopt it on first run
	if s.L.Team == nil {
		if exe, err := os.Executable(); err == nil {
			if b, err := os.ReadFile(filepath.Join(filepath.Dir(exe), "team.json")); err == nil {
				var t Team
				if json.Unmarshal(b, &t) == nil && len(t.Games) > 0 {
					s.L.Team = &t
					if s.L.Studio == "" {
						s.L.Studio = t.Repo
					}
				}
			}
		}
	}
	return s
}

func (s *Store) Save() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := os.MkdirAll(filepath.Dir(s.path), 0o700); err != nil {
		return err
	}
	b, _ := json.MarshalIndent(s.L, "", "  ")
	tmp := s.path + ".tmp"
	if err := os.WriteFile(tmp, b, 0o600); err != nil {
		return err
	}
	return os.Rename(tmp, s.path)
}

func (s *Store) Get() Local {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.L
}

func (s *Store) Update(f func(*Local)) error {
	s.mu.Lock()
	f(&s.L)
	s.mu.Unlock()
	return s.Save()
}
