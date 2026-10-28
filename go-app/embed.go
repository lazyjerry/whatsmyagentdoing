package main

// Everything the page needs ships inside the binary: static/ is a synced
// copy of the clone's public/, assets/ and package.json version
// (see scripts/sync-static.sh). The running program never reads the clone.

import (
	"embed"
	"io/fs"
	"strings"
)

//go:embed static
var staticRoot embed.FS

// staticFS is the embedded tree rooted at static/.
var staticFS = func() fs.FS {
	sub, err := fs.Sub(staticRoot, "static")
	if err != nil {
		panic(err)
	}
	return sub
}()

// embeddedVersion is the version synced from package.json ("0.0.0" if missing).
var embeddedVersion = func() string {
	b, err := fs.ReadFile(staticFS, "VERSION")
	if err != nil {
		return "0.0.0"
	}
	v := strings.TrimSpace(string(b))
	if v == "" {
		return "0.0.0"
	}
	return v
}()
