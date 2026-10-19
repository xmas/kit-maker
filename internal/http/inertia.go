package http

import (
	"io/fs"
	nethttp "net/http"
	"strings"

	inertia "github.com/romsar/gonertia/v2"

	"github.com/docker/kit-maker/web"
)

const manifestPath = ".vite/manifest.json"

// newInertia wires gonertia to the Vite build. When the Vite dev server is
// running it writes hotFile, and pages load from it with hot reload instead.
func newInertia(assets fs.FS, hotFile string, opts ...inertia.Option) (*inertia.ViteInstance, error) {
	if _, err := fs.Stat(assets, manifestPath); err == nil {
		// Asset version = manifest checksum, so a deploy makes Inertia reload.
		opts = append(opts, inertia.WithVersionFromFileFS(assets, manifestPath))
	}
	i, err := inertia.New(web.RootTemplate, opts...)
	if err != nil {
		return nil, err
	}
	return inertia.NewViteFromFS(i, assets,
		inertia.WithBuildManifest(manifestPath),
		inertia.WithFallbackManifest(manifestPath),
		inertia.WithBuildDir("/build/"),
		inertia.WithHotFile(hotFile),
	)
}

// assetHandler serves the hashed Vite output under /build/.
func assetHandler(assets fs.FS) nethttp.Handler {
	files := nethttp.StripPrefix("/build/", nethttp.FileServerFS(assets))
	return nethttp.HandlerFunc(func(w nethttp.ResponseWriter, r *nethttp.Request) {
		if strings.HasPrefix(r.URL.Path, "/build/.vite/") {
			nethttp.NotFound(w, r)
			return
		}
		if strings.HasPrefix(r.URL.Path, "/build/assets/") {
			w.Header().Set("Cache-Control", "public, max-age=31536000, immutable")
		}
		files.ServeHTTP(w, r)
	})
}
