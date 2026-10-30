package main

import (
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"testing"
	"time"
)

// agyFixture builds a conversation_summaries.db with the sqlite3 CLI (the
// same tool the watcher reads with) and points the watcher at it.
func agyFixture(t *testing.T, rows ...string) string {
	t.Helper()
	if _, err := exec.LookPath("sqlite3"); err != nil {
		t.Skip("sqlite3 not installed")
	}
	setLogDirs(t)
	db := agyDB
	sql := `create table conversation_summaries (conversation_id text primary key, title text not null default "", preview text not null default "", step_count integer not null default 0, last_modified_time datetime not null, workspace_uris text not null, status text not null default "", source text not null default "", parent_conversation_id text not null default "", nesting_depth integer not null default 0, not_fully_idle numeric not null default false, killed numeric not null default false, last_user_input_time datetime not null);`
	for _, r := range rows {
		sql += r
	}
	if err := os.MkdirAll(filepath.Dir(db), 0o755); err != nil {
		t.Fatal(err)
	}
	if out, err := exec.Command("sqlite3", db, sql).CombinedOutput(); err != nil {
		t.Fatalf("sqlite3: %v\n%s", err, out)
	}
	return db
}

func agyStamp(offset time.Duration) string {
	return time.Now().UTC().Add(offset).Format("2006-01-02 15:04:05.000000+00:00")
}

