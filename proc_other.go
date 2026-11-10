//go:build !windows

package main

import (
	"os/exec"
	"runtime"
)

func hideWindow(c *exec.Cmd) {}

func opener() string {
	if runtime.GOOS == "darwin" {
		return "open"
	}
	return "xdg-open"
}

// launchTool on mac/linux: open a terminal if we can find one, else run it detached.
func launchTool(dir, script, args string) error {
	sh := "cd '" + dir + "' && '" + script + "' " + args + "; echo; read -p 'press enter to close'"
	if runtime.GOOS == "darwin" {
		return exec.Command("osascript", "-e", `tell app "Terminal" to do script "`+sh+`"`).Start()
	}
	for _, t := range []string{"x-terminal-emulator", "gnome-terminal", "konsole", "xterm"} {
		if p, err := exec.LookPath(t); err == nil {
			return exec.Command(p, "-e", "bash", "-c", sh).Start()
		}
	}
	c := exec.Command("bash", "-c", "'"+script+"' "+args)
	c.Dir = dir
	return c.Start()
}

func openPath(p string) error { return exec.Command(opener(), p).Start() }
func openURL(u string) error  { return exec.Command(opener(), u).Start() }

func scriptExts() []string { return []string{".sh", ".command"} }
