package main

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"
)

// ocFixture builds an opencode.db (the columns the query touches) with the
// sqlite3 CLI and points the watcher at it. Statements run in order.
func ocFixture(t *testing.T, statements ...string) string {
	t.Helper()
	if _, err := exec.LookPath("sqlite3"); err != nil {
		t.Skip("sqlite3 not installed")
	}
	setLogDirs(t)
	db := ocDB
	sql := `create table project (id text primary key, worktree text not null);
create table session (id text primary key, project_id text not null, parent_id text, title text not null, directory text not null, model text, time_created integer not null, time_updated integer not null);
create table message (id text primary key, session_id text not null, time_created integer not null, time_updated integer not null, data text not null);
create table part (id text primary key, message_id text not null, session_id text not null, time_created integer not null, time_updated integer not null, data text not null);
insert into project values ('p1', '/srv/app'), ('global', '/');`
	for _, st := range statements {
		sql += st
	}
	if err := os.MkdirAll(filepath.Dir(db), 0o755); err != nil {
		t.Fatal(err)
	}
	if out, err := exec.Command("sqlite3", db, sql).CombinedOutput(); err != nil {
		t.Fatalf("sqlite3: %v\n%s", err, out)
	}
	return db
}

func ocAgo(d time.Duration) int64 { return time.Now().Add(-d).UnixMilli() }

func ocSessionSQL(id, project, parent, title, dir string, created, updated int64) string {
	p := "null"
	if parent != "" {
		p = "'" + parent + "'"
	}
	return fmt.Sprintf(`insert into session values ('%s', '%s', %s, '%s', '%s', '{"id":"big-pickle","providerID":"opencode","variant":"default"}', %d, %d);`, id, project, p, title, dir, created, updated)
}

func ocMessageSQL(id, session string, at int64, data string) string {
	return fmt.Sprintf(`insert into message values ('%s', '%s', %d, %d, '%s');`, id, session, at, at, data)
}

func ocPartSQL(id, message, session string, at int64, data string) string {
	return fmt.Sprintf(`insert into part values ('%s', '%s', '%s', %d, %d, '%s');`, id, message, session, at, at, data)
}

func TestOpenCodeRowsBecomeSessions(t *testing.T) {
	turn := ocAgo(time.Minute)
	ocFixture(t,
		// Mid-turn: the assistant is running a bash call.
		ocSessionSQL("run-1", "p1", "", "New session - 2026-10-26T04:21:31.288Z", "/srv/app", ocAgo(5*time.Minute), ocAgo(time.Minute)),
		ocMessageSQL("u1", "run-1", turn, fmt.Sprintf(`{"role":"user","time":{"created":%d}}`, turn)),
		ocPartSQL("u1-1", "u1", "run-1", turn, `{"type":"text","text":"list files"}`),
		ocPartSQL("u1-2", "u1", "run-1", turn, `{"type":"text","synthetic":true,"text":"Called the Read tool"}`),
		ocMessageSQL("a1", "run-1", ocAgo(50*time.Second), fmt.Sprintf(`{"role":"assistant","time":{"created":%d}}`, ocAgo(50*time.Second))),
		ocPartSQL("a1-1", "a1", "run-1", ocAgo(50*time.Second), `{"type":"step-start"}`),
		ocPartSQL("a1-2", "a1", "run-1", ocAgo(45*time.Second), `{"type":"reasoning","text":"Let me look."}`),
		ocPartSQL("a1-3", "a1", "run-1", ocAgo(40*time.Second), `{"type":"tool","tool":"bash","callID":"c1","state":{"status":"running","input":{"command":"ls -la"},"time":{"start":1}}}`),
		// Finished turn with a reply, in the "global" project.
		ocSessionSQL("done-1", "global", "", "Fix the build", "/tmp/x", ocAgo(10*time.Minute), ocAgo(2*time.Minute)),
		ocMessageSQL("u2", "done-1", ocAgo(3*time.Minute), `{"role":"user","time":{"created":1}}`),
		ocPartSQL("u2-1", "u2", "done-1", ocAgo(3*time.Minute), `{"type":"text","text":"fix it"}`),
		ocMessageSQL("a2", "done-1", ocAgo(2*time.Minute), `{"role":"assistant","time":{"created":1,"completed":2}}`),
		ocPartSQL("a2-1", "a2", "done-1", ocAgo(2*time.Minute), `{"type":"text","text":"Done, tests pass."}`),
		ocPartSQL("a2-2", "a2", "done-1", ocAgo(2*time.Minute), `{"type":"step-finish","reason":"stop"}`),
		// A subagent writing its reply.
		ocSessionSQL("sub-1", "p1", "run-1", "Explore repo (@explore subagent)", "/srv/app", ocAgo(time.Minute), ocAgo(30*time.Second)),
		ocMessageSQL("a3", "sub-1", ocAgo(30*time.Second), `{"role":"assistant","time":{"created":1}}`),
		ocPartSQL("a3-1", "a3", "sub-1", ocAgo(30*time.Second), `{"type":"text","text":"Looking around."}`),
		// Provider error, user abort, prompt without a reply yet, no messages.
		ocSessionSQL("err-1", "p1", "", "New session - x", "/srv/app", ocAgo(time.Minute), ocAgo(time.Minute)),
		ocMessageSQL("a4", "err-1", ocAgo(time.Minute), `{"role":"assistant","time":{"created":1,"completed":2},"error":{"name":"APIError","data":{"message":"This API key has expired.","statusCode":401}}}`),
		ocSessionSQL("abort-1", "p1", "", "New session - x", "/srv/app", ocAgo(time.Minute), ocAgo(time.Minute)),
		ocMessageSQL("a5", "abort-1", ocAgo(time.Minute), `{"role":"assistant","time":{"created":1},"error":{"name":"MessageAbortedError","data":{}}}`),
		ocSessionSQL("user-1", "p1", "", "New session - x", "/srv/app", ocAgo(time.Minute), ocAgo(time.Minute)),
		ocMessageSQL("u6", "user-1", ocAgo(time.Minute), `{"role":"user","time":{"created":1}}`),
		ocSessionSQL("empty-1", "p1", "", "New session - x", "/srv/app", ocAgo(time.Minute), ocAgo(time.Minute)),
		// Outside the window.
		ocSessionSQL("old-1", "p1", "", "ancient", "/srv/app", ocAgo(48*time.Hour), ocAgo(48*time.Hour)),
	)
	w := NewWatcher(24)
	w.Tick()
	byID := map[string]Session{}
	for _, s := range w.Sessions() {
		byID[s.ID] = s
	}
	if len(byID) != 7 || w.Count() != 7 {
		t.Fatalf("rows inside the window: %+v", byID)
	}
	run := byID["run-1"]
	if run.Agent != "opencode" || run.Platform != "cli" || run.Status != "tool" || run.Activity != "Bash: ls -la" || run.Prompt != "list files" ||
		run.Title != "" || run.Model != "opencode/big-pickle" || run.Cwd != "/srv/app" || run.Workspace != "/srv/app" ||
		run.SessionID != "run-1" || run.TurnStartedAt != turn || run.StartedAt == 0 || run.LastEventAt < turn || run.Subagent {
		t.Errorf("running row: %+v", run)
	}
	done := byID["done-1"]
	if done.Status != "done" || done.Reply != "Done, tests pass." || done.Prompt != "fix it" || done.Title != "Fix the build" || done.Workspace != "/tmp/x" {
		t.Errorf("finished row: %+v", done)
	}
	sub := byID["sub-1"]
	if !sub.Subagent || sub.ParentID != "run-1" || sub.Status != "typing" || sub.Reply != "Looking around." || sub.Title != "Explore repo (@explore subagent)" {
		t.Errorf("subagent row: %+v", sub)
	}
	if e := byID["err-1"]; e.Status != "error" || e.Error != "This API key has expired." {
		t.Errorf("error row: %+v", e)
	}
	if a := byID["abort-1"]; a.Status != "interrupted" {
		t.Errorf("aborted row: %+v", a)
	}
	if u := byID["user-1"]; u.Status != "thinking" {
		t.Errorf("prompt-only row: %+v", u)
	}
	if e := byID["empty-1"]; e.Status != "idle" {
		t.Errorf("empty row: %+v", e)
	}

	// Unchanged db: no re-read, no signal.
	for len(w.Changes()) > 0 {
		<-w.Changes()
	}
	w.Tick()
	if len(w.Changes()) != 0 {
		t.Error("idle tick must not signal")
	}
}

