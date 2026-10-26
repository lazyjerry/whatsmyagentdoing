package main

import (
	"errors"
	"net"
	"strings"
	"syscall"
	"testing"
)

func TestParseArgsDefaults(t *testing.T) {
	o := parseArgs(nil, "0.0.0")
	if o.Port != 4545 || o.Hours != 24 || !o.Open || o.Privacy || o.JSON || o.Demo || o.Stop || o.Update || o.Scenes || o.Scene != "" || o.Remove != "" {
		t.Errorf("defaults: %+v", o)
	}
}

func TestParseArgsAllFlags(t *testing.T) {
	o := parseArgs(strings.Fields("-p 5000 --hours 2 --no-open --private --json --demo --stop --update --scenes --scene cafe --remove-scene castle"), "0.0.0")
	if o.Port != 5000 || o.Hours != 2 || o.Open || !o.Privacy || !o.JSON || !o.Demo || !o.Stop || !o.Update || !o.Scenes || o.Scene != "cafe" || o.Remove != "castle" {
		t.Errorf("flags: %+v", o)
	}
	o = parseArgs(strings.Fields("--port=4546 --hours=1.5 --scene=station"), "0.0.0")
	if o.Port != 4546 || o.Hours != 1.5 || o.Scene != "station" {
		t.Errorf("equals form: %+v", o)
	}
	o = parseArgs(strings.Fields("--port 4547"), "0.0.0")
	if o.Port != 4547 {
		t.Errorf("long port: %+v", o)
	}
}

func TestHelpTextMentionsEveryFlag(t *testing.T) {
	h := helpText("1.2.3")
	for _, flag := range []string{"1.2.3", "--port", "--hours", "--no-open", "--private", "--json", "--demo", "--stop", "--update", "--scenes", "--scene", "--remove-scene", "CLAUDE_CONFIG_DIR", "WMAD_SRC"} {
		if !strings.Contains(h, flag) {
			t.Errorf("help lacks %s", flag)
		}
	}
	if !strings.Contains(h, claudeDir) || !strings.Contains(h, codexDir) || !strings.Contains(h, geminiDir) || !strings.Contains(h, ocDB) {
		t.Error("help lists the log directories it reads")
	}
}

func TestIsAddrInUse(t *testing.T) {
	if !isAddrInUse(syscall.EADDRINUSE) || !isAddrInUse(errors.New("listen tcp 127.0.0.1:4545: bind: address already in use")) {
		t.Error("EADDRINUSE forms")
	}
	if !isAddrInUse(errors.New("Only one usage of each socket address is normally permitted")) {
		t.Error("windows wording")
	}
	if isAddrInUse(errors.New("permission denied")) {
		t.Error("other errors")
	}
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer ln.Close()
	if _, err := net.Listen("tcp", ln.Addr().String()); err == nil || !isAddrInUse(err) {
		t.Errorf("a real bind clash must be detected: %v", err)
	}
}

func TestAlreadyRunningAndStopOnFreePort(t *testing.T) {
	port := freePort(t)
	if alreadyRunning(port) {
		t.Error("nothing listens there")
	}
	if stopAt(port) {
		t.Error("nothing to stop")
	}
}

// freePort returns a loopback port nothing listens on right now.
func freePort(t *testing.T) int {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	port := ln.Addr().(*net.TCPAddr).Port
	ln.Close()
	return port
}
