// Package webui embeds the built headless web UI (the same Svelte frontend, web
// build). The bundle is produced by `vite build --config vite.config.web.ts`
// into dist/. If it hasn't been built, FS reports not-available and the server
// falls back to an API-only placeholder.
package webui

import (
	"embed"
	"io/fs"
)

//go:embed all:dist
var dist embed.FS

// FS returns the built web UI rooted at dist/, and true if a real build is
// present (index.html exists). Returns (nil, false) before the first web build.
func FS() (fs.FS, bool) {
	sub, err := fs.Sub(dist, "dist")
	if err != nil {
		return nil, false
	}
	if _, err := fs.Stat(sub, "index.html"); err != nil {
		return nil, false
	}
	return sub, true
}
