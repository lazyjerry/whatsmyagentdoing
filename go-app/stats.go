package main

// Turn and tool-call counts for the boss's card (/api/stats): the last
// 5 hours, the last 24 hours and this week (since Monday 00:00 local time).
// The watcher only keeps each log's latest state and reads the head and tail
// of big logs, so the counts come from a reader of their own that goes
// through every log touched this week, once, and then only the bytes added
// since. Each record goes through the same apply functions as the watcher;
// a turn ends when the next prompt arrives, with whatever status the session
// had by then. Subagents count toward tool calls, not turns. OpenCode is
// counted straight from its database; Antigravity's summaries keep only each
// conversation's latest turn, so it is left out.

import (
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"sync"
	"time"
)

type StatWindow struct {
	Key         string `json:"key"` // 5h | 24h | week
	Since       int64  `json:"since"`
	Turns       int    `json:"turns"`
	Done        int    `json:"done"`
	Error       int    `json:"error"`
	Interrupted int    `json:"interrupted"`
	Running     int    `json:"running"` // still going: left out of the completion rate
	Tools       int    `json:"tools"`
}

type Stats struct {
	Now     int64        `json:"now"`
	Windows []StatWindow `json:"windows"`
}

// A turn that has ended: when it started and the status it ended on.
type statTurn struct {
	at     int64
	status string
}

// One log file's reader state.
type statFile struct {
	session *Session
	agent   string
	offset  int64
	mtime   int64
	partial string
	seen    map[string]bool // Gemini writes a message again each time its tool calls move on
	turns   []statTurn
	tools   []int64 // when each tool call was made
}

var statApply = map[string]applyFunc{"claude": applyClaude, "codex": applyCodex, "gemini": applyGemini}

type statsScanner struct {
	mu     sync.Mutex
	files  map[string]*statFile
	at     int64
	cached Stats
}

func newStatsScanner() *statsScanner {
	return &statsScanner{files: map[string]*statFile{}}
}

func weekStart(now time.Time) time.Time {
	y, m, d := now.Date()
	return time.Date(y, m, d-(int(now.Weekday())+6)%7, 0, 0, 0, 0, now.Location())
}

func statWindows(now time.Time) []StatWindow {
	ms := now.UnixMilli()
	return []StatWindow{
		{Key: "5h", Since: ms - (5 * time.Hour).Milliseconds()},
		{Key: "24h", Since: ms - (24 * time.Hour).Milliseconds()},
		{Key: "week", Since: weekStart(now).UnixMilli()},
	}
}

func (sc *statsScanner) compute(now time.Time) Stats {
	sc.mu.Lock()
	defer sc.mu.Unlock()
	nowMs := now.UnixMilli()
	if sc.at != 0 && nowMs-sc.at < StatsCacheFor.Milliseconds() {
		return sc.cached
	}
	ws := statWindows(now)
	since := nowMs
	for _, w := range ws {
		since = min(since, w.Since)
	}

	seen := map[string]bool{}
	for _, c := range append(append(claudeFiles(), codexFiles(since)...), geminiFiles()...) {
		st := statSafe(c.file)
		if st == nil || mtimeMs(st) < since {
			continue
		}
		seen[c.file] = true
		if err := sc.read(c, st); err != nil {
			delete(sc.files, c.file) // try again from the start next time
		}
	}
	for file := range sc.files {
		if !seen[file] {
			delete(sc.files, file)
		}
	}

	for _, f := range sc.files {
		f.prune(since)
		for _, t := range f.tools {
			for i := range ws {
				if t >= ws[i].Since {
					ws[i].Tools++
				}
			}
		}
		if f.session.Subagent {
			continue
		}
		for _, t := range f.turns {
			addTurn(ws, t.at, endedAs(t.status))
		}
		if s := f.session; s.TurnStartedAt != 0 {
			addTurn(ws, s.TurnStartedAt, openAs(*s, nowMs))
		}
	}
	ocStats(ws, since, nowMs)

	sc.at = nowMs
	sc.cached = Stats{Now: nowMs, Windows: ws}
	return sc.cached
}

