package http

import (
	nethttp "net/http"
	"net/url"
	"os"
	"strings"
)

// securityHeaders sets a strict CSP and friends (DESIGN.md §12). While the
// Vite dev server runs (hot file present), scripts, styles and its websocket
// are allowed from it so hot reload works.
func securityHeaders(hotFile string) func(nethttp.Handler) nethttp.Handler {
	return func(next nethttp.Handler) nethttp.Handler {
		return nethttp.HandlerFunc(func(w nethttp.ResponseWriter, r *nethttp.Request) {
			dev := ""
			if b, err := os.ReadFile(hotFile); err == nil {
				// The hot file holds Vite's URL including its base path
				// (/build). A CSP source with a path matches only that path,
				// so allow the origin.
				if u, err := url.Parse(strings.TrimSpace(string(b))); err == nil && u.Host != "" {
					dev = " " + u.Scheme + "://" + u.Host + " ws://" + u.Host
				}
			}
			h := w.Header()
			h.Set("Content-Security-Policy", strings.Join([]string{
				"default-src 'self'",
				"script-src 'self'" + dev,
				// Vue binds style attributes; no inline <style> or scripts.
				"style-src 'self' 'unsafe-inline'" + dev,
				"connect-src 'self'" + dev,
				"img-src 'self' data:",
				"font-src 'self' data:",
				"frame-ancestors 'none'",
				"base-uri 'self'",
				"form-action 'self'",
			}, "; "))
			h.Set("X-Content-Type-Options", "nosniff")
			h.Set("Referrer-Policy", "same-origin")
			h.Set("X-Frame-Options", "DENY")
			next.ServeHTTP(w, r)
		})
	}
}
