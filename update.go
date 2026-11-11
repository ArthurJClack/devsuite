package main

// auto-update from the devsuite releases. installing (folder + shortcuts) is install.ps1's job:
// an exe that copies itself around and spawns powershell looks like malware to antivirus.

import (
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"time"
)

const updateRepo = "ArthurJClack/devsuite"

func releaseRepo() string {
	if r := os.Getenv("HQ_UPDATE_REPO"); r != "" {
		return r
	}
	return updateRepo
}

func assetName() string {
	switch runtime.GOOS {
	case "windows":
		return "hq.exe"
	case "darwin":
		return "hq-macos"
	}
	return "hq-linux"
}

func exeName() string {
	if runtime.GOOS == "windows" {
		return "hq.exe"
	}
	return "hq"
}

func installDir() string {
	if d := os.Getenv("HQ_INSTALL_DIR"); d != "" {
		return d
	}
	if runtime.GOOS == "windows" {
		if la := os.Getenv("LOCALAPPDATA"); la != "" {
			return filepath.Join(la, "Programs", "hq")
		}
	}
	home, _ := os.UserHomeDir()
	return filepath.Join(home, ".local", "share", "hq")
}

// parseVer: "v1.2.3" -> [1 2 3]; ok=false for "dev" or junk.
func parseVer(v string) ([3]int, bool) {
	var out [3]int
	v = strings.TrimPrefix(strings.TrimSpace(v), "v")
	if i := strings.IndexAny(v, "-+ "); i >= 0 {
		v = v[:i]
	}
	parts := strings.Split(v, ".")
	if len(parts) == 0 || len(parts) > 3 {
		return out, false
	}
	for i, p := range parts {
		n, err := strconv.Atoi(p)
		if err != nil {
			return out, false
		}
		out[i] = n
	}
	return out, true
}

// newer reports whether a is a higher version than b. unparseable b ("dev") is never upgraded automatically.
func newer(a, b string) bool {
	va, ok1 := parseVer(a)
	vb, ok2 := parseVer(b)
	if !ok1 || !ok2 {
		return false
	}
	for i := 0; i < 3; i++ {
		if va[i] != vb[i] {
			return va[i] > vb[i]
		}
	}
	return false
}

func sameFile(a, b string) bool {
	ra, e1 := filepath.EvalSymlinks(a)
	rb, e2 := filepath.EvalSymlinks(b)
	if e1 != nil || e2 != nil {
		return strings.EqualFold(filepath.Clean(a), filepath.Clean(b))
	}
	return strings.EqualFold(ra, rb)
}

func exeVersion(path string) string {
	c := exec.Command(path, "--version")
	hideWindow(c)
	out, err := c.Output()
	if err != nil {
		return ""
	}
	f := strings.Fields(string(out))
	if len(f) < 2 {
		return ""
	}
	return f[1]
}

// ---------------------------------------------------------------- updater

type UpdateInfo struct {
	Current   string    `json:"current"`
	Latest    string    `json:"latest,omitempty"`
	Available bool      `json:"available"`
	Notes     string    `json:"notes,omitempty"`
	URL       string    `json:"url,omitempty"`
	Checked   time.Time `json:"checked"`
	Error     string    `json:"error,omitempty"`
	Applying  bool      `json:"applying"`
	Auto      bool      `json:"auto"`
	Installed bool      `json:"installed"`
	asset     string
}

type Updater struct {
	mu   sync.Mutex
	info UpdateInfo
	gh   *GH
}

func (u *Updater) Info() UpdateInfo {
	u.mu.Lock()
	defer u.mu.Unlock()
	i := u.info
	i.Current = version
	exe, _ := os.Executable()
	i.Installed = sameFile(exe, filepath.Join(installDir(), exeName()))
	return i
}

