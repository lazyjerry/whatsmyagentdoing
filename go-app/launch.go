package main

// New chat (the toolbar button on the page): pick an agent, a model, a folder
// and a prompt, and the agent starts interactively in a new terminal window,
// so permission prompts and follow-up turns happen there. This side only
// writes a one-off .command script and has macOS `open` it; the office then
// finds the new session in the agent's log like any other.
//
// macOS only. The terminal is the page's preference (Settings → Terminal):
// "default" is whatever app opens .command files (Terminal unless changed in
// Finder's Get Info), or one of the terminals found in the Applications folders.

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"runtime"
	"sort"
	"strings"
	"sync"
	"time"
)

const launchDirPrefix = "wmad-launch-"

// launchAgent is one CLI the dialog can start. id is the page's agent key;
// the first of bins found runs. The command line is the binary, printArgs
// (non-interactive only), --model, then the prompt, glued to the prompt flag
// when it ends in "=" (so a prompt starting with "-" is never read as a flag).
type launchAgent struct {
	id          string
	bins        []string
	promptFlag  string // interactive: start the session with this prompt
	printArgs   []string
	printPrompt string // non-interactive: run this one prompt and exit
}

var launchAgents = []launchAgent{
	{"claude", []string{"claude"}, "--", []string{"-p"}, "--"},
	// The folder was picked on purpose, so exec skips its git-repo check the
	// way the interactive trust prompt would be answered.
	{"codex", []string{"codex"}, "--", []string{"exec", "--skip-git-repo-check"}, "--"},
	// agy and Gemini CLI both take --prompt for a one-shot run.
	{"gemini", []string{"agy", "gemini"}, "--prompt-interactive=", nil, "--prompt="},
	{"opencode", []string{"opencode"}, "--prompt=", []string{"run"}, "--"},
}

// launchTerminal is a terminal app the script can open in. path "" is the
// system default (open's own pick for .command files).
type launchTerminal struct {
	ID   string `json:"id"`
	Name string `json:"name"`
	path string
}

var terminalApps = []struct{ id, name, bundle string }{
	{"terminal", "Terminal", "Terminal.app"},
	{"iterm", "iTerm2", "iTerm.app"},
	{"ghostty", "Ghostty", "Ghostty.app"},
}

// Overridable in tests.
var (
	launchSupported = runtime.GOOS == "darwin"
	launchAppDirs   = []string{"/Applications", "/Applications/Utilities", "/System/Applications/Utilities", filepath.Join(homeDir, "Applications")}
	// Where agents are looked for when they aren't on this process's PATH
	// (the dashboard may be started from somewhere with a shorter PATH).
	launchBinDirs = []string{filepath.Join(homeDir, ".local", "bin"), "/opt/homebrew/bin", "/usr/local/bin"}
	// openScript hands the script to the terminal.
	openScript = func(t launchTerminal, script string) error {
		args := []string{script}
		switch {
		case t.ID == "ghostty":
			// Ghostty doesn't run a script it is asked to open; -e in a new instance does.
			args = []string{"-na", t.path, "--args", "-e", script}
		case t.path != "":
			args = []string{"-a", t.path, script}
		}
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		if out, err := exec.CommandContext(ctx, "open", args...).CombinedOutput(); err != nil {
			return fmt.Errorf("open: %s", firstNonEmpty(strings.TrimSpace(string(out)), err.Error()))
		}
		return nil
	}
	// launchRun runs a CLI's model-listing command.
	launchRun = func(bin string, args ...string) (string, error) {
		ctx, cancel := context.WithTimeout(context.Background(), LaunchListTimeout)
		defer cancel()
		cmd := exec.CommandContext(ctx, bin, args...)
		cmd.Dir = os.TempDir() // not a project: no project instructions get loaded
		out, err := cmd.Output()
		return string(out), err
	}
)

var (
	reLaunchModel = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._:/@+\[\]-]{0,127}$`)
	reCodexModel  = regexp.MustCompile(`^\s*model\s*=\s*"([^"]+)"`)
)

// Claude Code has no command that lists models; its aliases always point at
// the latest of each family.
var claudeAliases = []string{"fable", "opus", "sonnet", "haiku"}

func findLaunchAgent(id string) (launchAgent, string, bool) {
	for _, a := range launchAgents {
		if a.id != id {
			continue
		}
		for _, b := range a.bins {
			if p, err := exec.LookPath(b); err == nil {
				return a, p, true
			}
			for _, d := range launchBinDirs {
				if p := filepath.Join(d, b); fileExists(p) {
					return a, p, true
				}
			}
		}
	}
	return launchAgent{}, "", false
}

