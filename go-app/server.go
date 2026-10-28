package main

// HTTP server: static page, JSON snapshot, server-sent events and update
// endpoints. Port of src/server.js. Listens on loopback only and
// refuses requests whose Host header isn't loopback (DNS rebinding).

import (
	"encoding/json"
	"fmt"
	"io"
	"io/fs"
	"net/http"
	"net/url"
	"os"
	"path"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"sync"
	"time"
)

var staticFiles = map[string][2]string{
	"/":            {"index.html", "text/html; charset=utf-8"},
	"/office.js":   {"office.js", "text/javascript; charset=utf-8"},
	"/office3d.js": {"office3d.js", "text/javascript; charset=utf-8"},
	"/i18n.js":     {"i18n.js", "text/javascript; charset=utf-8"},
	"/popout.js":   {"popout.js", "text/javascript; charset=utf-8"},
	"/sw.js":       {"sw.js", "text/javascript; charset=utf-8"},
	"/LICENSE":     {"LICENSE", "text/plain; charset=utf-8"},
}

var mimeTypes = map[string]string{
	".js":  "text/javascript; charset=utf-8",
	".glb": "model/gltf-binary",
	".png": "image/png",
}

var (
	rePortSuffix   = regexp.MustCompile(`:\d+$`)
	reNowPrefix    = regexp.MustCompile(`^\{"now":\d+,`)
	reActivityHead = regexp.MustCompile(`[:\s]`)
	// System temp folders. A session whose cwd lives here was almost always
	// spawned by a program (a hook, a daemon, another agent's skill), not by a
	// person: ai-audit runs `agy -p` from os.TempDir(), Claude's scratchpad
	// sits under /tmp. Both the $TMPDIR spelling and its resolved path
	// (/var/folders/… vs /private/var/folders/… on macOS) are kept.
	tempDirs = systemTempDirs()
)

type Options struct {
	Port    int
	Hours   float64
	Open    bool
	Privacy bool
	JSON    bool
	Demo    bool
	Stop    bool
	Update  bool
}

// View is one session as the page sees it. Pointers render as null when unset.
type View struct {
	Key           string  `json:"key"`
	Agent         string  `json:"agent"`
	ID            string  `json:"id"`
	SessionID     *string `json:"sessionId"`
	ParentID      *string `json:"parentId"`
	Subagent      bool    `json:"subagent"`
	Platform      *string `json:"platform"`
	Tool          *string `json:"tool"`
	PromptKind    *string `json:"promptKind"`
	Cwd           *string `json:"cwd"`
	Workspace     *string `json:"workspace"`
	Project       *string `json:"project"`
	Title         *string `json:"title"`
	Model         *string `json:"model"`
	Status        string  `json:"status"`
	Activity      *string `json:"activity"`
	Prompt        *string `json:"prompt"`
	Reply         *string `json:"reply"`
	Error         *string `json:"error"`
	TurnStartedAt *int64  `json:"turnStartedAt"`
	LastEventAt   *int64  `json:"lastEventAt"`
	StartedAt     *int64  `json:"startedAt"`

	TurnStartApprox bool     `json:"turnStartApprox"` // turnStartedAt is a lower bound: the turn began earlier
	ContextTokens   *int64   `json:"contextTokens"`   // context size of the latest request
	CompactAt       *int64   `json:"compactAt"`       // where the CLI compacts on its own
	Alerts          []string `json:"alerts"`          // "compact", "longTurn": shown apart from the status
}

type Snapshot struct {
	Now      int64   `json:"now"`
	Lang     *string `json:"lang"`
	Sessions []View  `json:"sessions"`
}

func systemTempDirs() []string {
	var out []string
	for _, d := range []string{os.TempDir(), "/tmp"} {
		d = filepath.Clean(d)
		out = append(out, d)
		if r, err := filepath.EvalSymlinks(d); err == nil && r != d {
			out = append(out, r)
		}
	}
	return out
}

// inTempDir reports whether cwd is one of the system temp folders or below.
func inTempDir(cwd string) bool {
	if cwd == "" {
		return false
	}
	cwd = filepath.Clean(cwd)
	for _, d := range tempDirs {
		if cwd == d || strings.HasPrefix(cwd, d+string(filepath.Separator)) {
			return true
		}
	}
	return false
}

