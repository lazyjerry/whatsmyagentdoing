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
	"errors"
	"io"
	"maps"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strconv"
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

// --- Live print runs (`agy -p`)
//
// A CLI run's row says IDLE from its first write to its last, so the only
// "still working" signal is the process itself. Each run writes its own log
// under ~/.gemini/antigravity-cli/log: the first line names the pid and, a few
// seconds in, a print run names its conversation. That conversation counts as
// working while the pid is alive and still an agy process. The live set is
// rebuilt every tick and nothing is latched, so a run that ends (or is killed)
// falls back to its row's own status on the next tick. A long run may not
// write its row until it ends; until then it shows as a stand-in session built
// from the log (workspace and start time, no prompt).

var agyLogDir = filepath.Join(geminiHomes[0], "antigravity-cli", "log")

const (
	agyLogHead  = 1 << 20         // the pid and conversation lines sit near the top
	agyConvWait = 2 * time.Minute // a log that hasn't named a conversation by now never will
)

var (
	reAgyPid  = regexp.MustCompile(`language server process with pid (\d+)`)
	reAgyConv = regexp.MustCompile(`Print mode: conversation=([0-9a-f-]{36})`)
	reAgyCwd  = regexp.MustCompile(`(?m)CLI store manager for workspace (.+)$`)
)

// agyRun is what one CLI log has said so far.
type agyRun struct {
	start int64 // from the file name; the newest log wins when a pid repeats
	size  int64 // bytes seen at the last read
	pid   int
	conv  string
	cwd   string
	dead  bool // process gone, or the log gave up on naming a conversation
}

// agyRuns follows the CLI logs and knows which conversations are running.
type agyRuns struct {
	dirMtime int64
	logs     map[string]*agyRun // log path -> what it said
	live     map[string]*agyRun // conversation id -> its run, while the process is running
}

func newAgyRuns() *agyRuns {
	return &agyRuns{logs: map[string]*agyRun{}, live: map[string]*agyRun{}}
}

// agyLogStart parses the start time out of "cli-20261030_233139.log".
func agyLogStart(name string) int64 {
	stamp, ok := strings.CutPrefix(name, "cli-")
	stamp, ok2 := strings.CutSuffix(stamp, ".log")
	if !ok || !ok2 {
		return 0
	}
	t, err := time.ParseInLocation("20060102_150405", stamp, time.Local)
	if err != nil {
		return 0
	}
	return t.UnixMilli()
}

// read picks the pid and conversation out of a log that grew since last time.
func (r *agyRun) read(path string, now int64) {
	if st := statSafe(path); st != nil && st.Size() != r.size {
		r.size = st.Size()
		if f, err := os.Open(path); err == nil {
			head, _ := io.ReadAll(io.LimitReader(f, agyLogHead))
			f.Close()
			if m := reAgyPid.FindSubmatch(head); m != nil {
				r.pid, _ = strconv.Atoi(string(m[1]))
			}
			if m := reAgyCwd.FindSubmatch(head); m != nil {
				r.cwd = strings.TrimSpace(string(m[1]))
			}
			if m := reAgyConv.FindSubmatch(head); m != nil && r.pid > 0 {
				r.conv = string(m[1])
			}
		}
	}
	if r.conv == "" && now-r.start > agyConvWait.Milliseconds() {
		r.dead = true
	}
}

// load refreshes the live set; returns true when it changed.
func (a *agyRuns) load(now, since int64) bool {
	if st := statSafe(agyLogDir); st != nil && mtimeMs(st) != a.dirMtime {
		a.dirMtime = mtimeMs(st)
		entries, _ := os.ReadDir(agyLogDir)
		for _, e := range entries {
			path := filepath.Join(agyLogDir, e.Name())
			if start := agyLogStart(e.Name()); start >= since && a.logs[path] == nil {
				a.logs[path] = &agyRun{start: start}
			}
		}
	}
	newest := map[int]*agyRun{}
	for path, r := range a.logs {
		if r.start < since {
			delete(a.logs, path)
			continue
		}
		if r.conv == "" && !r.dead {
			r.read(path, now)
		}
		if r.conv == "" || r.dead {
			continue
		}
		if n := newest[r.pid]; n == nil || r.start > n.start {
			newest[r.pid] = r
		}
	}
	live := map[string]*agyRun{}
	if len(newest) > 0 {
		pids := make([]int, 0, len(newest))
		for pid := range newest {
			pids = append(pids, pid)
		}
		// ps failing says nothing about the runs: none counts as live this
		// tick, none is written off.
		if alive, err := agyAlive(pids); err == nil {
			for pid, r := range newest {
				if alive[pid] {
					live[r.conv] = r
				} else {
					r.dead = true
				}
			}
		}
	}
	changed := !maps.Equal(live, a.live)
	a.live = live
	return changed
}

// pending returns a stand-in session for each live run whose row isn't in
// rows yet.
func (a *agyRuns) pending(rows map[string]*Session) []*Session {
	var out []*Session
	for conv, r := range a.live {
		if rows[conv] != nil {
			continue
		}
		s := newSession("gemini", conv, agyDB)
		s.SessionID = conv
		s.Tool = "agy"
		s.Platform = "cli"
		s.Cwd = r.cwd
		s.Workspace = r.cwd
		s.LastEventAt = r.start
		s.Mtime = r.start
		s.TurnStartedAt = r.start
		s.Status = "thinking"
		out = append(out, s)
	}
	return out
}

// agyAlive reports which pids are still agy processes; a pid handed to some
// other program after the run ended doesn't count. Overridable in tests.
var agyAlive = func(pids []int) (map[int]bool, error) {
	list := make([]string, len(pids))
	for i, pid := range pids {
		list[i] = strconv.Itoa(pid)
	}
	out, err := exec.Command("ps", "-o", "pid=,comm=", "-p", strings.Join(list, ",")).Output()
	// Some ps builds exit 1 when a listed pid is gone; what they print stands.
	var exit *exec.ExitError
	if err != nil && !errors.As(err, &exit) {
		return nil, err
	}
	alive := map[int]bool{}
	for _, line := range strings.Split(string(out), "\n") {
		f := strings.Fields(line)
		if len(f) < 2 || filepath.Base(strings.Join(f[1:], " ")) != "agy" {
			continue
		}
		if pid, err := strconv.Atoi(f[0]); err == nil {
			alive[pid] = true
		}
	}
	return alive, nil
}
