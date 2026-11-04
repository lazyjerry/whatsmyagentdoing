package main

// To-do list and agent names, kept for good in office.db (config.go) and
// written through the sqlite3 command line tool (sqlite.go). Both tables are
// also held in memory: every snapshot reads them, sqlite3 only runs on a
// change. Without sqlite3 the snapshot carries no to-dos (todos: null) and
// sessions get no names; everything else works.

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"
	"unicode"
)

// Todo is one item of the list. Status: todo (not started), doing, done,
// paused (on hold). A to-do can have many agents; an agent has at most one
// to-do (assigning it elsewhere moves it).
type Todo struct {
	ID        int64       `json:"id"`
	Text      string      `json:"text"`
	Status    string      `json:"status"`
	Agents    []TodoAgent `json:"agents"` // in the order they were assigned
	CreatedAt int64       `json:"createdAt"`
	UpdatedAt int64       `json:"updatedAt"`
}

// TodoAgent is one agent on a to-do. The slice holding it is never changed
// in place: snapshots handed out share it.
type TodoAgent struct {
	Key        string  `json:"key"`        // session key
	Name       *string `json:"name"`       // its name when assigned, kept after the session leaves the window
	AssignedAt int64   `json:"assignedAt"` // only its turns ending after this count
}

var todoStatuses = map[string]bool{"todo": true, "doing": true, "done": true, "paused": true}

const todoColumns = "id, text, status, created_at AS createdAt, updated_at AS updatedAt"

// todos.agent_key / agent_name / assigned_at held the one agent of a to-do
// before todo_agents; opening the database moves them over (the agent's
// latest to-do wins) and clears them.
const officeSchema = `CREATE TABLE IF NOT EXISTS todos (
  id INTEGER PRIMARY KEY AUTOINCREMENT,
  text TEXT NOT NULL,
  status TEXT NOT NULL DEFAULT 'todo',
  agent_key TEXT,
  agent_name TEXT,
  assigned_at INTEGER,
  created_at INTEGER NOT NULL,
  updated_at INTEGER NOT NULL
);
CREATE TABLE IF NOT EXISTS agent_names (
  agent_key TEXT PRIMARY KEY,
  name TEXT NOT NULL,
  day TEXT NOT NULL,
  created_at INTEGER NOT NULL
);
CREATE TABLE IF NOT EXISTS todo_agents (
  agent_key TEXT PRIMARY KEY,
  todo_id INTEGER NOT NULL,
  agent_name TEXT,
  assigned_at INTEGER NOT NULL
);
INSERT OR REPLACE INTO todo_agents (agent_key, todo_id, agent_name, assigned_at)
  SELECT agent_key, id, agent_name, COALESCE(assigned_at, updated_at) FROM todos WHERE agent_key IS NOT NULL ORDER BY updated_at, id;
UPDATE todos SET agent_key = NULL, agent_name = NULL, assigned_at = NULL WHERE agent_key IS NOT NULL;`

// Agent names: 25 first names × 20 surnames common in Taiwanese offices,
// 500 combinations. A name is a loose alias for a session key, not an id.
var (
	nameFirst = [...]string{"Jerry", "Kevin", "Jason", "Eric", "Allen", "Vincent", "Peter", "Tony", "Andy", "Jack", "Leo", "Ryan", "Sam", "Ken", "Steven",
		"Amy", "Vivian", "Grace", "Iris", "Cindy", "Emily", "Joyce", "Tina", "Wendy", "Annie"}
	nameLast = [...]string{"Chen", "Lin", "Huang", "Chang", "Lee", "Wang", "Wu", "Liu", "Tsai", "Yang",
		"Hsu", "Cheng", "Hsieh", "Hung", "Kuo", "Chiu", "Tseng", "Liao", "Lai", "Chou"}
)

const nameCount = len(nameFirst) * len(nameLast)

