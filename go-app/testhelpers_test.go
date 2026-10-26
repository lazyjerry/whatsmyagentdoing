package main

// Shared fixtures for the unit and functional tests. Log lines follow the
// shapes parsers.go reads (see jerry-report/structure.md §8); timestamps are
// relative to now so the 24h window never ages a fixture out.

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// fakeSource is a Source with a fixed session list, for server tests.
type fakeSource struct {
	sessions []Session
	changes  chan struct{}
	started  bool
	stopped  bool
}

func newFakeSource(sessions ...Session) *fakeSource {
	return &fakeSource{sessions: sessions, changes: make(chan struct{}, 1)}
}

func (f *fakeSource) Sessions() []Session      { return append([]Session(nil), f.sessions...) }
func (f *fakeSource) Changes() <-chan struct{} { return f.changes }
func (f *fakeSource) Start()                   { f.started = true }
func (f *fakeSource) Stop()                    { f.stopped = true }
func (f *fakeSource) set(sessions ...Session)  { f.sessions = sessions }
func (f *fakeSource) signal()                  { f.changes <- struct{}{} }

// ts returns an RFC 3339 timestamp offset from now (negative = past).
func ts(offset time.Duration) string {
	return time.Now().UTC().Add(offset).Format("2006-01-02T15:04:05.000Z")
}

// parseJSON turns one log line into the `any` the apply functions take.
func parseJSON(t *testing.T, line string) any {
	t.Helper()
	var v any
	if err := json.Unmarshal([]byte(line), &v); err != nil {
		t.Fatalf("fixture is not JSON: %v\n%s", err, line)
	}
	return v
}

// jsonLine marshals a fixture map into one log line (no trailing newline).
func jsonLine(t *testing.T, v any) string {
	t.Helper()
	b, err := json.Marshal(v)
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

// --- Claude Code fixtures

func claudeUser(text string) map[string]any {
	return map[string]any{
		"type": "user", "timestamp": ts(-time.Minute), "cwd": "/home/dev/proj",
		"message": map[string]any{"role": "user", "content": []any{map[string]any{"type": "text", "text": text}}},
	}
}

func claudeToolUse(name string, input map[string]any) map[string]any {
	return map[string]any{
		"type": "assistant", "timestamp": ts(-30 * time.Second),
		"message": map[string]any{
			"model": "claude-opus-5-5", "stop_reason": "tool_use",
			"content": []any{map[string]any{"type": "tool_use", "name": name, "input": input}},
		},
	}
}

func claudeToolResult() map[string]any {
	return map[string]any{
		"type": "user", "timestamp": ts(-20 * time.Second),
		"message": map[string]any{"role": "user", "content": []any{map[string]any{"type": "tool_result", "content": "ok"}}},
	}
}

func claudeText(text, stopReason string) map[string]any {
	return map[string]any{
		"type": "assistant", "timestamp": ts(-10 * time.Second),
		"message": map[string]any{
			"model": "claude-opus-5-5", "stop_reason": stopReason,
			"content": []any{map[string]any{"type": "text", "text": text}},
		},
	}
}

// --- files

func writeFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

func appendFile(t *testing.T, path, content string) {
	t.Helper()
	f, err := os.OpenFile(path, os.O_APPEND|os.O_WRONLY, 0o644)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	if _, err := f.WriteString(content); err != nil {
		t.Fatal(err)
	}
}

func lines(ls ...string) string { return strings.Join(ls, "\n") + "\n" }

// setLogDirs points the watcher at temp directories for the duration of a
// test and restores the real ones afterwards. Returns the three roots.
func setLogDirs(t *testing.T) (claude, codex, gemini string) {
	t.Helper()
	base := t.TempDir()
	claude = filepath.Join(base, "claude", "projects")
	codex = filepath.Join(base, "codex")
	gemini = filepath.Join(base, "gemini", ".gemini")
	for _, d := range []string{claude, filepath.Join(codex, "sessions"), filepath.Join(gemini, "tmp")} {
		if err := os.MkdirAll(d, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	oldClaude, oldCodexDir, oldCodexIndex, oldGemini, oldGeminiDir, oldAgy := claudeDir, codexDir, codexIndex, geminiHomes, geminiDir, agyDB
	claudeDir = claude
	codexDir = filepath.Join(codex, "sessions")
	codexIndex = filepath.Join(codex, "session_index.jsonl")
	geminiHomes = []string{gemini}
	geminiDir = filepath.Join(gemini, "tmp")
	agyDB = filepath.Join(gemini, "antigravity", "conversation_summaries.db")
	t.Cleanup(func() {
		claudeDir, codexDir, codexIndex, geminiHomes, geminiDir, agyDB = oldClaude, oldCodexDir, oldCodexIndex, oldGemini, oldGeminiDir, oldAgy
	})
	return claude, codex, gemini
}

// setSceneDir points scene storage at a temp directory.
func setSceneDir(t *testing.T) string {
	t.Helper()
	dir := filepath.Join(t.TempDir(), "scenes")
	old := sceneUserDir
	sceneUserDir = dir
	t.Cleanup(func() { sceneUserDir = old })
	return dir
}

// noNetwork keeps the version check and scene download from reaching out.
func noNetwork(t *testing.T) {
	t.Helper()
	t.Setenv("WMAD_NO_UPDATE_CHECK", "1")
	t.Setenv("WMAD_LATEST_VERSION", "")
	t.Setenv("WMAD_SCENES_LOCAL", "")
}
