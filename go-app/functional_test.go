package main

// Functional tests: the HTTP surface through a real listener (SSE included),
// and the CLI through a freshly built binary. The use cases they exercise are
// numbered as in jerry-report/use-cases.md.

import (
	"bufio"
	"bytes"
	"encoding/json"
	"flag"
	"fmt"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"
)

var builtBinary string // set by TestMain unless -short

func TestMain(m *testing.M) {
	flag.Parse()
	if !testing.Short() {
		dir, err := os.MkdirTemp("", "wmad-test-bin")
		if err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(1)
		}
		builtBinary = filepath.Join(dir, "whatsmyagentdoing")
		out, err := exec.Command("go", "build", "-o", builtBinary, ".").CombinedOutput()
		if err != nil {
			fmt.Fprintf(os.Stderr, "go build for functional tests failed: %v\n%s", err, out)
			os.RemoveAll(dir)
			os.Exit(1)
		}
		code := m.Run()
		os.RemoveAll(dir)
		os.Exit(code)
	}
	os.Exit(m.Run())
}

// --- UC-01/02/03/05/08 over a real listener

func TestFunctionalHTTPLiveMonitoring(t *testing.T) {
	claude, _, _ := setLogDirs(t)
	noNetwork(t)
	logPath := claudeLog(t, claude, "live", claudeUser("Add rate limiting"))

	w := NewWatcher(24)
	w.Tick() // no Start(): ticks are driven by hand so the test is deterministic
	stopped := make(chan struct{}, 1)
	app := NewServer(w, Options{}, NewUpdater(), func() { stopped <- struct{}{} }, nil)
	defer app.Close()
	srv := httptest.NewServer(app)
	defer srv.Close()

	// UC-05 shape over HTTP
	var snap Snapshot
	getJSON(t, srv.URL+"/api/sessions", &snap)
	if len(snap.Sessions) != 1 || snap.Sessions[0].Status != "thinking" || *snap.Sessions[0].Prompt != "Add rate limiting" {
		t.Fatalf("sessions: %+v", snap)
	}

	// UC-02: SSE gets a snapshot on connect, then a frame when the log grows.
	frames := openSSE(t, srv.URL+"/events")
	first := nextFrame(t, frames, 3*time.Second)
	if len(first.Sessions) != 1 || first.Sessions[0].Status != "thinking" {
		t.Fatalf("initial frame: %+v", first)
	}
	appendFile(t, logPath, jsonLine(t, claudeToolUse("Edit", map[string]any{"file_path": "/w/routes.go"}))+"\n")
	w.Tick()
	second := nextFrame(t, frames, 3*time.Second)
	if second.Sessions[0].Status != "tool" || *second.Sessions[0].Activity != "Edit routes.go" {
		t.Fatalf("pushed frame: %+v", second.Sessions[0])
	}
	// An idle tick pushes nothing.
	w.Tick()
	select {
	case f := <-frames:
		t.Fatalf("unexpected frame after idle tick: %+v", f)
	case <-time.After(300 * time.Millisecond):
	}

	// UC-03: shutdown needs the header, then fires the hook and ends the stream.
	res := post(t, srv.URL+"/api/shutdown", "")
	if res.StatusCode != 403 {
		t.Fatalf("shutdown without header: %d", res.StatusCode)
	}
	res = post(t, srv.URL+"/api/shutdown", "stop")
	if res.StatusCode != 200 {
		t.Fatalf("shutdown: %d", res.StatusCode)
	}
	select {
	case <-stopped:
	case <-time.After(2 * time.Second):
		t.Fatal("onShutdown hook not called")
	}
	select {
	case _, ok := <-frames:
		if ok {
			t.Fatal("expected the SSE stream to end")
		}
	case <-time.After(2 * time.Second):
		t.Fatal("SSE stream still open after shutdown")
	}
}