// agentName is name i of the 500. Neighbours differ in both first name and
// surname, so agents started one after another don't look alike.
func agentName(i int) string {
	f, k := i%len(nameFirst), i/len(nameFirst)
	return nameFirst[f] + " " + nameLast[(k+f)%len(nameLast)]
}

type officeStore struct {
	path  string
	mu    sync.Mutex
	ok    bool // tables exist and were read
	todos []Todo
	names map[string]string          // session key → name
	used  map[string]map[string]bool // local day → names given out that day
}

func openOfficeStore(path string) *officeStore {
	st := &officeStore{path: path, names: map[string]string{}, used: map[string]map[string]bool{}}
	if err := st.load(); err != nil {
		if !errors.Is(err, errNoSqlite) {
			fmt.Fprintln(os.Stderr, "to-do list off:", err)
		}
		return st
	}
	st.ok = true
	return st
}

func (st *officeStore) load() error {
	if err := os.MkdirAll(filepath.Dir(st.path), 0o700); err != nil {
		return err
	}
	if err := sqliteWrite(st.path, officeSchema, nil); err != nil {
		return err
	}
	var todos []Todo
	if err := sqliteWrite(st.path, "SELECT "+todoColumns+" FROM todos ORDER BY id;", &todos); err != nil {
		return err
	}
	var names []struct {
		Key  string `json:"k"`
		Name string `json:"name"`
		Day  string `json:"day"`
	}
	if err := sqliteWrite(st.path, "SELECT agent_key AS k, name, day FROM agent_names;", &names); err != nil {
		return err
	}
	var links []struct {
		TodoAgent
		TodoID int64 `json:"todoId"`
	}
	if err := sqliteWrite(st.path, "SELECT agent_key AS key, todo_id AS todoId, agent_name AS name, assigned_at AS assignedAt FROM todo_agents ORDER BY assigned_at, agent_key;", &links); err != nil {
		return err
	}
	for i := range todos {
		todos[i].Agents = []TodoAgent{}
		for _, l := range links {
			if l.TodoID == todos[i].ID {
				todos[i].Agents = append(todos[i].Agents, l.TodoAgent)
			}
		}
	}
	st.todos = append([]Todo{}, todos...)
	for _, n := range names {
		st.names[n.Key] = n.Name
		st.mark(n.Day, n.Name)
	}
	return nil
}

func (st *officeStore) available() bool {
	st.mu.Lock()
	defer st.mu.Unlock()
	return st.ok
}

func (st *officeStore) mark(day, name string) {
	if st.used[day] == nil {
		st.used[day] = map[string]bool{}
	}
	st.used[day][name] = true
}

// decorate adds names and to-dos to a snapshot, and finishes the to-dos
// whose agents finished.
func (st *officeStore) decorate(snap *Snapshot) {
	st.mu.Lock()
	defer st.mu.Unlock()
	if !st.ok {
		return
	}
	st.nameSessions(snap.Sessions, snap.Now)
	st.finish(snap.Sessions, snap.Now)
	snap.Todos = append([]Todo{}, st.todos...)
}

// nameSessions gives each session a name the first time it shows up. The
// slot is the second it was created, mod 500, moved on past names already
// given out that local day, so names repeat only across days (or past 500
// sessions in one day). Saved in agent_names, a name never changes.
func (st *officeStore) nameSessions(views []View, now int64) {
	type fresh struct {
		i  int
		at int64
	}
	var news []fresh
	for i := range views {
		if name, ok := st.names[views[i].Key]; ok {
			views[i].Name = nstr(name)
			continue
		}
		at := now
		if views[i].StartedAt != nil {
			at = *views[i].StartedAt
		}
		news = append(news, fresh{i, at})
	}
	if len(news) == 0 {
		return
	}
	// Earlier sessions pick first, so a batch names the same way every time.
	sort.SliceStable(news, func(a, b int) bool { return news[a].at < news[b].at })
	var sql strings.Builder
	for _, n := range news {
		v := &views[n.i]
		day := time.UnixMilli(n.at).Format("2006-01-02")
		slot := int((n.at / 1000) % int64(nameCount))
		name := ""
		for k := 0; k < nameCount; k++ {
			name = agentName((slot + k) % nameCount)
			if !st.used[day][name] {
				break
			}
		}
		st.names[v.Key] = name
		st.mark(day, name)
		v.Name = nstr(name)
		fmt.Fprintf(&sql, "INSERT OR IGNORE INTO agent_names VALUES (%s, %s, %s, %d);\n", sqlQuote(v.Key), sqlQuote(name), sqlQuote(day), n.at)
	}
	if err := sqliteWrite(st.path, sql.String(), nil); err != nil {
		fmt.Fprintln(os.Stderr, "agent names not saved:", err)
	}
}

