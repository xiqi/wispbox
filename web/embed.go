// Package web embeds the compiled frontend (webmail, admin, and setup apps)
// into the wispboxd binary. Node.js is a build-time tool only; at runtime
// everything is served from this embedded filesystem.
package web

import (
	"embed"
	"io/fs"
)

//go:embed all:dist
var dist embed.FS

// Dist returns the built frontend rooted at the dist directory.
func Dist() (fs.FS, error) {
	return fs.Sub(dist, "dist")
}
