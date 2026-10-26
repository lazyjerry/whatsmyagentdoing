package main

import (
	"net/url"
	"os"
	"path/filepath"
)

func urlHost(raw string) string {
	u, err := url.Parse(raw)
	if err != nil {
		return ""
	}
	return u.Host
}

func fileExists(p string) bool {
	st, err := os.Stat(p)
	return err == nil && !st.IsDir()
}

func dirExists(p string) bool {
	st, err := os.Stat(p)
	return err == nil && st.IsDir()
}

// sourceDir finds the go-app/ source folder this binary was built from, which
// is only needed for --update (git pull in the clone, then go build here).
// Order: WMAD_SRC, the folders exeSourceCandidates derives from the
// executable, the working directory. Returns "" when there is none; the
// binary works fine without it, updates are just off.
func sourceDir() string {
	var candidates []string
	if env := os.Getenv("WMAD_SRC"); env != "" {
		candidates = append(candidates, env)
	}
	if exe, err := os.Executable(); err == nil {
		if real, err := filepath.EvalSymlinks(exe); err == nil {
			exe = real
		}
		candidates = append(candidates, exeSourceCandidates(exe)...)
	}
	if cwd, err := os.Getwd(); err == nil {
		candidates = append(candidates, cwd)
	}
	for _, c := range candidates {
		if abs := asSourceDir(c); abs != "" {
			return abs
		}
	}
	return ""
}

// exeSourceCandidates lists where go-app/ may sit relative to the executable:
// its own folder (the README's `go build -o whatsmyagentdoing .` leaves it
// there) and <clone>/go-app when scripts/installSkill.sh copied it into
// <clone>/skills/myoffice/bin/.
func exeSourceCandidates(exe string) []string {
	dir := filepath.Dir(exe)
	return []string{dir, filepath.Join(dir, "..", "..", "..", "go-app")}
}

// asSourceDir returns c as an absolute path when it holds go.mod and main.go
// and its parent is a git clone, "" otherwise.
func asSourceDir(c string) string {
	abs, err := filepath.Abs(c)
	if err != nil {
		return ""
	}
	if fileExists(filepath.Join(abs, "go.mod")) && fileExists(filepath.Join(abs, "main.go")) && dirExists(filepath.Join(filepath.Dir(abs), ".git")) {
		return abs
	}
	return ""
}