// finish marks a "doing" to-do done once every one of its agents still in
// the window has finished a turn that ended after it was assigned (agents
// that left the window have no say; at least one must have finished).
func (st *officeStore) finish(views []View, now int64) {
	byKey := make(map[string]*View, len(views))
	for i := range views {
		byKey[views[i].Key] = &views[i]
	}
	var ids []string
	for _, t := range st.todos {
		if t.Status != "doing" {
			continue
		}
		finished, busy := false, false
		for _, a := range t.Agents {
			v := byKey[a.Key]
			if v == nil {
				continue
			}
			if v.Status == "done" && v.LastEventAt != nil && *v.LastEventAt > a.AssignedAt {
				finished = true
			} else {
				busy = true
			}
		}
		if !finished || busy {
			continue
		}
		ids = append(ids, strconv.FormatInt(t.ID, 10))
	}
	if len(ids) == 0 {
		return
	}
	sql := fmt.Sprintf("UPDATE todos SET status = 'done', updated_at = %d WHERE id IN (%s);", now, strings.Join(ids, ","))
	if err := sqliteWrite(st.path, sql, nil); err != nil {
		fmt.Fprintln(os.Stderr, "to-dos not finished:", err)
		return
	}
	done := map[string]bool{}
	for _, id := range ids {
		done[id] = true
	}
	for i := range st.todos {
		if done[strconv.FormatInt(st.todos[i].ID, 10)] {
			st.todos[i].Status, st.todos[i].UpdatedAt = "done", now
		}
	}
}

// cleanTodoText keeps one line: line breaks and tabs become spaces, other
// control characters go, runs of spaces shrink to one.
func cleanTodoText(s string) string {
	s = strings.Map(func(r rune) rune {
		if r == '\n' || r == '\r' || r == '\t' {
			return ' '
		}
		if unicode.IsControl(r) {
			return -1
		}
		return r
	}, s)
	s = strings.Join(strings.Fields(s), " ")
	if r := []rune(s); len(r) > TodoTextMax {
		s = string(r[:TodoTextMax])
	}
	return s
}

func sqlText(p *string) string {
	if p == nil {
		return "NULL"
	}
	return sqlQuote(*p)
}

func (st *officeStore) find(id int64) int {
	for i, t := range st.todos {
		if t.ID == id {
			return i
		}
	}
	return -1
}

// holder is the to-do the agent key is on (-1: none).
func (st *officeStore) holder(key string) int {
	for i, t := range st.todos {
		for _, a := range t.Agents {
			if a.Key == key {
				return i
			}
		}
	}
	return -1
}

// withoutAgent is a new slice: agents minus key.
func withoutAgent(agents []TodoAgent, key string) []TodoAgent {
	out := make([]TodoAgent, 0, len(agents))
	for _, a := range agents {
		if a.Key != key {
			out = append(out, a)
		}
	}
	return out
}

