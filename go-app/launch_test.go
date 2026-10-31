package main

import (
	"encoding/json"
	"errors"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"
)

func withValue[T any](t *testing.T, p *T, v T) {
	t.Helper()
	old := *p
	*p = v
	t.Cleanup(func() { *p = old })
}

// fakeAgent writes an executable named name that records its working folder
// and arguments (NUL-separated) in $OUT.
func fakeAgent(t *testing.T, dir, name string) string {
	t.Helper()
	p := filepath.Join(dir, name)
	body := "#!/bin/sh\npwd > \"$OUT.cwd\"\nfor a in \"$@\"; do printf '%s\\0' \"$a\"; done > \"$OUT\"\n"
	if err := os.WriteFile(p, []byte(body), 0o755); err != nil {
		t.Fatal(err)
	}
	return p
}

// launchEnv isolates agent and terminal lookup: only the fakes in bin/ and
// iTerm.app in apps/ exist, and opening a script just records it.
func launchEnv(t *testing.T) (bin string, opened *[]string) {
	t.Helper()
	root := t.TempDir()
	bin = filepath.Join(root, "bin")
	apps := filepath.Join(root, "apps")
	os.MkdirAll(bin, 0o755)
	os.MkdirAll(filepath.Join(apps, "iTerm.app"), 0o755)
	t.Setenv("PATH", bin)
	withValue(t, &launchSupported, true)
	withValue(t, &launchBinDirs, nil)
	withValue(t, &launchAppDirs, []string{apps})
	withValue(t, &claudeSettings, filepath.Join(root, "settings.json"))
	launchModelCache.Lock()
	launchModelCache.at, launchModelCache.list = map[string]time.Time{}, map[string][]string{}
	launchModelCache.Unlock()
	var got []string
	withValue(t, &openScript, func(term launchTerminal, script string) error {
		got = append(got, term.ID+" "+script)
		return nil
	})
	return bin, &got
}

func postLaunch(s *Server, body any) (int, map[string]any) {
	b, _ := json.Marshal(body)
	req := httptest.NewRequest("POST", "/api/launch", strings.NewReader(string(b)))
	req.Host = "localhost:4545"
	req.Header.Set("x-whatsmyagentdoing", "launch")
	rec := httptest.NewRecorder()
	s.ServeHTTP(rec, req)
	var out map[string]any
	json.Unmarshal(rec.Body.Bytes(), &out)
	return rec.Code, out
}

func TestLaunchGuard(t *testing.T) {
	launchEnv(t)
	s := newTestServer(t, newFakeSource(), Options{}, nil, nil)
	if rec := do(s, "POST", "/api/launch", nil); rec.Code != 403 {
		t.Errorf("launch without header: %d", rec.Code)
	}
}

func TestLaunchScriptRunsAgentWithPrompt(t *testing.T) {
	bin := t.TempDir()
	cwd := filepath.Join(t.TempDir(), "it's a folder")
	os.MkdirAll(cwd, 0o755)
	prompt := "-first line starts with a dash\nsecond 'quoted' \"double\" $(touch pwned) `id` $HOME\n\n"
	p := strings.TrimRight(prompt, "\n")
	cases := []struct {
		agent launchAgent
		model string
		print bool
		want  []string
	}{
		{launchAgents[0], "opus", false, []string{"--model", "opus", "--", p}},
		{launchAgents[2], "", false, []string{"--prompt-interactive=" + p}},
		{launchAgents[3], "openai/gpt-5", false, []string{"--model", "openai/gpt-5", "--prompt=" + p}},
		// non-interactive
		{launchAgents[0], "opus", true, []string{"-p", "--model", "opus", "--", p}},
		{launchAgents[1], "gpt-5", true, []string{"exec", "--skip-git-repo-check", "--model", "gpt-5", "--", p}},
		{launchAgents[2], "", true, []string{"--prompt=" + p}},
		{launchAgents[3], "", true, []string{"run", "--", p}},
	}
	for _, c := range cases {
		agent := fakeAgent(t, bin, c.agent.bins[0])
		dir, script, err := writeLaunchScript(c.agent, agent, c.model, cwd, prompt, c.print)
		if err != nil {
			t.Fatal(err)
		}
		if st, _ := os.Stat(script); st.Mode().Perm() != 0o700 {
			t.Errorf("script mode %v", st.Mode())
		}
		out := filepath.Join(t.TempDir(), "args")
		cmd := exec.Command("/bin/sh", script)
		// A non-interactive run ends in a login shell; true stands in for it.
		cmd.Env = append(os.Environ(), "OUT="+out, "SHELL=/usr/bin/true")
		cmd.Dir = t.TempDir()
		if b, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("%s: %v %s", c.agent.id, err, b)
		}
		raw, _ := os.ReadFile(out)
		got := strings.Split(strings.TrimSuffix(string(raw), "\x00"), "\x00")
		if !reflect.DeepEqual(got, c.want) {
			t.Errorf("%s args:\n got %q\nwant %q", c.agent.id, got, c.want)
		}
		if wd, _ := os.ReadFile(out + ".cwd"); strings.TrimSpace(string(wd)) != cwd {
			t.Errorf("%s ran in %q, want %q", c.agent.id, wd, cwd)
		}
		if dirExists(dir) {
			t.Errorf("%s: the script must remove its folder", c.agent.id)
		}
		if fileExists(filepath.Join(cwd, "pwned")) || fileExists(filepath.Join(cmd.Dir, "pwned")) {
			t.Fatal("the prompt went through the shell")
		}
	}
}