func TestFunctionalHTTPPrivateAndDemo(t *testing.T) {
	noNetwork(t)
	claude, _, _ := setLogDirs(t)
	claudeLog(t, claude, "p", claudeUser("Rotate the signing keys"), claudeToolUse("Bash", map[string]any{"command": "ls"}))
	w := NewWatcher(24)
	w.Tick()
	priv := httptest.NewServer(NewServer(w, Options{Privacy: true}, NewUpdater(), nil, nil))
	defer priv.Close()
	var snap Snapshot
	getJSON(t, priv.URL+"/api/sessions", &snap)
	s := snap.Sessions[0]
	if s.Prompt != nil || s.Title != nil || s.Reply != nil || *s.Activity != "Bash" || s.Project == nil {
		t.Errorf("UC-08: %+v", s)
	}

	demo := NewDemoWatcher()
	demo.Tick()
	ds := httptest.NewServer(NewServer(demo, Options{Demo: true}, NewUpdater(), nil, nil))
	defer ds.Close()
	getJSON(t, ds.URL+"/api/sessions", &snap)
	if len(snap.Sessions) != 10 || snap.Lang != nil {
		t.Errorf("UC-06: %d sessions, lang %v", len(snap.Sessions), snap.Lang)
	}
}

func TestFunctionalHTTPUpdateOutsideSource(t *testing.T) {
	noNetwork(t)
	t.Setenv("WMAD_SRC", t.TempDir())
	chdir(t, t.TempDir())
	srv := httptest.NewServer(NewServer(newFakeSource(), Options{}, NewUpdater(), nil, nil))
	defer srv.Close()
	var info VersionInfo
	getJSON(t, srv.URL+"/api/version", &info)
	if info.CanUpdate {
		t.Fatal("canUpdate must be false outside the source folder")
	}
	res := post(t, srv.URL+"/api/update", "update")
	body, _ := readAll(res)
	if res.StatusCode != 400 || !strings.Contains(body, `"ok":false`) {
		t.Fatalf("UC-04 A3: %d %s", res.StatusCode, body)
	}
}

// --- CLI through the built binary

// safeBuffer is a bytes.Buffer the exec copier can write while the test reads.
type safeBuffer struct {
	mu sync.Mutex
	b  bytes.Buffer
}

func (s *safeBuffer) Write(p []byte) (int, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.b.Write(p)
}

func (s *safeBuffer) String() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.b.String()
}

type cliEnv struct {
	home, claude, codex, gemini string
}

