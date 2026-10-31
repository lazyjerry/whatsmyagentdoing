package main

import (
	"encoding/json"
	"net/http/httptest"
	"path/filepath"
	"testing"
	"time"
)

// at sets a fixture record's timestamp to now+offset.
func at(rec map[string]any, offset time.Duration) map[string]any {
	rec["timestamp"] = ts(offset)
	return rec
}

func claudeAPIError(offset time.Duration) map[string]any {
	return map[string]any{"type": "system", "subtype": "api_error", "timestamp": ts(offset), "error": map[string]any{"message": "overloaded"}}
}

func window(t *testing.T, s Stats, key string) StatWindow {
	t.Helper()
	for _, w := range s.Windows {
		if w.Key == key {
			return w
		}
	}
	t.Fatalf("no %q window in %+v", key, s.Windows)
	return StatWindow{}
}

func TestStatsCountsTurnsAndTools(t *testing.T) {
	claude, codex, _ := setLogDirs(t)
	main := filepath.Join(claude, "proj", "s1.jsonl")
	writeFile(t, main, lines(
		// 6 hours ago: done, two tool calls in one message
		jsonLine(t, at(claudeUser("old"), -6*time.Hour)),
		jsonLine(t, map[string]any{"type": "assistant", "timestamp": ts(-6*time.Hour + time.Second), "message": map[string]any{
			"stop_reason": "tool_use", "content": []any{
				map[string]any{"type": "tool_use", "name": "Read", "input": map[string]any{}},
				map[string]any{"type": "tool_use", "name": "Grep", "input": map[string]any{}},
			}}}),
		jsonLine(t, at(claudeToolResult(), -6*time.Hour+2*time.Second)),
		jsonLine(t, at(claudeText("done", "end_turn"), -6*time.Hour+3*time.Second)),
		// 2 hours ago: an API error
		jsonLine(t, at(claudeUser("broken"), -2*time.Hour)),
		jsonLine(t, claudeAPIError(-2*time.Hour+time.Second)),
		// now: still running a tool
		jsonLine(t, at(claudeUser("now"), -time.Minute)),
		jsonLine(t, at(claudeToolUse("Bash", map[string]any{"command": "ls"}), -30*time.Second)),
	))
	// A subagent's tool calls count; its turns don't.
	writeFile(t, filepath.Join(claude, "proj", "s1", "subagents", "agent-a.jsonl"), lines(
		jsonLine(t, at(claudeUser("sub task"), -time.Hour)),
		jsonLine(t, at(claudeToolUse("Read", map[string]any{}), -time.Hour+time.Second)),
		jsonLine(t, at(claudeText("ok", "end_turn"), -time.Hour+2*time.Second)),
	))
	day := time.Now().Format("2006/01/02")
	writeFile(t, filepath.Join(codex, "sessions", day, "rollout-x-11111111-2222-3333-4444-555555555555.jsonl"), lines(
		jsonLine(t, map[string]any{"type": "event_msg", "timestamp": ts(-3 * time.Hour), "payload": map[string]any{"type": "task_started"}}),
		jsonLine(t, map[string]any{"type": "response_item", "timestamp": ts(-3*time.Hour + time.Second), "payload": map[string]any{"type": "function_call", "name": "shell", "arguments": "{}"}}),
		jsonLine(t, map[string]any{"type": "event_msg", "timestamp": ts(-3*time.Hour + 2*time.Second), "payload": map[string]any{"type": "turn_aborted"}}),
	))

	sc := newStatsScanner()
	got := sc.compute(time.Now())
	h5, h24 := window(t, got, "5h"), window(t, got, "24h")
	want5 := StatWindow{Key: "5h", Since: h5.Since, Turns: 3, Error: 1, Interrupted: 1, Running: 1, Tools: 3}
	if h5 != want5 {
		t.Errorf("5h = %+v, want %+v", h5, want5)
	}
	want24 := StatWindow{Key: "24h", Since: h24.Since, Turns: 4, Done: 1, Error: 1, Interrupted: 1, Running: 1, Tools: 5}
	if h24 != want24 {
		t.Errorf("24h = %+v, want %+v", h24, want24)
	}

	// Cached for StatsCacheFor; afterwards only the appended bytes are read.
	appendFile(t, main, lines(jsonLine(t, at(claudeText("all good", "end_turn"), -time.Second))))
	if again := window(t, sc.compute(time.Now()), "5h"); again.Running != 1 {
		t.Errorf("cached result changed: %+v", again)
	}
	sc.at = 0
	h5 = window(t, sc.compute(time.Now()), "5h")
	if h5.Running != 0 || h5.Done != 1 || h5.Turns != 3 {
		t.Errorf("after the reply: %+v", h5)
	}
}

func TestStatsGeminiToolCallsCountOnce(t *testing.T) {
	_, _, gemini := setLogDirs(t)
	call := func(status string) map[string]any {
		return map[string]any{"type": "gemini", "timestamp": ts(-time.Minute), "toolCalls": []any{map[string]any{"id": "c1", "name": "ls", "status": status}}}
	}
	writeFile(t, filepath.Join(gemini, "tmp", "p", "chats", "session-1.jsonl"), lines(
		jsonLine(t, map[string]any{"type": "user", "timestamp": ts(-2 * time.Minute), "content": "list files"}),
		jsonLine(t, call("executing")),
		jsonLine(t, call("success")),
	))
	if w := window(t, newStatsScanner().compute(time.Now()), "5h"); w.Tools != 1 || w.Turns != 1 {
		t.Errorf("gemini = %+v, want 1 tool call and 1 turn", w)
	}
}

func TestWeekStartsMonday(t *testing.T) {
	loc := time.FixedZone("UTC+8", 8*3600)
	for _, c := range []struct{ now, want string }{
		{"2026-10-31T15:00:00", "2026-10-26T00:00:00"}, // Saturday
		{"2026-10-26T00:30:00", "2026-10-26T00:00:00"}, // Monday
		{"2026-11-01T23:59:00", "2026-10-26T00:00:00"}, // Sunday
	} {
		now, _ := time.ParseInLocation("2006-01-02T15:04:05", c.now, loc)
		if got := weekStart(now).Format("2006-01-02T15:04:05"); got != c.want {
			t.Errorf("weekStart(%s) = %s, want %s", c.now, got, c.want)
		}
	}
}

func TestStatsEndpointDemo(t *testing.T) {
	noNetwork(t)
	srv := httptest.NewServer(NewServer(newFakeSource(), Options{Demo: true}, NewUpdater(), nil, nil))
	defer srv.Close()
	res, err := srv.Client().Get(srv.URL + "/api/stats")
	if err != nil {
		t.Fatal(err)
	}
	defer res.Body.Close()
	var s Stats
	if err := json.NewDecoder(res.Body).Decode(&s); err != nil {
		t.Fatal(err)
	}
	if len(s.Windows) != 3 || s.Windows[2].Key != "week" || s.Windows[2].Tools == 0 {
		t.Errorf("demo stats = %+v", s)
	}
}