func TestServeLaunch(t *testing.T) {
	bin, opened := launchEnv(t)
	fakeAgent(t, bin, "claude")
	cwd := t.TempDir()
	s := newTestServer(t, newFakeSource(), Options{}, nil, nil)
	ok := map[string]any{"agent": "claude", "model": "opus", "cwd": cwd, "prompt": "hi", "terminal": "iterm"}

	if code, out := postLaunch(s, ok); code != 200 || out["ok"] != true || len(*opened) != 1 {
		t.Fatalf("valid launch: %d %v %v", code, out, *opened)
	}
	script := strings.TrimPrefix((*opened)[0], "iterm ")
	if b, err := os.ReadFile(script); err != nil || !strings.Contains(string(b), "'--model' 'opus' '--' \"$prompt\"") {
		t.Errorf("script: %v %s", err, b)
	}
	if b, _ := os.ReadFile(filepath.Join(filepath.Dir(script), "prompt.txt")); string(b) != "hi" {
		t.Errorf("prompt file: %q", b)
	}
	os.RemoveAll(filepath.Dir(script))

	bad := []map[string]any{
		{"agent": "codex"},                  // not installed
		{"cwd": "relative/path"},            // not absolute
		{"cwd": filepath.Join(cwd, "none")}, // missing
		{"prompt": "  \n "},                 // empty
		{"prompt": strings.Repeat("x", LaunchPromptMax+1)},
		{"terminal": "ghostty"}, // not installed
	}
	for _, change := range bad {
		req := map[string]any{}
		for k, v := range ok {
			req[k] = v
		}
		for k, v := range change {
			req[k] = v
		}
		if code, out := postLaunch(s, req); code != 400 || out["ok"] != false {
			t.Errorf("%v: %d %v", change, code, out)
		}
	}
	if len(*opened) != 1 {
		t.Errorf("a refused launch opened a terminal: %v", *opened)
	}

	// No terminal given means the system default.
	delete(ok, "terminal")
	if code, _ := postLaunch(s, ok); code != 200 || !strings.HasPrefix((*opened)[1], "default ") {
		t.Errorf("default terminal: %d %v", code, *opened)
	}
	os.RemoveAll(filepath.Dir(strings.TrimPrefix((*opened)[1], "default ")))

	// A terminal that won't open leaves nothing behind.
	withValue(t, &openScript, func(term launchTerminal, script string) error {
		*opened = append(*opened, script)
		return errors.New("open: no such app")
	})
	if code, out := postLaunch(s, ok); code != 400 || out["error"] != "open: no such app" || dirExists(filepath.Dir((*opened)[2])) {
		t.Errorf("open failure: %d %v", code, out)
	}

	// A model the dialog didn't offer runs with the CLI's default; print runs the prompt non-interactively.
	withValue(t, &openScript, func(term launchTerminal, script string) error {
		*opened = append(*opened, script)
		return nil
	})
	ok["model"], ok["print"] = "not-a-listed-model", true
	if code, out := postLaunch(s, ok); code != 200 || out["model"] != "" {
		t.Fatalf("unlisted model: %d %v", code, out)
	}
	if b, _ := os.ReadFile((*opened)[3]); strings.Contains(string(b), "--model") || !strings.Contains(string(b), "'-p' '--' \"$prompt\"\nexec \"${SHELL:-/bin/sh}\" -l\n") {
		t.Errorf("unlisted model, non-interactive: %s", b)
	}
	os.RemoveAll(filepath.Dir((*opened)[3]))
}