func (u *Updater) Check() UpdateInfo {
	var rel struct {
		TagName string `json:"tag_name"`
		Body    string `json:"body"`
		HTMLURL string `json:"html_url"`
		Assets  []struct {
			Name string `json:"name"`
			URL  string `json:"browser_download_url"`
		} `json:"assets"`
	}
	_, err := u.gh.Do("GET", "/repos/"+releaseRepo()+"/releases/latest", nil, &rel)
	u.mu.Lock()
	u.info.Checked = time.Now()
	if err != nil {
		if isStatus(err, 404) {
			u.info.Error = "no releases yet"
		} else {
			u.info.Error = err.Error()
		}
	} else {
		u.info.Error = ""
		u.info.Latest, u.info.Notes, u.info.URL, u.info.asset = rel.TagName, rel.Body, rel.HTMLURL, ""
		for _, a := range rel.Assets {
			if a.Name == assetName() {
				u.info.asset = a.URL
			}
		}
		u.info.Available = u.info.asset != "" && (newer(rel.TagName, version) || (version == "dev" && os.Getenv("HQ_UPDATE_DEV") == "1"))
	}
	u.mu.Unlock()
	return u.Info()
}

// Apply downloads the new exe, swaps it in (renaming a running exe is allowed on windows,
// overwriting it isn't), then restart() relaunches.
func (u *Updater) Apply(restart func(exe string)) error {
	u.mu.Lock()
	if u.info.Applying {
		u.mu.Unlock()
		return fmt.Errorf("already updating")
	}
	asset, tag := u.info.asset, u.info.Latest
	if asset == "" || !u.info.Available {
		u.mu.Unlock()
		return fmt.Errorf("no update available")
	}
	u.info.Applying = true
	u.mu.Unlock()
	fail := func(err error) error {
		u.mu.Lock()
		u.info.Applying, u.info.Error = false, err.Error()
		u.mu.Unlock()
		return err
	}

	exe, err := os.Executable()
	if err != nil {
		return fail(err)
	}
	exe, _ = filepath.EvalSymlinks(exe)
	fresh := exe + ".new"
	resp, err := (&http.Client{Timeout: 10 * time.Minute}).Get(asset)
	if err != nil {
		return fail(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		return fail(fmt.Errorf("download failed: %s", resp.Status))
	}
	f, err := os.OpenFile(fresh, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0o755)
	if err != nil {
		return fail(err)
	}
	n, err := io.Copy(f, resp.Body)
	f.Close()
	if err != nil || n < 1<<20 {
		os.Remove(fresh)
		return fail(fmt.Errorf("download broke (%d bytes): %v", n, err))
	}
	// make sure it actually runs on this machine before swapping
	if got := exeVersion(fresh); got == "" {
		os.Remove(fresh)
		return fail(fmt.Errorf("the downloaded %s doesn't run here", tag))
	}
	old := exe + ".old"
	os.Remove(old)
	if err := os.Rename(exe, old); err != nil {
		os.Remove(fresh)
		return fail(err)
	}
	if err := os.Rename(fresh, exe); err != nil {
		_ = os.Rename(old, exe)
		return fail(err)
	}
	restart(exe)
	return nil
}

// cleanupOld removes the previous exe left behind by an update.
func cleanupOld() {
	if exe, err := os.Executable(); err == nil {
		exe, _ = filepath.EvalSymlinks(exe)
		for i := 0; i < 10; i++ { // the old process may still be exiting
			if err := os.Remove(exe + ".old"); err == nil || os.IsNotExist(err) {
				return
			}
			time.Sleep(500 * time.Millisecond)
		}
	}
}

// loop checks on start and every 3h; with auto on it updates as soon as nothing's running.
func (u *Updater) loop(auto func() bool, idle func() bool, restart func(string)) {
	time.Sleep(5 * time.Second)
	for {
		info := u.Check()
		u.mu.Lock()
		u.info.Auto = auto()
		u.mu.Unlock()
		if info.Available && auto() {
			for !idle() {
				time.Sleep(10 * time.Second)
			}
			fmt.Println("updating to", info.Latest)
			if err := u.Apply(restart); err != nil {
				fmt.Fprintln(os.Stderr, "update failed:", err)
			}
		}
		time.Sleep(3 * time.Hour)
	}
}
