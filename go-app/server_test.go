package main

import (
	"encoding/json"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// --- view() / snapshot()

func TestViewAgesSilentSessions(t *testing.T) {
	now := time.Now().UnixMilli()
	cases := []struct {
		status string
		quiet  time.Duration
		want   string
	}{
		{"typing", 44 * time.Second, "typing"},
		{"typing", 46 * time.Second, "done"},
		{"thinking", 14 * time.Minute, "thinking"},
		{"thinking", 16 * time.Minute, "idle"},
		{"tool", 44 * time.Minute, "tool"},
		{"tool", 46 * time.Minute, "idle"},
		{"waiting", 119 * time.Minute, "waiting"},
		{"waiting", 121 * time.Minute, "idle"},
		{"error", 121 * time.Minute, "idle"},
		{"done", 48 * time.Hour, "done"}, // done never ages
	}
	for _, c := range cases {
		s := Session{Agent: "claude", ID: "x", Status: c.status, LastEventAt: now - c.quiet.Milliseconds()}
		if got := view(s, now, false).Status; got != c.want {
			t.Errorf("%s quiet %v: got %s, want %s", c.status, c.quiet, got, c.want)
		}
	}
}

func TestViewFieldsAndNulls(t *testing.T) {
	now := time.Now().UnixMilli()
	s := Session{Agent: "codex", ID: "abc", Cwd: "/w/my-app", Status: "tool", Activity: "Shell: ls", Prompt: "p", Reply: "r", Title: "t", Mtime: now - 1000}
	v := view(s, now, false)
	if v.Key != "codex:abc" || v.Project == nil || *v.Project != "my-app" {
		t.Errorf("key/project: %+v", v)
	}
	if v.Activity == nil || *v.Activity != "Shell: ls" {
		t.Errorf("activity shown while tool: %+v", v)
	}
	if v.LastEventAt == nil || *v.LastEventAt != now-1000 {
		t.Errorf("LastEventAt falls back to Mtime: %+v", v)
	}
	if v.SessionID != nil || v.ParentID != nil || v.Model != nil || v.Error != nil || v.TurnStartedAt != nil || v.StartedAt != nil {
		t.Errorf("unset fields must be nil: %+v", v)
	}
	b, _ := json.Marshal(v)
	if !strings.Contains(string(b), `"sessionId":null`) || !strings.Contains(string(b), `"model":null`) {
		t.Errorf("nil pointers must serialise as null: %s", b)
	}

	s.Status = "thinking"
	if view(s, now, false).Activity != nil {
		t.Error("activity hidden unless tool/waiting")
	}
	s.Status = "waiting"
	if view(s, now, false).Activity == nil {
		t.Error("activity shown while waiting")
	}
	empty := view(Session{Agent: "a", ID: "b"}, now, false)
	if empty.Cwd != nil || empty.Project != nil || empty.LastEventAt != nil {
		t.Errorf("no cwd, no mtime: %+v", empty)
	}
}

func TestViewPrivacy(t *testing.T) {
	now := time.Now().UnixMilli()
	s := Session{Agent: "claude", ID: "x", Status: "tool", Activity: "Bash: npm test", Prompt: "secret", Reply: "secret", Title: "secret", Error: "err", Cwd: "/w/proj", LastEventAt: now}
	v := view(s, now, true)
	if v.Prompt != nil || v.Reply != nil || v.Title != nil {
		t.Errorf("prompt/reply/title must be hidden: %+v", v)
	}
	if v.Activity == nil || *v.Activity != "Bash" {
		t.Errorf("activity keeps the first word only: %+v", v)
	}
	if v.Error == nil || v.Project == nil || v.Status != "tool" {
		t.Errorf("error, project and status stay: %+v", v)
	}
	s.Activity = "github · create_pull_request"
	if got := *view(s, now, true).Activity; got != "github" {
		t.Errorf("space-separated activity: %q", got)
	}
}

func TestSnapshotSortsAndGuessesLanguage(t *testing.T) {
	now := time.Now().UnixMilli()
	src := newFakeSource(
		Session{Agent: "claude", ID: "old", Status: "done", LastEventAt: now - 60_000, Prompt: "請幫我修正這個錯誤"},
		Session{Agent: "codex", ID: "new", Status: "thinking", LastEventAt: now - 1000, Prompt: "然後把測試補上去"},
		Session{Agent: "gemini", ID: "sub", Status: "idle", LastEventAt: now - 500, Subagent: true, Prompt: "please fix the bug and make it work"},
	)
	snap := snapshot(src, Options{})
	if len(snap.Sessions) != 3 || snap.Sessions[0].ID != "sub" || snap.Sessions[1].ID != "new" || snap.Sessions[2].ID != "old" {
		t.Errorf("order by LastEventAt desc: %v", ids(snap))
	}
	if snap.Lang == nil || *snap.Lang != "zh-TW" {
		t.Errorf("subagent prompts don't vote; lang = %v", snap.Lang)
	}
	if snapshot(src, Options{Demo: true}).Lang != nil {
		t.Error("demo snapshots carry no lang")
	}
	if snap.Now < now {
		t.Error("now must be fresh")
	}
	if got := snapshot(newFakeSource(), Options{}).Sessions; got == nil || len(got) != 0 {
		t.Errorf("empty source gives [] not null: %v", got)
	}
}

func ids(s Snapshot) []string {
	var out []string
	for _, v := range s.Sessions {
		out = append(out, v.ID)
	}
	return out
}

// --- push dedupe

func TestPushSkipsUnchangedSnapshots(t *testing.T) {
	noNetwork(t)
	src := newFakeSource(Session{Agent: "claude", ID: "a", Status: "thinking", LastEventAt: time.Now().UnixMilli()})
	s := NewServer(src, Options{}, NewUpdater(), nil, nil)
	defer s.Close()
	ch := make(chan string, 8)
	s.mu.Lock()
	s.clients[ch] = struct{}{}
	s.mu.Unlock()

	s.push(false)
	s.push(false) // same content, only `now` differs
	if len(ch) != 1 {
		t.Fatalf("expected exactly one frame, got %d", len(ch))
	}
	src.set(Session{Agent: "claude", ID: "a", Status: "tool", LastEventAt: time.Now().UnixMilli()})
	s.push(false)
	if len(ch) != 2 {
		t.Fatalf("changed status must push, got %d frames", len(ch))
	}
	s.push(true)
	if len(ch) != 3 {
		t.Fatalf("force pushes even when unchanged, got %d", len(ch))
	}
	// A full buffer drops frames instead of blocking.
	for i := 0; i < 10; i++ {
		s.push(true)
	}
	if len(ch) != 8 {
		t.Fatalf("buffer is 8, got %d", len(ch))
	}
}

func TestBroadcastLoopReactsToChanges(t *testing.T) {
	noNetwork(t)
	src := newFakeSource(Session{Agent: "claude", ID: "a", Status: "thinking", LastEventAt: time.Now().UnixMilli()})
	s := NewServer(src, Options{}, NewUpdater(), nil, nil)
	defer s.Close()
	ch := make(chan string, 8)
	s.mu.Lock()
	s.clients[ch] = struct{}{}
	s.mu.Unlock()
	src.signal()
	select {
	case body := <-ch:
		if !strings.Contains(body, `"status":"thinking"`) {
			t.Errorf("unexpected frame: %s", body)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("broadcastLoop did not push after a change signal")
	}
}

func TestCloseIsIdempotentAndClosesClients(t *testing.T) {
	noNetwork(t)
	s := NewServer(newFakeSource(), Options{}, NewUpdater(), nil, nil)
	ch := make(chan string, 1)
	s.mu.Lock()
	s.clients[ch] = struct{}{}
	s.mu.Unlock()
	s.Close()
	s.Close()
	if _, ok := <-ch; ok {
		t.Error("client channel must be closed")
	}
	s.closeClients() // nothing left, must not panic
}

// --- handlers (recorder based; SSE is covered in functional_test.go)

func newTestServer(t *testing.T, src Source, opts Options, onShutdown, onRestart func()) *Server {
	t.Helper()
	noNetwork(t)
	s := NewServer(src, opts, NewUpdater(), onShutdown, onRestart)
	t.Cleanup(s.Close)
	return s
}

func do(s *Server, method, target string, header map[string]string) *httptest.ResponseRecorder {
	req := httptest.NewRequest(method, target, nil)
	req.Host = "localhost:4545"
	for k, v := range header {
		req.Header.Set(k, v)
	}
	rec := httptest.NewRecorder()
	s.ServeHTTP(rec, req)
	return rec
}

func TestHostAllowlist(t *testing.T) {
	s := newTestServer(t, newFakeSource(), Options{}, nil, nil)
	for _, host := range []string{"localhost:4545", "127.0.0.1:4545", "[::1]:4545", "localhost"} {
		req := httptest.NewRequest("GET", "/api/ping", nil)
		req.Host = host
		rec := httptest.NewRecorder()
		s.ServeHTTP(rec, req)
		if rec.Code != 200 {
			t.Errorf("host %q: %d", host, rec.Code)
		}
	}
	for _, host := range []string{"evil.com", "evil.com:4545", "127.0.0.1.nip.io:4545", ""} {
		req := httptest.NewRequest("GET", "/api/ping", nil)
		req.Host = host
		rec := httptest.NewRecorder()
		s.ServeHTTP(rec, req)
		if rec.Code != 403 {
			t.Errorf("host %q: %d, want 403", host, rec.Code)
		}
	}
}

func TestStaticAndAssets(t *testing.T) {
	s := newTestServer(t, newFakeSource(), Options{}, nil, nil)
	cases := []struct {
		path, typ string
	}{
		{"/", "text/html; charset=utf-8"},
		{"/office.js", "text/javascript; charset=utf-8"},
		{"/office3d.js", "text/javascript; charset=utf-8"},
		{"/i18n.js", "text/javascript; charset=utf-8"},
		{"/popout.js", "text/javascript; charset=utf-8"},
		{"/LICENSE", "text/plain; charset=utf-8"},
		{"/assets/agents/claude.png", "image/png"},
		{"/assets/kenney/furniture/desk.glb", "model/gltf-binary"},
	}
	for _, c := range cases {
		rec := do(s, "GET", c.path, nil)
		if rec.Code != 200 || rec.Header().Get("Content-Type") != c.typ || rec.Body.Len() == 0 {
			t.Errorf("%s: %d %q (%d bytes)", c.path, rec.Code, rec.Header().Get("Content-Type"), rec.Body.Len())
		}
	}
	if cc := do(s, "GET", "/", nil).Header().Get("Cache-Control"); cc != "no-store" {
		t.Errorf("page cache: %q", cc)
	}
	if cc := do(s, "GET", "/assets/agents/claude.png", nil).Header().Get("Cache-Control"); cc != "no-cache" {
		t.Errorf("asset cache: %q", cc)
	}
	if !strings.Contains(strings.ToLower(do(s, "GET", "/", nil).Body.String()), "<html") {
		t.Error("index.html must be served from the embedded tree")
	}
	for _, p := range []string{
		"/assets/../scenes.json", "/assets/../VERSION", "/assets/..%2Fscenes.json",
		"/assets/", "/assets/agents/", "/assets/agents/README.md", "/assets/nope.png",
		"/scenes.json", "/VERSION", "/static/VERSION", "/nope",
	} {
		if rec := do(s, "GET", p, nil); rec.Code != 404 {
			t.Errorf("%s: %d, want 404", p, rec.Code)
		}
	}
}

func TestApiPingAndSessions(t *testing.T) {
	now := time.Now().UnixMilli()
	src := newFakeSource(Session{Agent: "claude", ID: "a", Status: "tool", Activity: "Bash: ls", Cwd: "/w/p", LastEventAt: now})
	s := newTestServer(t, src, Options{}, nil, nil)

	rec := do(s, "GET", "/api/ping", nil)
	if rec.Code != 200 || !strings.Contains(rec.Body.String(), `"app":"whatsmyagentdoing"`) {
		t.Errorf("ping: %d %s", rec.Code, rec.Body.String())
	}

	rec = do(s, "GET", "/api/sessions", nil)
	var snap Snapshot
	if err := json.Unmarshal(rec.Body.Bytes(), &snap); err != nil || rec.Code != 200 {
		t.Fatalf("sessions: %d %v %s", rec.Code, err, rec.Body.String())
	}
	if len(snap.Sessions) != 1 || snap.Sessions[0].Key != "claude:a" || *snap.Sessions[0].Activity != "Bash: ls" {
		t.Errorf("sessions body: %+v", snap)
	}

	priv := newTestServer(t, src, Options{Privacy: true}, nil, nil)
	rec = do(priv, "GET", "/api/sessions", nil)
	if !strings.Contains(rec.Body.String(), `"activity":"Bash"`) || !strings.Contains(rec.Body.String(), `"prompt":null`) {
		t.Errorf("private sessions: %s", rec.Body.String())
	}
}

func TestShutdownGuard(t *testing.T) {
	called := make(chan struct{}, 1)
	s := newTestServer(t, newFakeSource(), Options{}, func() { called <- struct{}{} }, nil)

	if rec := do(s, "POST", "/api/shutdown", nil); rec.Code != 403 {
		t.Errorf("no header: %d", rec.Code)
	}
	if rec := do(s, "POST", "/api/shutdown", map[string]string{"x-whatsmyagentdoing": "update"}); rec.Code != 403 {
		t.Errorf("wrong header: %d", rec.Code)
	}
	if rec := do(s, "GET", "/api/shutdown", map[string]string{"x-whatsmyagentdoing": "stop"}); rec.Code != 404 {
		t.Errorf("GET is not shutdown: %d", rec.Code)
	}
	rec := do(s, "POST", "/api/shutdown", map[string]string{"x-whatsmyagentdoing": "stop"})
	if rec.Code != 200 || !strings.Contains(rec.Body.String(), `"ok":true`) {
		t.Fatalf("stop: %d %s", rec.Code, rec.Body.String())
	}
	select {
	case <-called:
	case <-time.After(time.Second):
		t.Fatal("onShutdown not called")
	}

	noHook := newTestServer(t, newFakeSource(), Options{}, nil, nil)
	if rec := do(noHook, "POST", "/api/shutdown", map[string]string{"x-whatsmyagentdoing": "stop"}); rec.Code != 403 {
		t.Errorf("without a shutdown hook: %d", rec.Code)
	}
}

func TestUpdateAndInstallGuards(t *testing.T) {
	s := newTestServer(t, newFakeSource(), Options{}, nil, nil)
	if rec := do(s, "POST", "/api/update", nil); rec.Code != 403 {
		t.Errorf("update without header: %d", rec.Code)
	}
	if rec := do(s, "POST", "/api/scenes/install?id=cafe", nil); rec.Code != 403 {
		t.Errorf("install without header: %d", rec.Code)
	}
	setSceneDir(t)
	rec := do(s, "POST", "/api/scenes/install?id=does-not-exist", map[string]string{"x-whatsmyagentdoing": "install"})
	if rec.Code != 400 || !strings.Contains(rec.Body.String(), "unknown scene") {
		t.Errorf("unknown scene: %d %s", rec.Code, rec.Body.String())
	}
}

func TestApiVersion(t *testing.T) {
	s := newTestServer(t, newFakeSource(), Options{}, nil, nil)
	rec := do(s, "GET", "/api/version?check=1", nil)
	var info VersionInfo
	if err := json.Unmarshal(rec.Body.Bytes(), &info); err != nil {
		t.Fatal(err)
	}
	if info.Version != embeddedVersion || info.Latest != nil || info.UpdateAvailable || info.Auto != AutoUpdateCheck {
		t.Errorf("checks off: %+v", info)
	}
	if rec.Header().Get("Cache-Control") != "no-store" {
		t.Error("version must not be cached")
	}

	t.Setenv("WMAD_NO_UPDATE_CHECK", "")
	t.Setenv("WMAD_LATEST_VERSION", "99.0.0")
	rec = do(s, "GET", "/api/version?check=1", nil)
	if err := json.Unmarshal(rec.Body.Bytes(), &info); err != nil {
		t.Fatal(err)
	}
	if info.Latest == nil || *info.Latest != "99.0.0" || !info.UpdateAvailable {
		t.Errorf("fake latest: %+v", info)
	}
}

func TestApiScenesLists(t *testing.T) {
	dir := setSceneDir(t)
	writeFile(t, filepath.Join(dir, "cafe", "manifest.json"), `{"name":{"en":"Café"},"version":1,"credits":"x"}`)
	s := newTestServer(t, newFakeSource(), Options{}, nil, nil)
	rec := do(s, "GET", "/api/scenes", nil)
	var body struct {
		Installed []map[string]any `json:"installed"`
		Available []map[string]any `json:"available"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	if len(body.Installed) != 1 || body.Installed[0]["id"] != "cafe" || body.Installed[0]["icon"] != "☕" {
		t.Errorf("installed (icon comes from the catalog): %v", body.Installed)
	}
	for _, a := range body.Available {
		if a["id"] == "cafe" {
			t.Error("installed scenes must not be listed as available")
		}
	}
	if len(body.Available) != 3 {
		t.Errorf("catalog has 4 scenes, 1 installed: %d available", len(body.Available))
	}
}

func TestServeSceneFiles(t *testing.T) {
	dir := setSceneDir(t)
	writeFile(t, filepath.Join(dir, "demo", "manifest.json"), `{"name":"Demo"}`)
	writeFile(t, filepath.Join(dir, "demo", "models", "a.glb"), "glb-bytes")
	writeFile(t, filepath.Join(dir, "demo", "LICENSE.txt"), "cc0")
	writeFile(t, filepath.Join(dir, "secret.json"), `{}`)
	s := newTestServer(t, newFakeSource(), Options{}, nil, nil)

	rec := do(s, "GET", "/scenes/demo/manifest.json", nil)
	if rec.Code != 200 || rec.Header().Get("Content-Type") != "application/json" || rec.Header().Get("Cache-Control") != "no-cache" {
		t.Errorf("manifest: %d %q", rec.Code, rec.Header().Get("Content-Type"))
	}
	rec = do(s, "GET", "/scenes/demo/models/a.glb", nil)
	if rec.Code != 200 || rec.Body.String() != "glb-bytes" {
		t.Errorf("model: %d %q", rec.Code, rec.Body.String())
	}
	for _, p := range []string{
		"/scenes/demo/LICENSE.txt",    // not in the mime allowlist
		"/scenes/demo/../secret.json", // escapes the scene folder
		"/scenes/demo/models/../../secret.json",
		"/scenes/Demo/manifest.json", // invalid id
		"/scenes/nope/manifest.json", // not installed
		"/scenes/demo/",              // directory
		"/scenes/demo/models",        // directory, no extension
	} {
		if rec := do(s, "GET", p, nil); rec.Code != 404 {
			t.Errorf("%s: %d, want 404", p, rec.Code)
		}
	}
}

func TestViewExposesPlatformAndPromptKind(t *testing.T) {
	now := time.Now().UnixMilli()
	v := view(Session{Agent: "claude", ID: "x", Platform: "vscode", PromptKind: "command"}, now, false)
	if v.Platform == nil || *v.Platform != "vscode" || v.PromptKind == nil || *v.PromptKind != "command" {
		t.Fatalf("got %+v", v)
	}
	b, _ := json.Marshal(v)
	if !strings.Contains(string(b), `"platform":"vscode"`) || !strings.Contains(string(b), `"promptKind":"command"`) {
		t.Errorf("json: %s", b)
	}
	empty := view(Session{Agent: "gemini", ID: "y"}, now, false)
	b, _ = json.Marshal(empty)
	if !strings.Contains(string(b), `"platform":null`) || !strings.Contains(string(b), `"promptKind":null`) {
		t.Errorf("unset fields must serialise as null: %s", b)
	}
}

func TestViewExposesWorkspace(t *testing.T) {
	now := time.Now().UnixMilli()
	b, _ := json.Marshal(view(Session{Agent: "claude", ID: "x", Cwd: "/w/sub", Workspace: "/w", Tool: "agy"}, now, false))
	if !strings.Contains(string(b), `"workspace":"/w"`) || !strings.Contains(string(b), `"cwd":"/w/sub"`) || !strings.Contains(string(b), `"tool":"agy"`) {
		t.Errorf("json: %s", b)
	}
	b, _ = json.Marshal(view(Session{Agent: "codex", ID: "y"}, now, false))
	if !strings.Contains(string(b), `"workspace":null`) || !strings.Contains(string(b), `"tool":null`) {
		t.Errorf("unset workspace and tool must be null: %s", b)
	}
}

func TestViewMarksTempDirSessionsAsNotifications(t *testing.T) {
	now := time.Now().UnixMilli()
	tmp := filepath.Join(os.TempDir(), "agy-run")
	v := view(Session{Agent: "gemini", ID: "a", Cwd: tmp, PromptKind: "command"}, now, false)
	if v.PromptKind == nil || *v.PromptKind != "notification" {
		t.Errorf("cwd under os.TempDir() must read as a background call: %+v", v)
	}
	if resolved, err := filepath.EvalSymlinks(os.TempDir()); err == nil {
		v = view(Session{Agent: "claude", ID: "b", Cwd: filepath.Join(resolved, "x")}, now, false)
		if v.PromptKind == nil || *v.PromptKind != "notification" {
			t.Errorf("the resolved temp path counts too: %+v", v)
		}
	}
	v = view(Session{Agent: "claude", ID: "c", Cwd: "/tmp/scratch"}, now, false)
	if v.PromptKind == nil || *v.PromptKind != "notification" {
		t.Errorf("/tmp counts: %+v", v)
	}
	v = view(Session{Agent: "claude", ID: "d", Cwd: "/home/dev/tmpfiles"}, now, false)
	if v.PromptKind != nil {
		t.Errorf("a folder merely named like tmp must not match: %+v", v)
	}
	v = view(Session{Agent: "codex", ID: "e"}, now, false)
	if v.PromptKind != nil {
		t.Errorf("no cwd, no rule: %+v", v)
	}
}

func TestApiTimeAndServiceWorker(t *testing.T) {
	s := newTestServer(t, newFakeSource(), Options{}, nil, nil)
	before := time.Now().UnixMilli()
	rec := do(s, "GET", "/api/time", nil)
	var got struct {
		Now int64 `json:"now"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil || rec.Code != 200 {
		t.Fatalf("time: %d %v %s", rec.Code, err, rec.Body.String())
	}
	if got.Now < before || got.Now > time.Now().UnixMilli() {
		t.Errorf("time: %d is not between %d and now", got.Now, before)
	}
	if rec.Header().Get("Cache-Control") != "no-store" {
		t.Errorf("time: Cache-Control %q, want no-store", rec.Header().Get("Cache-Control"))
	}

	// The worker that keeps the page's clock in step with /api/time.
	rec = do(s, "GET", "/sw.js", nil)
	if rec.Code != 200 || !strings.Contains(rec.Header().Get("Content-Type"), "javascript") || !strings.Contains(rec.Body.String(), "/api/time") {
		t.Errorf("sw.js: %d %s", rec.Code, rec.Header().Get("Content-Type"))
	}
}