func nstr(s string) *string {
	if s == "" {
		return nil
	}
	return &s
}

func nint(n int64) *int64 {
	if n == 0 {
		return nil
	}
	return &n
}

// Logs only record finished blocks, so a few states need a clock to resolve:
// a reply with no follow-up is a finished turn, and an "active" session that
// has gone silent for a long time was most likely closed mid-turn.
func view(s Session, now int64, privacy bool) View {
	last := s.LastEventAt
	if last == 0 {
		last = s.Mtime
	}
	if last == 0 {
		last = now
	}
	quiet := now - last
	status := s.Status
	switch {
	case status == "typing" && quiet > TypingDoneAfter.Milliseconds():
		status = "done"
	case status == "thinking" && quiet > ThinkingIdleAfter.Milliseconds():
		status = "idle"
	case status == "tool" && quiet > ToolIdleAfter.Milliseconds():
		status = "idle"
	case (status == "waiting" || status == "error") && quiet > AttentionIdleAfter.Milliseconds():
		status = "idle"
	}

	out := View{
		Key:           s.Agent + ":" + s.ID,
		Agent:         s.Agent,
		ID:            s.ID,
		SessionID:     nstr(s.SessionID),
		ParentID:      nstr(s.ParentID),
		Subagent:      s.Subagent,
		Platform:      nstr(s.Platform),
		Tool:          nstr(s.Tool),
		PromptKind:    nstr(s.PromptKind),
		Cwd:           nstr(s.Cwd),
		Workspace:     nstr(s.Workspace),
		Title:         nstr(s.Title),
		Model:         nstr(s.Model),
		Status:        status,
		Prompt:        nstr(s.Prompt),
		Reply:         nstr(s.Reply),
		Error:         nstr(s.Error),
		TurnStartedAt: nint(s.TurnStartedAt),
		StartedAt:     nint(s.StartedAt),
	}
	if s.TurnStartedAt == 0 && s.TurnStartFloor != 0 {
		out.TurnStartedAt = nint(s.TurnStartFloor)
		out.TurnStartApprox = true
	}
	// Warnings sit beside the status (config.go): the context is close to
	// where the CLI compacts on its own, or the turn has been working for long.
	if s.ContextTokens > 0 {
		out.ContextTokens = nint(s.ContextTokens)
		out.CompactAt = nint(s.CompactAt)
		if CompactWarnRatio > 0 && s.CompactAt > 0 && float64(s.ContextTokens) >= CompactWarnRatio*float64(s.CompactAt) {
			out.Alerts = append(out.Alerts, "compact")
		}
	}
	working := status == "thinking" || status == "tool" || status == "typing"
	if LongTurnAfter > 0 && working && out.TurnStartedAt != nil && now-*out.TurnStartedAt > LongTurnAfter.Milliseconds() {
		out.Alerts = append(out.Alerts, "longTurn")
	}
	if s.Cwd != "" {
		out.Project = nstr(baseName(s.Cwd))
	}
	// Work started from a temp folder is a background tool call: show the
	// bell, whatever kind of prompt the log says it was.
	if inTempDir(s.Cwd) {
		out.PromptKind = nstr("notification")
	}
	if status == "tool" || status == "waiting" {
		out.Activity = nstr(s.Activity)
	}
	if s.LastEventAt != 0 {
		out.LastEventAt = nint(s.LastEventAt)
	} else {
		out.LastEventAt = nint(s.Mtime)
	}
	if privacy {
		out.Prompt, out.Reply, out.Title = nil, nil, nil
		if out.Activity != nil {
			out.Activity = nstr(reActivityHead.Split(*out.Activity, 2)[0])
		}
	}
	return out
}

// The language the user writes their prompts in (recent sessions first);
// the page uses it as the default UI language.
func promptLanguage(raw []Session) string {
	var recent []Session
	for _, s := range raw {
		if !s.Subagent && s.Prompt != "" && !strings.HasPrefix(s.Prompt, "/") {
			recent = append(recent, s)
		}
	}
	sort.SliceStable(recent, func(i, j int) bool { return recent[i].LastEventAt > recent[j].LastEventAt })
	if len(recent) > 60 {
		recent = recent[:60]
	}
	prompts := make([]string, 0, len(recent))
	for _, s := range recent {
		prompts = append(prompts, s.Prompt)
	}
	return guessFromPrompts(prompts)
}

