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

// findRoot locates the repository root (the clone that holds public/, assets/
// and scenes.json). Static files are served from there rather than embedded,
// so the Go build shares one copy with the Node version. Order: WMAD_ROOT,
// the executable's parent folder (binary built inside go-app/), the
// executable's own folder, the working directory's parent (`go run .` inside
// go-app/), the working directory.
func findRoot() (string, error) {
	var candidates []string
	if env := os.Getenv("WMAD_ROOT"); env != "" {
		candidates = append(candidates, env)
	}
	if exe, err := os.Executable(); err == nil {
		if real, err := filepath.EvalSymlinks(exe); err == nil {
			exe = real
		}
		dir := filepath.Dir(exe)
		candidates = append(candidates, filepath.Dir(dir), dir)
	}
	if cwd, err := os.Getwd(); err == nil {
		candidates = append(candidates, filepath.Dir(cwd), cwd)
	}
	for _, c := range candidates {
		abs, err := filepath.Abs(c)
		if err != nil {
			continue
		}
		if fileExists(filepath.Join(abs, "public", "index.html")) && fileExists(filepath.Join(abs, "scenes.json")) {
			return abs, nil
		}
	}
	return "", errRootNotFound
}

type rootError struct{}

func (rootError) Error() string {
	return "cannot find the whatsmyagentdoing clone (public/index.html); build inside <clone>/go-app or set WMAD_ROOT=<clone>"
}

var errRootNotFound error = rootError{}
