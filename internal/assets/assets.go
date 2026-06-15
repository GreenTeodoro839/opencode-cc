// Package assets serves the built web panel (a static SPA). It prefers an
// on-disk web/dist (so the frontend can be iterated without recompiling Go),
// then falls back to assets embedded at build time, then a placeholder page.
package assets

import (
	"embed"
	"io/fs"
	"net/http"
	"os"
)

// embedded holds the panel built into the binary. The dist/ directory contains
// a .gitkeep so the embed pattern always matches even before the first build.
//
//go:embed all:dist
var embedded embed.FS

// FileSystem returns an http.FileSystem for the panel SPA. It prefers the
// on-disk web/dist (useful when running the Go binary during frontend
// development), then the embedded build.
func FileSystem() (http.FileSystem, bool) {
	if _, err := os.Stat("web/dist/index.html"); err == nil {
		return http.Dir("web/dist"), true
	}
	sub, err := fs.Sub(embedded, "dist")
	if err != nil {
		return nil, false
	}
	return http.FS(sub), true
}
