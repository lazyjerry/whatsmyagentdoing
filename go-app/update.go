package main

// Version check and self-update. The check reads one public file (the
// package.json on GitHub's main branch) and sends nothing else. By default it
// only runs when the "Check for updates" button on the page is pressed;
// AutoUpdateCheck in config.go makes it automatic. Set WMAD_NO_UPDATE_CHECK=1
// to turn it off.
//
// The binary carries its own copy of the page, so updating means refreshing
// the sources and rebuilding: `git pull --ff-only` in the clone that holds
// go-app/, `scripts/sync-static.sh`, then `go build` over this executable.
// That only works when the executable still sits in its go-app/ source
// folder (see sourceDir); elsewhere canUpdate is false and the page hides
// the Update button.

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
	mu        sync.Mutex
	latest    string
	checkedAt time.Time
	inflight  chan struct{} // closed when the current fetch finishes
}

func NewUpdater() *Updater { return &Updater{} }

func (u *Updater) LocalVersion() string { return embeddedVersion }

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

func (u *Updater) CanUpdate() bool { return sourceDir() != "" }

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

// Update pulls the clone, re-syncs static/ and rebuilds this binary in place.
// It returns the version the new binary carries (read from the synced VERSION
// file, since this process still runs the old one).
func (u *Updater) Update() (string, error) {
	src := sourceDir()
	if src == "" {
		return "", errors.New("this binary is not inside its go-app/ source folder; rebuild from a git clone to get updates (or set WMAD_SRC)")
	}
	clone := filepath.Dir(src)
	exe, err := os.Executable()
	if err != nil {
		return "", err
	}
	if real, err := filepath.EvalSymlinks(exe); err == nil {
		exe = real
	}

	run := func(timeout time.Duration, dir string, name string, args ...string) (string, error) {
		ctx, cancel := context.WithTimeout(context.Background(), timeout)
		defer cancel()
		cmd := exec.CommandContext(ctx, name, args...)
		cmd.Dir = dir
		out, err := cmd.CombinedOutput()
		return strings.TrimSpace(string(out)), err
	}

	if out, err := run(60*time.Second, clone, "git", "pull", "--ff-only", "--quiet"); err != nil {
		if out == "" {
			out = err.Error()
		}
		return "", errors.New(out)
	}
	if out, err := run(60*time.Second, src, "bash", filepath.Join(src, "scripts", "sync-static.sh")); err != nil {
		return "", fmt.Errorf("sources updated, but syncing static/ failed: %s", firstNonEmpty(out, err.Error()))
	}
	goBin, err := exec.LookPath("go")
	if err != nil {
		return "", fmt.Errorf("sources updated, but `go` is not on PATH; rebuild by hand: cd %s && go build -o %s .", src, exe)
	}
	if out, err := run(5*time.Minute, src, goBin, "build", "-o", exe, "."); err != nil {
		return "", fmt.Errorf("sources updated, but rebuild failed: %s", firstNonEmpty(out, err.Error()))
	}
	v, err := os.ReadFile(filepath.Join(src, "static", "VERSION"))
	if err != nil {
		return u.LocalVersion(), nil
	}
	return strings.TrimSpace(string(v)), nil
}
