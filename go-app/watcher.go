package main

// Finds recent session logs on disk and keeps reading them as they grow.
// Polling (not fsnotify) because it behaves the same on macOS, Linux and
// Windows, and a 1s stat loop over a few hundred files is cheap. Port of
// src/watcher.js.

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"time"
)

const (
	headBytes = 128 * 1024
	tailBytes = 768 * 1024
)

var (
	homeDir    = userHome()
	claudeDir  = filepath.Join(envOr("CLAUDE_CONFIG_DIR", filepath.Join(homeDir, ".claude")), "projects")
	codexHome  = envOr("CODEX_HOME", filepath.Join(homeDir, ".codex"))
	codexDir   = filepath.Join(codexHome, "sessions")
	codexIndex = filepath.Join(codexHome, "session_index.jsonl")
	// Gemini CLI: ~/.gemini (or $GEMINI_CLI_HOME/.gemini); sandboxed runs on
	// macOS keep their state under ~/.cache/.gemini instead.
	geminiHomes = []string{
		filepath.Join(envOr("GEMINI_CLI_HOME", homeDir), ".gemini"),
		filepath.Join(homeDir, ".cache", ".gemini"),
	}
	geminiDir = filepath.Join(geminiHomes[0], "tmp")
)

func userHome() string {
	h, err := os.UserHomeDir()
	if err != nil {
		return "."
	}
	return h
}

func envOr(key, def string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return def
}

func ls(dir string) []os.DirEntry {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil
	}
	return entries
}

func isFile(e os.DirEntry) bool { return e.Type().IsRegular() }

func statSafe(f string) os.FileInfo {
	st, err := os.Stat(f)
	if err != nil {
		return nil
	}
	return st
}

func mtimeMs(st os.FileInfo) int64 { return st.ModTime().UnixMilli() }

func readRange(file string, start, end int64) (string, error) {
	n := end - start
	if n <= 0 {
		return "", nil
	}
	f, err := os.Open(file)
	if err != nil {
		return "", err
	}
	defer f.Close()
	buf := make([]byte, n)
	read, err := f.ReadAt(buf, start)
	if err != nil && read == 0 {
		return "", err
	}
	return string(buf[:read]), nil
}

type candidate struct {
	agent    string
	file     string
	id       string
	parentID string
	project  string
}

func claudeFiles() []candidate {
	var out []candidate
	for _, proj := range ls(claudeDir) {
		if !proj.IsDir() {
			continue
		}
		pdir := filepath.Join(claudeDir, proj.Name())
		for _, e := range ls(pdir) {
			if isFile(e) && strings.HasSuffix(e.Name(), ".jsonl") {
				out = append(out, candidate{agent: "claude", file: filepath.Join(pdir, e.Name()), id: strings.TrimSuffix(e.Name(), ".jsonl")})
			} else if e.IsDir() {
				sub := filepath.Join(pdir, e.Name(), "subagents")
				for _, s := range ls(sub) {
					if isFile(s) && strings.HasSuffix(s.Name(), ".jsonl") {
						out = append(out, candidate{agent: "claude", file: filepath.Join(sub, s.Name()), id: strings.TrimSuffix(s.Name(), ".jsonl"), parentID: e.Name()})
					}
				}
			}
		}
	}
	return out
}

// A session renamed in the VS Code extension keeps the name next to its log,
// in <project>/<sessionId>/custom-title.json; the log itself may predate it.
func claudeCustomTitle(logFile string) string {
	side := filepath.Join(strings.TrimSuffix(logFile, ".jsonl"), "custom-title.json")
	b, err := os.ReadFile(side)
	if err != nil {
		return ""
	}
	var v struct {
		CustomTitle string `json:"customTitle"`
	}
	if json.Unmarshal(b, &v) != nil {
		return ""
	}
	return strings.TrimSpace(v.CustomTitle)
}

var (
	reGeminiSession = regexp.MustCompile(`^session-.*\.jsonl?$`)
	reJsonlExt      = regexp.MustCompile(`\.jsonl?$`)
	reCodexID       = regexp.MustCompile(`([0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12})\.jsonl$`)
)