func snapshot(src Source, opts Options) Snapshot {
	now := time.Now().UnixMilli()
	raw := src.Sessions()
	sessions := make([]View, 0, len(raw))
	for _, s := range raw {
		sessions = append(sessions, view(s, now, opts.Privacy))
	}
	sort.SliceStable(sessions, func(i, j int) bool {
		var a, b int64
		if sessions[i].LastEventAt != nil {
			a = *sessions[i].LastEventAt
		}
		if sessions[j].LastEventAt != nil {
			b = *sessions[j].LastEventAt
		}
		return a > b
	})
	snap := Snapshot{Now: now, Sessions: sessions}
	if !opts.Demo {
		snap.Lang = nstr(promptLanguage(raw))
	}
	return snap
}

type Server struct {
	src      Source
	opts     Options
	updater  *Updater
	shutdown func()
	restart  func()

	mu      sync.Mutex
	clients map[chan string]struct{}
	last    string
	done    chan struct{}
}

func NewServer(src Source, opts Options, updater *Updater, onShutdown, onRestart func()) *Server {
	s := &Server{
		src:      src,
		opts:     opts,
		updater:  updater,
		shutdown: onShutdown,
		restart:  onRestart,
		clients:  map[chan string]struct{}{},
		done:     make(chan struct{}),
	}
	go s.broadcastLoop()
	return s
}

func (s *Server) Close() {
	select {
	case <-s.done:
	default:
		close(s.done)
	}
	s.closeClients()
}

func (s *Server) closeClients() {
	s.mu.Lock()
	for ch := range s.clients {
		close(ch)
		delete(s.clients, ch)
	}
	s.mu.Unlock()
}

func (s *Server) push(force bool) {
	body, err := json.Marshal(snapshot(s.src, s.opts))
	if err != nil {
		fmt.Fprintln(os.Stderr, "snapshot failed:", err)
		return
	}
	// Compare without the timestamp so idle ticks don't spam clients.
	sig := reNowPrefix.ReplaceAllString(string(body), "{")
	s.mu.Lock()
	defer s.mu.Unlock()
	if !force && sig == s.last {
		return
	}
	s.last = sig
	for ch := range s.clients {
		select {
		case ch <- string(body):
		default: // a stalled client just misses this frame
		}
	}
}

func (s *Server) broadcastLoop() {
	// Status can flip with no new log lines (see view()), so re-check on a clock.
	tick := time.NewTicker(5 * time.Second)
	defer tick.Stop()
	for {
		select {
		case <-s.src.Changes():
			s.push(false)
		case <-tick.C:
			s.push(false)
		case <-s.done:
			return
		}
	}
}

func writeJSON(w http.ResponseWriter, status int, v any, noStore bool) {
	w.Header().Set("Content-Type", "application/json")
	if noStore {
		w.Header().Set("Cache-Control", "no-store")
	}
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(v)
}

// serveEmbedded answers with a file under one folder (mount) of the embedded
// static/ tree. Like the Node version, a path that escapes its folder or ends
// in a slash is a 404, even though the embedded tree itself has nothing secret.
func serveEmbedded(w http.ResponseWriter, mount, rel, typ, cache string) {
	if rel == "" || strings.HasSuffix(rel, "/") {
		w.WriteHeader(404)
		return
	}
	rel = path.Clean("/" + mount + "/" + rel)[1:]
	if typ == "" {
		typ = mimeTypes[strings.ToLower(path.Ext(rel))]
	}
	if typ == "" || !strings.HasPrefix(rel, mount+"/") || !fs.ValidPath(rel) {
		w.WriteHeader(404)
		return
	}
	f, err := staticFS.Open(rel)
	if err != nil {
		w.WriteHeader(404)
		return
	}
	defer f.Close()
	if st, err := f.Stat(); err != nil || st.IsDir() {
		w.WriteHeader(404)
		return
	}
	w.Header().Set("Content-Type", typ)
	w.Header().Set("Cache-Control", cache)
	w.WriteHeader(200)
	io.Copy(w, f)
}

