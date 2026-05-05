// Package adminui serves the embedded HTML/CSS/JS that backs the admin
// dashboard. The assets in static/ are baked into the binary via go:embed
// so deployment is just one binary plus its config file.
package adminui

import (
	"embed"
	"io/fs"
	"net/http"
)

//go:embed static
var staticFS embed.FS

// Handler returns an http.Handler that serves the embedded admin SPA.
//
// Mount it under /admin/ (with http.StripPrefix), behind the same Basic
// Auth middleware that protects the /admin/* JSON API. The handler:
//
//   - clears any Content-Type header set by upstream middleware
//     (JSONContentType marks every response as application/json, but the
//     file server only sets the correct type when none is present);
//   - sets Cache-Control: no-cache so the browser re-validates after the
//     binary is redeployed without a versioned URL scheme.
func Handler() http.Handler {
	sub, err := fs.Sub(staticFS, "static")
	if err != nil {
		// Compile-time embed paths cannot fail at runtime; refusing to
		// boot is correct if they ever do.
		panic("adminui: invalid embed sub-fs: " + err.Error())
	}
	fileServer := http.FileServer(http.FS(sub))

	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Del("Content-Type")
		w.Header().Set("Cache-Control", "no-cache")
		fileServer.ServeHTTP(w, r)
	})
}
