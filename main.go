// hq: fish market studios' all-in-one team app. One exe, opens in your browser, GitHub does the
// syncing: games are repos, the board and chat are issues in a private studio repo.
package main

import (
	"crypto/rand"
	"embed"
	"encoding/hex"
	"flag"
	"fmt"
	"io/fs"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
)

var version = "dev"

//go:embed web
var webDir embed.FS

func webFS() fs.FS {
	sub, _ := fs.Sub(webDir, "web")
	return sub
}

func hasGit() bool {
	_, err := exec.LookPath("git")
	return err == nil
}

// key persists so a second launch can find the running copy and just open it.
func loadKey() string {
	p := filepath.Join(configDir(), "key")
	if b, err := os.ReadFile(p); err == nil && len(strings.TrimSpace(string(b))) >= 32 {
		return strings.TrimSpace(string(b))
	}
	b := make([]byte, 24)
	_, _ = rand.Read(b)
	k := hex.EncodeToString(b)
	_ = os.MkdirAll(configDir(), 0o700)
	_ = os.WriteFile(p, []byte(k), 0o600)
	return k
}

func main() {
	port := flag.Int("port", 7313, "port to serve on (localhost only)")
	noBrowser := flag.Bool("no-browser", false, "don't open the browser")
	showVer := flag.Bool("version", false, "print version")
	flag.Parse()
	if *showVer {
		fmt.Println("hq", version)
		return
	}

	key := loadKey()
	addr := fmt.Sprintf("127.0.0.1:%d", *port)
	url := fmt.Sprintf("http://%s/#k=%s", addr, key)

	ln, err := net.Listen("tcp", addr)
	if err != nil {
		// already running? then just open it
		req, _ := http.NewRequest("GET", "http://"+addr+"/api/ping", nil)
		req.Header.Set("X-HQ-Key", key)
		if resp, e := (&http.Client{Timeout: 2 * time.Second}).Do(req); e == nil && resp.StatusCode == 200 {
			fmt.Println("hq is already running:", url)
			if !*noBrowser {
				_ = openURL(url)
			}
			return
		}
		fmt.Fprintln(os.Stderr, "can't listen on", addr, ":", err, "(try --port 7314)")
		os.Exit(1)
	}

	st := loadStore()
	s := &Server{store: st, jobs: newJobs(), key: key}
	s.gh = newGH(s.token)
	if st.Get().Token == "" {
		s.cliToken = ghCLIToken()
	}

	fmt.Printf("hq %s running at %s\nkeep this window open while you use it.\n", version, url)
	if !*noBrowser {
		go func() { time.Sleep(300 * time.Millisecond); _ = openURL(url) }()
	}
	srv := &http.Server{Handler: s.routes(), ReadHeaderTimeout: 10 * time.Second}
	if err := srv.Serve(ln); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
