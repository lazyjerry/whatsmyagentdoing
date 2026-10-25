package main

import (
	"testing"
	"time"
)

func TestDemoTeam(t *testing.T) {
	w := NewDemoWatcher()
	if len(w.list) != 10 || !w.list[2].asks || !w.list[6].asks || w.list[0].asks {
		t.Fatalf("team shape: %d members", len(w.list))
	}
	w.Tick()
	select {
	case <-w.Changes():
	default:
		t.Fatal("first tick must signal")
	}
	valid := map[string]bool{"thinking": true, "tool": true, "typing": true, "done": true, "waiting": true}
	seen := map[string]bool{}
	for i, s := range w.Sessions() {
		if s.ID != "demo-"+string(rune('0'+i)) || s.Agent == "" || s.Cwd == "" || s.Prompt == "" || s.Model == "" {
			t.Errorf("member %d: %+v", i, s)
		}
		if !valid[s.Status] {
			t.Errorf("member %d has status %q", i, s.Status)
		}
		if s.Status == "tool" && s.Activity == "" {
			t.Errorf("tool without activity: %+v", s)
		}
		if s.Status == "waiting" && s.Activity != "AskUserQuestion" {
			t.Errorf("waiting activity: %+v", s)
		}
		if (s.Status == "thinking" || s.Status == "typing" || s.Status == "done") && s.Activity != "" {
			t.Errorf("activity must be cleared outside tool/waiting: %+v", s)
		}
		// TurnStartedAt is only set when a member is seen at step 0, so it may
		// still be 0 right after the first tick; the other two are always set.
		if s.LastEventAt == 0 || s.StartedAt == 0 {
			t.Errorf("timestamps: %+v", s)
		}
		seen[s.Agent] = true
	}
	if !seen["claude"] || !seen["codex"] || !seen["gemini"] {
		t.Error("all three agents appear")
	}
}

func TestDemoTickIsIdempotentWithinAStep(t *testing.T) {
	w := NewDemoWatcher()
	w.Tick()
	<-w.Changes()
	before := w.Sessions()
	w.Tick() // same instant, same step: keys unchanged, no signal
	if len(w.Changes()) != 0 {
		// A step boundary may have been crossed in between; only fail if nothing moved.
		after := w.Sessions()
		moved := false
		for i := range before {
			if before[i].Status != after[i].Status || before[i].Activity != after[i].Activity {
				moved = true
			}
		}
		if !moved {
			t.Error("signal without any visible change")
		}
	}
}

func TestDemoScriptCycle(t *testing.T) {
	total := 0.0
	for _, s := range demoScript {
		total += s.secs
	}
	if total != 31 {
		t.Errorf("one cycle is 31s (documented in jerry-report), got %v", total)
	}
	if demoScript[len(demoScript)-1].status != "done" {
		t.Error("cycle ends with done")
	}
}

func TestDemoStartStop(t *testing.T) {
	w := NewDemoWatcher()
	w.Start()
	time.Sleep(20 * time.Millisecond)
	w.Stop()
	w.Stop()
	if len(w.Sessions()) != 10 {
		t.Error("sessions after stop")
	}
}
