package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// --- line feeding

func TestFeedKeepsPartialLines(t *testing.T) {
	var got []string
	tr := &tracker{session: newSession("claude", "x", "f"), apply: func(_ *Session, v any) {
		got = append(got, asString(asMap(v)["k"]))
	}}
	feed(tr, `{"k":"a"}`+"\n"+`{"k":"b"`, false, false)
	if len(got) != 1 || got[0] != "a" || tr.partial != `{"k":"b"` {
		t.Fatalf("after chunk 1: got=%v partial=%q", got, tr.partial)
	}
	feed(tr, `}`+"\n"+`{"k":"c"}`+"\n", false, false)
	if len(got) != 3 || got[1] != "b" || got[2] != "c" || tr.partial != "" {
		t.Fatalf("after chunk 2: got=%v partial=%q", got, tr.partial)
	}
	feed(tr, "not json\n\n"+`{"k":"d"}`+"\n", false, false)
	if len(got) != 4 || got[3] != "d" {
		t.Fatalf("bad and blank lines are skipped: %v", got)
	}
}

func TestFeedDropFlags(t *testing.T) {
	var got []string
	tr := &tracker{session: newSession("claude", "x", "f"), apply: func(_ *Session, v any) {
		got = append(got, asString(asMap(v)["k"]))
	}}
	// tail of a big file: the first line is a torn one and must be dropped
	feed(tr, `"k":"torn"}`+"\n"+`{"k":"ok"}`+"\n", false, true)
	if len(got) != 1 || got[0] != "ok" {
		t.Fatalf("dropFirst: %v", got)
	}
	// head of a big file: whatever is left over is not a real line
	feed(tr, `{"k":"h"}`+"\n"+`{"k":"cut`, true, false)
	if len(got) != 2 || tr.partial != "" {
		t.Fatalf("dropLastPartial: got=%v partial=%q", got, tr.partial)
	}
}

func TestReadRange(t *testing.T) {
	f := filepath.Join(t.TempDir(), "x")
	writeFile(t, f, "0123456789")
	if s, err := readRange(f, 2, 5); err != nil || s != "234" {
		t.Errorf("middle: %q %v", s, err)
	}
	if s, err := readRange(f, 8, 20); err != nil || s != "89" {
		t.Errorf("past end returns what exists: %q %v", s, err)
	}
	if s, err := readRange(f, 5, 5); err != nil || s != "" {
		t.Errorf("empty range: %q %v", s, err)
	}
	if _, err := readRange(filepath.Join(t.TempDir(), "missing"), 0, 1); err == nil {
		t.Error("missing file must error")
	}
}

// --- directory walks

func TestClaudeFiles(t *testing.T) {
	claude, _, _ := setLogDirs(t)
	writeFile(t, filepath.Join(claude, "-proj", "sess1.jsonl"), "")
	writeFile(t, filepath.Join(claude, "-proj", "sess1", "subagents", "agent-a.jsonl"), "")
	writeFile(t, filepath.Join(claude, "-proj", "notes.txt"), "")
	writeFile(t, filepath.Join(claude, "stray.jsonl"), "") // files directly under projects/ are not sessions
	got := claudeFiles()
	if len(got) != 2 {
		t.Fatalf("want 2 candidates, got %+v", got)
	}
	byID := map[string]candidate{}
	for _, c := range got {
		byID[c.id] = c
	}
	if byID["sess1"].parentID != "" || byID["agent-a"].parentID != "sess1" {
		t.Errorf("subagent parent: %+v", byID)
	}
}