func geminiFiles() []candidate {
	// <root>/tmp/<project>/chats/session-*.jsonl (+ legacy .json), and
	// subagents in <root>/tmp/<project>/chats/<parentSessionId>/<id>.jsonl
	var out []candidate
	for _, root := range geminiHomes {
		tmp := filepath.Join(root, "tmp")
		for _, proj := range ls(tmp) {
			if !proj.IsDir() {
				continue
			}
			chats := filepath.Join(tmp, proj.Name(), "chats")
			for _, e := range ls(chats) {
				if isFile(e) && reGeminiSession.MatchString(e.Name()) {
					out = append(out, candidate{agent: "gemini", file: filepath.Join(chats, e.Name()), id: reJsonlExt.ReplaceAllString(e.Name(), ""), project: proj.Name()})
				} else if e.IsDir() {
					for _, f := range ls(filepath.Join(chats, e.Name())) {
						if isFile(f) && strings.HasSuffix(f.Name(), ".jsonl") {
							out = append(out, candidate{agent: "gemini", file: filepath.Join(chats, e.Name(), f.Name()), id: strings.TrimSuffix(f.Name(), ".jsonl"), project: proj.Name(), parentID: e.Name()})
						}
					}
				}
			}
		}
	}
	return out
}

func codexFiles(sinceMs int64) []candidate {
	// sessions/YYYY/MM/DD/rollout-*.jsonl — only walk days inside the window.
	var out []candidate
	cutoff := time.UnixMilli(sinceMs - 86400000)
	minKey := cutoff.Format("2006-01-02")
	for _, y := range ls(codexDir) {
		if !y.IsDir() {
			continue
		}
		for _, m := range ls(filepath.Join(codexDir, y.Name())) {
			if !m.IsDir() {
				continue
			}
			for _, d := range ls(filepath.Join(codexDir, y.Name(), m.Name())) {
				if !d.IsDir() || y.Name()+"-"+m.Name()+"-"+d.Name() < minKey {
					continue
				}
				dir := filepath.Join(codexDir, y.Name(), m.Name(), d.Name())
				for _, f := range ls(dir) {
					if !isFile(f) || !strings.HasSuffix(f.Name(), ".jsonl") {
						continue
					}
					id := f.Name()
					if m := reCodexID.FindStringSubmatch(f.Name()); m != nil {
						id = m[1]
					}
					out = append(out, candidate{agent: "codex", file: filepath.Join(dir, f.Name()), id: id})
				}
			}
		}
	}
	return out
}

type applyFunc func(*Session, any)

type tracker struct {
	session *Session
	offset  int64
	partial string
	apply   applyFunc
	mtime   int64
	whole   bool
}

// Source is what the server reads sessions from: the real Watcher or the demo.
type Source interface {
	Sessions() []Session
	Changes() <-chan struct{}
	Start()
	Stop()
}

type Watcher struct {
	mu                  sync.Mutex
	windowMs            int64
	interval            time.Duration
	trackers            map[string]*tracker // file -> state
	failed              map[string]int64    // file -> retry-after time, for logs we couldn't read
	codexNames          map[string]string
	geminiProjects      map[string]string // tmp dir name -> project path
	geminiRegistryMtime int64
	codexIndexMtime     int64
	agy                 map[string]*Session // conversation id -> Antigravity row (see antigravity.go)
	agyMtime            int64               // db + WAL mtime at the last successful read
	agyUnavailable      bool                // sqlite3 missing or the db unreadable: logged once, then skipped
	lastScan            int64
	candidates          []candidate
	changes             chan struct{}
	stop                chan struct{}
}

func NewWatcher(hours float64) *Watcher {
	return &Watcher{
		windowMs:       int64(hours * 3600 * 1000),
		interval:       time.Second,
		trackers:       map[string]*tracker{},
		failed:         map[string]int64{},
		codexNames:     map[string]string{},
		geminiProjects: map[string]string{},
		agy:            map[string]*Session{},
		changes:        make(chan struct{}, 1),
		stop:           make(chan struct{}),
	}
}

func (w *Watcher) Changes() <-chan struct{} { return w.changes }

func (w *Watcher) Start() {
	w.Tick()
	go func() {
		t := time.NewTicker(w.interval)
		defer t.Stop()
		for {
			select {
			case <-t.C:
				w.Tick()
			case <-w.stop:
				return
			}
		}
	}()
}

func (w *Watcher) Stop() {
	select {
	case <-w.stop:
	default:
		close(w.stop)
	}
}

// ForceRescan makes the next Tick walk the directories again (used by --json).
func (w *Watcher) ForceRescan() {
	w.mu.Lock()
	w.lastScan = 0
	w.mu.Unlock()
}

