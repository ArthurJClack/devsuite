//go:build !windows

package main

import (
	"os"
	"os/exec"
	"path/filepath"
	"syscall"
)

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
