package http

import (
	nethttp "net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// While Vite runs, the CSP must allow its origin. The hot file holds Vite's
// URL with the /build base path, and a CSP source with a path matches only
// that exact path, which blocked every module.
func TestCSPAllowsViteOrigin(t *testing.T) {
	hot := filepath.Join(t.TempDir(), "hot")
	if err := os.WriteFile(hot, []byte("http://localhost:5173/build\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	rec := httptest.NewRecorder()
	securityHeaders(hot)(nethttp.HandlerFunc(func(nethttp.ResponseWriter, *nethttp.Request) {})).
		ServeHTTP(rec, httptest.NewRequest("GET", "/", nil))
	csp := rec.Header().Get("Content-Security-Policy")
	if !strings.Contains(csp, "script-src 'self' http://localhost:5173 ws://localhost:5173;") {
		t.Fatalf("CSP = %q", csp)
	}
}