func (w *Watcher) loadCodexIndex() bool {
	st := statSafe(codexIndex)
	if st == nil || mtimeMs(st) == w.codexIndexMtime {
		return false
	}
	w.codexIndexMtime = mtimeMs(st)
	start := st.Size() - 2*1024*1024
	if start < 0 {
		start = 0
	}
	text, _ := readRange(codexIndex, start, st.Size())
	for _, line := range strings.Split(text, "\n") {
		var d map[string]any
		if json.Unmarshal([]byte(line), &d) != nil {
			continue
		}
		if id, name := str(d, "id"), str(d, "thread_name"); id != "" && name != "" {
			w.codexNames[id] = name
		}
	}
	return true
}

// ~/.gemini/projects.json maps project paths to the short slugs used as
// tmp dir names; older versions used sha256(path) instead.
func (w *Watcher) loadGeminiProjects() bool {
	var mt int64
	files := make([]string, 0, len(geminiHomes))
	for _, r := range geminiHomes {
		f := filepath.Join(r, "projects.json")
		files = append(files, f)
		if st := statSafe(f); st != nil {
			mt += mtimeMs(st)
		}
	}
	if mt == w.geminiRegistryMtime {
		return false
	}
	w.geminiRegistryMtime = mt
	for _, f := range files {
		raw, err := os.ReadFile(f)
		if err != nil {
			continue
		}
		var reg struct {
			Projects map[string]string `json:"projects"`
		}
		if json.Unmarshal(raw, &reg) != nil {
			continue
		}
		for dir, slug := range reg.Projects {
			w.geminiProjects[slug] = dir
			sum := sha256.Sum256([]byte(dir))
			w.geminiProjects[hex.EncodeToString(sum[:])] = dir
		}
	}
	return true
}

// loadAntigravity re-reads conversation_summaries.db when it (or its WAL)
// changed; returns true when any session differs from the last read.
func (w *Watcher) loadAntigravity(since int64) bool {
	if w.agyUnavailable {
		return false
	}
	st := statSafe(agyDB)
	if st == nil {
		return false
	}
	mtime := mtimeMs(st)
	if wal := statSafe(agyDB + "-wal"); wal != nil && mtimeMs(wal) > mtime {
		mtime = mtimeMs(wal)
	}
	if mtime == w.agyMtime {
		return false
	}
	rows, err := agyRows(agyDB, since)
	if err != nil {
		w.agyUnavailable = true
		fmt.Fprintf(os.Stderr, "antigravity: %s not read (%v); Antigravity sessions are off until restart\n", agyDB, err)
		return false
	}
	w.agyMtime = mtime
	next := map[string]*Session{}
	changed := len(rows) != len(w.agy)
	for _, r := range rows {
		s := agySession(r)
		next[s.ID] = s
		if old := w.agy[s.ID]; old == nil || *old != *s {
			changed = true
		}
	}
	w.agy = next
	return changed
}

func (w *Watcher) Tick() {
	w.mu.Lock()
	now := time.Now().UnixMilli()
	since := now - w.windowMs
	changed := w.loadCodexIndex()
	if w.loadGeminiProjects() {
		changed = true
	}
	if w.loadAntigravity(since) {
		changed = true
	}

	// Directory walk every 5s; stat known files every tick.
	if now-w.lastScan > 5000 {
		w.lastScan = now
		w.candidates = append(append(claudeFiles(), codexFiles(since)...), geminiFiles()...)
	}

	for _, c := range w.candidates {
		// A log we can't read (permissions, deleted mid-read…) is skipped and
		// retried a minute later; it must never take the server down.
		if w.failed[c.file] > now {
			continue
		}
		ch, err := w.track(c, since)
		if err != nil {
			w.failed[c.file] = now + 60000
			delete(w.trackers, c.file)
			continue
		}
		if ch {
			changed = true
		}
	}

	for file, t := range w.trackers {
		last := t.session.LastEventAt
		if last == 0 {
			last = t.session.Mtime
		}
		if last < since {
			delete(w.trackers, file)
			changed = true
		}
	}
	for id, s := range w.agy {
		if s.LastEventAt < since {
			delete(w.agy, id)
			changed = true
		}
	}
	w.mu.Unlock()

	if changed {
		select {
		case w.changes <- struct{}{}:
		default:
		}
	}
}

