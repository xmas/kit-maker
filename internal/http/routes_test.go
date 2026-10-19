package http

import (
	nethttp "net/http"
	"testing"
	"testing/fstest"

	"github.com/alexedwards/scs/v2"
	"github.com/go-chi/chi/v5"

	"github.com/docker/kit-maker/internal/authz"
	"github.com/docker/kit-maker/internal/testutil"
)

// TestEveryRouteHasAGuard fails the build when a route is added with r.Get
// (or similar) instead of s.handle, so nothing ships without a permission.
func TestEveryRouteHasAGuard(t *testing.T) {
	h, err := NewHandler(Deps{
		Log:         testutil.Logger(),
		Assets:      fstest.MapFS{},
		ViteHotFile: t.TempDir() + "/hot",
		Sessions:    scs.New(),
	})
	if err != nil {
		t.Fatal(err)
	}
	mux := h.(*chi.Mux)
	g := lastGuards
	n := 0
	err = chi.Walk(mux, func(method, route string, _ nethttp.Handler, _ ...func(nethttp.Handler) nethttp.Handler) error {
		n++
		if _, ok := g.Guard(method, route); ok {
			return nil
		}
		if _, ok := g.Guard("*", route); ok {
			return nil
		}
		t.Errorf("%s %s has no guard: register it with s.handle and a permission (or authz.Public / authz.SignedIn)", method, route)
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if n == 0 {
		t.Fatal("walked no routes")
	}
	for k, guard := range g.m {
		if guard == authz.Public || guard == authz.SignedIn {
			continue
		}
		if _, ok := authz.Lookup(guard); !ok {
			t.Errorf("%s guarded by unknown permission %s", k, guard)
		}
	}
}
