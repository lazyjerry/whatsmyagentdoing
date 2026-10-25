package main

// Scene packs ("DLC"): extra 3D scenes that aren't part of the clone.
// scenes.json (in the repo) lists what exists; a pack is downloaded only when
// someone picks it, checked against the sha256 pinned in scenes.json, and
// unpacked into ~/.whatsmyagentdoing/scenes/<id>/. Packs are data only
// (manifest.json, .glb, .png), so installing one never runs code.

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path"
	"path/filepath"
	"regexp"
	"strings"
	"time"
)

const sceneMaxBytes = 30 * 1024 * 1024

var (
	reSceneID       = regexp.MustCompile(`^[a-z0-9-]+$`)
	sceneAllowedExt = map[string]bool{".json": true, ".glb": true, ".png": true, ".txt": true}
	sceneUserDir    = filepath.Join(homeDir, ".whatsmyagentdoing", "scenes")
)

// Scenes resolves catalog and pack locations under one repo root.
type Scenes struct {
	root string
}

func (sc Scenes) dirs() []string { return []string{sceneUserDir, filepath.Join(sc.root, "scenes")} }

// CatalogEntry is one row of scenes.json plus the computed download URL.
type CatalogEntry struct {
	ID      string `json:"id"`
	Name    any    `json:"name"`
	Version any    `json:"version"`
	Credits any    `json:"credits"`
	Size    int64  `json:"size"`
	Sha256  string `json:"sha256"`
	File    string `json:"file"`
	Icon    string `json:"icon"`
	URL     string `json:"-"`
}

// Installed is a pack found on disk, read from its manifest.json.
type Installed struct {
	ID      string `json:"id"`
	Name    any    `json:"name"`
	Version any    `json:"version"`
	Credits any    `json:"credits"`
	Icon    string `json:"icon"`
	Dir     string `json:"-"`
}

func (sc Scenes) Catalog() []CatalogEntry {
	// scenes.json stores only the pack's file name; the release it lives in comes from config.
	raw, err := os.ReadFile(filepath.Join(sc.root, "scenes.json"))
	if err != nil {
		return nil
	}
	var doc struct {
		Scenes []CatalogEntry `json:"scenes"`
	}
	if json.Unmarshal(raw, &doc) != nil {
		return nil
	}
	for i := range doc.Scenes {
		doc.Scenes[i].URL = sceneReleaseURL(doc.Scenes[i].File)
	}
	return doc.Scenes
}

func (sc Scenes) InstalledList() []Installed {
	var out []Installed
	seen := map[string]bool{}
	for _, dir := range sc.dirs() {
		for _, e := range ls(dir) {
			if !e.IsDir() || seen[e.Name()] || !reSceneID.MatchString(e.Name()) {
				continue
			}
			raw, err := os.ReadFile(filepath.Join(dir, e.Name(), "manifest.json"))
			if err != nil {
				continue
			}
			var m Installed
			if json.Unmarshal(raw, &m) != nil {
				continue
			}
			seen[e.Name()] = true
			m.ID = e.Name()
			m.Dir = filepath.Join(dir, e.Name())
			out = append(out, m)
		}
	}
	return out
}

func (sc Scenes) SceneDir(id string) string {
	if !reSceneID.MatchString(id) {
		return ""
	}
	for _, dir := range sc.dirs() {
		if fileExists(filepath.Join(dir, id, "manifest.json")) {
			return filepath.Join(dir, id)
		}
	}
	return ""
}

