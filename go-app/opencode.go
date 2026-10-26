package main

// OpenCode (opencode.ai: the `opencode` TUI and `opencode run`) keeps every
// session, message and message part in one SQLite database,
// ~/.local/share/opencode/opencode.db ($XDG_DATA_HOME honoured), and writes
// it as the turn goes: the assistant message appears when a turn starts and
// gets time.completed when it ends; each tool call is a part whose state
// moves running → completed/error. That covers every state except
// "waiting": permission prompts are not persisted, so a tool stuck on one
// looks like it is still running (same as Claude Code). Rows are read
// through the sqlite3 CLI (see sqlite.go); no sqlite3 → no OpenCode sessions.

import (
	"encoding/json"
	"fmt"
	"path/filepath"
	"strings"
)

var ocDB = filepath.Join(envOr("XDG_DATA_HOME", filepath.Join(homeDir, ".local", "share")), "opencode", "opencode.db")

// ocRow is one session with its newest messages folded in, as `sqlite3 -json`
// prints it. The JSON columns (message and part `data`) stay strings here.
type ocRow struct {
	ID          string `json:"id"`
	ParentID    string `json:"parent_id"`
	Title       string `json:"title"`
	Directory   string `json:"directory"`
	Worktree    string `json:"worktree"`
	Model       string `json:"model"`
	Created     int64  `json:"time_created"`
	Updated     int64  `json:"time_updated"`
	MsgUpdated  int64  `json:"msg_updated"`
	PartUpdated int64  `json:"part_updated"`
	UserTime    int64  `json:"user_time"`
	LastMsg     string `json:"last_msg"`
	PromptPart  string `json:"prompt_part"`
	LastPart    string `json:"last_part"`
	ReplyPart   string `json:"reply_part"`
}

// One row per session in the window: the newest user message (prompt, turn
// start) and the newest message of any role with its last part (state) and
// last text part (reply). Part ids are time-ordered, so `order by id` walks a
// message in write order.
const ocSQL = `select x.*,
 (select p.data from part p where p.message_id = x.user_id and json_extract(p.data, '$.type') = 'text' and coalesce(json_extract(p.data, '$.synthetic'), 0) = 0 order by p.id limit 1) as prompt_part,
 (select p.data from part p where p.message_id = x.last_id order by p.id desc limit 1) as last_part,
 (select p.data from part p where p.message_id = x.last_id and json_extract(p.data, '$.type') = 'text' order by p.id desc limit 1) as reply_part
from (select s.id, s.parent_id, s.title, s.directory, coalesce(pj.worktree, '') as worktree, s.model, s.time_created, s.time_updated,
 (select max(m.time_updated) from message m where m.session_id = s.id) as msg_updated,
 (select max(p.time_updated) from part p where p.session_id = s.id) as part_updated,
 (select m.id from message m where m.session_id = s.id and json_extract(m.data, '$.role') = 'user' order by m.time_created desc, m.id desc limit 1) as user_id,
 (select m.time_created from message m where m.session_id = s.id and json_extract(m.data, '$.role') = 'user' order by m.time_created desc, m.id desc limit 1) as user_time,
 (select m.id from message m where m.session_id = s.id order by m.time_created desc, m.id desc limit 1) as last_id,
 (select m.data from message m where m.session_id = s.id order by m.time_created desc, m.id desc limit 1) as last_msg
 from session s left join project pj on pj.id = s.project_id
 where s.time_updated >= %d order by s.time_updated desc limit 500) x`

// ocRead is the dbSource reader: every session updated at or after sinceMs.
func ocRead(db string, sinceMs int64) ([]*Session, error) {
	var rows []ocRow
	if err := sqliteJSON(db, fmt.Sprintf(ocSQL, sinceMs), &rows); err != nil {
		return nil, err
	}
	out := make([]*Session, 0, len(rows))
	for _, r := range rows {
		out = append(out, ocSession(r))
	}
	return out, nil
}

// ocJSON decodes one JSON object column; nil for empty or malformed text.
func ocJSON(raw string) map[string]any {
	if raw == "" {
		return nil
	}
	var v any
	if json.Unmarshal([]byte(raw), &v) != nil {
		return nil
	}
	return asMap(v)
}

