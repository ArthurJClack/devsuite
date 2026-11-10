//go:build windows

package main

import (
	"os/exec"
	"syscall"
)

func hideWindow(c *exec.Cmd) {
	c.SysProcAttr = &syscall.SysProcAttr{HideWindow: true, CreationFlags: 0x08000000} // CREATE_NO_WINDOW
}

// launchTool runs a game script in its own console window, the way double-clicking it would.
func launchTool(dir, script, args string) error {
	line := `start "" /D "` + dir + `" "` + script + `"`
	if args != "" {
		line += " " + args
	}
	c := exec.Command("cmd", "/C", line)
	c.SysProcAttr = &syscall.SysProcAttr{CmdLine: `cmd /C ` + line, HideWindow: true}
	c.Dir = dir
	return c.Start()
}

func openPath(p string) error { return exec.Command("explorer.exe", p).Start() }

func openURL(u string) error {
	return exec.Command("rundll32", "url.dll,FileProtocolHandler", u).Start()
}

func scriptExts() []string { return []string{".bat", ".cmd"} }
