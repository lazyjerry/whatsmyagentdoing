package main

import (
	"path/filepath"
	"testing"
)

func TestCompactLimitsDefaults(t *testing.T) {
	setLogDirs(t) // no settings files
	var c compactLimits
	if !c.load() || c.load() {
		t.Fatal("first load reads, an unchanged second load does not")
	}
	cases := []struct {
		s    Session
		want int64
	}{
		{Session{Agent: "claude", Model: "claude-opus-5-5", ContextTokens: 100_000}, ClaudeDefaultWindow - ClaudeCompactReserve},
		{Session{Agent: "claude", Model: "claude-opus-5-5", ContextTokens: 400_000}, ClaudeLargeWindow - ClaudeCompactReserve},
		{Session{Agent: "codex", ContextWindow: 258_400}, int64(258_400 * CodexCompactRatio)},
		{Session{Agent: "codex"}, 0},
		{Session{Agent: "gemini", ContextTokens: 100_000}, 0},
		{Session{Agent: "opencode", ContextTokens: 100_000}, 0},
	}
	for _, k := range cases {
		if got := c.at(&k.s); got != k.want {
			t.Errorf("%s %d: got %d, want %d", k.s.Agent, k.s.ContextTokens, got, k.want)
		}
	}
}

func TestCompactLimitsFromSettings(t *testing.T) {
	_, codex, _ := setLogDirs(t)
	writeFile(t, claudeSettings, `{"model":"opus","modelSettings":{"claude-opus-5-5":{"autoCompactWindow":400000,"effortLevel":"high"},"claude-fable-5-1":{"effortLevel":"low"}}}`)
	writeFile(t, filepath.Join(codex, "config.toml"), lines(
		`model = "gpt-5.6"`,
		`model_auto_compact_token_limit = 240_000`,
		`[profiles.fast]`,
		`model_auto_compact_token_limit = 100000`,
	))
	var c compactLimits
	c.load()
	if got := c.at(&Session{Agent: "claude", Model: "claude-opus-5-5", ContextTokens: 10}); got != 367_000 {
		t.Errorf("autoCompactWindow 400k → 367k: %d", got)
	}
	if got := c.at(&Session{Agent: "claude", Model: "claude-fable-5-1", ContextTokens: 10}); got != ClaudeDefaultWindow-ClaudeCompactReserve {
		t.Errorf("model without autoCompactWindow falls back: %d", got)
	}
	if got := c.at(&Session{Agent: "codex", ContextWindow: 258_400}); got != 240_000 {
		t.Errorf("top-level limit wins, profile tables are ignored: %d", got)
	}
}

func TestWatcherSetsCompactAt(t *testing.T) {
	claude, _, _ := setLogDirs(t)
	writeFile(t, claudeSettings, `{"modelSettings":{"claude-opus-5-5":{"autoCompactWindow":400000}}}`)
	writeFile(t, filepath.Join(claude, "-p", "s.jsonl"), lines(jsonLine(t, claudeUser("Hi")), jsonLine(t, claudeText("Hello", "end_turn"))))
	w := NewWatcher(24)
	w.Tick()
	got := w.Sessions()
	if len(got) != 1 || got[0].CompactAt != 367_000 {
		t.Errorf("Sessions() fills CompactAt from the settings: %+v", got)
	}
}
