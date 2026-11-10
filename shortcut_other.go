//go:build !windows

package main

import (
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"syscall"
)

// makeShortcuts: a ~/.local/bin/hq link and (linux) a menu entry.
func makeShortcuts(target string) {
	home, _ := os.UserHomeDir()
	bin := filepath.Join(home, ".local", "bin")
	if os.MkdirAll(bin, 0o755) == nil {
		link := filepath.Join(bin, "hq")
		os.Remove(link)
		_ = os.Symlink(target, link)
	}
	if runtime.GOOS == "linux" {
		apps := filepath.Join(home, ".local", "share", "applications")
		if os.MkdirAll(apps, 0o755) == nil {
			_ = os.WriteFile(filepath.Join(apps, "hq.desktop"), []byte("[Desktop Entry]\nType=Application\nName=hq · fish market studios\nExec="+target+"\nTerminal=true\nCategories=Development;\n"), 0o644)
		}
	}
}

func relaunch(exe string, args ...string) error {
	c := exec.Command(exe, args...)
	c.Dir = filepath.Dir(exe)
	c.SysProcAttr = &syscall.SysProcAttr{Setsid: true}
	_ = os.MkdirAll(configDir(), 0o700)
	if f, err := os.OpenFile(filepath.Join(configDir(), "hq.log"), os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o644); err == nil {
		c.Stdout, c.Stderr = f, f
	}
	return c.Start()
}
