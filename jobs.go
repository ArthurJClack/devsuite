package main

import (
	"bufio"
	"fmt"
	"io"
	"os/exec"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"
)

// Job is anything slow (clone, push, publish). The UI polls its log so long pushes visibly move.
type Job struct {
	ID      string    `json:"id"`
	Title   string    `json:"title"`
	Game    string    `json:"game,omitempty"`
	Started time.Time `json:"started"`
	mu      sync.Mutex
	log     strings.Builder
	done    bool
	ok      bool
	Env     []string `json:"-"`
}

type Jobs struct {
	mu   sync.Mutex
	all  map[string]*Job
	next atomic.Int64
	// one git op per game folder at a time
	locks sync.Map
}

func newJobs() *Jobs { return &Jobs{all: map[string]*Job{}} }

func (js *Jobs) Start(title, game string, env []string, fn func(j *Job) error) *Job {
	j := &Job{ID: strconv.FormatInt(js.next.Add(1), 10), Title: title, Game: game, Started: time.Now(), Env: env}
	js.mu.Lock()
	js.all[j.ID] = j
	js.mu.Unlock()
	go func() {
		if game != "" {
			l, _ := js.locks.LoadOrStore(game, &sync.Mutex{})
			l.(*sync.Mutex).Lock()
			defer l.(*sync.Mutex).Unlock()
		}
		err := func() (err error) {
			defer func() {
				if r := recover(); r != nil {
					err = fmt.Errorf("crashed: %v", r)
				}
			}()
			return fn(j)
		}()
		j.mu.Lock()
		if err != nil {
			fmt.Fprintf(&j.log, "\n✗ %v\n", err)
		} else {
			fmt.Fprintf(&j.log, "\n✓ done (%s)\n", time.Since(j.Started).Round(time.Second))
		}
		j.done, j.ok = true, err == nil
		j.mu.Unlock()
	}()
	return j
}

func (js *Jobs) Get(id string) *Job {
	js.mu.Lock()
	defer js.mu.Unlock()
	return js.all[id]
}

func (j *Job) Printf(f string, a ...any) {
	j.mu.Lock()
	fmt.Fprintf(&j.log, f, a...)
	j.mu.Unlock()
}

func (j *Job) Snapshot(from int) (string, int, bool, bool) {
	j.mu.Lock()
	defer j.mu.Unlock()
	s := j.log.String()
	if from > len(s) || from < 0 {
		from = 0
	}
	return s[from:], len(s), j.done, j.ok
}

// Run executes a command in dir, streaming its output into the log. Returns trimmed stdout+stderr.
func (j *Job) Run(dir, name string, args ...string) (string, error) {
	j.Printf("$ %s %s\n", name, strings.Join(redact(args), " "))
	cmd := exec.Command(name, args...)
	cmd.Dir = dir
	cmd.Env = append(cmd.Environ(), j.Env...)
	hideWindow(cmd)
	pr, pw := io.Pipe()
	cmd.Stdout, cmd.Stderr = pw, pw
	var out strings.Builder
	done := make(chan struct{})
	go func() {
		sc := bufio.NewScanner(pr)
		sc.Buffer(make([]byte, 64*1024), 1024*1024)
		sc.Split(scanLinesCR)
		for sc.Scan() {
			line := sc.Text()
			out.WriteString(line + "\n")
			j.Printf("%s\n", line)
		}
		close(done)
	}()
	err := cmd.Start()
	if err == nil {
		err = cmd.Wait()
	}
	pw.Close()
	<-done
	return strings.TrimSpace(out.String()), err
}

// git progress uses \r; treat it as a line break so the log updates live.
func scanLinesCR(data []byte, atEOF bool) (int, []byte, error) {
	for i, b := range data {
		if b == '\n' || b == '\r' {
			return i + 1, data[:i], nil
		}
	}
	if atEOF && len(data) > 0 {
		return len(data), data, nil
	}
	return 0, nil, nil
}

func redact(args []string) []string {
	out := make([]string, len(args))
	for i, a := range args {
		if strings.Contains(strings.ToLower(a), "authorization") {
			a = "<auth header>"
		}
		out[i] = a
	}
	return out
}

func (js *Jobs) Running() int {
	js.mu.Lock()
	defer js.mu.Unlock()
	n := 0
	for _, j := range js.all {
		j.mu.Lock()
		if !j.done {
			n++
		}
		j.mu.Unlock()
	}
	return n
}