func TestCodexFilesOnlyWalksWindow(t *testing.T) {
	_, codex, _ := setLogDirs(t)
	now := time.Now()
	today := filepath.Join(codex, "sessions", now.Format("2006"), now.Format("01"), now.Format("02"))
	writeFile(t, filepath.Join(today, "rollout-2026-10-26T01-00-00-0f1e2d3c-4b5a-6978-8a9b-0c1d2e3f4a5b.jsonl"), "")
	writeFile(t, filepath.Join(today, "rollout-noid.jsonl"), "")
	writeFile(t, filepath.Join(codex, "sessions", "2000", "01", "01", "rollout-old.jsonl"), "")
	got := codexFiles(now.Add(-time.Hour).UnixMilli())
	if len(got) != 2 {
		t.Fatalf("old day must be skipped, got %+v", got)
	}
	ids := map[string]bool{}
	for _, c := range got {
		ids[c.id] = true
	}
	if !ids["0f1e2d3c-4b5a-6978-8a9b-0c1d2e3f4a5b"] || !ids["rollout-noid.jsonl"] {
		t.Errorf("uuid extracted, otherwise file name: %v", ids)
	}
}

func TestGeminiFiles(t *testing.T) {
	_, _, gemini := setLogDirs(t)
	chats := filepath.Join(gemini, "tmp", "slug1", "chats")
	writeFile(t, filepath.Join(chats, "session-2026-10-26-abc.jsonl"), "")
	writeFile(t, filepath.Join(chats, "session-legacy.json"), "")
	writeFile(t, filepath.Join(chats, "parent-1", "child-1.jsonl"), "")
	writeFile(t, filepath.Join(chats, "other.jsonl"), "") // not a session-* file
	got := geminiFiles()
	if len(got) != 3 {
		t.Fatalf("want 3, got %+v", got)
	}
	byID := map[string]candidate{}
	for _, c := range got {
		byID[c.id] = c
		if c.project != "slug1" {
			t.Errorf("project slug: %+v", c)
		}
	}
	if _, ok := byID["session-legacy"]; !ok {
		t.Errorf("legacy id must strip .json: %v", byID)
	}
	if byID["child-1"].parentID != "parent-1" || byID["session-2026-10-26-abc"].parentID != "" {
		t.Errorf("subagent parent: %+v", byID)
	}
}

// --- Watcher end to end (no goroutine: Tick is called by hand)

func claudeLog(t *testing.T, claude, id string, records ...map[string]any) string {
	t.Helper()
	var ls []string
	for _, r := range records {
		ls = append(ls, jsonLine(t, r))
	}
	p := filepath.Join(claude, "-home-dev-proj", id+".jsonl")
	writeFile(t, p, lines(ls...))
	return p
}

func TestWatcherTracksAndReadsIncrements(t *testing.T) {
	claude, _, _ := setLogDirs(t)
	p := claudeLog(t, claude, "s1", claudeUser("Fix the bug"))
	w := NewWatcher(24)
	w.Tick()
	select {
	case <-w.Changes():
	default:
		t.Fatal("first tick must signal a change")
	}
	got := w.Sessions()
	if len(got) != 1 || got[0].Status != "thinking" || got[0].Prompt != "Fix the bug" || got[0].Cwd != "/home/dev/proj" || got[0].ID != "s1" {
		t.Fatalf("after first tick: %+v", got)
	}
	if w.Count() != 1 {
		t.Errorf("Count = %d", w.Count())
	}

	appendFile(t, p, jsonLine(t, claudeToolUse("Bash", map[string]any{"command": "go test"}))+"\n")
	w.Tick()
	got = w.Sessions()
	if got[0].Status != "tool" || got[0].Activity != "Bash: go test" {
		t.Fatalf("increment not applied: %+v", got[0])
	}

	// Nothing changed: no signal.
	for len(w.Changes()) > 0 {
		<-w.Changes()
	}
	w.Tick()
	if len(w.Changes()) != 0 {
		t.Error("idle tick must not signal")
	}

	// Rewritten shorter: tracker restarts next tick.
	writeFile(t, p, lines(jsonLine(t, claudeUser("Second"))))
	w.Tick()
	w.Tick()
	got = w.Sessions()
	if len(got) != 1 || got[0].Prompt != "Second" {
		t.Fatalf("after rewrite: %+v", got)
	}
}