// dropAgent takes key off t (a copy): one left with nobody stops being "doing".
func dropAgent(t Todo, key string, now int64) Todo {
	t.Agents, t.UpdatedAt = withoutAgent(t.Agents, key), now
	if len(t.Agents) == 0 && t.Status == "doing" {
		t.Status = "todo"
	}
	return t
}

func todoRowSQL(t Todo) string {
	return fmt.Sprintf("UPDATE todos SET status = %s, updated_at = %d WHERE id = %d;\n", sqlQuote(t.Status), t.UpdatedAt, t.ID)
}

// save writes sql and, once that worked, the changed to-dos over their copies
// in memory (by index).
func (st *officeStore) save(sql string, changed map[int]Todo) error {
	if err := sqliteWrite(st.path, sql, nil); err != nil {
		return err
	}
	for i, t := range changed {
		st.todos[i] = t
	}
	return nil
}

func (st *officeStore) add(text, status string, now int64) error {
	st.mu.Lock()
	defer st.mu.Unlock()
	if text = cleanTodoText(text); text == "" {
		return errors.New("the to-do is empty")
	}
	if status == "" {
		status = "todo"
	}
	if !todoStatuses[status] || status == "doing" {
		return errors.New("bad status: " + status)
	}
	var rows []struct {
		ID int64 `json:"id"`
	}
	sql := fmt.Sprintf("INSERT INTO todos (text, status, created_at, updated_at) VALUES (%s, %s, %d, %d);\nSELECT last_insert_rowid() AS id;",
		sqlQuote(text), sqlQuote(status), now, now)
	if err := sqliteWrite(st.path, sql, &rows); err != nil {
		return err
	}
	if len(rows) != 1 {
		return errors.New("the to-do was not saved")
	}
	st.todos = append(st.todos, Todo{ID: rows[0].ID, Text: text, Status: status, Agents: []TodoAgent{}, CreatedAt: now, UpdatedAt: now})
	return nil
}

func (st *officeStore) setStatus(id int64, status string, now int64) error {
	st.mu.Lock()
	defer st.mu.Unlock()
	i := st.find(id)
	if i < 0 {
		return errors.New("to-do not found")
	}
	if !todoStatuses[status] {
		return errors.New("bad status: " + status)
	}
	t := st.todos[i]
	sql := ""
	// Back to "doing" by hand: only turns ending from now on finish it,
	// not the ones that already did.
	if status == "doing" && t.Status != "doing" && len(t.Agents) > 0 {
		agents := make([]TodoAgent, len(t.Agents))
		for k, a := range t.Agents {
			a.AssignedAt = now
			agents[k] = a
		}
		t.Agents = agents
		sql = fmt.Sprintf("UPDATE todo_agents SET assigned_at = %d WHERE todo_id = %d;\n", now, t.ID)
	}
	t.Status, t.UpdatedAt = status, now
	return st.save(sql+todoRowSQL(t), map[int]Todo{i: t})
}

// assign puts the session v on the to-do, taking it off the one it was on
// (an agent has one to-do at a time). Assigning mid-turn counts the running
// turn: it ending can finish the to-do.
func (st *officeStore) assign(id int64, v *View, now int64) error {
	st.mu.Lock()
	defer st.mu.Unlock()
	i := st.find(id)
	if i < 0 {
		return errors.New("to-do not found")
	}
	at := now
	if (v.Status == "thinking" || v.Status == "tool" || v.Status == "typing") && v.TurnStartedAt != nil {
		at = *v.TurnStartedAt
	}
	changed := map[int]Todo{}
	sql := ""
	if j := st.holder(v.Key); j >= 0 && j != i {
		old := dropAgent(st.todos[j], v.Key, now)
		changed[j] = old
		sql += todoRowSQL(old)
	}
	t := st.todos[i]
	a := TodoAgent{Key: v.Key, Name: nstr(st.names[v.Key]), AssignedAt: at}
	t.Agents = append(withoutAgent(t.Agents, v.Key), a)
	t.Status, t.UpdatedAt = "doing", now
	changed[i] = t
	sql += fmt.Sprintf("INSERT OR REPLACE INTO todo_agents (agent_key, todo_id, agent_name, assigned_at) VALUES (%s, %d, %s, %d);\n",
		sqlQuote(a.Key), t.ID, sqlText(a.Name), a.AssignedAt)
	return st.save(sql+todoRowSQL(t), changed)
}

