package main

// The TV in the sofa corner plays GIFs: the ones in the TV folder (--tv-dir,
// default ~/TVDir) in file-name order, or, while that folder has none, the
// public-domain clips built into the binary (static/assets/tv/). The page
// decodes them itself; this side only lists and serves the files.

import (
	"io/fs"
	"net/http"
	"net/url"
	"os"
	"path"
	"path/filepath"
	"strings"
)

func (s *Server) tvDir() string {
	if s.opts.TVDir != "" {
		return s.opts.TVDir
	}
	return filepath.Join(userHome(), TVDir)
}

// tvFile is a GIF in the TV folder the page may fetch: a bare file name (no
// folders, not hidden) ending in .gif, naming a regular file within TVMaxBytes.
func tvFile(dir, name string) (string, os.FileInfo, bool) {
	if name == "" || strings.ContainsAny(name, `/\`) || strings.HasPrefix(name, ".") || !strings.EqualFold(filepath.Ext(name), ".gif") {
		return "", nil, false
	}
	f := filepath.Join(dir, name)
	st, err := os.Stat(f)
	if err != nil || !st.Mode().IsRegular() || st.Size() > TVMaxBytes {
		return "", nil, false
	}
	return f, st, true
}

// tvClips lists the URLs the page plays, in order.
func (s *Server) tvClips() []string {
	clips := []string{}
	dir := s.tvDir()
	for _, e := range ls(dir) {
		if _, _, ok := tvFile(dir, e.Name()); ok {
			clips = append(clips, "/api/tv/"+url.PathEscape(e.Name()))
		}
	}
	if len(clips) == 0 {
		entries, _ := fs.ReadDir(staticFS, "assets/tv")
		for _, e := range entries {
			if !e.IsDir() && strings.EqualFold(path.Ext(e.Name()), ".gif") {
				clips = append(clips, "/assets/tv/"+url.PathEscape(e.Name()))
			}
		}
	}
	if len(clips) > TVMaxClips {
		clips = clips[:TVMaxClips]
	}
	return clips
}

func (s *Server) serveTV(w http.ResponseWriter, r *http.Request, name string) {
	f, st, ok := tvFile(s.tvDir(), name)
	if !ok {
		w.WriteHeader(404)
		return
	}
	fh, err := os.Open(f)
	if err != nil {
		w.WriteHeader(404)
		return
	}
	defer fh.Close()
	w.Header().Set("Content-Type", "image/gif")
	w.Header().Set("Cache-Control", "no-cache")
	http.ServeContent(w, r, "", st.ModTime(), fh)
}
