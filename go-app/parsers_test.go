package main

import (
	"encoding/json"
	"strings"
	"testing"
	"time"
)

// --- helpers

func TestAsString(t *testing.T) {
	cases := []struct {
		in   any
		want string
	}{
		{"x", "x"}, {nil, ""}, {float64(42), "42"}, {1.5, "1.5"}, {true, "true"}, {[]any{}, ""},
	}
	for _, c := range cases {
		if got := asString(c.in); got != c.want {
			t.Errorf("asString(%v) = %q, want %q", c.in, got, c.want)
		}
	}
}

func TestBaseName(t *testing.T) {
	cases := map[string]string{
		"/a/b/c":            "c",
		"/a/b/":             "b",
		`C:\Users\x\proj`:   "proj",
		`C:\Users\x\proj\`:  "proj",
		"plain":             "plain",
		"":                  "",
		"/mixed\\sep/final": "final",
	}
	for in, want := range cases {
		if got := baseName(in); got != want {
			t.Errorf("baseName(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestTruncateCountsRunes(t *testing.T) {
	if got := truncate("héllo wörld", 5); got != "héll…" {
		t.Errorf("got %q", got)
	}
	if got := truncate("😀😀😀", 2); got != "😀…" {
		t.Errorf("emoji: got %q", got)
	}
	if got := truncate("short", 10); got != "short" {
		t.Errorf("no-op: got %q", got)
	}
}

func TestParseTime(t *testing.T) {
	want := time.Date(2026, 10, 26, 1, 2, 3, 500_000_000, time.UTC).UnixMilli()
	for _, in := range []string{"2026-10-26T01:02:03.500Z", "2026-10-26T01:02:03.5+00:00", "2026-10-26T01:02:03.500"} {
		if got := parseTime(in); got != want {
			t.Errorf("parseTime(%q) = %d, want %d", in, got, want)
		}
	}
	if parseTime("") != 0 || parseTime("yesterday") != 0 {
		t.Error("unparsable stamps must return 0")
	}
}

func TestStamp(t *testing.T) {
	s := newSession("claude", "id", "f")
	a := stamp(s, "2026-10-26T01:00:00Z")
	b := stamp(s, "2026-10-26T00:00:00Z") // earlier: must not move LastEventAt back
	if s.StartedAt != a || s.LastEventAt != a || b >= a {
		t.Errorf("StartedAt=%d LastEventAt=%d a=%d b=%d", s.StartedAt, s.LastEventAt, a, b)
	}
}

// --- clean

func TestCleanStripsInjectedBlocks(t *testing.T) {
	in := "<system-reminder>secret</system-reminder> hello **world** `x`\n# h"
	if got := clean(in, 220); got != "hello world x h" {
		t.Errorf("got %q", got)
	}
}

func TestCleanDropsLongPairedTagKeepsShort(t *testing.T) {
	long := "<ide_selection>" + strings.Repeat("x", 400) + "</ide_selection> real prompt"
	if got := clean(long, 220); got != "real prompt" {
		t.Errorf("long tag: got %q", got)
	}
	if got := clean(`<b class="k">bold</b> text`, 220); got != "bold text" {
		t.Errorf("short tag: got %q", got)
	}
	if got := clean("a < b and b > c", 220); got != "a < b and b > c" {
		t.Errorf("bare angle brackets must survive: got %q", got)
	}
}

func TestCleanCodexRequestMarker(t *testing.T) {
	if got := clean("injected context\n## My request: do this", 220); got != "do this" {
		t.Errorf("got %q", got)
	}
}

func TestCleanMarkdownLinksAndImages(t *testing.T) {
	if got := clean("see [docs](http://x) ![img](y.png) done", 220); got != "see docs done" {
		t.Errorf("got %q", got)
	}
}

func TestCleanTruncates(t *testing.T) {
	got := clean(strings.Repeat("word ", 100), 20)
	if len([]rune(got)) != 20 || !strings.HasSuffix(got, "…") {
		t.Errorf("got %q (%d runes)", got, len([]rune(got)))
	}
	if clean("   ", 10) != "" || clean("", 10) != "" {
		t.Error("blank input must clean to empty")
	}
}

// --- Claude Code

func TestApplyClaudeTurn(t *testing.T) {
	s := newSession("claude", "s1", "f")
	applyClaude(s, parseJSON(t, jsonLine(t, claudeUser("Fix the bug"))))
	if s.Status != "thinking" || s.Prompt != "Fix the bug" || s.Cwd != "/home/dev/proj" || s.TurnStartedAt == 0 {
		t.Fatalf("after prompt: %+v", s)
	}
	applyClaude(s, parseJSON(t, jsonLine(t, claudeToolUse("Bash", map[string]any{"command": "npm test", "description": "Run tests"}))))
	if s.Status != "tool" || s.Activity != "Bash: Run tests" || s.Model != "claude-opus-5-5" {
		t.Fatalf("after tool_use: %+v", s)
	}
	applyClaude(s, parseJSON(t, jsonLine(t, claudeToolResult())))
	if s.Status != "thinking" {
		t.Fatalf("after tool_result: %+v", s)
	}
	applyClaude(s, parseJSON(t, jsonLine(t, claudeText("Still writing", ""))))
	if s.Status != "typing" || s.Reply != "Still writing" {
		t.Fatalf("after partial text: %+v", s)
	}
	applyClaude(s, parseJSON(t, jsonLine(t, claudeText("All **done**", "end_turn"))))
	if s.Status != "done" || s.Reply != "All done" || s.Activity != "" {
		t.Fatalf("after end_turn: %+v", s)
	}
	// next prompt clears the reply
	applyClaude(s, parseJSON(t, jsonLine(t, claudeUser("Next"))))
	if s.Reply != "" || s.Status != "thinking" {
		t.Fatalf("new prompt must clear reply: %+v", s)
	}
}

func TestApplyClaudeWaitingTools(t *testing.T) {
	for _, name := range []string{"AskUserQuestion", "ExitPlanMode"} {
		s := newSession("claude", "s", "f")
		applyClaude(s, parseJSON(t, jsonLine(t, claudeToolUse(name, nil))))
		if s.Status != "waiting" || s.Activity != name {
			t.Errorf("%s: %+v", name, s)
		}
	}
}

func TestApplyClaudeSystemAndErrors(t *testing.T) {
	s := newSession("claude", "s", "f")
	applyClaude(s, parseJSON(t, `{"type":"system","subtype":"api_error","timestamp":"`+ts(0)+`","error":{"formatted":"Rate limited"}}`))
	if s.Status != "error" || s.Error != "Rate limited" {
		t.Fatalf("api_error: %+v", s)
	}
	applyClaude(s, parseJSON(t, `{"type":"system","subtype":"stop_hook_summary","timestamp":"`+ts(0)+`"}`))
	if s.Status != "done" || s.Error != "" {
		t.Fatalf("stop_hook_summary must clear error: %+v", s)
	}
	applyClaude(s, parseJSON(t, jsonLine(t, claudeUser("[Request interrupted by user]"))))
	if s.Status != "interrupted" {
		t.Fatalf("interrupted: %+v", s)
	}
	s = newSession("claude", "s", "f")
	applyClaude(s, parseJSON(t, jsonLine(t, claudeText("boom", ""))))
	m := claudeText("boom", "")
	m["isApiErrorMessage"] = true
	applyClaude(s, parseJSON(t, jsonLine(t, m)))
	if s.Status != "error" || s.Error != "boom" {
		t.Fatalf("isApiErrorMessage: %+v", s)
	}
}

func TestApplyClaudeTitlesAndSkips(t *testing.T) {
	s := newSession("claude", "s", "f")
	applyClaude(s, parseJSON(t, `{"type":"agent-name","agentName":"Reviewer"}`))
	applyClaude(s, parseJSON(t, `{"type":"custom-title","customTitle":"My title"}`))
	applyClaude(s, parseJSON(t, `{"type":"agent-name","agentName":"Other"}`))
	if s.Title != "My title" {
		t.Errorf("custom-title wins, agent-name only fills blank: %q", s.Title)
	}

	meta := claudeUser("ignored")
	meta["isMeta"] = true
	applyClaude(s, parseJSON(t, jsonLine(t, meta)))
	if s.Prompt != "" {
		t.Errorf("isMeta must be skipped: %q", s.Prompt)
	}

	applyClaude(s, parseJSON(t, jsonLine(t, claudeUser("<local-command-stdout>out</local-command-stdout>"))))
	if s.Prompt != "" || s.Status != "idle" {
		t.Errorf("local command output must be skipped: %+v", s)
	}

	applyClaude(s, parseJSON(t, jsonLine(t, claudeUser("<command-message>x</command-message><command-name>/myoffice</command-name>"))))
	if s.Prompt != "/myoffice" {
		t.Errorf("command-name: %q", s.Prompt)
	}

	applyClaude(s, parseJSON(t, jsonLine(t, claudeUser("  <task-notification>agent done</task-notification>"))))
	if s.Prompt != "/myoffice" || s.Status != "thinking" {
		t.Errorf("task-notification keeps prompt but sets thinking: %+v", s)
	}

	side := claudeUser("Side chain prompt")
	side["isSidechain"] = true
	s2 := newSession("claude", "s2", "f")
	applyClaude(s2, parseJSON(t, jsonLine(t, side)))
	if s2.Title != "Side chain prompt" {
		t.Errorf("sidechain title: %q", s2.Title)
	}

	applyClaude(s, parseJSON(t, `{"type":"assistant","message":{"model":"<synthetic>","content":[]}}`))
	if s.Model == "<synthetic>" {
		t.Error("synthetic model must be ignored")
	}
	applyClaude(s, "not an object") // must not panic
}

func TestApplyClaudeAiTitleAndWorkspace(t *testing.T) {
	s := newSession("claude", "s", "f")
	applyClaude(s, parseJSON(t, `{"type":"user","cwd":"/home/dev/ws","message":{"role":"user","content":"hi"}}`))
	applyClaude(s, parseJSON(t, `{"type":"ai-title","aiTitle":"Build script"}`))
	if s.Title != "Build script" {
		t.Errorf("ai-title fills the title: %q", s.Title)
	}
	applyClaude(s, parseJSON(t, `{"type":"custom-title","customTitle":"Renamed"}`))
	applyClaude(s, parseJSON(t, `{"type":"ai-title","aiTitle":"Later guess"}`))
	if s.Title != "Renamed" {
		t.Errorf("custom-title must beat a later ai-title: %q", s.Title)
	}
	applyClaude(s, parseJSON(t, `{"type":"user","cwd":"/home/dev/ws/sub","message":{"role":"user","content":"cd"}}`))
	if s.Workspace != "/home/dev/ws" || s.Cwd != "/home/dev/ws/sub" {
		t.Errorf("workspace keeps the first cwd, cwd follows: %q %q", s.Workspace, s.Cwd)
	}
}

func TestApplyClaudeCompactSummaryKeepsTurn(t *testing.T) {
	s := newSession("claude", "s", "f")
	applyClaude(s, parseJSON(t, jsonLine(t, claudeUser("Long task"))))
	applyClaude(s, parseJSON(t, jsonLine(t, claudeToolUse("Bash", map[string]any{"command": "make"}))))
	started := s.TurnStartedAt
	summary := map[string]any{
		"type": "user", "timestamp": ts(0), "isCompactSummary": true, "isVisibleInTranscriptOnly": true,
		"message": map[string]any{"role": "user", "content": "This session is being continued from a previous conversation that ran out of context."},
	}
	applyClaude(s, parseJSON(t, jsonLine(t, summary)))
	if s.Prompt != "Long task" || s.TurnStartedAt != started || s.Status != "tool" {
		t.Errorf("compact summary must not start a new turn: %+v", s)
	}
}

func TestApplyClaudeContextTokens(t *testing.T) {
	withUsage := func(input, created, read float64) map[string]any {
		m := claudeText("x", "")
		m["message"].(map[string]any)["usage"] = map[string]any{
			"input_tokens": input, "cache_creation_input_tokens": created, "cache_read_input_tokens": read, "output_tokens": 500,
		}
		return m
	}
	s := newSession("claude", "s", "f")
	applyClaude(s, parseJSON(t, jsonLine(t, withUsage(2, 25_000, 300_000))))
	if s.ContextTokens != 325_002 {
		t.Fatalf("context = input + cache creation + cache read: %d", s.ContextTokens)
	}
	applyClaude(s, parseJSON(t, jsonLine(t, withUsage(0, 0, 0))))
	if s.ContextTokens != 325_002 {
		t.Errorf("an all-zero usage (synthetic message) must not reset it: %d", s.ContextTokens)
	}
	side := withUsage(1, 1, 1)
	side["isSidechain"] = true
	applyClaude(s, parseJSON(t, jsonLine(t, side)))
	if s.ContextTokens != 325_002 {
		t.Errorf("sidechain usage is another context: %d", s.ContextTokens)
	}
	applyClaude(s, parseJSON(t, `{"type":"system","subtype":"compact_boundary","timestamp":"`+ts(0)+`","compactMetadata":{"trigger":"auto","preTokens":367491,"postTokens":18630}}`))
	if s.ContextTokens != 18_630 {
		t.Errorf("compact_boundary drops the context to postTokens: %d", s.ContextTokens)
	}
}

func TestDescribeClaudeTool(t *testing.T) {
	cases := []struct {
		name  string
		input map[string]any
		want  string
	}{
		{"mcp__github__create_pull_request", nil, "github · create_pull_request"},
		{"Bash", map[string]any{"command": "ls -la"}, "Bash: ls -la"},
		{"Read", map[string]any{"file_path": "/a/b/main.go"}, "Read main.go"},
		{"Edit", map[string]any{"file_path": `C:\x\y.ts`}, "Edit y.ts"},
		{"Grep", map[string]any{"pattern": "TODO"}, "Grep TODO"},
		{"WebFetch", map[string]any{"url": "https://example.com/x"}, "WebFetch example.com"},
		{"WebFetch", map[string]any{"url": "::bad"}, "WebFetch"},
		{"WebSearch", map[string]any{"query": "go embed"}, "WebSearch “go embed”"},
		{"Agent", map[string]any{"description": "Explore repo"}, "Subagent: Explore repo"},
		{"Task", map[string]any{"subagent_type": "Explore"}, "Subagent: Explore"},
		{"Skill", map[string]any{"skill": "tdd"}, "Skill tdd"},
		{"TodoWrite", nil, "Updating todo list"},
		{"SomethingElse", nil, "SomethingElse"},
	}
	for _, c := range cases {
		if got := describeClaudeTool(c.name, c.input); got != c.want {
			t.Errorf("%s: got %q, want %q", c.name, got, c.want)
		}
	}
}

// --- Codex

func codexLine(typ string, payload map[string]any) string {
	b, _ := json.Marshal(map[string]any{"type": typ, "timestamp": ts(-time.Minute), "payload": payload})
	return string(b)
}

func TestApplyCodexMetaAndTurn(t *testing.T) {
	s := newSession("codex", "c1", "f")
	applyCodex(s, parseJSON(t, codexLine("session_meta", map[string]any{"cwd": "/w/app", "parent_thread_id": "parent-1"})))
	if s.Cwd != "/w/app" || !s.Subagent || s.ParentID != "parent-1" {
		t.Fatalf("session_meta: %+v", s)
	}
	applyCodex(s, parseJSON(t, codexLine("turn_context", map[string]any{"model": "gpt-5-codex"})))
	if s.Model != "gpt-5-codex" {
		t.Fatalf("turn_context: %+v", s)
	}
	s.Reply = "stale"
	applyCodex(s, parseJSON(t, codexLine("event_msg", map[string]any{"type": "task_started"})))
	if s.Status != "thinking" || s.Reply != "" || s.TurnStartedAt == 0 {
		t.Fatalf("task_started: %+v", s)
	}
	applyCodex(s, parseJSON(t, codexLine("event_msg", map[string]any{"type": "user_message", "message": "Make it responsive"})))
	if s.Prompt != "Make it responsive" {
		t.Fatalf("user_message: %+v", s)
	}
	applyCodex(s, parseJSON(t, codexLine("response_item", map[string]any{"type": "function_call", "name": "shell", "arguments": `{"cmd":["bash","-lc","ls"]}`})))
	if s.Status != "tool" || s.Activity != "Shell: ls" {
		t.Fatalf("function_call: %+v", s)
	}
	applyCodex(s, parseJSON(t, codexLine("response_item", map[string]any{"type": "function_call_output"})))
	if s.Status != "thinking" {
		t.Fatalf("function_call_output: %+v", s)
	}
	applyCodex(s, parseJSON(t, codexLine("event_msg", map[string]any{"type": "agent_message", "message": "Working on it"})))
	if s.Status != "typing" || s.Reply != "Working on it" {
		t.Fatalf("agent_message: %+v", s)
	}
	applyCodex(s, parseJSON(t, codexLine("event_msg", map[string]any{"type": "task_complete", "last_agent_message": "Done."})))
	if s.Status != "done" || s.Reply != "Done." {
		t.Fatalf("task_complete: %+v", s)
	}
}

func TestApplyCodexTokenCount(t *testing.T) {
	s := newSession("codex", "c", "f")
	applyCodex(s, parseJSON(t, codexLine("event_msg", map[string]any{"type": "token_count", "info": nil})))
	if s.ContextTokens != 0 || s.ContextWindow != 0 {
		t.Fatalf("token_count without info: %+v", s)
	}
	applyCodex(s, parseJSON(t, codexLine("event_msg", map[string]any{"type": "token_count", "info": map[string]any{
		"total_token_usage":    map[string]any{"input_tokens": 900_000},
		"last_token_usage":     map[string]any{"input_tokens": 237_489, "cached_input_tokens": 200_000},
		"model_context_window": 258_400,
	}})))
	if s.ContextTokens != 237_489 || s.ContextWindow != 258_400 {
		t.Fatalf("context is the last request's input, not the running total: %+v", s)
	}
	applyCodex(s, parseJSON(t, codexLine("compacted", map[string]any{"message": ""})))
	if s.ContextTokens != 0 || s.ContextWindow != 258_400 {
		t.Errorf("compacted clears the context until the next token_count: %+v", s)
	}
}

func TestApplyCodexWaitingAndErrors(t *testing.T) {
	for _, typ := range []string{"exec_approval_request", "apply_patch_approval_request", "request_user_input"} {
		s := newSession("codex", "c", "f")
		applyCodex(s, parseJSON(t, codexLine("event_msg", map[string]any{"type": typ})))
		if s.Status != "waiting" || s.Activity != "Needs your approval" {
			t.Errorf("%s: %+v", typ, s)
		}
	}
	s := newSession("codex", "c", "f")
	applyCodex(s, parseJSON(t, codexLine("response_item", map[string]any{"type": "function_call", "name": "request_user_input", "arguments": "{}"})))
	if s.Status != "waiting" || s.Activity != "Asking you a question" {
		t.Errorf("request_user_input call: %+v", s)
	}
	applyCodex(s, parseJSON(t, codexLine("event_msg", map[string]any{"type": "error", "message": "boom"})))
	if s.Status != "error" || s.Error != "boom" {
		t.Errorf("error: %+v", s)
	}
	applyCodex(s, parseJSON(t, codexLine("event_msg", map[string]any{"type": "turn_aborted"})))
	if s.Status != "interrupted" || s.Error != "" {
		t.Errorf("turn_aborted: %+v", s)
	}
}

func TestApplyCodexItems(t *testing.T) {
	s := newSession("codex", "c", "f")
	item := func(evt string, it map[string]any) any {
		return parseJSON(t, codexLine("event_msg", map[string]any{"type": evt, "item": it}))
	}
	applyCodex(s, item("item_started", map[string]any{"type": "FileChange", "changes": map[string]any{"/w/b.ts": 1, "/w/a.ts": 1}}))
	if s.Status != "tool" || s.Activity != "Editing a.ts, b.ts" {
		t.Errorf("FileChange must sort names: %+v", s)
	}
	applyCodex(s, item("item_completed", map[string]any{"type": "CommandExecution", "command": []any{"bash", "-lc", "pnpm test"}}))
	if s.Status != "thinking" || s.Activity != "Shell: pnpm test" {
		t.Errorf("CommandExecution completed: %+v", s)
	}
	applyCodex(s, item("item_started", map[string]any{"type": "McpToolCall", "server": "github", "tool": "list_prs"}))
	if s.Activity != "github · list_prs" {
		t.Errorf("McpToolCall: %+v", s)
	}
	applyCodex(s, item("item_completed", map[string]any{"type": "AgentMessage", "content": []any{map[string]any{"text": "hi "}, map[string]any{"text": "there"}}}))
	if s.Status != "typing" || s.Reply != "hi there" {
		t.Errorf("AgentMessage: %+v", s)
	}
	applyCodex(s, item("item_completed", map[string]any{"type": "UserMessage", "content": "the ask"}))
	if s.Prompt != "the ask" {
		t.Errorf("UserMessage: %+v", s)
	}
}

func TestDescribeCodexCall(t *testing.T) {
	cases := []struct {
		name string
		raw  any
		want string
	}{
		{"request_user_input_async", nil, "Asking you a question"},
		{"shell", map[string]any{"command": "git status"}, "Shell: git status"},
		{"search", map[string]any{"query": "embed fs"}, "search “embed fs”"},
		{"exec", `cmd: "npm \"run\" build\nx"`, `Shell: npm "run" build x`},
		{"exec", `await tools.read_file({})`, "read_file"},
		{"custom", "plain string", "custom"},
	}
	for _, c := range cases {
		if got := describeCodexCall(c.name, c.raw); got != c.want {
			t.Errorf("%s: got %q, want %q", c.name, got, c.want)
		}
	}
}

// --- Gemini CLI

func TestApplyGeminiMetadata(t *testing.T) {
	s := newSession("gemini", "g1", "f")
	applyGemini(s, parseJSON(t, `{"sessionId":"abc","kind":"subagent","directories":["/p/one"],"summary":"**Summary** here","startTime":"`+ts(-time.Hour)+`","messages":[{"type":"user","timestamp":"`+ts(-time.Minute)+`","content":"nested prompt"}]}`))
	if s.SessionID != "abc" || !s.Subagent || s.Cwd != "/p/one" || s.Title != "Summary here" || s.StartedAt == 0 {
		t.Fatalf("metadata: %+v", s)
	}
	if s.Prompt != "nested prompt" || s.Status != "thinking" {
		t.Fatalf("nested messages must be applied: %+v", s)
	}
	applyGemini(s, parseJSON(t, `{"$set":{"summary":"Renamed"}}`))
	if s.Title != "Renamed" {
		t.Fatalf("$set: %+v", s)
	}
	applyGemini(s, parseJSON(t, `{"$patch":{"x":1}}`))
	applyGemini(s, parseJSON(t, `{"no":"type"}`))
	if s.Title != "Renamed" || s.Prompt != "nested prompt" {
		t.Fatalf("$patch and untyped lines must be ignored: %+v", s)
	}
}

func TestApplyGeminiTurn(t *testing.T) {
	s := newSession("gemini", "g", "f")
	line := func(m map[string]any) any {
		m["timestamp"] = ts(-time.Minute)
		return parseJSON(t, jsonLine(t, m))
	}
	applyGemini(s, line(map[string]any{"type": "user", "content": []any{map[string]any{"text": "/help me please"}}}))
	if s.Prompt != "/help" || s.Status != "thinking" {
		t.Fatalf("slash prompt keeps command only: %+v", s)
	}
	applyGemini(s, line(map[string]any{"type": "user", "displayContent": "Backfill events", "content": "raw"}))
	if s.Prompt != "Backfill events" || s.TurnStartedAt == 0 {
		t.Fatalf("displayContent wins: %+v", s)
	}
	applyGemini(s, line(map[string]any{"type": "gemini", "model": "gemini-3-pro", "toolCalls": []any{
		map[string]any{"name": "run_shell_command", "status": "executing", "args": map[string]any{"command": "ls -la"}},
	}}))
	if s.Status != "tool" || s.Activity != "Shell: ls -la" || s.Model != "gemini-3-pro" {
		t.Fatalf("executing tool: %+v", s)
	}
	applyGemini(s, line(map[string]any{"type": "gemini", "toolCalls": []any{
		map[string]any{"name": "write_file", "displayName": "WriteFile", "status": "awaiting_approval", "args": map[string]any{"file_path": "/x/out.py"}},
	}}))
	if s.Status != "waiting" || s.Activity != "WriteFile out.py" {
		t.Fatalf("awaiting_approval: %+v", s)
	}
	applyGemini(s, line(map[string]any{"type": "gemini", "toolCalls": []any{map[string]any{"name": "glob", "status": "success"}}}))
	if s.Status != "thinking" {
		t.Fatalf("tools done, no text: %+v", s)
	}
	applyGemini(s, line(map[string]any{"type": "gemini", "content": []any{map[string]any{"text": "Here is "}, map[string]any{"text": "the answer"}}}))
	if s.Status != "typing" || s.Reply != "Here is the answer" {
		t.Fatalf("text reply: %+v", s)
	}
	applyGemini(s, line(map[string]any{"type": "info", "content": "Request cancelled by user"}))
	if s.Status != "interrupted" {
		t.Fatalf("cancel info: %+v", s)
	}
	applyGemini(s, line(map[string]any{"type": "error", "content": "quota exceeded"}))
	if s.Status != "error" || s.Error != "quota exceeded" {
		t.Fatalf("error: %+v", s)
	}
}

func TestApplyGeminiDocument(t *testing.T) {
	s := newSession("gemini", "g", "f.json")
	applyGeminiDocument(s, parseJSON(t, `{"sessionId":"legacy","messages":[{"type":"user","timestamp":"`+ts(-time.Minute)+`","content":"old style"}]}`))
	if s.SessionID != "legacy" || s.Prompt != "old style" {
		t.Fatalf("legacy document: %+v", s)
	}
}

func TestDescribeGeminiTool(t *testing.T) {
	cases := []struct {
		tc   map[string]any
		want string
	}{
		{map[string]any{"name": "run_shell_command", "args": map[string]any{"description": "List files"}}, "Shell: List files"},
		{map[string]any{"name": "read_file", "args": map[string]any{"absolute_path": "/a/b.go"}}, "read_file b.go"},
		{map[string]any{"name": "grep", "displayName": "SearchText", "args": map[string]any{"pattern": "TODO"}}, "SearchText TODO"},
		{map[string]any{"name": "google_web_search", "args": map[string]any{"query": "go"}}, "WebSearch “go”"},
		{map[string]any{"name": "web_fetch", "args": map[string]any{"url": "https://x.y"}}, "WebFetch https://x.y"},
		{map[string]any{"displayName": "Custom"}, "Custom"},
		{map[string]any{}, "tool"},
	}
	for _, c := range cases {
		if got := describeGeminiTool(c.tc); got != c.want {
			t.Errorf("%v: got %q, want %q", c.tc, got, c.want)
		}
	}
}

// --- platform and prompt kind

func TestCleanDropsIDEContextBlocks(t *testing.T) {
	in := "<ide_opened_file>The user opened the file /x/README.md in the IDE. This may or may not be related to the current task.</ide_opened_file>\n<ide_selection>const a = 1;</ide_selection>\n請評估這段程式"
	if got := clean(in, 220); got != "請評估這段程式" {
		t.Errorf("got %q", got)
	}
}

func TestClaudePlatformFromEntrypoint(t *testing.T) {
	for entrypoint, want := range map[string]string{"claude-vscode": "vscode", "cli": "cli", "sdk-cli": "sdk"} {
		s := newSession("claude", "p", "f")
		line := claudeUser("hi")
		line["entrypoint"] = entrypoint
		applyClaude(s, parseJSON(t, jsonLine(t, line)))
		if s.Platform != want {
			t.Errorf("%s: got %q want %q", entrypoint, s.Platform, want)
		}
	}
	s := newSession("claude", "p", "f")
	applyClaude(s, parseJSON(t, jsonLine(t, claudeUser("hi"))))
	if s.Platform != "" {
		t.Errorf("no entrypoint: got %q", s.Platform)
	}
}

func TestCodexPlatformFromOriginator(t *testing.T) {
	cases := []struct {
		originator string
		source     any
		want       string
	}{
		{"codex_vscode", "vscode", "vscode"},
		{"codex_exec", "exec", "sdk"},
		{"codex_cli_rs", "cli", "cli"},
		{"Codex Desktop", "vscode", "desktop"},
		{"codex_work_desktop", map[string]any{"subagent": map[string]any{"other": "guardian"}}, "desktop"},
	}
	for _, c := range cases {
		s := newSession("codex", "c", "f")
		applyCodex(s, parseJSON(t, codexLine("session_meta", map[string]any{"cwd": "/p", "originator": c.originator, "source": c.source})))
		if s.Platform != c.want {
			t.Errorf("%s: got %q want %q", c.originator, s.Platform, c.want)
		}
	}
}

func TestGeminiPlatformIsCLI(t *testing.T) {
	s := newSession("gemini", "g", "f")
	applyGemini(s, parseJSON(t, `{"sessionId":"g","projectHash":"h","startTime":"`+ts(-time.Hour)+`","lastUpdated":"`+ts(-time.Hour)+`","kind":"main"}`))
	if s.Platform != "cli" {
		t.Errorf("got %q", s.Platform)
	}
}

func TestClaudePromptKind(t *testing.T) {
	s := newSession("claude", "k", "f")
	applyClaude(s, parseJSON(t, jsonLine(t, claudeUser("<command-message>myoffice</command-message>\n<command-name>/myoffice</command-name>"))))
	if s.Prompt != "/myoffice" || s.PromptKind != "command" {
		t.Fatalf("command: %+v", s)
	}
	applyClaude(s, parseJSON(t, jsonLine(t, claudeUser("<task-notification>\n<task-id>1</task-id>\n<status>completed</status>\n</task-notification>"))))
	if s.Prompt != "/myoffice" || s.PromptKind != "notification" {
		t.Fatalf("notification keeps the prompt: %+v", s)
	}
	applyClaude(s, parseJSON(t, jsonLine(t, claudeUser("修一下這個 bug"))))
	if s.Prompt != "修一下這個 bug" || s.PromptKind != "" {
		t.Fatalf("plain text: %+v", s)
	}
}

func TestGeminiPromptKind(t *testing.T) {
	s := newSession("gemini", "k", "f")
	line := func(text string) any {
		return parseJSON(t, jsonLine(t, map[string]any{"type": "user", "timestamp": ts(-time.Minute), "content": []any{map[string]any{"text": text}}}))
	}
	applyGemini(s, line("/chat list"))
	if s.Prompt != "/chat" || s.PromptKind != "command" {
		t.Fatalf("command: %+v", s)
	}
	applyGemini(s, line("hello"))
	if s.PromptKind != "" {
		t.Fatalf("plain text: %+v", s)
	}
}

// --- Full text for the detail dialog

func TestCleanFullKeepsLinesAndMarkdown(t *testing.T) {
	in := "<system-reminder>secret</system-reminder>\nline **one**\n\n- item `x`\n"
	if got := cleanFull(in); got != "line **one**\n\n- item `x`" {
		t.Errorf("got %q", got)
	}
	if got := cleanFull("ctx\n## My request: do\nthis"); got != "do\nthis" {
		t.Errorf("codex marker: got %q", got)
	}
	if got := cleanFull(strings.Repeat("a", FullTextMax+50)); len([]rune(got)) != FullTextMax {
		t.Errorf("cap: %d runes", len([]rune(got)))
	}
}

func TestApplyClaudeKeepsFullText(t *testing.T) {
	long := strings.Repeat("word ", 100) + "\nend"
	s := newSession("claude", "s1", "f")
	applyClaude(s, parseJSON(t, jsonLine(t, claudeUser("Fix\nthe bug"))))
	if s.FullPrompt != "Fix\nthe bug" || s.Prompt != "Fix the bug" {
		t.Fatalf("prompt: %q / %q", s.FullPrompt, s.Prompt)
	}
	applyClaude(s, parseJSON(t, jsonLine(t, claudeText(long, "end_turn"))))
	if s.FullReply != strings.TrimSpace(long) || len([]rune(s.Reply)) != 220 {
		t.Fatalf("reply: %q / %q", s.FullReply, s.Reply)
	}
	applyClaude(s, parseJSON(t, jsonLine(t, claudeUser("Next"))))
	if s.FullReply != "" || s.FullPrompt != "Next" {
		t.Fatalf("new prompt must clear the full reply: %+v", s)
	}
}