// read brings one file's counts up to date: the bytes added since the last
// read, or the whole file when it is new, shrank, or is a legacy .json
// document that gets rewritten in place.
func (sc *statsScanner) read(c candidate, st os.FileInfo) error {
	f := sc.files[c.file]
	whole := strings.HasSuffix(c.file, ".json")
	if f != nil && (st.Size() < f.offset || (whole && mtimeMs(st) != f.mtime)) {
		f = nil
	}
	if f == nil {
		s := newSession(c.agent, c.id, c.file)
		s.Subagent = c.parentID != ""
		f = &statFile{session: s, agent: c.agent, seen: map[string]bool{}}
		sc.files[c.file] = f
	}
	f.mtime = mtimeMs(st)
	if st.Size() == f.offset {
		return nil
	}
	if whole {
		text, err := readRange(c.file, 0, st.Size())
		if err != nil {
			return err
		}
		var doc any
		if json.Unmarshal([]byte(text), &doc) == nil {
			f.record(map[string]any{"$set": doc})
		}
		f.offset = st.Size()
		return nil
	}
	const chunk = 8 << 20
	for f.offset < st.Size() {
		end := min(f.offset+chunk, st.Size())
		text, err := readRange(c.file, f.offset, end)
		if err != nil {
			return err
		}
		lines := strings.Split(f.partial+text, "\n")
		f.partial = lines[len(lines)-1]
		for _, line := range lines[:len(lines)-1] {
			var d any
			if line != "" && json.Unmarshal([]byte(line), &d) == nil {
				f.record(d)
			}
		}
		f.offset = end
	}
	return nil
}

// record applies one log record and notes the turn it closed and the tool
// calls it made.
func (f *statFile) record(v any) {
	d := asMap(v)
	if d == nil {
		return
	}
	// A Gemini metadata record can carry the whole message list: take the
	// messages one at a time so each prompt still closes the turn before it.
	if f.agent == "gemini" {
		meta := asMap(d["$set"])
		if meta == nil && truthy(d["sessionId"]) && !truthy(d["type"]) {
			meta = d
		}
		if msgs := asSlice(meta["messages"]); len(msgs) > 0 {
			rest := map[string]any{}
			for k, x := range meta {
				if k != "messages" {
					rest[k] = x
				}
			}
			f.record(map[string]any{"$set": rest})
			for _, m := range msgs {
				f.record(m)
			}
			return
		}
	}
	s := f.session
	turn, status := s.TurnStartedAt, s.Status
	statApply[f.agent](s, d)
	at := parseTime(str(d, "timestamp"))
	if at == 0 {
		at = s.LastEventAt
	}
	for n := toolCalls(f.agent, d, f.seen); n > 0; n-- {
		f.tools = append(f.tools, at)
	}
	if turn != 0 && s.TurnStartedAt != turn {
		f.turns = append(f.turns, statTurn{turn, status})
	}
}

// prune drops what started before the oldest window.
func (f *statFile) prune(since int64) {
	k := 0
	for _, t := range f.turns {
		if t.at >= since {
			f.turns[k] = t
			k++
		}
	}
	f.turns = f.turns[:k]
	k = 0
	for _, t := range f.tools {
		if t >= since {
			f.tools[k] = t
			k++
		}
	}
	f.tools = f.tools[:k]
}

// toolCalls is how many tool calls one record makes.
func toolCalls(agent string, d map[string]any, seen map[string]bool) int {
	n := 0
	switch agent {
	case "claude":
		if str(d, "type") == "assistant" {
			for _, b := range asSlice(asMap(d["message"])["content"]) {
				if str(asMap(b), "type") == "tool_use" {
					n++
				}
			}
		}
	case "codex":
		if str(d, "type") == "response_item" {
			switch str(asMap(d["payload"]), "type") {
			case "function_call", "custom_tool_call", "local_shell_call", "web_search_call":
				n++
			}
		}
	case "gemini":
		if str(d, "type") == "gemini" {
			for _, cv := range asSlice(d["toolCalls"]) {
				if id := str(asMap(cv), "id"); id != "" {
					if seen[id] {
						continue
					}
					seen[id] = true
				}
				n++
			}
		}
	}
	return n
}

