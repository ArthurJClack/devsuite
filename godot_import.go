package main

import (
	"os"
	"path/filepath"
	"strings"
)

// a game run straight from source (godot --path, no -e) never imports anything: assets that
// aren't in .godot/ yet just fail to load and the game closes. so before launching we import
// whenever the checkout moved to a commit we haven't imported. checking only "does .godot exist"
// wasn't enough: a copy whose whole project got replaced (contraband became a shitbox fork) kept
// the old project's cache, skipped the import and wouldn't open.

func importMarker(proj string) string { return filepath.Join(proj, ".godot", "hq_imported") }

func importedAt(proj string) string {
	b, err := os.ReadFile(importMarker(proj))
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(b))
}

func ensureImported(j *Job, godot, proj string) {
	head := gitQuick(proj, "rev-parse", "HEAD")
	if head != "" && importedAt(proj) == head {
		return
	}
	if dirExists(filepath.Join(proj, ".godot")) {
		j.Printf("new commits since the last import: importing changed assets…\n")
	} else {
		j.Printf("first time: importing assets, can take a few minutes…\n")
	}
	if _, err := j.Run(proj, godot, "--headless", "--path", proj, "--import"); err != nil {
		j.Printf("import exited with an error (if the game has .blend models, check godot's blender path in editor settings), trying to launch anyway\n")
		return
	}
	if head != "" {
		_ = os.WriteFile(importMarker(proj), []byte(head+"\n"), 0o644)
	}
}