// unassign takes the agent key off the to-do.
func (st *officeStore) unassign(id int64, key string, now int64) error {
	st.mu.Lock()
	defer st.mu.Unlock()
	i := st.find(id)
	if i < 0 {
		return errors.New("to-do not found")
	}
	if st.holder(key) != i {
		return errors.New("the agent is not on this to-do")
	}
	t := dropAgent(st.todos[i], key, now)
	sql := fmt.Sprintf("DELETE FROM todo_agents WHERE agent_key = %s;\n", sqlQuote(key))
	return st.save(sql+todoRowSQL(t), map[int]Todo{i: t})
}

// remove deletes a to-do in any status; the page asks first (twice for an
// unfinished one).
func (st *officeStore) remove(id int64) error {
	st.mu.Lock()
	defer st.mu.Unlock()
	i := st.find(id)
	if i < 0 {
		return errors.New("to-do not found")
	}
	if err := sqliteWrite(st.path, fmt.Sprintf("DELETE FROM todo_agents WHERE todo_id = %d;\nDELETE FROM todos WHERE id = %d;", id, id), nil); err != nil {
		return err
	}
	st.todos = append(st.todos[:i], st.todos[i+1:]...)
	return nil
}

// removeDone deletes every finished to-do at once.
func (st *officeStore) removeDone() error {
	st.mu.Lock()
	defer st.mu.Unlock()
	if err := sqliteWrite(st.path, "DELETE FROM todo_agents WHERE todo_id IN (SELECT id FROM todos WHERE status = 'done');\nDELETE FROM todos WHERE status = 'done';", nil); err != nil {
		return err
	}
	kept := st.todos[:0]
	for _, t := range st.todos {
		if t.Status != "done" {
			kept = append(kept, t)
		}
	}
	st.todos = kept
	return nil
}

type todoRequest struct {
	Op     string `json:"op"` // add | status | assign | unassign | delete | deleteDone
	ID     int64  `json:"id"`
	Text   string `json:"text"`
	Status string `json:"status"`
	Agent  string `json:"agent"` // session key
}

// serveTodo changes the list, then pushes a snapshot so every open page
// (and the PiP window) shows it.
func (s *Server) serveTodo(w http.ResponseWriter, r *http.Request) {
	fail := func(msg string) { writeJSON(w, 400, map[string]any{"ok": false, "error": msg}, false) }
	if s.store == nil || !s.store.available() {
		fail("the to-do list needs the sqlite3 command")
		return
	}
	var req todoRequest
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 16*1024)).Decode(&req); err != nil {
		fail("bad request")
		return
	}
	now := time.Now().UnixMilli()
	var err error
	switch req.Op {
	case "add":
		err = s.store.add(req.Text, req.Status, now)
	case "status":
		err = s.store.setStatus(req.ID, req.Status, now)
	case "assign":
		var v *View
		for _, x := range s.snap().Sessions {
			if x.Key == req.Agent {
				v = &x
				break
			}
		}
		if v == nil {
			fail("agent not found: " + req.Agent)
			return
		}
		err = s.store.assign(req.ID, v, now)
	case "unassign":
		err = s.store.unassign(req.ID, req.Agent, now)
	case "delete":
		err = s.store.remove(req.ID)
	case "deleteDone":
		err = s.store.removeDone()
	default:
		err = errors.New("unknown op: " + req.Op)
	}
	if err != nil {
		fail(err.Error())
		return
	}
	writeJSON(w, 200, map[string]any{"ok": true}, false)
	s.push(true)
}
