package main

import (
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
)

// discoverTools: the game's own listed tools, then every script in the root and tools/ folders.
func discoverTools(dir string, g Game) []Tool {
	var out []Tool
	seen := map[string]bool{}
	add := func(t Tool) {
		p := strings.Fields(t.Path)
		if len(p) == 0 || seen[strings.ToLower(t.Path)] {
			return
		}
		seen[strings.ToLower(t.Path)] = true
		out = append(out, t)
	}
	for _, t := range g.Tools {
		if fileExists(filepath.Join(dir, scriptOf(t.Path))) {
			add(t)
		}
	}
	exts := scriptExts()
	for _, sub := range []string{"", "tools", filepath.Join("tools", "sh")} {
		ents, err := os.ReadDir(filepath.Join(dir, sub))
		if err != nil {
			continue
		}
		var names []string
		for _, e := range ents {
			if e.IsDir() {
				continue
			}
			ext := strings.ToLower(filepath.Ext(e.Name()))
			for _, x := range exts {
				if ext == x {
					names = append(names, e.Name())
				}
			}
		}
		sort.Strings(names)
		for _, n := range names {
			rel := filepath.ToSlash(filepath.Join(sub, n))
			add(Tool{Name: prettyName(n), Path: rel})
		}
	}
	if len(out) > 40 {
		out = out[:40]
	}
	return out
}

// scriptOf strips args: "tools/publish_update.bat --hotfix" -> "tools/publish_update.bat".
// Paths with spaces are fine as long as the args start with "-".
func scriptOf(p string) string {
	if i := strings.Index(p, " -"); i > 0 {
		return p[:i]
	}
	return p
}

func argsOf(p string) string {
	if i := strings.Index(p, " -"); i > 0 {
		return strings.TrimSpace(p[i:])
	}
	return ""
}

func prettyName(n string) string {
	n = strings.TrimSuffix(n, filepath.Ext(n))
	n = strings.NewReplacer("_", " ", "-", " ").Replace(n)
	return strings.ToLower(strings.TrimSpace(n))
}

func fileExists(p string) bool {
	st, err := os.Stat(p)
	return err == nil && !st.IsDir()
}

// findGodotProject finds project.godot in the repo root or up to two folders down.
func findGodotProject(dir string) string {
	if fileExists(filepath.Join(dir, "project.godot")) {
		return dir
	}
	for _, pat := range []string{"*/project.godot", "*/*/project.godot"} {
		m, _ := filepath.Glob(filepath.Join(dir, pat))
		for _, p := range m {
			if !strings.Contains(p, ".git") && !strings.Contains(p, "addons") {
				return filepath.Dir(p)
			}
		}
	}
	return ""
}

// findGodot: the saved path, then PATH, then the usual places people unzip Godot.
func findGodot(saved string) string {
	if saved != "" && fileExists(saved) {
		return saved
	}
	for _, n := range []string{"godot", "godot4", "Godot", "godot-mono"} {
		if p, err := exec.LookPath(n); err == nil {
			return p
		}
	}
	home, _ := os.UserHomeDir()
	var pats []string
	switch runtime.GOOS {
	case "windows":
		pf := os.Getenv("ProgramFiles")
		for _, d := range []string{filepath.Join(home, "Downloads"), filepath.Join(home, "Desktop"), filepath.Join(home, "Documents"), pf, `C:\Godot`, filepath.Join(home, "Godot")} {
			pats = append(pats, filepath.Join(d, "Godot*.exe"), filepath.Join(d, "Godot*", "Godot*.exe"), filepath.Join(d, "Godot*", "*", "Godot*.exe"))
		}
		pats = append(pats, filepath.Join(os.Getenv("ProgramFiles(x86)"), "Steam", "steamapps", "common", "Godot Engine", "godot*.exe"),
			filepath.Join(home, "scoop", "shims", "godot*.exe"))
	case "darwin":
		pats = []string{"/Applications/Godot*.app/Contents/MacOS/Godot", filepath.Join(home, "Applications", "Godot*.app", "Contents", "MacOS", "Godot")}
	default:
		pats = []string{filepath.Join(home, "Downloads", "Godot*"), filepath.Join(home, ".local", "bin", "godot*"), "/opt/godot*/godot*"}
	}
	var hits []string
	for _, p := range pats {
		m, _ := filepath.Glob(p)
		for _, h := range m {
			if fileExists(h) && !strings.Contains(strings.ToLower(h), "console") {
				hits = append(hits, h)
			}
		}
	}
	if len(hits) == 0 {
		return ""
	}
	sort.Strings(hits) // newest version tends to sort last
	return hits[len(hits)-1]
}

func startDetached(exe string, args ...string) error {
	c := exec.Command(exe, args...)
	return c.Start()
}
