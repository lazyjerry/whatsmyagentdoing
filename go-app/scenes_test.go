package main

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

type tarEntry struct {
	name string
	typ  byte
	body string
}

func makeTarGz(t *testing.T, entries ...tarEntry) []byte {
	t.Helper()
	var buf bytes.Buffer
	zw := gzip.NewWriter(&buf)
	tw := tar.NewWriter(zw)
	for _, e := range entries {
		h := &tar.Header{Name: e.name, Typeflag: e.typ, Mode: 0o644, Size: int64(len(e.body))}
		if e.typ == tar.TypeSymlink {
			h.Linkname = "/etc/passwd"
		}
		if err := tw.WriteHeader(h); err != nil {
			t.Fatal(err)
		}
		if _, err := tw.Write([]byte(e.body)); err != nil {
			t.Fatal(err)
		}
	}
	tw.Close()
	zw.Close()
	return buf.Bytes()
}

func TestUntarAllowlist(t *testing.T) {
	dest := t.TempDir()
	gz := makeTarGz(t,
		tarEntry{"./manifest.json", tar.TypeReg, `{"name":"x"}`},
		tarEntry{"models/", tar.TypeDir, ""},
		tarEntry{"models/a.glb", tar.TypeReg, "glb"},
		tarEntry{"textures/t.png", tar.TypeReg, "png"},
		tarEntry{"LICENSE.txt", tar.TypeReg, "cc0"},
		tarEntry{"../escape.json", tar.TypeReg, "{}"},
		tarEntry{"/abs.json", tar.TypeReg, "{}"},
		tarEntry{"models/../../deep.json", tar.TypeReg, "{}"},
		tarEntry{"run.sh", tar.TypeReg, "rm -rf /"},
		tarEntry{"index.html", tar.TypeReg, "<script>"},
		tarEntry{"link.json", tar.TypeSymlink, ""},
	)
	if err := untar(gz, dest); err != nil {
		t.Fatal(err)
	}
	// Leading "./", "../" and "/" are stripped, so those land inside dest.
	for _, want := range []string{"manifest.json", "models/a.glb", "textures/t.png", "LICENSE.txt", "escape.json", "abs.json"} {
		if !fileExists(filepath.Join(dest, want)) {
			t.Errorf("%s should be extracted", want)
		}
	}
	for _, bad := range []string{"run.sh", "index.html", "link.json", "deep.json"} {
		if _, err := os.Lstat(filepath.Join(dest, bad)); err == nil {
			t.Errorf("%s must be skipped", bad)
		}
	}
	parent := filepath.Dir(dest)
	for _, out := range []string{"escape.json", "deep.json", "abs.json"} {
		if fileExists(filepath.Join(parent, out)) || fileExists(filepath.Join("/", out)) {
			t.Errorf("%s must not escape the destination", out)
		}
	}
	if untar([]byte("not gzip"), dest) == nil {
		t.Error("garbage must error")
	}
}

func TestVersionAtLeast(t *testing.T) {
	if !versionAtLeast(float64(2), float64(1)) || !versionAtLeast(float64(1), float64(1)) || versionAtLeast(float64(1), float64(2)) {
		t.Error("numeric compare")
	}
	if versionAtLeast("a", "b") || !versionAtLeast("b", "a") || !versionAtLeast(nil, nil) {
		t.Error("string fallback")
	}
}

func TestSceneName(t *testing.T) {
	if sceneName(map[string]any{"en": "Café", "zh-TW": "咖啡廳"}, "cafe") != "Café" {
		t.Error("english first")
	}
	if sceneName(map[string]any{"ja": "カフェ"}, "cafe") != "カフェ" {
		t.Error("any language when no en")
	}
	if sceneName("Plain", "x") != "Plain" || sceneName(nil, "fallback") != "fallback" || sceneName(map[string]any{}, "id") != "id" {
		t.Error("string and fallback")
	}
}

func TestCatalogFromEmbeddedJSON(t *testing.T) {
	cat := Scenes{}.Catalog()
	if len(cat) != 4 {
		t.Fatalf("scenes.json lists 4 packs, got %d", len(cat))
	}
	ids := map[string]CatalogEntry{}
	for _, e := range cat {
		ids[e.ID] = e
		if e.Sha256 == "" || e.File == "" || e.Size == 0 {
			t.Errorf("incomplete entry: %+v", e)
		}
		if !strings.HasPrefix(e.URL, "https://github.com/"+Repo+"/releases/download/scenes/") || !strings.HasSuffix(e.URL, e.File) {
			t.Errorf("url: %s", e.URL)
		}
	}
	for _, id := range []string{"cafe", "castle", "pirate", "station"} {
		if _, ok := ids[id]; !ok {
			t.Errorf("missing %s", id)
		}
	}
}

