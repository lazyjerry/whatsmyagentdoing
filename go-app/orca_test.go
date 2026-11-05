package main

import (
	"encoding/json"
	"testing"
	"time"
)

// orcaEnv fakes the orca CLI: worktree list answers with roots, and every
// other call is recorded.
func orcaEnv(t *testing.T, roots ...string) *[][]string {
	t.Helper()
	withValue(t, &orcaSupported, true)
	var calls [][]string
	withValue(t, &orcaRun, func(_ time.Duration, args ...string) ([]byte, error) {
		if len(args) > 1 && args[0] == "worktree" && args[1] == "list" {
			var ws []map[string]any
			for _, r := range roots {
				ws = append(ws, map[string]any{"path": r, "hostId": "local"})
			}
			ws = append(ws, map[string]any{"path": "/remote/box", "hostId": "ssh-1"}, map[string]any{"path": "/old", "hostId": "local", "isArchived": true})
			return json.Marshal(map[string]any{"ok": true, "result": map[string]any{"worktrees": ws}})
		}
		calls = append(calls, args)
		return []byte(`{"ok":true}`), nil
	})
	return &calls
}

func TestOrcaRoot(t *testing.T) {
	roots := []string{"/ws/app", "/ws/app/sub", "/ws/other"}
	for cwd, want := range map[string]string{
		"/ws/app":          "/ws/app",
		"/ws/app/src":      "/ws/app",
		"/ws/app/sub/deep": "/ws/app/sub",
		"/ws/apple":        "",
		"/elsewhere":       "",
		"":                 "",
	} {
		if got := orcaRoot(roots, cwd); got != want {
			t.Errorf("orcaRoot(%q) = %q, want %q", cwd, got, want)
		}
	}
}

func TestOrcaMarksSessionsInWorkspaces(t *testing.T) {
	orcaEnv(t, "/ws/app")
	var o orcaWorkspaces
	o.refresh()
	views := []View{
		{Key: "claude:a", Cwd: nstr("/ws/app/src")},
		{Key: "claude:b", Cwd: nstr("/elsewhere")},
		{Key: "claude:c", Cwd: nstr("/ws/app"), Subagent: true},
		{Key: "claude:d", Cwd: nstr("/remote/box")},
		{Key: "claude:e", Cwd: nstr("/old")},
	}
	o.mark(views)
	for i, want := range []bool{true, false, false, false, false} {
		if views[i].Orca != want {
			t.Errorf("%s: orca = %v, want %v", views[i].Key, views[i].Orca, want)
		}
	}
}