func detectTerminals() []launchTerminal {
	out := []launchTerminal{{ID: "default"}}
	for _, t := range terminalApps {
		for _, d := range launchAppDirs {
			if p := filepath.Join(d, t.bundle); dirExists(p) {
				out = append(out, launchTerminal{ID: t.id, Name: t.name, path: p})
				break
			}
		}
	}
	return out
}

// launchOptions is what the dialog and the settings panel offer.
func (s *Server) launchOptions() map[string]any {
	if !launchSupported {
		return map[string]any{"supported": false, "agents": []string{}, "terminals": []launchTerminal{}}
	}
	agents := []string{}
	for _, a := range launchAgents {
		if _, _, ok := findLaunchAgent(a.id); ok {
			agents = append(agents, a.id)
		}
	}
	return map[string]any{"supported": true, "demo": s.opts.Demo, "agents": agents, "terminals": detectTerminals()}
}

var launchModelCache = struct {
	sync.Mutex
	at   map[string]time.Time
	list map[string][]string
}{at: map[string]time.Time{}, list: map[string][]string{}}

// launchModels lists the models the dialog suggests for one agent: the
// configured default first, then what the CLI itself lists (cached for
// LaunchModelsCacheFor), then the models seen in recent sessions. The dialog
// offers only these (plus the CLI's default), and serveLaunch takes no other.
func (s *Server) launchModels(agent string) []string {
	a, bin, ok := findLaunchAgent(agent)
	if !ok {
		return []string{}
	}
	launchModelCache.Lock()
	listed, fresh := launchModelCache.list[agent], time.Since(launchModelCache.at[agent]) < LaunchModelsCacheFor
	launchModelCache.Unlock()
	if !fresh {
		listed = cliModels(a, bin)
		launchModelCache.Lock()
		launchModelCache.at[agent], launchModelCache.list[agent] = time.Now(), listed
		launchModelCache.Unlock()
	}
	seen := append([]Session(nil), s.src.Sessions()...)
	sort.SliceStable(seen, func(i, j int) bool { return seen[i].LastEventAt > seen[j].LastEventAt })
	all := append([]string{}, listed...)
	for _, ss := range seen {
		if ss.Agent == agent {
			all = append(all, ss.Model)
		}
	}
	out, dup := []string{}, map[string]bool{}
	for _, m := range all {
		if reLaunchModel.MatchString(m) && !dup[m] {
			dup[m] = true
			out = append(out, m)
		}
	}
	return out
}

func cliModels(a launchAgent, bin string) []string {
	var out []string
	switch a.id {
	case "claude":
		var cfg struct {
			Model string `json:"model"`
		}
		if b, err := os.ReadFile(claudeSettings); err == nil && json.Unmarshal(b, &cfg) == nil && cfg.Model != "" {
			out = append(out, cfg.Model)
		}
		out = append(out, claudeAliases...)
	case "codex":
		// config.toml's top-level model, then the visible ones in the CLI's own cache.
		if b, err := os.ReadFile(codexConfig); err == nil {
			for _, line := range strings.Split(string(b), "\n") {
				if strings.HasPrefix(strings.TrimSpace(line), "[") {
					break
				}
				if m := reCodexModel.FindStringSubmatch(line); m != nil {
					out = append(out, m[1])
				}
			}
		}
		var cache struct {
			Models []struct {
				Slug       string `json:"slug"`
				Visibility string `json:"visibility"`
			} `json:"models"`
		}
		if b, err := os.ReadFile(filepath.Join(codexHome, "models_cache.json")); err == nil && json.Unmarshal(b, &cache) == nil {
			for _, m := range cache.Models {
				if m.Visibility == "" || m.Visibility == "list" {
					out = append(out, m.Slug)
				}
			}
		}
	case "gemini":
		// `agy models` prints "<id>\t<name>" lines; Gemini CLI has no such command.
		if filepath.Base(bin) == "agy" {
			if text, err := launchRun(bin, "models"); err == nil {
				for _, line := range strings.Split(text, "\n") {
					if id, _, ok := strings.Cut(line, "\t"); ok {
						out = append(out, strings.TrimSpace(id))
					}
				}
			}
		}
	case "opencode":
		if text, err := launchRun(bin, "models"); err == nil {
			for _, line := range strings.Split(text, "\n") {
				if line = strings.TrimSpace(line); strings.Contains(line, "/") {
					out = append(out, line)
				}
			}
		}
	}
	return out
}

type launchRequest struct {
	Agent    string `json:"agent"`
	Model    string `json:"model"`
	Cwd      string `json:"cwd"`
	Prompt   string `json:"prompt"`
	Terminal string `json:"terminal"`
	Print    bool   `json:"print"` // non-interactive: run the one prompt and exit
}

