//go:build windows

package main

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
)

// makeShortcuts puts hq in the start menu and on the desktop (once; deleting the desktop one sticks).
func makeShortcuts(target string) {
	appdata := os.Getenv("APPDATA")
	home, _ := os.UserHomeDir()
	if appdata == "" {
		return
	}
	start := filepath.Join(appdata, "Microsoft", "Windows", "Start Menu", "Programs", "hq - fish market studios.lnk")
	desk := filepath.Join(home, "Desktop", "hq.lnk")
	marker := filepath.Join(filepath.Dir(target), ".shortcuts")
	links := []string{start}
	if !fileExists(marker) {
		links = append(links, desk)
	}
	q := func(s string) string { return "'" + strings.ReplaceAll(s, "'", "''") + "'" }
	var ps strings.Builder
	ps.WriteString("$w=New-Object -ComObject WScript.Shell;")
	for _, l := range links {
		ps.WriteString("$s=$w.CreateShortcut(" + q(l) + ");$s.TargetPath=" + q(target) +
			";$s.WorkingDirectory=" + q(filepath.Dir(target)) + ";$s.Description='fish market studios hq';$s.Save();")
	}
	c := exec.Command("powershell", "-NoProfile", "-NonInteractive", "-Command", ps.String())
	c.SysProcAttr = &syscall.SysProcAttr{HideWindow: true, CreationFlags: 0x08000000}
	if c.Run() == nil {
		_ = os.WriteFile(marker, []byte("1"), 0o644)
	}
}

// relaunch starts the exe detached in its own console window.
func relaunch(exe string, args ...string) error {
	c := exec.Command(exe, args...)
	c.Dir = filepath.Dir(exe)
	c.SysProcAttr = &syscall.SysProcAttr{CreationFlags: 0x00000010} // CREATE_NEW_CONSOLE
	return c.Start()
}
