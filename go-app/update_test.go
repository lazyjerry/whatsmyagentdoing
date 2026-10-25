package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestNewer(t *testing.T) {
	cases := []struct {
		a, b string
		want bool
	}{
		{"0.4.0", "0.3.0", true},
		{"0.3.0", "0.3.0", false},
		{"0.3", "0.3.0", false},
		{"0.3.10", "0.3.9", true},
		{"1.0.0", "0.9.9", true},
		{"0.2.9", "0.3.0", false},
		{"x", "0.3.0", false},
	}
	for _, c := range cases {
		if got := newer(c.a, c.b); got != c.want {
			t.Errorf("newer(%q, %q) = %v", c.a, c.b, got)
		}
	}
}

func TestLocalVersionIsEmbedded(t *testing.T) {
	u := NewUpdater()
	if u.LocalVersion() != embeddedVersion || embeddedVersion == "0.0.0" || embeddedVersion == "" {
		t.Errorf("LocalVersion=%q embedded=%q", u.LocalVersion(), embeddedVersion)
	}
	disk, err := os.ReadFile(filepath.Join("static", "VERSION"))
	if err == nil && strings.TrimSpace(string(disk)) != embeddedVersion {
		t.Errorf("embedded %q differs from static/VERSION %q: rebuild after sync", embeddedVersion, strings.TrimSpace(string(disk)))
	}
}

func TestCheckLatestEnvOverrides(t *testing.T) {
	t.Setenv("WMAD_NO_UPDATE_CHECK", "1")
	t.Setenv("WMAD_LATEST_VERSION", "9.9.9")
	u := NewUpdater()
	if got := u.CheckLatest(true); got != "" {
		t.Errorf("WMAD_NO_UPDATE_CHECK wins: %q", got)
	}
	if info := u.Info(); info.Latest != nil || info.UpdateAvailable {
		t.Errorf("no latest known: %+v", info)
	}

	t.Setenv("WMAD_NO_UPDATE_CHECK", "")
	if got := u.CheckLatest(false); got != "9.9.9" {
		t.Errorf("WMAD_LATEST_VERSION: %q", got)
	}
	info := u.Info()
	if info.Latest == nil || *info.Latest != "9.9.9" || !info.UpdateAvailable || info.Version != embeddedVersion || info.Auto != AutoUpdateCheck {
		t.Errorf("info: %+v", info)
	}

	t.Setenv("WMAD_LATEST_VERSION", embeddedVersion)
	u.CheckLatest(true)
	if u.Info().UpdateAvailable {
		t.Error("same version is not an update")
	}
}

func TestSourceDir(t *testing.T) {
	clone := t.TempDir()
	src := filepath.Join(clone, "go-app")
	writeFile(t, filepath.Join(src, "go.mod"), "module x\n")
	writeFile(t, filepath.Join(src, "main.go"), "package main\n")
	if err := os.MkdirAll(filepath.Join(clone, ".git"), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("WMAD_SRC", src)
	if got := sourceDir(); got != src {
		t.Errorf("WMAD_SRC: %q", got)
	}

	// Not a clone: fall through. The working directory of `go test` is the
	// real go-app/, which is a valid source folder, so step away from it.
	noGit := t.TempDir()
	writeFile(t, filepath.Join(noGit, "go-app", "go.mod"), "module x\n")
	writeFile(t, filepath.Join(noGit, "go-app", "main.go"), "package main\n")
	t.Setenv("WMAD_SRC", filepath.Join(noGit, "go-app"))
	chdir(t, t.TempDir())
	if got := sourceDir(); got != "" {
		t.Errorf("without .git in the parent: %q", got)
	}
	t.Setenv("WMAD_SRC", "")
	if got := sourceDir(); got != "" {
		t.Errorf("test binary dir and a bare temp cwd are not sources: %q", got)
	}
}

func TestUpdateRefusesOutsideSource(t *testing.T) {
	t.Setenv("WMAD_SRC", t.TempDir())
	chdir(t, t.TempDir())
	u := NewUpdater()
	if u.CanUpdate() {
		t.Fatal("CanUpdate must be false outside a source folder")
	}
	if _, err := u.Update(); err == nil || !strings.Contains(err.Error(), "go-app/ source folder") {
		t.Errorf("Update must refuse without touching git: %v", err)
	}
}

// chdir changes the working directory for one test and restores it.
func chdir(t *testing.T, dir string) {
	t.Helper()
	old, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Chdir(dir); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.Chdir(old) })
}
