package main

import (
	"encoding/json"
	"net/http/httptest"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func needSqlite(t *testing.T) {
	t.Helper()
	if _, err := exec.LookPath("sqlite3"); err != nil {
		t.Skip("sqlite3 not installed")
	}
}

func TestAgentNamesAre500DistinctAndNeighboursDiffer(t *testing.T) {
	seen := map[string]bool{}
	for i := 0; i < nameCount; i++ {
		seen[agentName(i)] = true
	}
	if nameCount != 500 || len(seen) != 500 {
		t.Fatalf("want 500 distinct names, got %d of %d", len(seen), nameCount)
	}
	for i := 0; i+1 < nameCount; i++ {
		a, b := strings.Fields(agentName(i)), strings.Fields(agentName(i+1))
		if a[0] == b[0] || a[1] == b[1] {
			t.Fatalf("%v and %v share a name", a, b)
		}
	}
}

func TestSessionNamesStayApartWithinADayAndAreKept(t *testing.T) {
	needSqlite(t)
	db := filepath.Join(t.TempDir(), "office.db")
	at := time.Date(2026, 10, 31, 9, 0, 0, 0, time.Local).UnixMilli()
	views := []View{{Key: "claude:a", StartedAt: &at}, {Key: "codex:b", StartedAt: &at}, {Key: "gemini:c", StartedAt: &at}}
	st := openOfficeStore(db)
	if !st.available() {
		t.Fatal("store did not open")
	}
	snap := Snapshot{Now: at, Sessions: views}
	st.decorate(&snap)
	names := map[string]string{}
	for _, v := range snap.Sessions {
		if v.Name == nil {
			t.Fatalf("%s got no name", v.Key)
		}
		names[v.Key] = *v.Name
	}
	if len(map[string]bool{names["claude:a"]: true, names["codex:b"]: true, names["gemini:c"]: true}) != 3 {
		t.Fatalf("same-day sessions share a name: %v", names)
	}
	// Reopened, the same sessions keep their names.
	again := Snapshot{Now: at + 1000, Sessions: []View{{Key: "codex:b"}, {Key: "claude:a"}}}
	openOfficeStore(db).decorate(&again)
	if *again.Sessions[0].Name != names["codex:b"] || *again.Sessions[1].Name != names["claude:a"] {
		t.Fatalf("names changed after reopening: %v vs %v", names, again.Sessions)
	}
}

func postTodo(s *Server, body string, header bool) (int, map[string]any) {
	req := httptest.NewRequest("POST", "/api/todos", strings.NewReader(body))
	req.Host = "localhost:4545"
	if header {
		req.Header.Set("x-whatsmyagentdoing", "todo")
	}
	rec := httptest.NewRecorder()
	s.ServeHTTP(rec, req)
	var out map[string]any
	json.Unmarshal(rec.Body.Bytes(), &out)
	return rec.Code, out
}

func TestTodoLifecycle(t *testing.T) {
	needSqlite(t)
	now := time.Now().UnixMilli()
	working := Session{Agent: "claude", ID: "a", Status: "thinking", StartedAt: now - 3_600_000, TurnStartedAt: now - 60_000, LastEventAt: now - 1000}
	src := newFakeSource(working)
	db := filepath.Join(t.TempDir(), "office.db")
	s := newTestServer(t, src, Options{DB: db}, nil, nil)

	if code, _ := postTodo(s, `{"op":"add","text":"x"}`, false); code != 403 {
		t.Fatalf("without the header: %d, want 403", code)
	}
	if code, out := postTodo(s, `{"op":"add","text":"  Ship\nthe   list  "}`, true); code != 200 {
		t.Fatalf("add: %d %v", code, out)
	}
	todos := s.snap().Todos
	if len(todos) != 1 || todos[0].Text != "Ship the list" || todos[0].Status != "todo" || todos[0].CreatedAt == 0 {
		t.Fatalf("after add: %+v", todos)
	}
	id := todos[0].ID
	body := func(op string, extra string) string { return `{"op":"` + op + `","id":` + itoa(id) + extra + `}` }

	if code, out := postTodo(s, body("assign", `,"agent":"claude:a"`), true); code != 200 {
		t.Fatalf("assign: %d %v", code, out)
	}
	got := s.snap().Todos[0]
	if got.Status != "doing" || got.Agent == nil || *got.Agent != "claude:a" || got.AgentName == nil || *got.AgentName == "" {
		t.Fatalf("after assign: %+v", got)
	}
	// Assigned mid-turn: the running turn counts, so it finishing finishes the to-do.
	if *got.AssignedAt != working.TurnStartedAt {
		t.Fatalf("assignedAt %d, want the turn start %d", *got.AssignedAt, working.TurnStartedAt)
	}
	finished := working
	finished.Status, finished.LastEventAt = "done", now
	src.set(finished)
	if got := s.snap().Todos[0]; got.Status != "done" {
		t.Fatalf("agent finished, to-do is %q", got.Status)
	}
	// By hand back to doing: the turn that already ended doesn't finish it again.
	postTodo(s, body("status", `,"status":"doing"`), true)
	if got := s.snap().Todos[0]; got.Status != "doing" {
		t.Fatalf("set back to doing, got %q", got.Status)
	}
	if code, _ := postTodo(s, body("status", `,"status":"later"`), true); code != 400 {
		t.Fatal("an unknown status was accepted")
	}
	if code, _ := postTodo(s, body("assign", `,"agent":"claude:gone"`), true); code != 400 {
		t.Fatal("assigning to a missing agent was accepted")
	}
	postTodo(s, body("assign", `,"agent":""`), true)
	if got := s.snap().Todos[0]; got.Agent != nil || got.Status != "todo" {
		t.Fatalf("after taking it back: %+v", got)
	}
	postTodo(s, body("status", `,"status":"done"`), true)

	// Kept in the file: a new store reads the same row.
	reopened := openOfficeStore(db)
	if len(reopened.todos) != 1 || reopened.todos[0].Status != "done" || reopened.todos[0].Text != "Ship the list" {
		t.Fatalf("reopened: %+v", reopened.todos)
	}
	if code, out := postTodo(s, body("delete", ""), true); code != 200 {
		t.Fatalf("delete: %d %v", code, out)
	}
	if todos := s.snap().Todos; todos == nil || len(todos) != 0 {
		t.Fatalf("after delete: %+v", todos)
	}
}

func TestTodoDeleteAnyStatusAndAllDone(t *testing.T) {
	needSqlite(t)
	db := filepath.Join(t.TempDir(), "office.db")
	s := newTestServer(t, newFakeSource(), Options{DB: db}, nil, nil)
	for _, text := range []string{"a", "b", "c", "d"} {
		postTodo(s, `{"op":"add","text":"`+text+`"}`, true)
	}
	ids := map[string]int64{}
	for _, td := range s.snap().Todos {
		ids[td.Text] = td.ID
	}
	postTodo(s, `{"op":"status","id":`+itoa(ids["a"])+`,"status":"done"}`, true)
	postTodo(s, `{"op":"status","id":`+itoa(ids["b"])+`,"status":"done"}`, true)
	postTodo(s, `{"op":"status","id":`+itoa(ids["c"])+`,"status":"paused"}`, true)

	// An unfinished one can be deleted too (the page asks twice).
	if code, out := postTodo(s, `{"op":"delete","id":`+itoa(ids["c"])+`}`, true); code != 200 {
		t.Fatalf("deleting a paused to-do: %d %v", code, out)
	}
	if code, out := postTodo(s, `{"op":"deleteDone"}`, true); code != 200 {
		t.Fatalf("deleteDone: %d %v", code, out)
	}
	todos := s.snap().Todos
	if len(todos) != 1 || todos[0].Text != "d" {
		t.Fatalf("after deleting: %+v", todos)
	}
	if reopened := openOfficeStore(db); len(reopened.todos) != 1 || reopened.todos[0].Text != "d" {
		t.Fatalf("reopened: %+v", reopened.todos)
	}
}

func TestNoDBMeansNoTodos(t *testing.T) {
	s := newTestServer(t, newFakeSource(), Options{}, nil, nil)
	if s.snap().Todos != nil {
		t.Fatal("todos without a database")
	}
	if code, _ := postTodo(s, `{"op":"add","text":"x"}`, true); code != 400 {
		t.Fatalf("add without a database: %d", code)
	}
}

func itoa(n int64) string { b, _ := json.Marshal(n); return string(b) }