func (s *Server) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	// Refuse anything not addressed to loopback (blocks DNS-rebinding pages
	// from reading your prompts through this server).
	host := rePortSuffix.ReplaceAllString(r.Host, "")
	if host != "localhost" && host != "127.0.0.1" && host != "[::1]" {
		w.WriteHeader(403)
		return
	}
	p := r.URL.Path
	q := r.URL.Query()
	header := r.Header.Get("x-whatsmyagentdoing")

	switch {
	case staticFiles[p] != [2]string{}:
		entry := staticFiles[p]
		serveEmbedded(w, "public", entry[0], entry[1], "no-store")

	case strings.HasPrefix(p, "/assets/"):
		// 3D models, served as-is (3D view only).
		rel, _ := url.PathUnescape(strings.TrimPrefix(p, "/assets/"))
		serveEmbedded(w, "assets", rel, "", "no-cache")

	case p == "/api/sessions":
		writeJSON(w, 200, snapshot(s.src, s.opts), false)

	case p == "/api/shutdown" && r.Method == "POST":
		// Only our CLI sends this header; a web page can't add a custom header
		// to a cross-site request without a CORS preflight, which we never allow.
		if header != "stop" || s.shutdown == nil {
			w.WriteHeader(403)
			return
		}
		writeJSON(w, 200, map[string]any{"ok": true}, false)
		s.closeClients()
		time.AfterFunc(50*time.Millisecond, s.shutdown)

	case p == "/api/version":
		// ?check=1 (the page's "Check for updates" button) always goes to GitHub.
		// A plain GET only does so with AutoUpdateCheck on (at most every 30
		// minutes); otherwise it answers from what is already known.
		force := q.Get("check") == "1"
		if force || AutoUpdateCheck {
			s.updater.CheckLatest(force)
		}
		writeJSON(w, 200, s.updater.Info(), true)

	case p == "/api/update" && r.Method == "POST":
		// Same guard as shutdown: web pages can't send this header cross-site.
		if header != "update" {
			w.WriteHeader(403)
			return
		}
		version, err := s.updater.Update()
		if err != nil {
			writeJSON(w, 400, map[string]any{"ok": false, "error": err.Error()}, false)
			return
		}
		writeJSON(w, 200, map[string]any{"ok": true, "version": version}, false)
		s.closeClients()
		if s.restart != nil {
			time.AfterFunc(100*time.Millisecond, s.restart)
		}

	case p == "/api/ping":
		writeJSON(w, 200, map[string]any{"app": "whatsmyagentdoing"}, false)

	case p == "/api/time":
		// This host's clock; the page's corner clock syncs to it through sw.js.
		writeJSON(w, 200, map[string]any{"now": time.Now().UnixMilli()}, true)

	case p == "/events":
		s.serveEvents(w, r)

	default:
		w.WriteHeader(404)
	}
}

func (s *Server) serveEvents(w http.ResponseWriter, r *http.Request) {
	flusher, ok := w.(http.Flusher)
	if !ok {
		w.WriteHeader(500)
		return
	}
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("Connection", "keep-alive")
	w.WriteHeader(200)
	first, _ := json.Marshal(snapshot(s.src, s.opts))
	fmt.Fprintf(w, "data: %s\n\n", first)
	flusher.Flush()

	ch := make(chan string, 8)
	s.mu.Lock()
	s.clients[ch] = struct{}{}
	s.mu.Unlock()
	defer func() {
		s.mu.Lock()
		if _, ok := s.clients[ch]; ok {
			delete(s.clients, ch)
			close(ch)
		}
		s.mu.Unlock()
	}()

	ping := time.NewTicker(20 * time.Second)
	defer ping.Stop()
	for {
		select {
		case body, ok := <-ch:
			if !ok {
				return // server closed this client
			}
			if _, err := fmt.Fprintf(w, "data: %s\n\n", body); err != nil {
				return
			}
			flusher.Flush()
		case <-ping.C:
			if _, err := fmt.Fprint(w, ": ping\n\n"); err != nil {
				return
			}
			flusher.Flush()
		case <-r.Context().Done():
			return
		}
	}
}