func TestWatcherSubagentsAndWindow(t *testing.T) {
	claude, _, _ := setLogDirs(t)
	claudeLog(t, claude, "main", claudeUser("Main prompt"))
	sub := filepath.Join(claude, "-home-dev-proj", "main", "subagents", "agent-x.jsonl")
	writeFile(t, sub, lines(jsonLine(t, claudeUser("Sub prompt"))))
	old := claudeLog(t, claude, "old", claudeUser("Ancient"))
	stale := time.Now().Add(-48 * time.Hour)
	if err := os.Chtimes(old, stale, stale); err != nil {
		t.Fatal(err)
	}
	w := NewWatcher(24)
	w.Tick()
	got := w.Sessions()
	if len(got) != 2 {
		t.Fatalf("stale file must be ignored, got %+v", got)
	}
	for _, s := range got {
		if s.ID == "agent-x" && (!s.Subagent || s.ParentID != "main") {
			t.Errorf("subagent: %+v", s)
		}
		if s.ID == "main" && s.Subagent {
			t.Errorf("main is not a subagent: %+v", s)
		}
	}
}

func TestWatcherDropsSessionsThatAgeOut(t *testing.T) {
	claude, _, _ := setLogDirs(t)
	rec := claudeUser("Soon stale")
	rec["timestamp"] = ts(-90 * time.Minute)
	claudeLog(t, claude, "s", rec)
	w := NewWatcher(2)
	w.Tick()
	if w.Count() != 1 {
		t.Fatalf("inside 2h window: %d", w.Count())
	}
	w.windowMs = int64((time.Hour).Milliseconds())
	w.Tick()
	if w.Count() != 0 {
		t.Fatalf("outside 1h window the tracker is dropped: %d", w.Count())
	}
}

func TestWatcherBigFileReadsHeadAndTail(t *testing.T) {
	claude, _, _ := setLogDirs(t)
	var b strings.Builder
	b.WriteString(jsonLine(t, claudeUser("Head prompt")) + "\n")
	filler := strings.Repeat("x", 1000) + "\n"
	for b.Len() < headBytes+tailBytes+100_000 {
		b.WriteString(filler)
	}
	b.WriteString(jsonLine(t, claudeText("Tail reply", "end_turn")) + "\n")
	p := filepath.Join(claude, "-p", "big.jsonl")
	writeFile(t, p, b.String())

	w := NewWatcher(24)
	w.Tick()
	got := w.Sessions()
	if len(got) != 1 {
		t.Fatalf("got %+v", got)
	}
	s := got[0]
	if s.Cwd != "/home/dev/proj" || s.Prompt != "Head prompt" {
		t.Errorf("head metadata missing: %+v", s)
	}
	if s.Status != "done" || s.Reply != "Tail reply" {
		t.Errorf("tail state missing: %+v", s)
	}
}

func TestWatcherCodexNamesAndGeminiProjects(t *testing.T) {
	_, codex, gemini := setLogDirs(t)
	now := time.Now()
	uuid := "0f1e2d3c-4b5a-6978-8a9b-0c1d2e3f4a5b"
	day := filepath.Join(codex, "sessions", now.Format("2006"), now.Format("01"), now.Format("02"))
	writeFile(t, filepath.Join(day, "rollout-x-"+uuid+".jsonl"),
		lines(`{"type":"session_meta","timestamp":"`+ts(-time.Minute)+`","payload":{"cwd":"/w/app"}}`))
	writeFile(t, filepath.Join(codex, "session_index.jsonl"),
		lines(`{"id":"other","thread_name":"nope"}`, `{"id":"`+uuid+`","thread_name":"Billing retries"}`))

	writeFile(t, filepath.Join(gemini, "projects.json"), `{"projects":{"/real/project/path":"slug1"}}`)
	writeFile(t, filepath.Join(gemini, "tmp", "slug1", "chats", "session-a.jsonl"),
		lines(`{"sessionId":"g1"}`, `{"type":"user","timestamp":"`+ts(-2*time.Hour)+`","content":"hello there"}`))
	writeFile(t, filepath.Join(gemini, "tmp", "unmapped", "chats", "session-b.jsonl"),
		lines(`{"sessionId":"g2"}`, `{"type":"user","timestamp":"`+ts(-time.Minute)+`","content":"hi"}`))

	w := NewWatcher(24)
	w.Tick()
	byID := map[string]Session{}
	for _, s := range w.Sessions() {
		byID[s.ID] = s
	}
	if byID[uuid].Title != "Billing retries" || byID[uuid].Cwd != "/w/app" {
		t.Errorf("codex title from index: %+v", byID[uuid])
	}
	if byID["session-a"].Cwd != "/real/project/path" {
		t.Errorf("gemini cwd from projects.json: %+v", byID["session-a"])
	}
	if byID["session-b"].Cwd != "unmapped" {
		t.Errorf("unmapped gemini falls back to the slug: %+v", byID["session-b"])
	}
	// Gemini re-appends messages with old stamps; the file mtime is the real activity signal.
	if byID["session-a"].LastEventAt < time.Now().Add(-time.Minute).UnixMilli() {
		t.Errorf("gemini LastEventAt must be lifted to mtime: %d", byID["session-a"].LastEventAt)
	}
}

