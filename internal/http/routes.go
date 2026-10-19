package http

import (
	nethttp "net/http"
	"sync"

	"github.com/go-chi/chi/v5"
	inertia "github.com/romsar/gonertia/v2"

	"github.com/docker/kit-maker/internal/auth"
	"github.com/docker/kit-maker/internal/authz"
)

// guards records the guard every route was registered with, so a test can
// prove no route was added without one (DESIGN.md §14).
type guards struct {
	mu sync.Mutex
	m  map[string]authz.Key
}

func (g *guards) set(method, pattern string, k authz.Key) {
	g.mu.Lock()
	defer g.mu.Unlock()
	if g.m == nil {
		g.m = map[string]authz.Key{}
	}
	g.m[method+" "+pattern] = k
}

// Guard returns the guard registered for method and pattern.
func (g *guards) Guard(method, pattern string) (authz.Key, bool) {
	g.mu.Lock()
	defer g.mu.Unlock()
	k, ok := g.m[method+" "+pattern]
	return k, ok
}

// handle registers a route with its guard:
//   - authz.Public: no sign-in
//   - authz.SignedIn: a session, no permission
//   - a permission: a session and the permission somewhere (deployment-wide
//     or in at least one team). Handlers that act on one object then call
//     s.allowed with the object's owner team.
func (s *server) handle(r chi.Router, method, pattern string, guard authz.Key, h nethttp.HandlerFunc) {
	if guard != authz.Public && guard != authz.SignedIn {
		authz.MustScope(guard) // unknown permission: fail at startup
	}
	s.guards.set(method, pattern, guard)
	var handler nethttp.Handler = h
	switch guard {
	case authz.Public:
	case authz.SignedIn:
		handler = auth.RequireUser(handler)
	default:
		handler = auth.RequireUser(s.requirePermission(guard, handler))
	}
	r.Method(method, pattern, handler)
}

func (s *server) requirePermission(perm authz.Key, next nethttp.Handler) nethttp.Handler {
	return nethttp.HandlerFunc(func(w nethttp.ResponseWriter, r *nethttp.Request) {
		if !authz.SubjectFrom(r.Context()).CanAnywhere(perm) {
			s.forbidden(w, r, perm)
			return
		}
		next.ServeHTTP(w, r)
	})
}

// allowed checks perm against an object's owner team and answers 403 when
// it's missing. It reports whether the handler may continue.
func (s *server) allowed(w nethttp.ResponseWriter, r *nethttp.Request, perm authz.Key, ownerTeamID string) bool {
	if authz.SubjectFrom(r.Context()).Can(perm, ownerTeamID) {
		return true
	}
	s.forbidden(w, r, perm)
	return false
}

func (s *server) forbidden(w nethttp.ResponseWriter, r *nethttp.Request, perm authz.Key) {
	s.Log.InfoContext(r.Context(), "forbidden", "path", r.URL.Path, "permission", perm)
	w.WriteHeader(nethttp.StatusForbidden)
	s.render(w, r, "Error", inertia.Props{
		"status":  nethttp.StatusForbidden,
		"title":   "You don't have access to this",
		"message": "This needs the " + string(perm) + " permission. Ask a team admin to give you a role that includes it.",
	})
}

// loadSubject resolves the signed-in user's permissions for the request.
func (s *server) loadSubject(next nethttp.Handler) nethttp.Handler {
	return nethttp.HandlerFunc(func(w nethttp.ResponseWriter, r *nethttp.Request) {
		u := auth.UserFrom(r.Context())
		if u == nil {
			next.ServeHTTP(w, r)
			return
		}
		sub, err := s.Authz.Load(r.Context(), u.ID)
		if err != nil {
			s.fail(w, r, "load permissions", err)
			return
		}
		next.ServeHTTP(w, r.WithContext(authz.WithSubject(r.Context(), sub)))
	})
}