// ocModel renders the session's model column ({"id":…,"providerID":…} as of
// 1.19; a plain string in older databases) as provider/model.
func ocModel(raw string) string {
	m := ocJSON(raw)
	if m == nil {
		return strings.TrimSpace(raw)
	}
	id, provider := str(m, "id"), str(m, "providerID")
	if id == "" {
		return provider
	}
	if provider == "" {
		return id
	}
	return provider + "/" + id
}

// describeOCTool names a tool call the way the Claude Code parser does, with
// OpenCode's lower-case tool names and camelCase inputs.
func describeOCTool(name string, input map[string]any) string {
	base := func(p string) string {
		if p == "" {
			return ""
		}
		return baseName(p)
	}
	switch name {
	case "bash":
		return "Bash: " + short(firstNonEmpty(str(input, "description"), str(input, "command")), 90)
	case "read", "edit", "write", "multiedit", "patch":
		return strings.Title(name) + " " + base(str(input, "filePath"))
	case "grep", "glob":
		return strings.Title(name) + " " + short(str(input, "pattern"), 60)
	case "list":
		return "List " + base(str(input, "path"))
	case "webfetch":
		if h := urlHost(str(input, "url")); h != "" {
			return "WebFetch " + h
		}
		return "WebFetch"
	case "websearch":
		return "WebSearch “" + short(str(input, "query"), 60) + "”"
	case "task":
		return "Subagent: " + short(firstNonEmpty(str(input, "description"), str(input, "subagent_type")), 60)
	case "skill":
		return "Skill " + str(input, "name")
	case "todowrite", "todoread":
		return "Updating todo list"
	}
	return name
}

// ocStatus derives the page state from the newest message and its last part.
func ocStatus(s *Session, msg map[string]any, lastPart string) {
	if msg == nil {
		set(s, "idle")
		return
	}
	if str(msg, "role") != "assistant" {
		set(s, "thinking") // the prompt is in, the reply has not started
		return
	}
	if e := asMap(msg["error"]); e != nil {
		name := str(e, "name")
		if name == "MessageAbortedError" {
			set(s, "interrupted")
			return
		}
		s.Error = short(firstNonEmpty(str(asMap(e["data"]), "message"), name, "Error"), 120)
		set(s, "error")
		return
	}
	if asMap(msg["time"])["completed"] != nil {
		set(s, "done")
		return
	}
	part := ocJSON(lastPart)
	switch str(part, "type") {
	case "tool":
		state := asMap(part["state"])
		if st := str(state, "status"); st == "running" || st == "pending" {
			setA(s, "tool", describeOCTool(str(part, "tool"), asMap(state["input"])))
			return
		}
		set(s, "thinking") // the call finished, the model is on its next step
	case "text":
		set(s, "typing")
	default: // step-start, step-finish, reasoning, nothing yet
		set(s, "thinking")
	}
}

func ocSession(r ocRow) *Session {
	s := newSession("opencode", r.ID, ocDB)
	s.SessionID = r.ID
	s.Cwd = r.Directory
	s.Workspace = r.Worktree
	if s.Workspace == "" || s.Workspace == "/" { // the "global" project has no worktree
		s.Workspace = r.Directory
	}
	// Untitled sessions carry "New session - <iso time>"; the prompt reads better.
	if !strings.HasPrefix(r.Title, "New session - ") {
		s.Title = clean(r.Title, 80)
	}
	s.Model = ocModel(r.Model)
	s.Platform = "cli"
	if r.ParentID != "" {
		s.Subagent = true
		s.ParentID = r.ParentID
	}
	s.StartedAt = r.Created
	s.TurnStartedAt = r.UserTime
	s.LastEventAt = max(r.Updated, r.MsgUpdated, r.PartUpdated)
	s.Mtime = s.LastEventAt
	if p := ocJSON(r.PromptPart); p != nil {
		s.Prompt = clean(str(p, "text"), 220)
	}
	msg := ocJSON(r.LastMsg)
	if str(msg, "role") == "assistant" {
		if p := ocJSON(r.ReplyPart); p != nil {
			s.Reply = clean(str(p, "text"), 220)
		}
	}
	ocStatus(s, msg, r.LastPart)
	return s
}
