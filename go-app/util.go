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
// Order: WMAD_SRC, the executable's own folder (the README's `go build -o
// whatsmyagentdoing .` leaves it there), the working directory. A folder
// counts when it holds go.mod and its parent is a git clone. Returns "" when
// there is none; the binary works fine without it, updates are just off.
func sourceDir() string {
	var candidates []string
	if env := os.Getenv("WMAD_SRC"); env != "" {
		candidates = append(candidates, env)
	}
	if exe, err := os.Executable(); err == nil {
		if real, err := filepath.EvalSymlinks(exe); err == nil {
			exe = real
		}
		candidates = append(candidates, filepath.Dir(exe))
	}
	if cwd, err := os.Getwd(); err == nil {
		candidates = append(candidates, cwd)
	}
	for _, c := range candidates {
		abs, err := filepath.Abs(c)
		if err != nil {
			continue
		}
		if fileExists(filepath.Join(abs, "go.mod")) && fileExists(filepath.Join(abs, "main.go")) && dirExists(filepath.Join(filepath.Dir(abs), ".git")) {
			return abs
		}
	}
	return ""
}
