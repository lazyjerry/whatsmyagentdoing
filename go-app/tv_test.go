package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func tvList(t *testing.T, s *Server) []string {
	t.Helper()
	rec := do(s, "GET", "/api/tv", nil)
	var body struct{ Clips []string }
	if rec.Code != 200 || json.Unmarshal(rec.Body.Bytes(), &body) != nil {
		t.Fatalf("/api/tv: %d %s", rec.Code, rec.Body.String())
	}
	return body.Clips
}

func TestTVPlaysBuiltInClipsWhileTheFolderHasNone(t *testing.T) {
	dir := t.TempDir()
	os.WriteFile(filepath.Join(dir, "notes.txt"), []byte("x"), 0o644)
	s := newTestServer(t, newFakeSource(), Options{TVDir: dir}, nil, nil)
	clips := tvList(t, s)
	if len(clips) != 4 {
		t.Fatalf("built-in clips: %v", clips)
	}
	for _, c := range clips {
		if !strings.HasPrefix(c, "/assets/tv/") {
			t.Errorf("not a built-in clip: %s", c)
		}
		if rec := do(s, "GET", c, nil); rec.Code != 200 || rec.Header().Get("Content-Type") != "image/gif" || !strings.HasPrefix(rec.Body.String(), "GIF8") {
			t.Errorf("%s: %d %q", c, rec.Code, rec.Header().Get("Content-Type"))
		}
	}
	if rec := do(s, "GET", "/assets/tv/README.md", nil); rec.Code != 404 {
		t.Errorf("README served: %d", rec.Code)
	}
	// A folder that does not exist plays the built-ins too.
	s = newTestServer(t, newFakeSource(), Options{TVDir: filepath.Join(dir, "missing")}, nil, nil)
	if len(tvList(t, s)) != 4 {
		t.Error("missing folder should fall back to the built-in clips")
	}
}

func TestTVPlaysTheFolderInNameOrder(t *testing.T) {
	dir := t.TempDir()
	gif := []byte("GIF89a fake")
	for _, n := range []string{"b clip.gif", "a.GIF", ".hidden.gif", "big.gif", "still.png"} {
		os.WriteFile(filepath.Join(dir, n), gif, 0o644)
	}
	os.Truncate(filepath.Join(dir, "big.gif"), TVMaxBytes+1)
	os.Mkdir(filepath.Join(dir, "folder.gif"), 0o755)
	os.WriteFile(filepath.Join(filepath.Dir(dir), "outside.gif"), gif, 0o644)
	s := newTestServer(t, newFakeSource(), Options{TVDir: dir}, nil, nil)
	clips := tvList(t, s)
	if strings.Join(clips, ",") != "/api/tv/a.GIF,/api/tv/b%20clip.gif" {
		t.Fatalf("clips: %v", clips)
	}
	rec := do(s, "GET", "/api/tv/b%20clip.gif", nil)
	if rec.Code != 200 || rec.Header().Get("Content-Type") != "image/gif" || rec.Body.String() != string(gif) {
		t.Errorf("clip: %d %q %q", rec.Code, rec.Header().Get("Content-Type"), rec.Body.String())
	}
	for _, p := range []string{"/api/tv/", "/api/tv/.hidden.gif", "/api/tv/big.gif", "/api/tv/still.png", "/api/tv/folder.gif",
		"/api/tv/missing.gif", "/api/tv/../outside.gif", "/api/tv/..%2Foutside.gif", "/api/tv/x%5C..%5Coutside.gif"} {
		if rec := do(s, "GET", p, nil); rec.Code != 404 {
			t.Errorf("%s: %d, want 404", p, rec.Code)
		}
	}
}

func TestParseArgsTVDir(t *testing.T) {
	if o := parseArgs(strings.Fields("--tv-dir /tmp/tv"), "0.0.0"); o.TVDir != "/tmp/tv" {
		t.Errorf("--tv-dir: %+v", o)
	}
	if o := parseArgs(strings.Fields("--tv-dir=/tmp/tv2"), "0.0.0"); o.TVDir != "/tmp/tv2" {
		t.Errorf("--tv-dir=: %+v", o)
	}
	if !strings.Contains(helpText("0.0.0"), "--tv-dir") {
		t.Error("help lacks --tv-dir")
	}
}
