package main

// Resume (the "Open ▾" menu on a card): reopen a session that has stopped,
// in its own folder, either in a terminal tab Orca opens in the session's
// workspace (orca.go) or in a new window of a terminal app (the same ones New
// chat offers, launch.go). A session that is working or waiting is refused:
// its own process still has it open, and two would write it.

import (
	"encoding/json"
	"net/http"
	"os"
	"path/filepath"
	"time"
)

// resumeCommand is the page's resumeCmd without the cd: the CLI line that
// reopens the session, with the CLI found by bin. "" for an unknown agent.
func resumeCommand(s Session, bin func(string) string) string {
	switch {
	case s.Agent == "claude":
		return shQuote(bin("claude")) + " --resume " + shQuote(s.ID)
	case s.Agent == "gemini" && s.Tool == "agy":
		return shQuote(bin("agy")) + " --conversation " + shQuote(s.ID)
	case s.Agent == "gemini":
		return shQuote(bin("gemini")) + " --resume " + shQuote(firstNonEmpty(s.SessionID, s.ID))
	case s.Agent == "opencode":
		return shQuote(bin("opencode")) + " --session " + shQuote(s.ID)
	case s.Agent == "codex":
		return shQuote(bin("codex")) + " resume " + shQuote(s.ID)
	}
	return ""
}

// Orca's terminals start a login shell, so the bare name is found there.
func bareBin(name string) string { return name }

// A terminal window may not (Ghostty runs the script directly, with the
// environment the dashboard started it with), so the path found here.
func foundBin(name string) string {
	if p, ok := findBin(name); ok {
		return p
	}
	return name
}

func (s *Server) serveResume(w http.ResponseWriter, r *http.Request) {
	fail := func(msg string) { writeJSON(w, 400, map[string]any{"ok": false, "error": msg}, false) }
	if !launchSupported {
		fail("only available on macOS")
		return
	}
	var req struct {
		Key string `json:"key"`
		App string `json:"app"` // "orca", or a terminal id from /api/launch
	}
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 4096)).Decode(&req); err != nil {
		fail("bad request")
		return
	}
	var sess Session
	found := false
	for _, x := range s.src.Sessions() {
		if x.Agent+":"+x.ID == req.Key && !x.Subagent {
			sess, found = x, true
		}
	}
	if !found {
		fail("session not found")
		return
	}
	switch view(sess, time.Now().UnixMilli(), false).Status {
	case "thinking", "tool", "typing", "waiting":
		fail("the session is still open; resume it once it is done")
		return
	}
	if resumeCommand(sess, bareBin) == "" {
		fail("this session can't be resumed")
		return
	}
	var term launchTerminal
	if req.App != "orca" {
		for _, t := range detectTerminals() {
			if t.ID == req.App {
				term = t
			}
		}
		if term.ID == "" {
			fail("terminal not found: " + req.App)
			return
		}
	}
	// The demo shows made-up sessions; it never starts a real agent.
	if s.opts.Demo {
		writeJSON(w, 200, map[string]any{"ok": true, "demo": true}, false)
		return
	}
	cwd := filepath.Clean(sess.Cwd)
	if sess.Cwd == "" || !dirExists(cwd) {
		fail("folder not found: " + sess.Cwd)
		return
	}

	if req.App == "orca" {
		root, err := s.orca.root(cwd)
		if err != nil {
			fail(err.Error())
			return
		}
		if root == "" {
			fail("the folder isn't in an Orca workspace")
			return
		}
		line := "cd " + shQuote(cwd) + " && " + resumeCommand(sess, bareBin)
		if _, err := orcaRun(OrcaOpenTimeout, "terminal", "create", "--worktree", "path:"+root, "--command", line, "--focus", "--json"); err != nil {
			fail(err.Error())
			return
		}
		writeJSON(w, 200, map[string]any{"ok": true}, false)
		return
	}

	sweepLaunchDirs()
	dir, script, err := writeResumeScript(cwd, resumeCommand(sess, foundBin))
	if err != nil {
		fail(err.Error())
		return
	}
	if err := openScript(term, script); err != nil {
		os.RemoveAll(dir)
		fail(err.Error())
		return
	}
	writeJSON(w, 200, map[string]any{"ok": true}, false)
}

// writeResumeScript writes a .command script that removes its own folder,
// then runs the resume command in the session's folder.
func writeResumeScript(cwd, cmd string) (dir, script string, err error) {
	dir, err = os.MkdirTemp("", launchDirPrefix)
	if err != nil {
		return "", "", err
	}
	body := "#!/bin/sh\n" +
		"# Written by whatsmyagentdoing (Open → Resume): reopens the session in its folder.\n" +
		"cd " + shQuote(cwd) + " || exit 1\n" +
		"rm -rf " + shQuote(dir) + "\n" +
		"exec " + cmd + "\n"
	script = filepath.Join(dir, "resume.command")
	if err = os.WriteFile(script, []byte(body), 0o700); err != nil {
		os.RemoveAll(dir)
		return "", "", err
	}
	return dir, script, nil
}
