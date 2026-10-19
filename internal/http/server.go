// Package http serves Kit Maker: Inertia pages, health probes, and (later)
// SSE AI streams and the sbx registration API.
package http

import (
	"context"
	"database/sql"
	"errors"
	"io/fs"
	"log/slog"
	nethttp "net/http"
	"time"

	"github.com/alexedwards/scs/v2"
	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"
	"github.com/jackc/pgx/v5"
	"github.com/riverqueue/river"
	inertia "github.com/romsar/gonertia/v2"
	"gocloud.dev/blob"

	"github.com/docker/kit-maker/internal/ai"
	"github.com/docker/kit-maker/internal/auth"
	"github.com/docker/kit-maker/internal/authz"
	"github.com/docker/kit-maker/internal/ent"
)

// Deps are what the HTTP layer needs. Assets is the built frontend (web.Dist()
// in production); tests pass a fake manifest.
type Deps struct {
	Log         *slog.Logger
	DB          *sql.DB
	Ent         *ent.Client
	Bucket      *blob.Bucket
	Jobs        *river.Client[pgx.Tx]
	Assets      fs.FS
	ViteHotFile string
	Sessions    *scs.SessionManager
	Auth        *auth.Service
	Authz       *authz.Authorizer
	AI          *ai.Provider
}

// NewHandler builds the router.
func NewHandler(d Deps) (nethttp.Handler, error) {
	i, err := newInertia(d.Assets, d.ViteHotFile, inertia.WithFlashProvider(auth.FlashProvider{M: d.Sessions}))
	if err != nil {
		return nil, err
	}
	if d.Authz == nil {
		d.Authz = &authz.Authorizer{Ent: d.Ent}
	}
	s := &server{Deps: d, inertia: i, guards: &guards{}}
	lastGuards = s.guards

	r := chi.NewRouter()
	r.Use(middleware.RealIP)
	r.Use(middleware.Recoverer)
	r.Use(nethttp.NewCrossOriginProtection().Handler)

	// Probes skip request logging so they don't flood the logs.
	s.handle(r, "GET", "/healthz", authz.Public, s.healthz)
	s.handle(r, "GET", "/readyz", authz.Public, s.readyz)

	s.guards.set("*", "/build/*", authz.Public)
	r.Handle("/build/*", assetHandler(d.Assets))

	r.Group(func(r chi.Router) {
		r.Use(requestLogger(d.Log))
		r.Use(securityHeaders(d.ViteHotFile))
		r.Use(d.Sessions.LoadAndSave)
		r.Use(auth.LoadUser(d.Sessions, d.Ent))
		r.Use(i.Middleware)
		r.Use(shareAuth)

		r.Use(s.loadSubject)

		s.handle(r, "GET", "/login", authz.Public, s.loginPage)
		s.handle(r, "POST", "/login", authz.Public, s.login)
		s.handle(r, "GET", "/", authz.SignedIn, func(w nethttp.ResponseWriter, r *nethttp.Request) {
			nethttp.Redirect(w, r, "/kits", nethttp.StatusFound)
		})
		s.handle(r, "POST", "/logout", authz.SignedIn, s.logout)
		s.handle(r, "POST", "/account/sign-out-everywhere", authz.SignedIn, s.signOutEverywhere)
		s.handle(r, "GET", "/kits", authz.KitView, s.kitsIndex)
		s.handle(r, "GET", "/kits/new", authz.KitCreate, s.kitNew)
		s.handle(r, "POST", "/kits", authz.KitCreate, s.kitCreate)
		s.handle(r, "GET", "/kits/{kitID}", authz.KitView, s.kitShow)
		s.handle(r, "PUT", "/kits/{kitID}/goal", authz.KitEdit, s.kitGoal)
		s.handle(r, "POST", "/kits/{kitID}/slots", authz.KitEdit, s.slotAdd)
		s.handle(r, "DELETE", "/kits/{kitID}/slots/{slotKey}", authz.KitEdit, s.slotRemove)
		s.handle(r, "POST", "/kits/{kitID}/slots/{slotKey}/custom", authz.BlockCreate, s.slotCustom)
		s.handle(r, "POST", "/kits/{kitID}/slots/{slotKey}/update", authz.LinkRebaseApply, s.slotUpdate)
		s.handle(r, "POST", "/kits/{kitID}/slots/{slotKey}/relink", authz.BlockUse, s.slotRelink)
		s.handle(r, "POST", "/kits/{kitID}/slots/{slotKey}/save-as-block", authz.BlockCreate, s.slotSaveAsBlock)
		s.handle(r, "POST", "/kits/{kitID}/checks", authz.KitChecksEdit, s.checkAdd)
		s.handle(r, "DELETE", "/kits/{kitID}/checks/{checkID}", authz.KitChecksEdit, s.checkRemove)
		s.handle(r, "GET", "/blocks", authz.BlockView, s.blocksIndex)
		s.handle(r, "POST", "/blocks", authz.BlockCreate, s.blockCreate)
		s.handle(r, "GET", "/blocks/{blockID}", authz.BlockView, s.blockShow)
		s.handle(r, "PUT", "/blocks/{blockID}", authz.BlockEdit, s.blockUpdate)
		s.handle(r, "POST", "/blocks/{blockID}/versions", authz.BlockPublishVersion, s.blockPublish)
		s.handle(r, "POST", "/blocks/{blockID}/versions/{version}/restore", authz.BlockPublishVersion, s.blockRestoreVersion)
		s.handle(r, "GET", "/kits/{kitID}/slots/{slotKey}", authz.KitView, s.kitShow)
		s.handle(r, "PUT", "/kits/{kitID}/slots/{slotKey}", authz.KitEdit, s.slotSave)
		s.handle(r, "POST", "/kits/{kitID}/slots/{slotKey}/preview", authz.KitEdit, s.slotPreview)
		s.handle(r, "POST", "/kits/{kitID}/slots/{slotKey}/revert", "link.revert", s.slotRevert)
		s.handle(r, "POST", "/kits/{kitID}/slots/{slotKey}/resync", "link.revert", s.slotResync)
		s.handle(r, "GET", "/kits/{kitID}/history", authz.KitHistoryView, s.kitShow)
		s.handle(r, "GET", "/kits/{kitID}/history.json", authz.KitHistoryView, s.kitHistoryJSON)
		s.handle(r, "GET", "/kits/{kitID}/revisions/{revID}/yaml", authz.KitHistoryView, s.revisionYAML)
		s.handle(r, "POST", "/kits/{kitID}/revisions/{revID}/restore", authz.KitEdit, s.revisionRestore)
		s.handle(r, "POST", "/kits/{kitID}/slots/{slotKey}/link", authz.BlockUse, s.slotLink)
		s.handle(r, "POST", "/kits/{kitID}/slots/{slotKey}/unlink", authz.KitEdit, s.slotUnlink)
		s.handle(r, "POST", "/kits/{kitID}/slots/{slotKey}/explain", authz.AIUse, s.slotExplain)
	})
	return r, nil
}

