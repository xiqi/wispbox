package security

import "net/http"

// Headers applies baseline security headers to every response.
func Headers(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		h := w.Header()
		h.Set("X-Content-Type-Options", "nosniff")
		h.Set("X-Frame-Options", "DENY")
		h.Set("Referrer-Policy", "same-origin")
		h.Set("Permissions-Policy", "camera=(), microphone=(), geolocation=()")
		// The frontend is fully self-hosted: no external scripts, styles,
		// external fonts, or connections. font-src allows data: for Vite's
		// bundled font assets. img-src additionally allows https: so that
		// remote images a user explicitly chooses to load in an email render
		// (they are stripped by the server-side sanitizer unless opted in, so
		// this only permits the deliberate case); data: covers inline images.
		h.Set("Content-Security-Policy",
			"default-src 'self'; script-src 'self'; style-src 'self' 'unsafe-inline'; "+
				"img-src 'self' data: https:; font-src 'self' data:; connect-src 'self'; "+
				"frame-ancestors 'none'; base-uri 'none'; form-action 'self'")
		if r.TLS != nil {
			h.Set("Strict-Transport-Security", "max-age=63072000")
		}
		next.ServeHTTP(w, r)
	})
}
