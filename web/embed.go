// Package web holds the Vue 3 + Inertia frontend. `npm run build` writes
// web/dist, which is embedded in the binary.
package web

import (
	"embed"
	"io/fs"
)

//go:embed all:dist
var dist embed.FS

//go:embed app.html
var RootTemplate string

// Dist returns the built assets (Vite manifest under .vite/).
func Dist() fs.FS {
	sub, err := fs.Sub(dist, "dist")
	if err != nil {
		panic(err)
	}
	return sub
}
