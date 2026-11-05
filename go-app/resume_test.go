package main

import (
	"encoding/json"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func postResume(s *Server, key, app string) (int, map[string]any) {
	b, _ := json.Marshal(map[string]any{"key": key, "app": app})
	req := httptest.NewRequest("POST", "/api/resume", strings.NewReader(string(b)))
	req.Host = "localhost:4545"
	req.Header.Set("x-whatsmyagentdoing", "resume")
	rec := httptest.NewRecorder()
	s.ServeHTTP(rec, req)
	var out map[string]any
	json.Unmarshal(rec.Body.Bytes(), &out)
	return rec.Code, out
}

// resumeEnv: a fake claude in bin/, iTerm installed, an Orca workspace at
// ws/ with a session folder in it, and a folder outside any workspace.
func resumeEnv(t *testing.T) (s *Server, bin, inWS, outside string, calls *[][]string, opened *[]string) {
	t.Helper()
	bin, opened = launchEnv(t)
	fakeAgent(t, bin, "claude")
	root := t.TempDir()
	ws := filepath.Join(root, "ws")
	inWS = filepath.Join(ws, "it's here")
	outside = filepath.Join(root, "elsewhere")
	os.MkdirAll(inWS, 0o755)
	os.MkdirAll(outside, 0o755)
	calls = orcaEnv(t, ws)
	now := time.Now().UnixMilli()
	src := newFakeSource(
		Session{Agent: "claude", ID: "done-1", Cwd: inWS, Status: "done", LastEventAt: now},
		Session{Agent: "codex", ID: "busy", Cwd: ws, Status: "thinking", LastEventAt: now},
		Session{Agent: "claude", ID: "out", Cwd: outside, Status: "done", LastEventAt: now},
		Session{Agent: "claude", ID: "gone", Cwd: filepath.Join(ws, "deleted"), Status: "done", LastEventAt: now},
	)
	return newTestServer(t, src, Options{}, nil, nil), bin, inWS, outside, calls, opened
}

func TestResumeRefuses(t *testing.T) {
	s, _, _, _, calls, opened := resumeEnv(t)
	if rec := do(s, "POST", "/api/resume", nil); rec.Code != 403 {
		t.Errorf("without header: %d", rec.Code)
	}
	for _, c := range []struct{ key, app, why string }{
		{"codex:busy", "orca", "working session"},
		{"codex:busy", "default", "working session in a terminal"},
		{"claude:nope", "orca", "unknown session"},
		{"claude:out", "orca", "outside Orca"},
		{"claude:gone", "orca", "folder deleted"},
		{"claude:gone", "default", "folder deleted, terminal"},
		{"claude:done-1", "ghostty", "terminal not installed"},
	} {
		if code, _ := postResume(s, c.key, c.app); code != 400 {
			t.Errorf("%s: %d", c.why, code)
		}
	}
	if len(*calls) != 0 || len(*opened) != 0 {
		t.Fatalf("refused requests ran something: orca %v, opened %v", *calls, *opened)
	}
}

func TestResumeInOrca(t *testing.T) {
	s, _, inWS, _, calls, _ := resumeEnv(t)
	code, out := postResume(s, "claude:done-1", "orca")
	if code != 200 || out["ok"] != true {
		t.Fatalf("resume: %d %v", code, out)
	}
	ws := filepath.Dir(inWS)
	want := []string{"terminal", "create", "--worktree", "path:" + ws, "--command", "cd " + shQuote(inWS) + " && 'claude' --resume 'done-1'", "--focus", "--json"}
	if len(*calls) != 1 || strings.Join((*calls)[0], "\x00") != strings.Join(want, "\x00") {
		t.Errorf("orca args = %q, want %q", *calls, want)
	}
}

func TestResumeInTerminal(t *testing.T) {
	s, bin, _, outside, _, opened := resumeEnv(t)
	// A terminal doesn't need an Orca workspace.
	code, out := postResume(s, "claude:out", "iterm")
	if code != 200 || out["ok"] != true {
		t.Fatalf("resume: %d %v", code, out)
	}
	if len(*opened) != 1 || !strings.HasPrefix((*opened)[0], "iterm ") {
		t.Fatalf("opened = %v", *opened)
	}
	script := strings.TrimPrefix((*opened)[0], "iterm ")
	body, err := os.ReadFile(script)
	if err != nil {
		t.Fatal(err)
	}
	dir := filepath.Dir(script)
	want := "#!/bin/sh\n" +
		"# Written by whatsmyagentdoing (Open → Resume): reopens the session in its folder.\n" +
		"cd " + shQuote(outside) + " || exit 1\n" +
		"rm -rf " + shQuote(dir) + "\n" +
		"exec " + shQuote(filepath.Join(bin, "claude")) + " --resume 'out'\n"
	if string(body) != want {
		t.Errorf("script:\n%s\nwant:\n%s", body, want)
	}
	os.RemoveAll(dir)
}

func TestResumeCommand(t *testing.T) {
	for _, c := range []struct {
		s    Session
		want string
	}{
		{Session{Agent: "claude", ID: "a"}, "'claude' --resume 'a'"},
		{Session{Agent: "codex", ID: "b"}, "'codex' resume 'b'"},
		{Session{Agent: "gemini", ID: "c", Tool: "agy"}, "'agy' --conversation 'c'"},
		{Session{Agent: "gemini", ID: "file", SessionID: "d"}, "'gemini' --resume 'd'"},
		{Session{Agent: "opencode", ID: "e"}, "'opencode' --session 'e'"},
		{Session{Agent: "other", ID: "f"}, ""},
	} {
		if got := resumeCommand(c.s, bareBin); got != c.want {
			t.Errorf("%s: %q, want %q", c.s.Agent, got, c.want)
		}
	}
}