func TestServeLaunchDemoAndUnsupported(t *testing.T) {
	bin, opened := launchEnv(t)
	fakeAgent(t, bin, "claude")
	req := map[string]any{"agent": "claude", "cwd": t.TempDir(), "prompt": "hi"}
	demo := newTestServer(t, newFakeSource(), Options{Demo: true}, nil, nil)
	if code, out := postLaunch(demo, req); code != 200 || out["demo"] != true || len(*opened) != 0 {
		t.Errorf("demo must not start an agent: %d %v %v", code, out, *opened)
	}

	launchSupported = false
	s := newTestServer(t, newFakeSource(), Options{}, nil, nil)
	if code, _ := postLaunch(s, req); code != 400 || len(*opened) != 0 {
		t.Errorf("unsupported platform: %d", code)
	}
	var opts struct {
		Supported bool `json:"supported"`
	}
	json.Unmarshal(do(s, "GET", "/api/launch", nil).Body.Bytes(), &opts)
	if opts.Supported {
		t.Error("unsupported platform reports supported")
	}
}

func TestLaunchOptions(t *testing.T) {
	bin, _ := launchEnv(t)
	fakeAgent(t, bin, "claude")
	fakeAgent(t, bin, "agy") // Antigravity stands in for Gemini
	s := newTestServer(t, newFakeSource(), Options{}, nil, nil)
	var got struct {
		Supported bool             `json:"supported"`
		Agents    []string         `json:"agents"`
		Terminals []launchTerminal `json:"terminals"`
	}
	if err := json.Unmarshal(do(s, "GET", "/api/launch", nil).Body.Bytes(), &got); err != nil {
		t.Fatal(err)
	}
	if !got.Supported || !reflect.DeepEqual(got.Agents, []string{"claude", "gemini"}) {
		t.Errorf("agents: %+v", got)
	}
	if !reflect.DeepEqual(got.Terminals, []launchTerminal{{ID: "default"}, {ID: "iterm", Name: "iTerm2"}}) {
		t.Errorf("terminals: %+v", got.Terminals)
	}
}

func TestLaunchModels(t *testing.T) {
	bin, _ := launchEnv(t)
	for _, b := range []string{"claude", "codex", "agy", "opencode"} {
		fakeAgent(t, bin, b)
	}
	dir := t.TempDir()
	withValue(t, &claudeSettings, filepath.Join(dir, "settings.json"))
	withValue(t, &codexHome, dir)
	withValue(t, &codexConfig, filepath.Join(dir, "config.toml"))
	os.WriteFile(claudeSettings, []byte(`{"model":"opus[1m]"}`), 0o600)
	os.WriteFile(codexConfig, []byte("model = \"gpt-b\"\n[profiles.x]\nmodel = \"gpt-profile\"\n"), 0o600)
	os.WriteFile(filepath.Join(dir, "models_cache.json"), []byte(`{"models":[{"slug":"gpt-a","visibility":"list"},{"slug":"gpt-hidden","visibility":"hide"},{"slug":"gpt-b","visibility":"list"}]}`), 0o600)
	withValue(t, &launchRun, func(b string, args ...string) (string, error) {
		switch filepath.Base(b) {
		case "agy":
			return "Fetching available models...\ngem-high\tGem (High)\ngem-low\tGem (Low)\n", nil
		case "opencode":
			return "opencode/big\nopenai/gpt-x\n", nil
		}
		return "", errors.New("unexpected")
	})
	launchModelCache.Lock()
	launchModelCache.at, launchModelCache.list = map[string]time.Time{}, map[string][]string{}
	launchModelCache.Unlock()

	src := newFakeSource(
		Session{Agent: "claude", ID: "old", Model: "claude-old", LastEventAt: 1},
		Session{Agent: "claude", ID: "new", Model: "claude-new", LastEventAt: 2},
		Session{Agent: "claude", ID: "dup", Model: "opus", LastEventAt: 3},
		Session{Agent: "gemini", ID: "g", Model: "Gemini 4 (High)", LastEventAt: 3}, // a display name, not passable to --model
	)
	s := newTestServer(t, src, Options{}, nil, nil)
	want := map[string][]string{
		"claude":   {"opus[1m]", "fable", "opus", "sonnet", "haiku", "claude-new", "claude-old"},
		"codex":    {"gpt-b", "gpt-a"},
		"gemini":   {"gem-high", "gem-low"},
		"opencode": {"opencode/big", "openai/gpt-x"},
		"nope":     {},
	}
	for agent, w := range want {
		var got struct {
			Models []string `json:"models"`
		}
		json.Unmarshal(do(s, "GET", "/api/launch/models?agent="+agent, nil).Body.Bytes(), &got)
		if !reflect.DeepEqual(got.Models, w) {
			t.Errorf("%s: got %q, want %q", agent, got.Models, w)
		}
	}
}
