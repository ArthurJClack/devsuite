package main

// hq as an app: the ui opens in its own window (edge/chrome app mode: no tabs, no address bar,
// its own taskbar icon), and hq quits when that window is closed. --browser uses a normal tab.

import (
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"sync/atomic"
	"time"
)

func appBrowser() string {
	var c []string
	switch runtime.GOOS {
	case "windows":
		for _, base := range []string{os.Getenv("ProgramFiles(x86)"), os.Getenv("ProgramFiles"), os.Getenv("LOCALAPPDATA")} {
			if base != "" {
				c = append(c, filepath.Join(base, "Microsoft", "Edge", "Application", "msedge.exe"),
					filepath.Join(base, "Google", "Chrome", "Application", "chrome.exe"))
			}
		}
	case "darwin":
		c = []string{"/Applications/Google Chrome.app/Contents/MacOS/Google Chrome", "/Applications/Microsoft Edge.app/Contents/MacOS/Microsoft Edge",
			"/Applications/Chromium.app/Contents/MacOS/Chromium"}
	default:
		for _, n := range []string{"google-chrome", "chromium", "chromium-browser", "microsoft-edge"} {
			if p, err := exec.LookPath(n); err == nil {
				c = append(c, p)
			}
		}
	}
	for _, p := range c {
		if fileExists(p) {
			return p
		}
	}
	return ""
}

// openApp opens the ui in an app window; falls back to the default browser. Returns true if it
// got an app window (so closing it may quit hq).
func openApp(url string) bool {
	if b := appBrowser(); b != "" {
		profile := filepath.Join(configDir(), "window")
		cmd := exec.Command(b, "--app="+url, "--user-data-dir="+profile, "--window-size=1440,920",
			"--no-first-run", "--no-default-browser-check", "--disable-features=Translate")
		if err := cmd.Start(); err == nil {
			go cmd.Wait()
			return true
		}
	}
	_ = openURL(url)
	return false
}

// ---------------------------------------------------------------- quit when the window goes away

type life struct {
	seen atomic.Int64 // unix ms of the last api request from the page
	bye  atomic.Int64 // unix ms the page said goodbye (window closing or reloading)
}

func (l *life) touch() { l.seen.Store(time.Now().UnixMilli()) }

// watch exits hq once the window has been gone a little while (a reload says bye, then pings
// again within a second) and nothing is running. 4 quiet minutes count as gone too.
func (l *life) watch(busy func() bool, quit func()) {
	for range time.Tick(3 * time.Second) {
		now := time.Now().UnixMilli()
		seen, bye := l.seen.Load(), l.bye.Load()
		if seen == 0 || busy() {
			continue
		}
		if (bye > seen && now-bye > 6000) || now-seen > 4*60*1000 {
			quit()
			return
		}
	}
}
