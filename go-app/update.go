package main

// Version check and self-update. The check reads one public file (the
// package.json on GitHub's main branch) and sends nothing else. By default it
// only runs when the "Check for updates" button on the page is pressed;
// AutoUpdateCheck in config.go makes it automatic. Set WMAD_NO_UPDATE_CHECK=1
// to turn it off.
//
// Updating is `git pull --ff-only` in the clone followed by `go build` in
// go-app/, so it needs a git clone and a Go toolchain on PATH. The running
// binary is replaced in place and the CLI restarts it.

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"
)

const checkEvery = 30 * time.Minute

type Updater struct {
	root string

	mu        sync.Mutex
	latest    string
	checkedAt time.Time
	inflight  chan struct{} // closed when the current fetch finishes
}

func NewUpdater(root string) *Updater { return &Updater{root: root} }

func (u *Updater) LocalVersion() string {
	raw, err := os.ReadFile(filepath.Join(u.root, "package.json"))
	if err != nil {
		return "0.0.0"
	}
	var pkg struct {
		Version string `json:"version"`
	}
	if json.Unmarshal(raw, &pkg) != nil || pkg.Version == "" {
		return "0.0.0"
	}
	return pkg.Version
}

func newer(a, b string) bool {
	pa, pb := strings.Split(a, "."), strings.Split(b, ".")
	num := func(p []string, i int) int {
		if i >= len(p) {
			return 0
		}
		n, _ := strconv.Atoi(p[i])
		return n
	}
	for i := 0; i < 3; i++ {
		if num(pa, i) != num(pb, i) {
			return num(pa, i) > num(pb, i)
		}
	}
	return false
}

func fetchLatest() string {
	ctx, cancel := context.WithTimeout(context.Background(), 4*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, "GET", LatestURL, nil)
	if err != nil {
		return ""
	}
	req.Header.Set("User-Agent", "whatsmyagentdoing")
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		return ""
	}
	defer res.Body.Close()
	if res.StatusCode != 200 {
		return ""
	}
	body, err := io.ReadAll(io.LimitReader(res.Body, 64*1024))
	if err != nil {
		return ""
	}
	var pkg struct {
		Version string `json:"version"`
	}
	if json.Unmarshal(body, &pkg) != nil {
		return ""
	}
	return pkg.Version
}

// CheckLatest returns the newest known version ("" when unknown). Concurrent
// callers share one request.
func (u *Updater) CheckLatest(force bool) string {
	if os.Getenv("WMAD_NO_UPDATE_CHECK") != "" {
		return ""
	}
	if v := os.Getenv("WMAD_LATEST_VERSION"); v != "" { // for testing
		u.mu.Lock()
		u.latest = v
		u.mu.Unlock()
		return v
	}
	u.mu.Lock()
	if !force && u.latest != "" && time.Since(u.checkedAt) < checkEvery {
		defer u.mu.Unlock()
		return u.latest
	}
	if u.inflight == nil {
		done := make(chan struct{})
		u.inflight = done
		go func() {
			v := fetchLatest()
			u.mu.Lock()
			if v != "" {
				u.latest = v
				u.checkedAt = time.Now()
			}
			u.inflight = nil
			u.mu.Unlock()
			close(done)
		}()
	}
	wait := u.inflight
	u.mu.Unlock()
	<-wait
	u.mu.Lock()
	defer u.mu.Unlock()
	return u.latest
}

func (u *Updater) CanUpdate() bool {
	_, err := os.Stat(filepath.Join(u.root, ".git"))
	return err == nil
}

type VersionInfo struct {
	Version         string  `json:"version"`
	Latest          *string `json:"latest"`
	UpdateAvailable bool    `json:"updateAvailable"`
	CanUpdate       bool    `json:"canUpdate"`
	Auto            bool    `json:"auto"`
}

func (u *Updater) Info() VersionInfo {
	u.mu.Lock()
	latest := u.latest
	u.mu.Unlock()
	version := u.LocalVersion()
	info := VersionInfo{Version: version, CanUpdate: u.CanUpdate(), Auto: AutoUpdateCheck}
	if latest != "" {
		info.Latest = &latest
		info.UpdateAvailable = newer(latest, version)
	}
	return info
}

// Update pulls the clone and rebuilds this binary in place.
func (u *Updater) Update() (string, error) {
	if !u.CanUpdate() {
		return "", fmt.Errorf("not a git clone (%s); reinstall it with git clone to get updates", u.root)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	pull := exec.CommandContext(ctx, "git", "-C", u.root, "pull", "--ff-only", "--quiet")
	if out, err := pull.CombinedOutput(); err != nil {
		msg := strings.TrimSpace(string(out))
		if msg == "" {
			msg = err.Error()
		}
		return "", errors.New(msg)
	}
	if err := u.rebuild(); err != nil {
		return "", err
	}
	return u.LocalVersion(), nil
}

func (u *Updater) rebuild() error {
	exe, err := os.Executable()
	if err != nil {
		return err
	}
	if real, err := filepath.EvalSymlinks(exe); err == nil {
		exe = real
	}
	goBin, err := exec.LookPath("go")
	if err != nil {
		return fmt.Errorf("sources updated, but `go` is not on PATH; rebuild by hand: cd %s && go build -o %s .", filepath.Join(u.root, "go-app"), exe)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()
	build := exec.CommandContext(ctx, goBin, "build", "-o", exe, ".")
	build.Dir = filepath.Join(u.root, "go-app")
	if out, err := build.CombinedOutput(); err != nil {
		return fmt.Errorf("sources updated, but rebuild failed: %s", strings.TrimSpace(string(out)))
	}
	return nil
}
