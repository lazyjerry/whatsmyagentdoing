package main

import (
	"os"
	"os/exec"
	"path/filepath"
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