func download(url string) ([]byte, error) {
	if local := os.Getenv("WMAD_SCENES_LOCAL"); local != "" {
		// for testing the installer against a local file
		return os.ReadFile(filepath.Join(local, path.Base(url)))
	}
	if !strings.HasPrefix(url, "https://") {
		return nil, errors.New("scene packs are only fetched over https")
	}
	req, err := http.NewRequest("GET", url, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", "whatsmyagentdoing")
	client := &http.Client{Timeout: 5 * time.Minute}
	res, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer res.Body.Close()
	if res.StatusCode != 200 {
		return nil, fmt.Errorf("download failed: HTTP %d", res.StatusCode)
	}
	data, err := io.ReadAll(io.LimitReader(res.Body, sceneMaxBytes+1))
	if err != nil {
		return nil, err
	}
	if len(data) > sceneMaxBytes {
		return nil, errors.New("scene pack too large")
	}
	return data, nil
}

// untar writes regular files only: no links, no absolute paths, no escaping
// the destination, and only the extensions a scene pack may contain.
func untar(gz []byte, dest string) error {
	zr, err := gzip.NewReader(bytes.NewReader(gz))
	if err != nil {
		return err
	}
	tr := tar.NewReader(zr)
	for {
		h, err := tr.Next()
		if err == io.EOF {
			return nil
		}
		if err != nil {
			return err
		}
		if h.Typeflag != tar.TypeReg && h.Typeflag != tar.TypeRegA {
			continue // dirs are created as needed; skip links etc.
		}
		rel := path.Clean(strings.TrimLeft(h.Name, "./"))
		if rel == "." || strings.HasPrefix(rel, "..") || path.IsAbs(rel) || !sceneAllowedExt[strings.ToLower(path.Ext(rel))] {
			continue
		}
		file := filepath.Join(dest, filepath.FromSlash(rel))
		if !strings.HasPrefix(file, dest+string(filepath.Separator)) {
			continue
		}
		if err := os.MkdirAll(filepath.Dir(file), 0o755); err != nil {
			return err
		}
		body, err := io.ReadAll(io.LimitReader(tr, sceneMaxBytes))
		if err != nil {
			return err
		}
		if err := os.WriteFile(file, body, 0o644); err != nil {
			return err
		}
	}
}

// versionAtLeast mirrors JavaScript's `have >= want` on the manifest values,
// which scenes.json writes as numbers.
func versionAtLeast(have, want any) bool {
	hf, hok := have.(float64)
	wf, wok := want.(float64)
	if hok && wok {
		return hf >= wf
	}
	return asString(have) >= asString(want)
}

func (sc Scenes) Install(id string) (*Installed, error) {
	var entry *CatalogEntry
	for _, e := range sc.Catalog() {
		if e.ID == id {
			c := e
			entry = &c
			break
		}
	}
	if entry == nil {
		return nil, fmt.Errorf("unknown scene: %s", id)
	}
	for _, have := range sc.InstalledList() {
		if have.ID == id && versionAtLeast(have.Version, entry.Version) {
			h := have
			return &h, nil
		}
	}
	gz, err := download(entry.URL)
	if err != nil {
		return nil, err
	}
	sum := sha256.Sum256(gz)
	if hex.EncodeToString(sum[:]) != entry.Sha256 {
		return nil, fmt.Errorf("checksum mismatch for %s; not installed", id)
	}
	tmp := filepath.Join(sceneUserDir, fmt.Sprintf(".%s-%d", id, os.Getpid()))
	os.RemoveAll(tmp)
	if err := os.MkdirAll(tmp, 0o755); err != nil {
		return nil, err
	}
	if err := untar(gz, tmp); err != nil {
		os.RemoveAll(tmp)
		return nil, err
	}
	if !fileExists(filepath.Join(tmp, "manifest.json")) {
		os.RemoveAll(tmp)
		return nil, errors.New("scene pack has no manifest.json")
	}
	final := filepath.Join(sceneUserDir, id)
	os.RemoveAll(final)
	if err := os.Rename(tmp, final); err != nil {
		return nil, err
	}
	for _, have := range sc.InstalledList() {
		if have.ID == id {
			h := have
			return &h, nil
		}
	}
	return nil, errors.New("scene installed but not found")
}

func (sc Scenes) Remove(id string) bool {
	if !reSceneID.MatchString(id) {
		return false
	}
	dir := filepath.Join(sceneUserDir, id)
	if _, err := os.Stat(dir); err != nil {
		return false
	}
	return os.RemoveAll(dir) == nil
}

// sceneName picks the English name (or the first one) for CLI listings.
func sceneName(name any, id string) string {
	if m, ok := name.(map[string]any); ok {
		if en, ok := m["en"].(string); ok && en != "" {
			return en
		}
		for _, v := range m {
			if s, ok := v.(string); ok && s != "" {
				return s
			}
		}
	}
	if s, ok := name.(string); ok && s != "" {
		return s
	}
	return id
}