func TestAgyRowsBecomeGeminiSessions(t *testing.T) {
	agyFixture(t,
		`insert into conversation_summaries values ('cli-1','','用一句話摘要以下任務',3,'`+agyStamp(-time.Minute)+`','["file:///Users/dev/%E5%B0%88%E6%A1%88/app"]','CASCADE_RUN_STATUS_IDLE','CORTEX_TRAJECTORY_SOURCE_CLI','',0,0,0,'`+agyStamp(-2*time.Minute)+`');`,
		`insert into conversation_summaries values ('ide-1','Add quotes','Add quotes',164,'`+agyStamp(-30*time.Second)+`','[]','CASCADE_RUN_STATUS_RUNNING','','',0,0,0,'`+agyStamp(-time.Minute)+`');`,
		`insert into conversation_summaries values ('sub-1','','child',2,'`+agyStamp(-20*time.Second)+`','[]','CASCADE_RUN_STATUS_IDLE','','ide-1',1,0,1,'`+agyStamp(-time.Minute)+`');`,
		`insert into conversation_summaries values ('old-1','','ancient',5,'`+agyStamp(-48*time.Hour)+`','[]','CASCADE_RUN_STATUS_IDLE','CORTEX_TRAJECTORY_SOURCE_CLI','',0,0,0,'`+agyStamp(-48*time.Hour)+`');`,
	)
	w := NewWatcher(24)
	w.Tick()
	byID := map[string]Session{}
	for _, s := range w.Sessions() {
		byID[s.ID] = s
	}
	if len(byID) != 3 || w.Count() != 3 {
		t.Fatalf("rows inside the window: %+v", byID)
	}
	cli := byID["cli-1"]
	if cli.Agent != "gemini" || cli.Tool != "agy" || cli.Platform != "cli" || cli.Status != "done" || cli.Prompt != "用一句話摘要以下任務" ||
		cli.Cwd != "/Users/dev/專案/app" || cli.Workspace != cli.Cwd || cli.SessionID != "cli-1" || cli.LastEventAt == 0 || cli.TurnStartedAt == 0 {
		t.Errorf("cli row: %+v", cli)
	}
	ide := byID["ide-1"]
	if ide.Platform != "desktop" || ide.Status != "thinking" || ide.Title != "Add quotes" || ide.Cwd != "" {
		t.Errorf("ide row: %+v", ide)
	}
	sub := byID["sub-1"]
	if !sub.Subagent || sub.ParentID != "ide-1" || sub.Status != "interrupted" {
		t.Errorf("nested killed row: %+v", sub)
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

func TestAgyUnreadableDbIsSkippedOnce(t *testing.T) {
	db := agyFixture(t, `insert into conversation_summaries values ('x','','p',1,'`+agyStamp(0)+`','[]','CASCADE_RUN_STATUS_IDLE','','',0,0,0,'`+agyStamp(0)+`');`)
	old := sqliteBinary
	sqliteBinary = filepath.Join(t.TempDir(), "no-such-sqlite3")
	t.Cleanup(func() { sqliteBinary = old })
	w := NewWatcher(24)
	w.Tick()
	if w.Count() != 0 || !w.agy.unavailable {
		t.Fatalf("missing sqlite3 must disable the source, db %s: count=%d unavailable=%v", db, w.Count(), w.agy.unavailable)
	}
}

func TestAgyReadErrorIsRetriedLater(t *testing.T) {
	db := agyFixture(t)
	writeFile(t, db, "not a database")
	w := NewWatcher(24)
	w.Tick()
	if w.agy.unavailable || w.agy.retryAt == 0 {
		t.Fatalf("a failed read must back off, not switch the source off: unavailable=%v retryAt=%d", w.agy.unavailable, w.agy.retryAt)
	}
	w.agy.retryAt = 0
	// Fixed in the meantime: the next tick reads it.
	if err := os.Remove(db); err != nil {
		t.Fatal(err)
	}
	if out, err := exec.Command("sqlite3", db, `create table conversation_summaries (conversation_id text primary key, title text not null default "", preview text not null default "", step_count integer not null default 0, last_modified_time datetime not null, workspace_uris text not null, status text not null default "", source text not null default "", parent_conversation_id text not null default "", nesting_depth integer not null default 0, not_fully_idle numeric not null default false, killed numeric not null default false, last_user_input_time datetime not null); insert into conversation_summaries values ('x','','p',1,'`+agyStamp(0)+`','[]','CASCADE_RUN_STATUS_IDLE','','',0,0,0,'`+agyStamp(0)+`');`).CombinedOutput(); err != nil {
		t.Fatalf("sqlite3: %v\n%s", err, out)
	}
	w.Tick()
	if w.Count() != 1 {
		t.Fatalf("after the db became readable: count=%d", w.Count())
	}
}

func TestAgyHelpers(t *testing.T) {
	if got := agyTime("2026-10-26 04:00:37.785627+00:00"); got != 1792987237785 {
		t.Errorf("agyTime with fraction = %d", got)
	}
	if got := agyTime("2026-10-26 04:00:37+00:00"); got != 1792987237000 {
		t.Errorf("agyTime without fraction = %d", got)
	}
	if agyTime("nope") != 0 {
		t.Error("garbage must parse to 0")
	}
	if got := agyWorkspace(`["file:///private/var/folders/T","file:///b"]`); got != "/private/var/folders/T" {
		t.Errorf("first uri: %q", got)
	}
	if agyWorkspace("") != "" || agyWorkspace("[]") != "" || agyWorkspace(`["http://x"]`) != "" {
		t.Error("non file uris must be empty")
	}
	if got := agyStatus(agyRow{Status: "CASCADE_RUN_STATUS_IDLE", StepCount: 0}); got != "idle" {
		t.Errorf("no steps yet: %q", got)
	}
	if got := agyStatus(agyRow{Status: "CASCADE_RUN_STATUS_ERROR", StepCount: 3}); got != "error" {
		t.Errorf("error: %q", got)
	}
	if got := agyStatus(agyRow{Status: "CASCADE_RUN_STATUS_IDLE", StepCount: 3, NotFullyIdle: 1}); got != "thinking" {
		t.Errorf("not fully idle: %q", got)
	}
}

// agyLog writes a CLI log the way `agy -p` starts one; conv "" leaves the
// conversation line out.
func agyLog(t *testing.T, started time.Time, pid int, conv string) {
	t.Helper()
	body := "I1030 23:31:39.631904      27 server.go:1897] Starting language server process with pid " + strconv.Itoa(pid) + "\n"
	if conv != "" {
		body += "I1030 23:31:42.763408       1 session.go:201] Print mode: conversation=" + conv + ", sending message\n"
	}
	writeFile(t, filepath.Join(agyLogDir, "cli-"+started.Format("20060102_150405")+".log"), body)
}

// fakeAgyAlive stands in for ps: the pids in alive are agy processes, err
// makes the lookup fail.
func fakeAgyAlive(t *testing.T, alive map[int]bool, err *error) {
	t.Helper()
	old := agyAlive
	agyAlive = func([]int) (map[int]bool, error) { return alive, *err }
	t.Cleanup(func() { agyAlive = old })
}

func TestAgyPrintRunIsWorkingOnlyWhileItsProcessLives(t *testing.T) {
	const conv = "78c9afda-69f7-4902-8a95-dd2da40c5499"
	agyFixture(t, `insert into conversation_summaries values ('`+conv+`','','翻譯',2,'`+agyStamp(-time.Minute)+`','[]','CASCADE_RUN_STATUS_IDLE','CORTEX_TRAJECTORY_SOURCE_CLI','',0,0,0,'`+agyStamp(-2*time.Minute)+`');`)
	agyLog(t, time.Now().Add(-2*time.Minute), 4242, conv)
	alive := map[int]bool{4242: true}
	var psErr error
	fakeAgyAlive(t, alive, &psErr)

	w := NewWatcher(24)
	status := func() string {
		for _, s := range w.Sessions() {
			if s.ID == conv {
				return s.Status
			}
		}
		return "missing"
	}
	drain := func() {
		for len(w.Changes()) > 0 {
			<-w.Changes()
		}
	}

	w.Tick()
	if got := status(); got != "thinking" {
		t.Fatalf("row of a running print process: %q", got)
	}

	// ps failing: not live this tick, but not written off either.
	drain()
	psErr = errors.New("ps: boom")
	w.Tick()
	if got := status(); got != "done" || len(w.Changes()) == 0 {
		t.Fatalf("ps failure must fall back to the row's status: %q", got)
	}
	psErr = nil
	w.Tick()
	if got := status(); got != "thinking" {
		t.Fatalf("after ps recovers: %q", got)
	}

	// Process gone: back to the row's status, and it stays there even if the
	// pid shows up again (handed to a later process).
	drain()
	delete(alive, 4242)
	w.Tick()
	if got := status(); got != "done" || len(w.Changes()) == 0 {
		t.Fatalf("after the process ended: %q", got)
	}
	alive[4242] = true
	w.Tick()
	if got := status(); got != "done" {
		t.Fatalf("a dead run must not come back: %q", got)
	}
}

func TestAgyRunLogsThatNeverNameAConversationAreDropped(t *testing.T) {
	setLogDirs(t)
	agyLog(t, time.Now().Add(-5*time.Minute), 7, "")
	agyLog(t, time.Now().Add(-10*time.Second), 8, "")
	calls := 0
	old := agyAlive
	agyAlive = func([]int) (map[int]bool, error) { calls++; return map[int]bool{7: true, 8: true}, nil }
	t.Cleanup(func() { agyAlive = old })

	a := newAgyRuns()
	a.load(time.Now().UnixMilli(), time.Now().Add(-24*time.Hour).UnixMilli())
	if len(a.live) != 0 || calls != 0 {
		t.Fatalf("no conversation, no live run and no ps: live=%v calls=%d", a.live, calls)
	}
	var stale, fresh *agyRun
	for _, r := range a.logs {
		if r.pid == 7 {
			stale = r
		} else {
			fresh = r
		}
	}
	if stale == nil || !stale.dead || fresh == nil || fresh.dead {
		t.Fatalf("past the wait the log is given up, before it is kept: stale=%+v fresh=%+v", stale, fresh)
	}
}

func TestAgyLogStart(t *testing.T) {
	want := time.Date(2026, 10, 30, 23, 31, 39, 0, time.Local).UnixMilli()
	if got := agyLogStart("cli-20261030_233139.log"); got != want {
		t.Errorf("agyLogStart = %d, want %d", got, want)
	}
	if agyLogStart("hub.log") != 0 || agyLogStart("cli-nope.log") != 0 {
		t.Error("other names must parse to 0")
	}
}
