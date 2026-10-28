package main

// Where each CLI compacts a session on its own, so the page can warn before
// it happens. The thresholds live in the CLIs' own settings files; the
// defaults for when they say nothing are in config.go.
//
// Claude Code: ~/.claude/settings.json modelSettings.<model>.autoCompactWindow,
// minus ClaudeCompactReserve. Codex: ~/.codex/config.toml
// model_auto_compact_token_limit. Not seen from here: env overrides on the
// CLI's own process (CLAUDE_CODE_AUTO_COMPACT_WINDOW,
// CLAUDE_AUTOCOMPACT_PCT_OVERRIDE), project-level settings and Codex profiles.

import (
	"encoding/json"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
)

var (
	claudeSettings = filepath.Join(envOr("CLAUDE_CONFIG_DIR", filepath.Join(homeDir, ".claude")), "settings.json")
	codexConfig    = filepath.Join(codexHome, "config.toml")

	reCodexCompactLimit = regexp.MustCompile(`^\s*model_auto_compact_token_limit\s*=\s*([0-9_]+)`)
)

type compactLimits struct {
	mtime  int64            // both files' mtimes added up at the last read
	claude map[string]int64 // model -> autoCompactWindow
	codex  int64            // model_auto_compact_token_limit; 0 when unset
}

// load re-reads both settings files when either changed; returns true when it did.
func (c *compactLimits) load() bool {
	var mt int64
	for _, f := range []string{claudeSettings, codexConfig} {
		if st := statSafe(f); st != nil {
			mt += mtimeMs(st)
		}
	}
	if mt == c.mtime && c.claude != nil {
		return false
	}
	c.mtime = mt
	c.claude = map[string]int64{}
	if raw, err := os.ReadFile(claudeSettings); err == nil {
		var cfg struct {
			ModelSettings map[string]struct {
				AutoCompactWindow int64 `json:"autoCompactWindow"`
			} `json:"modelSettings"`
		}
		if json.Unmarshal(raw, &cfg) == nil {
			for model, m := range cfg.ModelSettings {
				if m.AutoCompactWindow > 0 {
					c.claude[model] = m.AutoCompactWindow
				}
			}
		}
	}
	c.codex = 0
	if raw, err := os.ReadFile(codexConfig); err == nil {
		for _, line := range strings.Split(string(raw), "\n") {
			if strings.HasPrefix(strings.TrimSpace(line), "[") {
				break // top-level keys only: what follows belongs to a table
			}
			if m := reCodexCompactLimit.FindStringSubmatch(line); m != nil {
				c.codex, _ = strconv.ParseInt(strings.ReplaceAll(m[1], "_", ""), 10, 64)
			}
		}
	}
	return true
}

// at is the session's auto-compact threshold in tokens; 0 when unknown.
func (c *compactLimits) at(s *Session) int64 {
	switch s.Agent {
	case "claude":
		window := c.claude[s.Model]
		if window == 0 {
			window = ClaudeDefaultWindow
			// A session already past the default threshold runs on a bigger window.
			if s.ContextTokens > window-ClaudeCompactReserve {
				window = ClaudeLargeWindow
			}
		}
		return window - ClaudeCompactReserve
	case "codex":
		if c.codex > 0 {
			return c.codex
		}
		return int64(float64(s.ContextWindow) * CodexCompactRatio)
	}
	return 0
}