func TestWatcherLegacyGeminiDocumentRereadsOnChange(t *testing.T) {
	_, _, gemini := setLogDirs(t)
	p := filepath.Join(gemini, "tmp", "slug", "chats", "session-legacy.json")
	writeFile(t, p, `{"sessionId":"L","messages":[{"type":"user","timestamp":"`+ts(-time.Minute)+`","content":"first"}]}`)
	w := NewWatcher(24)
	w.Tick()
	got := w.Sessions()
	if len(got) != 1 || got[0].Prompt != "first" {
		t.Fatalf("legacy document: %+v", got)
	}
	writeFile(t, p, `{"sessionId":"L","messages":[{"type":"user","timestamp":"`+ts(-time.Minute)+`","content":"second"}]}`)
	future := time.Now().Add(2 * time.Second)
	if err := os.Chtimes(p, future, future); err != nil {
		t.Fatal(err)
	}
	w.Tick()
	if got := w.Sessions(); got[0].Prompt != "second" {
		t.Fatalf("whole-file session must be re-read when mtime changes: %+v", got)
	}
}

func TestWatcherUnreadableFileIsRetriedLater(t *testing.T) {
	if os.Getuid() == 0 {
		t.Skip("root can read anything")
	}
	claude, _, _ := setLogDirs(t)
	p := claudeLog(t, claude, "locked", claudeUser("x"))
	if err := os.Chmod(p, 0o000); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.Chmod(p, 0o644) })
	w := NewWatcher(24)
	w.Tick()
	if w.Count() != 0 {
		t.Fatalf("unreadable log must not be tracked: %d", w.Count())
	}
	w.mu.Lock()
	retry, ok := w.failed[p]
	w.mu.Unlock()
	if !ok || retry <= time.Now().UnixMilli() {
		t.Fatalf("must be scheduled for retry: %v %d", ok, retry)
	}
	os.Chmod(p, 0o644)
	w.Tick()
	if w.Count() != 0 {
		t.Fatal("still inside the retry back-off")
	}
	w.mu.Lock()
	w.failed[p] = 0
	w.mu.Unlock()
	w.Tick()
	if w.Count() != 1 {
		t.Fatal("after the back-off the file is picked up")
	}
}

func TestWatcherStartStop(t *testing.T) {
	setLogDirs(t)
	w := NewWatcher(1)
	w.Start()
	w.Stop()
	w.Stop() // idempotent
	if len(w.Sessions()) != 0 {
		t.Error("empty dirs give no sessions")
	}
}

func TestForceRescanWalksAgain(t *testing.T) {
	claude, _, _ := setLogDirs(t)
	w := NewWatcher(24)
	w.Tick()
	claudeLog(t, claude, "late", claudeUser("Appeared after the first walk"))
	w.Tick() // within 5s: known files only
	if w.Count() != 0 {
		t.Fatal("no rescan inside 5s")
	}
	w.ForceRescan()
	w.Tick()
	if w.Count() != 1 {
		t.Fatal("ForceRescan must pick the new file up")
	}
}