// endedAs is how a turn ended, from the status it had when the next prompt
// came: a reply still being written counts as done; one cut off while it was
// thinking, calling a tool or waiting on you is "other".
func endedAs(status string) string {
	switch status {
	case "done", "error", "interrupted":
		return status
	case "typing":
		return "done"
	}
	return "other"
}

// openAs is how the latest turn stands now: still running while the
// dashboard would show it working or waiting on you, otherwise how it ended.
func openAs(s Session, now int64) string {
	switch view(s, now, false).Status {
	case "thinking", "tool", "typing", "waiting":
		return "running"
	}
	return endedAs(s.Status)
}

func addTurn(ws []StatWindow, at int64, outcome string) {
	for i := range ws {
		w := &ws[i]
		if at < w.Since {
			continue
		}
		w.Turns++
		switch outcome {
		case "done":
			w.Done++
		case "error":
			w.Error++
		case "interrupted":
			w.Interrupted++
		case "running":
			w.Running++
		}
	}
}

// OpenCode: each user message is a turn; its last assistant message (they
// point back to it through parentID) says how it ended. Tool calls are parts.
const ocStatsSQL = `with t as (
 select u.time_created as at, s.time_updated as updated, coalesce(s.parent_id, '') != '' as sub,
  (select a.data from message a where a.session_id = u.session_id and json_extract(a.data, '$.parentID') = u.id order by a.time_created desc, a.id desc limit 1) as last
 from message u join session s on s.id = u.session_id
 where u.time_created >= %[1]d and json_extract(u.data, '$.role') = 'user')
select at, updated, sub, coalesce(json_extract(last, '$.error.name'), '') as err, json_extract(last, '$.time.completed') is not null as completed, 0 as tool from t
union all
select time_created, 0, 0, '', 0, 1 from part where time_created >= %[1]d and json_extract(data, '$.type') = 'tool'`

type ocStatRow struct {
	At        int64  `json:"at"`
	Updated   int64  `json:"updated"`
	Sub       int    `json:"sub"`
	Err       string `json:"err"`
	Completed int    `json:"completed"`
	Tool      int    `json:"tool"`
}

func ocStats(ws []StatWindow, since, now int64) {
	if statSafe(ocDB) == nil {
		return
	}
	var rows []ocStatRow
	if sqliteJSON(ocDB, fmt.Sprintf(ocStatsSQL, since), &rows) != nil {
		return
	}
	for _, r := range rows {
		switch {
		case r.Tool == 1:
			for i := range ws {
				if r.At >= ws[i].Since {
					ws[i].Tools++
				}
			}
		case r.Sub == 1:
		case r.Err == "MessageAbortedError":
			addTurn(ws, r.At, "interrupted")
		case r.Err != "":
			addTurn(ws, r.At, "error")
		case r.Completed == 1:
			addTurn(ws, r.At, "done")
		case now-r.Updated < ToolIdleAfter.Milliseconds():
			addTurn(ws, r.At, "running")
		default:
			addTurn(ws, r.At, "other")
		}
	}
}

// demoStats is what --demo shows: made-up numbers that look lived in.
func demoStats(now time.Time) Stats {
	ws := statWindows(now)
	for i, n := range [][6]int{{14, 11, 1, 1, 1, 96}, {52, 44, 3, 2, 1, 410}, {187, 160, 9, 7, 1, 1530}} {
		ws[i].Turns, ws[i].Done, ws[i].Error, ws[i].Interrupted, ws[i].Running, ws[i].Tools = n[0], n[1], n[2], n[3], n[4], n[5]
	}
	return Stats{Now: now.UnixMilli(), Windows: ws}
}
