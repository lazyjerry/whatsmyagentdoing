package main

// `--demo`: a made-up team cycling through work, for screenshots and for
// trying the UI on a machine with no sessions. Same interface as Watcher.

import (
	"fmt"
	"math"
	"sync"
	"time"
)

type demoMember struct {
	agent, project, prompt string
}

var demoTeam = []demoMember{
	{"claude", "api-server", "Add rate limiting to /v1/orders"},
	{"codex", "landing-page", "Make the hero section responsive"},
	{"claude", "mobile-app", "Fix the crash on the settings screen"},
	{"gemini", "data-pipeline", "Backfill last week of events"},
	{"claude", "design-system", "Add a dark variant to Button"},
	{"claude", "docs-site", "Write the getting-started guide"},
	{"codex", "billing", "Handle failed card retries"},
	{"claude", "auth-service", "Rotate the signing keys"},
	{"gemini", "infra", "Bump the Postgres instance size"},
	{"claude", "blog", "Draft the release notes for v2.3"},
}

var demoTools = map[string][]string{
	"claude": {"Bash: npm test", "Edit routes.ts", "Read schema.prisma", "Grep \"retry\"", "Bash: git diff", "Write README.md", "WebSearch “Postgres upgrade”", "github · create_pull_request"},
	"gemini": {"Shell: npm run lint", "ReadFile schema.sql", "WebSearch “pg_upgrade”", "Edit pipeline.py"},
	"codex":  {"Shell: pnpm test", "Shell: rg TODO src", "Editing app.tsx", "Shell: npx tsc --noEmit", "Shell: git status"},
}

var demoReplies = []string{
	"Done — tests pass and the PR is up.",
	"Fixed. The crash came from a null user on first launch.",
	"Added it behind a feature flag; ready for review.",
	"Updated the docs and the example config.",
}

var demoModels = map[string]string{"claude": "claude-opus-5-5", "codex": "gpt-5-codex", "gemini": "gemini-3-pro"}

// One work cycle, in seconds per step.
var demoScript = []struct {
	status string
	secs   float64
}{
	{"thinking", 3}, {"tool", 4}, {"thinking", 2}, {"tool", 3}, {"typing", 3}, {"done", 16},
}

type demoSession struct {
	Session
	offset float64
	asks   bool
	key    string
}

type DemoWatcher struct {
	mu      sync.Mutex
	list    []*demoSession
	changes chan struct{}
	stop    chan struct{}
}

func NewDemoWatcher() *DemoWatcher {
	now := time.Now().UnixMilli()
	w := &DemoWatcher{changes: make(chan struct{}, 1), stop: make(chan struct{})}
	for i, m := range demoTeam {
		w.list = append(w.list, &demoSession{
			Session: Session{
				Agent:       m.agent,
				ID:          fmt.Sprintf("demo-%d", i),
				Cwd:         "/home/dev/" + m.project,
				Title:       m.prompt,
				Model:       demoModels[m.agent],
				Status:      "idle",
				Prompt:      m.prompt,
				LastEventAt: now,
				StartedAt:   now - int64(i+1)*600_000,
			},
			// Stagger the team so some work while others take a break.
			offset: math.Mod(float64(i)*7.3, 37),
			// A couple of them sometimes stop to ask you something.
			asks: i == 2 || i == 6,
		})
	}
	return w
}

func (w *DemoWatcher) Changes() <-chan struct{} { return w.changes }

func (w *DemoWatcher) Start() {
	w.Tick()
	go func() {
		t := time.NewTicker(500 * time.Millisecond)
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

func (w *DemoWatcher) Stop() {
	select {
	case <-w.stop:
	default:
		close(w.stop)
	}
}

func (w *DemoWatcher) Tick() {
	t := float64(time.Now().UnixMilli()) / 1000
	total := 0.0
	for _, step := range demoScript {
		total += step.secs
	}
	changed := false
	w.mu.Lock()
	for _, s := range w.list {
		cycle := int(math.Floor((t + s.offset) / total))
		k := math.Mod(t+s.offset, total)
		step := 0
		for k >= demoScript[step].secs {
			k -= demoScript[step].secs
			step++
		}
		status := demoScript[step].status
		if s.asks && cycle%2 == 1 && status == "typing" {
			status = "waiting"
		}
		key := fmt.Sprintf("%d:%d:%s", cycle, step, status)
		if key == s.key {
			continue
		}
		s.key = key
		changed = true
		now := time.Now().UnixMilli()
		if step == 0 {
			s.TurnStartedAt = now - int64(k*1000)
			s.Reply = ""
		}
		s.Status = status
		s.LastEventAt = now
		tools := demoTools[s.Agent]
		switch status {
		case "tool":
			s.Activity = tools[(cycle*3+step+int(s.offset))%len(tools)]
		case "waiting":
			s.Activity = "AskUserQuestion"
		default:
			s.Activity = ""
		}
		if status == "done" {
			s.Reply = demoReplies[(cycle+int(s.offset))%len(demoReplies)]
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

func (w *DemoWatcher) Sessions() []Session {
	w.mu.Lock()
	defer w.mu.Unlock()
	out := make([]Session, 0, len(w.list))
	for _, s := range w.list {
		out = append(out, s.Session)
	}
	return out
}