// track returns true when this file's session changed.
func (w *Watcher) track(c candidate, since int64) (bool, error) {
	st := statSafe(c.file)
	if st == nil {
		return false, nil
	}
	t := w.trackers[c.file]
	if t == nil {
		if mtimeMs(st) < since {
			return false, nil
		}
		return true, w.open(c, st)
	}
	changed := false
	switch {
	case t.whole && mtimeMs(st) != t.mtime:
		// legacy single-document session: re-read it whole
		delete(w.trackers, c.file)
		return true, w.open(c, st)
	case st.Size() > t.offset:
		chunk, err := readRange(c.file, t.offset, st.Size())
		if err != nil {
			return false, err
		}
		feed(t, chunk, false, false)
		t.offset = st.Size()
		changed = true
	case st.Size() < t.offset:
		// File was rewritten; start over.
		delete(w.trackers, c.file)
	}
	t.session.Mtime = mtimeMs(st)
	return changed, nil
}

func (w *Watcher) open(c candidate, st os.FileInfo) error {
	session := newSession(c.agent, c.id, c.file)
	if c.parentID != "" {
		session.ParentID = c.parentID
		session.Subagent = true
	}
	session.Mtime = mtimeMs(st)
	if c.agent == "gemini" {
		session.GeminiProject = c.project
	}
	if c.agent == "claude" && c.parentID == "" {
		if t := claudeCustomTitle(c.file); t != "" {
			session.Title = t
			session.titleLocked = true
		}
	}
	apply := map[string]applyFunc{"claude": applyClaude, "codex": applyCodex, "gemini": applyGemini}[c.agent]
	t := &tracker{session: session, offset: st.Size(), apply: apply, mtime: mtimeMs(st)}
	if c.agent == "gemini" && strings.HasSuffix(c.file, ".json") {
		t.whole = true
		if text, err := readRange(c.file, 0, st.Size()); err == nil {
			var doc any
			if json.Unmarshal([]byte(text), &doc) == nil {
				applyGeminiDocument(session, doc)
			}
		}
		w.trackers[c.file] = t
		return nil
	}
	if st.Size() <= headBytes+tailBytes {
		text, err := readRange(c.file, 0, st.Size())
		if err != nil {
			return err
		}
		feed(t, text, false, false)
	} else {
		// Big log: head for metadata (cwd, title, model), tail for current state.
		head, err := readRange(c.file, 0, headBytes)
		if err != nil {
			return err
		}
		feed(t, head, true, false)
		t.partial = ""
		tail, err := readRange(c.file, st.Size()-tailBytes, st.Size())
		if err != nil {
			return err
		}
		feed(t, tail, false, true)
	}
	w.trackers[c.file] = t
	return nil
}

func feed(t *tracker, chunk string, dropLastPartial, dropFirst bool) {
	lines := strings.Split(t.partial+chunk, "\n")
	t.partial = lines[len(lines)-1]
	lines = lines[:len(lines)-1]
	if dropFirst && len(lines) > 0 {
		lines = lines[1:]
	}
	if dropLastPartial {
		t.partial = ""
	}
	for _, line := range lines {
		if line == "" {
			continue
		}
		var d any
		if json.Unmarshal([]byte(line), &d) != nil {
			continue
		}
		t.apply(t.session, d)
	}
}

func (w *Watcher) Sessions() []Session {
	w.mu.Lock()
	defer w.mu.Unlock()
	out := make([]Session, 0, len(w.trackers))
	for _, t := range w.trackers {
		s := t.session
		if s.Agent == "gemini" && s.Cwd == "" {
			if dir, ok := w.geminiProjects[s.GeminiProject]; ok {
				s.Cwd = dir
			}
		}
		if s.Agent == "gemini" && s.Cwd == "" {
			s.Cwd = s.GeminiProject
		}
		// Gemini re-appends whole messages with their original timestamps, so
		// the file's own write time is the best "last activity" signal.
		if s.Agent == "gemini" && s.Mtime > s.LastEventAt {
			s.LastEventAt = s.Mtime
		}
		if s.Agent == "codex" && s.Title == "" {
			if name, ok := w.codexNames[s.ID]; ok {
				s.Title = name
			}
		}
		out = append(out, *s)
	}
	for _, s := range w.agy {
		out = append(out, *s)
	}
	return out
}

// Count is len(Sessions()) without the copy.
func (w *Watcher) Count() int {
	w.mu.Lock()
	defer w.mu.Unlock()
	return len(w.trackers) + len(w.agy)
}

func describeDirs() string {
	return fmt.Sprintf("  %s\n  %s\n  %s\n  %s", claudeDir, codexDir, geminiDir, agyDB)
}
