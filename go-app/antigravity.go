package main

// Google Antigravity (the `agy` CLI and the Antigravity IDE) keeps no
// transcript on disk for CLI runs: the only record of a conversation is a
// row in ~/.gemini/antigravity/conversation_summaries.db (SQLite), written a
// few seconds into the run and again when it ends. Rows are read through the
// sqlite3 CLI (see sqlite.go); no sqlite3 → no Antigravity sessions.
// Sessions keep the `gemini` agent (same model family, same badge) and carry
// Tool "agy" so the page offers `agy --conversation <id>`.

import (
	"encoding/json"
	"net/url"
	"path/filepath"
	"strings"
	"time"
)

var agyDB = filepath.Join(geminiHomes[0], "antigravity", "conversation_summaries.db")

// agyRow is one conversation_summaries row as `sqlite3 -json` prints it.
type agyRow struct {
	ID            string `json:"conversation_id"`
	Title         string `json:"title"`
	Preview       string `json:"preview"`
	StepCount     int    `json:"step_count"`
	LastModified  string `json:"last_modified_time"`
	LastUserInput string `json:"last_user_input_time"`
	WorkspaceURIs string `json:"workspace_uris"`
	Status        string `json:"status"`
	Source        string `json:"source"`
	ParentID      string `json:"parent_conversation_id"`
	NestingDepth  int    `json:"nesting_depth"`
	NotFullyIdle  int    `json:"not_fully_idle"`
	Killed        int    `json:"killed"`
}

const agySQL = `select conversation_id, title, preview, step_count, last_modified_time, last_user_input_time, workspace_uris, status, source, parent_conversation_id, nesting_depth, not_fully_idle, killed from conversation_summaries where last_modified_time >= '%s' order by last_modified_time desc limit 500`

// agyRows runs the query against db for rows modified at or after sinceMs.
func agyRows(db string, sinceMs int64) ([]agyRow, error) {
	since := time.UnixMilli(sinceMs).UTC().Format("2006-01-02 15:04:05")
	var rows []agyRow
	err := sqliteJSON(db, strings.Replace(agySQL, "%s", since, 1), &rows)
	return rows, err
}

// agyRead is the dbSource reader: every row in the window as a Session.
func agyRead(db string, sinceMs int64) ([]*Session, error) {
	rows, err := agyRows(db, sinceMs)
	if err != nil {
		return nil, err
	}
	out := make([]*Session, 0, len(rows))
	for _, r := range rows {
		out = append(out, agySession(r))
	}
	return out, nil
}

// agyTime parses "2026-10-26 04:00:37.785627+00:00" (fraction optional).
func agyTime(s string) int64 {
	for _, layout := range []string{"2006-01-02 15:04:05.999999999-07:00", "2006-01-02 15:04:05-07:00", "2006-01-02 15:04:05.999999999", "2006-01-02 15:04:05"} {
		if t, err := time.Parse(layout, s); err == nil {
			return t.UnixMilli()
		}
	}
	return 0
}

// agyWorkspace returns the first workspace folder as a local path; the column
// is a JSON array of file:// URIs with percent-encoded non-ASCII.
func agyWorkspace(uris string) string {
	var list []string
	if json.Unmarshal([]byte(uris), &list) != nil || len(list) == 0 {
		return ""
	}
	u, err := url.Parse(list[0])
	if err != nil || u.Scheme != "file" {
		return ""
	}
	return u.Path
}

// agyStatus maps the CASCADE_RUN_STATUS_* enum onto the page's states. CLI
// runs only ever report IDLE (even mid-run), so a finished-looking row may
// still be going; the IDE reports RUNNING while it works.
func agyStatus(r agyRow) string {
	st := strings.ToUpper(r.Status)
	switch {
	case r.Killed != 0:
		return "interrupted"
	case strings.Contains(st, "ERROR"):
		return "error"
	case strings.Contains(st, "RUNNING") || r.NotFullyIdle != 0:
		return "thinking"
	case r.StepCount == 0:
		return "idle"
	case strings.Contains(st, "IDLE"):
		return "done"
	}
	return "idle"
}

func agySession(r agyRow) *Session {
	s := newSession("gemini", r.ID, agyDB)
	s.SessionID = r.ID
	s.Tool = "agy"
	s.Cwd = agyWorkspace(r.WorkspaceURIs)
	s.Workspace = s.Cwd
	s.Title = strings.TrimSpace(r.Title)
	s.Prompt = clean(r.Preview, 220)
	if strings.Contains(strings.ToUpper(r.Source), "CLI") {
		s.Platform = "cli"
	} else {
		s.Platform = "desktop"
	}
	if r.NestingDepth > 0 || r.ParentID != "" {
		s.Subagent = true
		s.ParentID = r.ParentID
	}
	s.LastEventAt = agyTime(r.LastModified)
	s.Mtime = s.LastEventAt
	s.TurnStartedAt = agyTime(r.LastUserInput)
	s.Status = agyStatus(r)
	return s
}
