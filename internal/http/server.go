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
	"github.com/docker/kit-maker/internal/settings"
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
	// Settings resolves the default registry and build runner for publishing.
	Settings *settings.Service
	// DevUser, when set, signs in every request without a session as this
	// email (kitmaker dev only, never serve).
	DevUser string
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
		if d.DevUser != "" {
			r.Use(auth.AutoSignIn(d.Ent, d.DevUser))
		}
		r.Use(i.Middleware)
		r.Use(shareAuth)

		r.Use(s.loadSubject)

		s.handle(r, "GET", "/login", authz.Public, s.loginPage)
		s.handle(r, "POST", "/login", authz.Public, s.login)
		s.handle(r, "GET", "/", authz.SignedIn, func(w nethttp.ResponseWriter, r *nethttp.Request) {
			nethttp.Redirect(w, r, "/blueprints", nethttp.StatusFound)
		})
		s.handle(r, "POST", "/logout", authz.SignedIn, s.logout)
		s.handle(r, "POST", "/account/sign-out-everywhere", authz.SignedIn, s.signOutEverywhere)
		s.handle(r, "GET", "/kits", authz.SignedIn, s.kitsRedirect)
		s.handle(r, "GET", "/kits/*", authz.SignedIn, s.kitsRedirect)
		s.handle(r, "GET", "/blueprints", authz.BlueprintView, s.blueprintsIndex)
		s.handle(r, "GET", "/blueprints/new", authz.BlueprintCreate, s.blueprintNew)
		s.handle(r, "POST", "/blueprints", authz.BlueprintCreate, s.blueprintCreate)
		s.handle(r, "GET", "/blueprints/{blueprintID}", authz.BlueprintView, s.blueprintShow)
		s.handle(r, "PUT", "/blueprints/{blueprintID}/goal", authz.BlueprintEdit, s.blueprintGoal)
		s.handle(r, "POST", "/blueprints/{blueprintID}/slots", authz.BlueprintEdit, s.slotAdd)
		s.handle(r, "DELETE", "/blueprints/{blueprintID}/slots/{slotKey}", authz.BlueprintEdit, s.slotRemove)
		s.handle(r, "POST", "/blueprints/{blueprintID}/slots/{slotKey}/custom", authz.BlockCreate, s.slotCustom)
		s.handle(r, "POST", "/blueprints/{blueprintID}/slots/{slotKey}/update", authz.LinkRebaseApply, s.slotUpdate)
		s.handle(r, "POST", "/blueprints/{blueprintID}/slots/{slotKey}/relink", authz.BlockUse, s.slotRelink)
		s.handle(r, "POST", "/blueprints/{blueprintID}/slots/{slotKey}/save-as-block", authz.BlockCreate, s.slotSaveAsBlock)
		s.handle(r, "POST", "/blueprints/{blueprintID}/checks", authz.BlueprintChecksEdit, s.checkAdd)
		s.handle(r, "DELETE", "/blueprints/{blueprintID}/checks/{checkID}", authz.BlueprintChecksEdit, s.checkRemove)
		s.handle(r, "GET", "/blocks", authz.BlockView, s.blocksIndex)
		s.handle(r, "POST", "/blocks/search", authz.BlockView, s.blockSearch)
		s.handle(r, "POST", "/blocks", authz.BlockCreate, s.blockCreate)
		s.handle(r, "GET", "/blocks/{blockID}", authz.BlockView, s.blockShow)
		s.handle(r, "PUT", "/blocks/{blockID}", authz.BlockEdit, s.blockUpdate)
		s.handle(r, "POST", "/blocks/{blockID}/versions", authz.BlockPublishVersion, s.blockPublish)
		s.handle(r, "POST", "/blocks/{blockID}/versions/{version}/restore", authz.BlockPublishVersion, s.blockRestoreVersion)
		s.handle(r, "GET", "/blueprints/{blueprintID}/slots/{slotKey}", authz.BlueprintView, s.blueprintShow)
		s.handle(r, "PUT", "/blueprints/{blueprintID}/slots/{slotKey}", authz.BlueprintEdit, s.slotSave)
		s.handle(r, "POST", "/blueprints/{blueprintID}/slots/{slotKey}/preview", authz.BlueprintEdit, s.slotPreview)
		s.handle(r, "POST", "/blueprints/{blueprintID}/slots/{slotKey}/revert", "link.revert", s.slotRevert)
		s.handle(r, "POST", "/blueprints/{blueprintID}/slots/{slotKey}/hide", authz.BlueprintEdit, s.slotHide)
		s.handle(r, "POST", "/blueprints/{blueprintID}/slots/{slotKey}/resync", "link.revert", s.slotResync)
		s.handle(r, "GET", "/blueprints/{blueprintID}/history", authz.BlueprintHistoryView, s.blueprintShow)
		s.handle(r, "GET", "/blueprints/{blueprintID}/run", authz.BlueprintDownload, s.blueprintRunInfo)
		s.handle(r, "POST", "/blueprints/{blueprintID}/publish", authz.BlueprintPublish, s.blueprintPublish)
		s.handle(r, "GET", "/blueprints/{blueprintID}/download", authz.BlueprintDownload, s.blueprintDownload)
		s.handle(r, "GET", "/blueprints/{blueprintID}/sbxenv.yaml", authz.BlueprintDownload, s.blueprintSbxenv)
		s.handle(r, "GET", "/blueprints/{blueprintID}/history.json", authz.BlueprintHistoryView, s.blueprintHistoryJSON)
		s.handle(r, "GET", "/blueprints/{blueprintID}/revisions/{revID}/yaml", authz.BlueprintHistoryView, s.revisionYAML)
		s.handle(r, "POST", "/blueprints/{blueprintID}/revisions/{revID}/restore", authz.BlueprintEdit, s.revisionRestore)
		s.handle(r, "POST", "/blueprints/{blueprintID}/slots/{slotKey}/link", authz.BlockUse, s.slotLink)
		s.handle(r, "POST", "/blueprints/{blueprintID}/slots/{slotKey}/unlink", authz.BlueprintEdit, s.slotUnlink)
		s.handle(r, "POST", "/blueprints/{blueprintID}/slots/{slotKey}/explain", authz.AIUse, s.slotExplain)
		s.handle(r, "POST", "/blueprints/{blueprintID}/slots/{slotKey}/draft", authz.AIUse, s.slotDraft)
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