func newCLIEnv(t *testing.T) cliEnv {
	t.Helper()
	base := t.TempDir()
	e := cliEnv{home: filepath.Join(base, "home"), claude: filepath.Join(base, "claude"), codex: filepath.Join(base, "codex"), gemini: filepath.Join(base, "gemini")}
	for _, d := range []string{e.home, filepath.Join(e.claude, "projects"), filepath.Join(e.codex, "sessions"), filepath.Join(e.gemini, ".gemini", "tmp")} {
		if err := os.MkdirAll(d, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	return e
}

func (e cliEnv) cmd(t *testing.T, args ...string) *exec.Cmd {
	t.Helper()
	if builtBinary == "" {
		t.Skip("binary not built (-short)")
	}
	c := exec.Command(builtBinary, args...)
	c.Dir = e.home
	c.Env = append(os.Environ(),
		"HOME="+e.home, "USERPROFILE="+e.home,
		"CLAUDE_CONFIG_DIR="+e.claude, "CODEX_HOME="+e.codex, "GEMINI_CLI_HOME="+e.gemini,
		"WMAD_NO_UPDATE_CHECK=1", "WMAD_SRC=",
	)
	return c
}

func (e cliEnv) run(t *testing.T, args ...string) (string, int) {
	t.Helper()
	var out bytes.Buffer
	c := e.cmd(t, args...)
	c.Stdout, c.Stderr = &out, &out
	err := c.Run()
	code := 0
	if ee, ok := err.(*exec.ExitError); ok {
		code = ee.ExitCode()
	} else if err != nil {
		t.Fatalf("run %v: %v\n%s", args, err, out.String())
	}
	return out.String(), code
}

func TestCLIJSONSnapshot(t *testing.T) {
	e := newCLIEnv(t)
	out, code := e.run(t, "--json")
	if code != 0 || strings.TrimSpace(out) != "[]" {
		t.Fatalf("empty: %d %q", code, out)
	}

	writeFile(t, filepath.Join(e.claude, "projects", "-w-app", "s1.jsonl"),
		lines(jsonLine(t, claudeUser("Fix the crash")), jsonLine(t, claudeToolUse("Read", map[string]any{"file_path": "/w/app/main.go"}))))
	out, code = e.run(t, "--json", "--hours", "2")
	var views []View
	if err := json.Unmarshal([]byte(out), &views); err != nil || code != 0 {
		t.Fatalf("UC-05: %d %v\n%s", code, err, out)
	}
	if len(views) != 1 || views[0].Key != "claude:s1" || views[0].Status != "tool" || *views[0].Activity != "Read main.go" || *views[0].Project != "proj" {
		t.Fatalf("UC-05 content: %+v", views[0])
	}

	out, _ = e.run(t, "--json", "--private")
	if !strings.Contains(out, `"prompt": null`) || !strings.Contains(out, `"activity": "Read"`) {
		t.Fatalf("UC-08 via CLI:\n%s", out)
	}
}

func TestCLIInfoCommands(t *testing.T) {
	e := newCLIEnv(t)
	out, code := e.run(t, "--version")
	if code != 0 || strings.TrimSpace(out) != embeddedVersion {
		t.Errorf("--version: %d %q", code, out)
	}
	out, code = e.run(t, "--help")
	if code != 0 || !strings.Contains(out, "Usage:") || !strings.Contains(out, e.claude) {
		t.Errorf("--help: %d\n%s", code, out)
	}
	out, code = e.run(t, "--bogus")
	if code != 1 || !strings.Contains(out, "Unknown option: --bogus") {
		t.Errorf("unknown flag: %d\n%s", code, out)
	}
	out, code = e.run(t, "--port", "abc")
	if code != 1 {
		t.Errorf("bad number: %d\n%s", code, out)
	}
	out, code = e.run(t, "--scenes")
	if code != 0 || !strings.Contains(out, "office      Office (built in)") || !strings.Contains(out, "cafe") || !strings.Contains(out, "(download,") {
		t.Errorf("UC-07 list: %d\n%s", code, out)
	}
	out, code = e.run(t, "--remove-scene", "cafe")
	if code != 0 || !strings.Contains(out, "isn't a downloaded scene") {
		t.Errorf("UC-07 remove missing: %d\n%s", code, out)
	}
	out, code = e.run(t, "--stop", "-p", strconv.Itoa(freePort(t)))
	if code != 0 || strings.TrimSpace(out) != "Not running." {
		t.Errorf("UC-03 nothing to stop: %d %q", code, out)
	}
}

func TestCLIServerLifecycle(t *testing.T) {
	e := newCLIEnv(t)
	writeFile(t, filepath.Join(e.claude, "projects", "-w-app", "s1.jsonl"), lines(jsonLine(t, claudeUser("Hello"))))
	port := freePort(t)
	p := strconv.Itoa(port)

	var startOut safeBuffer
	server := e.cmd(t, "--no-open", "-p", p)
	server.Stdout, server.Stderr = &startOut, &startOut
	if err := server.Start(); err != nil {
		t.Fatal(err)
	}
	exited := make(chan error, 1)
	go func() { exited <- server.Wait() }()
	t.Cleanup(func() {
		select {
		case <-exited:
		default:
			server.Process.Kill()
		}
	})

	// UC-01: wait for the dashboard to answer.
	base := "http://127.0.0.1:" + p
	deadline := time.Now().Add(10 * time.Second)
	for !alreadyRunning(port) {
		if time.Now().After(deadline) {
			t.Fatalf("dashboard never came up\n%s", startOut.String())
		}
		time.Sleep(50 * time.Millisecond)
	}
	if !strings.Contains(startOut.String(), "whatsmyagentdoing v"+embeddedVersion+" → http://localhost:"+p) || !strings.Contains(startOut.String(), "1 session in the last 24h") {
		t.Errorf("start banner:\n%s", startOut.String())
	}
	res, err := http.Get(base + "/")
	if err != nil || res.StatusCode != 200 || !strings.HasPrefix(res.Header.Get("Content-Type"), "text/html") {
		t.Fatalf("page: %v %v", err, res)
	}
	res.Body.Close()
	var snap Snapshot
	getJSON(t, base+"/api/sessions", &snap)
	if len(snap.Sessions) != 1 || *snap.Sessions[0].Prompt != "Hello" {
		t.Fatalf("sessions via binary: %+v", snap)
	}

	// UC-01 A1: a second start only reports the running one.
	out, code := e.run(t, "--no-open", "-p", p)
	if code != 0 || !strings.Contains(out, "Already running at http://localhost:"+p) {
		t.Errorf("already running: %d %q", code, out)
	}

	// UC-03: stop it from a second process.
	out, code = e.run(t, "--stop", "-p", p)
	if code != 0 || !strings.Contains(out, "Stopped http://localhost:"+p) {
		t.Errorf("stop: %d %q", code, out)
	}
	select {
	case err := <-exited:
		if err != nil {
			t.Errorf("server exit: %v\n%s", err, startOut.String())
		}
	case <-time.After(5 * time.Second):
		t.Fatal("server process did not exit after --stop")
	}
	if alreadyRunning(port) {
		t.Error("port still answering")
	}
}

func TestCLIPortFallback(t *testing.T) {
	e := newCLIEnv(t)
	// Occupy the requested port with something that is not us.
	blocker := httptest.NewUnstartedServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.Write([]byte("other")) }))
	blocker.Start()
	defer blocker.Close()
	port := blocker.Listener.Addr().(*net.TCPAddr).Port

	var out safeBuffer
	server := e.cmd(t, "--no-open", "--demo", "-p", strconv.Itoa(port))
	server.Stdout, server.Stderr = &out, &out
	if err := server.Start(); err != nil {
		t.Fatal(err)
	}
	defer server.Process.Kill()
	deadline := time.Now().Add(10 * time.Second)
	for !alreadyRunning(port + 1) {
		if time.Now().After(deadline) {
			t.Fatalf("no fallback to port %d\n%s", port+1, out.String())
		}
		time.Sleep(50 * time.Millisecond)
	}
	if !strings.Contains(out.String(), "localhost:"+strconv.Itoa(port+1)) || !strings.Contains(out.String(), "demo team") {
		t.Errorf("banner:\n%s", out.String())
	}
	var snap Snapshot
	getJSON(t, fmt.Sprintf("http://127.0.0.1:%d/api/sessions", port+1), &snap)
	if len(snap.Sessions) != 10 {
		t.Errorf("demo over fallback port: %d sessions", len(snap.Sessions))
	}
	out2, code := e.run(t, "--stop", "-p", strconv.Itoa(port))
	if code != 0 || !strings.Contains(out2, "Stopped http://localhost:"+strconv.Itoa(port+1)) {
		t.Errorf("--stop must find the fallback port: %d %q", code, out2)
	}
	deadline = time.Now().Add(5 * time.Second)
	for alreadyRunning(port + 1) {
		if time.Now().After(deadline) {
			t.Fatal("fallback instance still answering after --stop")
		}
		time.Sleep(50 * time.Millisecond)
	}
}

