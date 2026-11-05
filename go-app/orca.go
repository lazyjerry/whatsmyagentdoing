package main

// Orca's workspaces, for "Open ▾ → Resume in Orca" (resume.go): a session
// whose folder is in one of them can be resumed in a terminal tab Orca opens
// in that workspace. Orca has no link or command that brings up an existing
// chat by session id, so that is a new tab, not the chat the session ran in.
//
// The workspace list comes from `orca worktree list`, run at most every
// OrcaRefreshEvery in the background; only `orca open` starts the app, so
// nothing here launches Orca when it isn't running (the list is just empty).

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"time"
)

type orcaWorkspaces struct {
	mu    sync.Mutex
	roots []string
	at    time.Time
	busy  bool
}

// Overridable in tests.
var (
	orcaSupported = runtime.GOOS == "darwin"
	orcaBinDirs   = []string{"/Applications/Orca.app/Contents/Resources/bin", filepath.Join(homeDir, "Applications", "Orca.app", "Contents", "Resources", "bin")}
	// orcaRun runs the orca CLI and returns its stdout.
	orcaRun = func(timeout time.Duration, args ...string) ([]byte, error) {
		bin := findOrca()
		if bin == "" {
			return nil, fmt.Errorf("orca CLI not found")
		}
		ctx, cancel := context.WithTimeout(context.Background(), timeout)
		defer cancel()
		cmd := exec.CommandContext(ctx, bin, args...)
		cmd.Dir = os.TempDir()
		var stderr bytes.Buffer
		cmd.Stderr = &stderr
		out, err := cmd.Output()
		if err != nil {
			msg := firstNonEmpty(strings.TrimSpace(stderr.String()), strings.TrimSpace(string(out)), err.Error())
			if len(msg) > 300 {
				msg = msg[:300]
			}
			return out, fmt.Errorf("orca: %s", msg)
		}
		return out, nil
	}
)

// findOrca: the CLI Orca points its own terminals at, then PATH, then the app bundle.
func findOrca() string {
	if p := os.Getenv("ORCA_CLI_COMMAND"); p != "" && fileExists(p) {
		return p
	}
	if p, err := exec.LookPath("orca"); err == nil {
		return p
	}
	for _, d := range orcaBinDirs {
		if p := filepath.Join(d, "orca"); fileExists(p) {
			return p
		}
	}
	return ""
}

// listOrcaWorkspaces returns the folders of Orca's workspaces on this machine.
func listOrcaWorkspaces() ([]string, error) {
	out, err := orcaRun(OrcaListTimeout, "worktree", "list", "--json", "--limit", "1000")
	if err != nil {
		return nil, err
	}
	var res struct {
		OK     bool `json:"ok"`
		Result struct {
			Worktrees []struct {
				Path       string `json:"path"`
				HostID     string `json:"hostId"`
				IsArchived bool   `json:"isArchived"`
			} `json:"worktrees"`
		} `json:"result"`
	}
	if err := json.Unmarshal(out, &res); err != nil || !res.OK {
		return nil, fmt.Errorf("orca: unexpected worktree list")
	}
	var roots []string
	for _, w := range res.Result.Worktrees {
		if w.HostID == "local" && !w.IsArchived && filepath.IsAbs(w.Path) {
			roots = append(roots, filepath.Clean(w.Path))
		}
	}
	return roots, nil
}

// orcaRoot is the deepest workspace folder holding cwd, "" when none does.
func orcaRoot(roots []string, cwd string) string {
	if cwd == "" {
		return ""
	}
	cwd = filepath.Clean(cwd)
	best := ""
	for _, r := range roots {
		if (cwd == r || strings.HasPrefix(cwd, r+string(filepath.Separator))) && len(r) > len(best) {
			best = r
		}
	}
	return best
}

// mark sets Orca on the sessions in an Orca workspace, and starts a refresh
// of the workspace list when it is due.
func (o *orcaWorkspaces) mark(views []View) {
	if !orcaSupported {
		return
	}
	o.mu.Lock()
	roots := o.roots
	if !o.busy && time.Since(o.at) > OrcaRefreshEvery {
		o.busy = true
		go o.refresh()
	}
	o.mu.Unlock()
	for i := range views {
		if !views[i].Subagent && views[i].Cwd != nil && orcaRoot(roots, *views[i].Cwd) != "" {
			views[i].Orca = true
		}
	}
}

func (o *orcaWorkspaces) refresh() {
	roots, _ := listOrcaWorkspaces()
	o.mu.Lock()
	o.roots, o.at, o.busy = roots, time.Now(), false
	o.mu.Unlock()
}

// root is the workspace holding cwd, from a fresh list.
func (o *orcaWorkspaces) root(cwd string) (string, error) {
	roots, err := listOrcaWorkspaces()
	o.mu.Lock()
	if err == nil {
		o.roots, o.at = roots, time.Now()
	}
	o.mu.Unlock()
	if err != nil {
		return "", err
	}
	return orcaRoot(roots, cwd), nil
}
