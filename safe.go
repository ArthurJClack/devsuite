package main

// crash safety: hq has no console on windows, so a crash used to just make the window say
// "failed to fetch". everything is logged to <config>/hq.log, go runtime crashes included, and
// background goroutines recover from panics instead of taking the whole app down.

import (
	"io"
	"log"
	"os"
	"path/filepath"
	"runtime/debug"
)

var logFile *os.File

func setupLog() {
	p := filepath.Join(configDir(), "hq.log")
	_ = os.MkdirAll(configDir(), 0o700)
	if st, err := os.Stat(p); err == nil && st.Size() > 2<<20 {
		_ = os.Rename(p, p+".old")
	}
	f, err := os.OpenFile(p, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o600)
	if err != nil {
		return
	}
	logFile = f
	log.SetOutput(io.MultiWriter(f, os.Stderr))
	log.SetFlags(log.LstdFlags)
	_ = debug.SetCrashOutput(f, debug.CrashOptions{}) // fatal runtime errors land here too
	log.Printf("---- hq %s starting (pid %d)", version, os.Getpid())
}

// goSafe runs f in a goroutine; a panic is logged instead of killing hq.
func goSafe(name string, f func()) {
	go func() {
		defer func() {
			if r := recover(); r != nil {
				log.Printf("PANIC in %s: %v\n%s", name, r, debug.Stack())
			}
		}()
		f()
	}()
}

// recoverLog: defer it first thing in a goroutine.
func recoverLog(where string) {
	if r := recover(); r != nil {
		log.Printf("PANIC in %s: %v\n%s", where, r, debug.Stack())
	}
}

func logf(f string, a ...any) { log.Printf(f, a...) }