func TestOpenCodeMissingSqliteDisablesOnlyThatSource(t *testing.T) {
	ocFixture(t, ocSessionSQL("x", "p1", "", "t", "/d", ocAgo(0), ocAgo(0)))
	old := sqliteBinary
	sqliteBinary = filepath.Join(t.TempDir(), "no-such-sqlite3")
	t.Cleanup(func() { sqliteBinary = old })
	w := NewWatcher(24)
	w.Tick()
	if w.Count() != 0 || !w.oc.unavailable {
		t.Fatalf("missing sqlite3 must disable the source: count=%d unavailable=%v", w.Count(), w.oc.unavailable)
	}
}

func TestOpenCodeHelpers(t *testing.T) {
	if got := ocModel(`{"id":"big-pickle","providerID":"opencode","variant":"default"}`); got != "opencode/big-pickle" {
		t.Errorf("model json: %q", got)
	}
	if got := ocModel("anthropic/claude-sonnet-5-5"); got != "anthropic/claude-sonnet-5-5" {
		t.Errorf("plain model: %q", got)
	}
	if ocModel("") != "" {
		t.Error("empty model")
	}
	cases := map[string]string{
		`bash|{"command":"go test ./..."}`:         "Bash: go test ./...",
		`read|{"filePath":"/a/b/index.js"}`:        "Read index.js",
		`grep|{"pattern":"retry"}`:                 "Grep retry",
		`webfetch|{"url":"https://example.com/x"}`: "WebFetch example.com",
		`task|{"description":"Find the config"}`:   "Subagent: Find the config",
		`skill|{"name":"myoffice"}`:                "Skill myoffice",
		`todowrite|{}`:                             "Updating todo list",
		`github_create_pull_request|{"title":"x"}`: "github_create_pull_request",
	}
	for in, want := range cases {
		name, raw, _ := cut3(in)
		if got := describeOCTool(name, ocJSON(raw)); got != want {
			t.Errorf("describeOCTool(%s) = %q, want %q", in, got, want)
		}
	}
	s := ocSession(ocRow{ID: "s", Title: "New session - 2026-10-26T04:21:31.288Z", Directory: "/d", Worktree: "/"})
	if s.Title != "" || s.Workspace != "/d" || s.Status != "idle" {
		t.Errorf("untitled global session: %+v", s)
	}
}

// cut3 splits "name|json" for the table above.
func cut3(s string) (string, string, bool) {
	for i := 0; i < len(s); i++ {
		if s[i] == '|' {
			return s[:i], s[i+1:], true
		}
	}
	return s, "", false
}
