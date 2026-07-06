// Package web serves the embedded frontend with SPA routing:
//
//	/            -> webmail app (index.html)
//	/admin/*     -> admin app (admin.html)
//	/setup/*     -> setup app (setup.html) until initialized, then redirect
package web

import (
	"io/fs"
	"net/http"
	"path"
	"strings"
)

// Handler serves static assets and SPA entry points. initialized reports
// whether first-run setup has completed (checked per request so completing
// setup takes effect without a restart).
func Handler(dist fs.FS, initialized func(*http.Request) bool) http.Handler {
	fileServer := http.FileServer(http.FS(dist))

	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		p := path.Clean(r.URL.Path)

		// Real files (hashed assets, favicons) are served as-is with
		// immutable caching for the hashed asset directory.
		if p != "/" && !strings.HasSuffix(p, ".html") {
			if f, err := dist.Open(strings.TrimPrefix(p, "/")); err == nil {
				f.Close()
				if strings.HasPrefix(p, "/assets/") {
					w.Header().Set("Cache-Control", "public, max-age=31536000, immutable")
				}
				fileServer.ServeHTTP(w, r)
				return
			}
		}

		switch {
		case p == "/setup" || strings.HasPrefix(p, "/setup/"):
			if initialized(r) {
				// After initialization the setup surface disappears.
				http.Redirect(w, r, "/admin", http.StatusTemporaryRedirect)
				return
			}
			serveEntry(w, r, dist, "setup.html")
		case p == "/admin" || strings.HasPrefix(p, "/admin/"):
			serveEntry(w, r, dist, "admin.html")
		default:
			if !initialized(r) {
				http.Redirect(w, r, "/setup", http.StatusTemporaryRedirect)
				return
			}
			serveEntry(w, r, dist, "index.html")
		}
	})
}

func serveEntry(w http.ResponseWriter, r *http.Request, dist fs.FS, name string) {
	body, err := fs.ReadFile(dist, name)
	if err != nil {
		// Fall back to the webmail entry (single-entry dev builds).
		body, err = fs.ReadFile(dist, "index.html")
		if err != nil {
			http.Error(w, "frontend not built", http.StatusNotImplemented)
			return
		}
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Header().Set("Cache-Control", "no-cache")
	w.WriteHeader(http.StatusOK)
	if r.Method != http.MethodHead {
		_, _ = w.Write(body)
	}
}
