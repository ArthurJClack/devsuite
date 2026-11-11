//go:build windows

package main

import (
	"os/exec"
	"path/filepath"
	"syscall"
)

// relaunch starts the exe detached in its own console window.
func relaunch(exe string, args ...string) error {
	c := exec.Command(exe, args...)
	c.Dir = filepath.Dir(exe)
	c.SysProcAttr = &syscall.SysProcAttr{CreationFlags: 0x00000010} // CREATE_NEW_CONSOLE
	return c.Start()
}