func TestInstalledListAndSceneDir(t *testing.T) {
	dir := setSceneDir(t)
	sc := Scenes{}
	if got := sc.InstalledList(); len(got) != 0 || sc.SceneDir("cafe") != "" {
		t.Fatalf("nothing installed yet: %+v", got)
	}
	writeFile(t, filepath.Join(dir, "cafe", "manifest.json"), `{"name":{"en":"Café"},"version":1,"icon":"☕"}`)
	writeFile(t, filepath.Join(dir, "Bad_ID", "manifest.json"), `{}`)
	writeFile(t, filepath.Join(dir, ".cafe-123", "manifest.json"), `{}`) // install in progress
	writeFile(t, filepath.Join(dir, "nomanifest", "x.glb"), "")
	writeFile(t, filepath.Join(dir, "broken", "manifest.json"), "not json")
	got := sc.InstalledList()
	if len(got) != 1 || got[0].ID != "cafe" || got[0].Icon != "☕" || got[0].Dir != filepath.Join(dir, "cafe") {
		t.Fatalf("installed: %+v", got)
	}
	if sc.SceneDir("cafe") != filepath.Join(dir, "cafe") {
		t.Error("SceneDir finds the manifest")
	}
	for _, bad := range []string{"CAFE", "cafe/..", "../cafe", "", "nomanifest", "broken"} {
		if sc.SceneDir(bad) != "" && bad != "broken" {
			t.Errorf("SceneDir(%q) must be empty", bad)
		}
	}
}

func TestInstallErrors(t *testing.T) {
	dir := setSceneDir(t)
	noNetwork(t)
	sc := Scenes{}
	if _, err := sc.Install("nope"); err == nil || !strings.Contains(err.Error(), "unknown scene") {
		t.Errorf("unknown: %v", err)
	}
	// A local pack whose bytes don't match the pinned sha256 is refused and nothing is written.
	local := t.TempDir()
	writeFile(t, filepath.Join(local, "cafe-v1.tar.gz"), "tampered")
	t.Setenv("WMAD_SCENES_LOCAL", local)
	if _, err := sc.Install("cafe"); err == nil || !strings.Contains(err.Error(), "checksum mismatch") {
		t.Errorf("checksum: %v", err)
	}
	if entries, _ := os.ReadDir(dir); len(entries) != 0 {
		t.Errorf("nothing may be installed after a checksum failure: %v", entries)
	}
	// Missing local file surfaces as a download error.
	if _, err := sc.Install("castle"); err == nil {
		t.Error("missing pack must error")
	}
}

func TestInstallSkipsWhenAlreadyCurrent(t *testing.T) {
	dir := setSceneDir(t)
	noNetwork(t)
	t.Setenv("WMAD_SCENES_LOCAL", t.TempDir()) // empty: any download attempt would fail
	writeFile(t, filepath.Join(dir, "cafe", "manifest.json"), `{"name":{"en":"Café"},"version":1}`)
	sc := Scenes{}
	got, err := sc.Install("cafe")
	if err != nil || got == nil || got.ID != "cafe" {
		t.Fatalf("installed and current must short-circuit: %v %+v", err, got)
	}
	// Older than the catalog: a download is attempted (and fails here).
	writeFile(t, filepath.Join(dir, "pirate", "manifest.json"), `{"name":{"en":"Pirate"},"version":1}`)
	if _, err := sc.Install("pirate"); err == nil {
		t.Error("outdated install must try to download")
	}
}

// TestInstallRealPackOffline installs the real packs shipped in the top-level
// jerry-report/scenes/ folder (same bytes as the GitHub release) through the
// whole path: sha256 check, untar, manifest check, rename.
func TestInstallRealPackOffline(t *testing.T) {
	packs, err := filepath.Abs(filepath.Join("..", "jerry-report", "scenes"))
	if err != nil || !fileExists(filepath.Join(packs, "cafe-v1.tar.gz")) {
		t.Skip("no local copy of the scene packs")
	}
	dir := setSceneDir(t)
	noNetwork(t)
	t.Setenv("WMAD_SCENES_LOCAL", packs)
	sc := Scenes{}
	got, err := sc.Install("cafe")
	if err != nil {
		t.Fatalf("install: %v", err)
	}
	if got.ID != "cafe" || got.Dir != filepath.Join(dir, "cafe") || !fileExists(filepath.Join(dir, "cafe", "manifest.json")) {
		t.Fatalf("installed: %+v", got)
	}
	if entries, _ := os.ReadDir(dir); len(entries) != 1 {
		t.Errorf("temp dir must be renamed away: %v", entries)
	}
	if !sc.Remove("cafe") || sc.SceneDir("cafe") != "" {
		t.Error("remove")
	}
}

func TestRemove(t *testing.T) {
	dir := setSceneDir(t)
	sc := Scenes{}
	if sc.Remove("missing") || sc.Remove("Bad_ID") || sc.Remove("../x") {
		t.Error("missing or invalid ids return false")
	}
	writeFile(t, filepath.Join(dir, "cafe", "manifest.json"), `{}`)
	if !sc.Remove("cafe") || dirExists(filepath.Join(dir, "cafe")) {
		t.Error("remove deletes the folder")
	}
}

func TestDownloadRequiresHTTPS(t *testing.T) {
	noNetwork(t)
	if _, err := download("http://example.com/x.tar.gz"); err == nil || !strings.Contains(err.Error(), "https") {
		t.Errorf("plain http must be refused: %v", err)
	}
	if _, err := download("ftp://example.com/x"); err == nil {
		t.Error("ftp must be refused")
	}
}