func (s *Server) serveLaunch(w http.ResponseWriter, r *http.Request) {
	fail := func(msg string) { writeJSON(w, 400, map[string]any{"ok": false, "error": msg}, false) }
	if !launchSupported {
		fail("only available on macOS")
		return
	}
	var req launchRequest
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 2*LaunchPromptMax+64*1024)).Decode(&req); err != nil {
		fail("bad request")
		return
	}
	a, bin, ok := findLaunchAgent(req.Agent)
	if !ok {
		fail("agent not found: " + req.Agent)
		return
	}
	cwd := req.Cwd
	if cwd == "~" || strings.HasPrefix(cwd, "~/") {
		cwd = filepath.Join(homeDir, cwd[1:])
	}
	if !filepath.IsAbs(cwd) || !dirExists(cwd) {
		fail("folder not found: " + req.Cwd)
		return
	}
	cwd = filepath.Clean(cwd)
	if strings.TrimSpace(req.Prompt) == "" || len(req.Prompt) > LaunchPromptMax || strings.ContainsRune(req.Prompt, 0) {
		fail("the prompt is empty or too long")
		return
	}
	var term launchTerminal
	for _, t := range detectTerminals() {
		if t.ID == firstNonEmpty(req.Terminal, "default") {
			term = t
		}
	}
	if term.ID == "" {
		fail("terminal not found: " + req.Terminal)
		return
	}
	// Only a model the dialog offers goes through. The CLIs refuse a model they
	// don't know instead of falling back, so anything else (a stale pick, a
	// hand-made request) runs with the CLI's own default.
	model := ""
	for _, m := range s.launchModels(req.Agent) {
		if m == req.Model {
			model = m
		}
	}
	// The demo shows made-up sessions; it never starts a real agent.
	if s.opts.Demo {
		writeJSON(w, 200, map[string]any{"ok": true, "demo": true, "model": model}, false)
		return
	}

	sweepLaunchDirs()
	dir, script, err := writeLaunchScript(a, bin, model, cwd, req.Prompt, req.Print)
	if err != nil {
		fail(err.Error())
		return
	}
	if err := openScript(term, script); err != nil {
		os.RemoveAll(dir)
		fail(err.Error())
		return
	}
	writeJSON(w, 200, map[string]any{"ok": true, "model": model}, false)
}

// writeLaunchScript writes the prompt and a .command script that runs the
// agent with it into a fresh private folder. The script removes that folder
// before handing over to the agent; the prompt never goes through the shell's
// parsing, only into a quoted variable. A non-interactive run leaves a login
// shell in the folder afterwards, so the window (Ghostty closes it when the
// command exits) stays open on the output.
func writeLaunchScript(a launchAgent, bin, model, cwd, prompt string, print bool) (dir, script string, err error) {
	dir, err = os.MkdirTemp("", launchDirPrefix)
	if err != nil {
		return "", "", err
	}
	promptFile := filepath.Join(dir, "prompt.txt")
	if err = os.WriteFile(promptFile, []byte(prompt), 0o600); err != nil {
		os.RemoveAll(dir)
		return "", "", err
	}
	argv := []string{shQuote(bin)}
	flag := a.promptFlag
	if print {
		for _, x := range a.printArgs {
			argv = append(argv, shQuote(x))
		}
		flag = a.printPrompt
	}
	if model != "" {
		argv = append(argv, shQuote("--model"), shQuote(model))
	}
	last := `"$prompt"`
	if strings.HasSuffix(flag, "=") {
		last = `"` + flag + `$prompt"`
	} else {
		argv = append(argv, shQuote(flag))
	}
	argv = append(argv, last)
	run := "exec " + strings.Join(argv, " ") + "\n"
	if print {
		run = strings.Join(argv, " ") + "\n" + `exec "${SHELL:-/bin/sh}" -l` + "\n"
	}
	body := "#!/bin/sh\n" +
		"# Written by whatsmyagentdoing (New chat): starts the agent with the prompt saved next to this file.\n" +
		"cd " + shQuote(cwd) + " || exit 1\n" +
		"prompt=$(cat " + shQuote(promptFile) + ") || exit 1\n" +
		"rm -rf " + shQuote(dir) + "\n" +
		run
	script = filepath.Join(dir, "start.command")
	if err = os.WriteFile(script, []byte(body), 0o700); err != nil {
		os.RemoveAll(dir)
		return "", "", err
	}
	return dir, script, nil
}

func shQuote(s string) string { return "'" + strings.ReplaceAll(s, "'", `'\''`) + "'" }

// sweepLaunchDirs removes scripts left by launches whose terminal never ran them.
func sweepLaunchDirs() {
	tmp := os.TempDir()
	for _, e := range ls(tmp) {
		if !e.IsDir() || !strings.HasPrefix(e.Name(), launchDirPrefix) {
			continue
		}
		if st, err := e.Info(); err == nil && time.Since(st.ModTime()) > time.Hour {
			os.RemoveAll(filepath.Join(tmp, e.Name()))
		}
	}
}