// --- HTTP helpers

func getJSON(t *testing.T, url string, v any) {
	t.Helper()
	res, err := http.Get(url)
	if err != nil {
		t.Fatalf("GET %s: %v", url, err)
	}
	defer res.Body.Close()
	if res.StatusCode != 200 {
		t.Fatalf("GET %s: %d", url, res.StatusCode)
	}
	if err := json.NewDecoder(res.Body).Decode(v); err != nil {
		t.Fatalf("GET %s: %v", url, err)
	}
}

func post(t *testing.T, url, header string) *http.Response {
	t.Helper()
	req, _ := http.NewRequest("POST", url, nil)
	if header != "" {
		req.Header.Set("x-whatsmyagentdoing", header)
	}
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("POST %s: %v", url, err)
	}
	return res
}

func readAll(res *http.Response) (string, error) {
	defer res.Body.Close()
	var b bytes.Buffer
	_, err := b.ReadFrom(res.Body)
	return b.String(), err
}

// openSSE connects to /events and streams decoded `data:` frames; the channel
// closes when the server ends the stream.
func openSSE(t *testing.T, url string) <-chan Snapshot {
	t.Helper()
	res, err := http.Get(url)
	if err != nil || res.StatusCode != 200 || res.Header.Get("Content-Type") != "text/event-stream" {
		t.Fatalf("SSE connect: %v %v", err, res)
	}
	out := make(chan Snapshot, 16)
	go func() {
		defer close(out)
		defer res.Body.Close()
		sc := bufio.NewScanner(res.Body)
		sc.Buffer(make([]byte, 1024*1024), 8*1024*1024)
		for sc.Scan() {
			line := sc.Text()
			if !strings.HasPrefix(line, "data: ") {
				continue
			}
			var snap Snapshot
			if json.Unmarshal([]byte(strings.TrimPrefix(line, "data: ")), &snap) == nil {
				out <- snap
			}
		}
	}()
	return out
}

func nextFrame(t *testing.T, frames <-chan Snapshot, within time.Duration) Snapshot {
	t.Helper()
	select {
	case f, ok := <-frames:
		if !ok {
			t.Fatal("SSE stream closed early")
		}
		return f
	case <-time.After(within):
		t.Fatal("no SSE frame in time")
	}
	return Snapshot{}
}