// lastGuards is the guard table of the most recently built handler, for
// TestEveryRouteHasAGuard.
var lastGuards *guards

type server struct {
	Deps
	inertia *inertia.ViteInstance
	guards  *guards
}

// render renders an Inertia page, logging failures.
func (s *server) render(w nethttp.ResponseWriter, r *nethttp.Request, component string, props inertia.Props) {
	if err := s.inertia.Render(w, r, component, props); err != nil {
		s.fail(w, r, "render "+component, err)
	}
}

// fail logs err and answers 500 without leaking details.
func (s *server) fail(w nethttp.ResponseWriter, r *nethttp.Request, what string, err error) {
	s.Log.ErrorContext(r.Context(), what, "error", err)
	nethttp.Error(w, "something went wrong", nethttp.StatusInternalServerError)
}

// shareAuth exposes the signed-in user to every page as props.auth.user.
func shareAuth(next nethttp.Handler) nethttp.Handler {
	return nethttp.HandlerFunc(func(w nethttp.ResponseWriter, r *nethttp.Request) {
		var who any
		if u := auth.UserFrom(r.Context()); u != nil {
			who = map[string]string{"id": u.ID, "email": u.Email, "displayName": u.DisplayName}
		}
		ctx := inertia.SetProp(r.Context(), "auth", map[string]any{"user": who})
		next.ServeHTTP(w, r.WithContext(ctx))
	})
}

// Serve runs h on addr until ctx is cancelled, then shuts down gracefully.
func Serve(ctx context.Context, addr string, h nethttp.Handler, log *slog.Logger) error {
	srv := &nethttp.Server{
		Addr:              addr,
		Handler:           h,
		ReadHeaderTimeout: 10 * time.Second,
	}
	errc := make(chan error, 1)
	go func() {
		log.Info("http listening", "addr", addr)
		errc <- srv.ListenAndServe()
	}()
	select {
	case err := <-errc:
		return err
	case <-ctx.Done():
	}
	shutdownCtx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	if err := srv.Shutdown(shutdownCtx); err != nil {
		return err
	}
	if err := <-errc; !errors.Is(err, nethttp.ErrServerClosed) {
		return err
	}
	return nil
}

func requestLogger(log *slog.Logger) func(nethttp.Handler) nethttp.Handler {
	return func(next nethttp.Handler) nethttp.Handler {
		return nethttp.HandlerFunc(func(w nethttp.ResponseWriter, r *nethttp.Request) {
			ww := middleware.NewWrapResponseWriter(w, r.ProtoMajor)
			start := time.Now()
			next.ServeHTTP(ww, r)
			log.Info("http request",
				"method", r.Method, "path", r.URL.Path, "status", ww.Status(),
				"bytes", ww.BytesWritten(), "duration_ms", time.Since(start).Milliseconds())
		})
	}
}
