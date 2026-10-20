package auth

import (
	"context"
	"encoding/gob"
	"net/http"
	"strings"
	"time"

	"github.com/alexedwards/scs/pgxstore"
	"github.com/alexedwards/scs/v2"
	"github.com/jackc/pgx/v5/pgxpool"
	inertia "github.com/romsar/gonertia/v2"

	"github.com/docker/kit-maker/internal/ent"
	"github.com/docker/kit-maker/internal/ent/user"
)

// Session lifetimes (DESIGN.md §12): idle and absolute timeouts.
const (
	IdleTimeout     = 2 * time.Hour
	AbsoluteTimeout = 12 * time.Hour
)

const (
	keyUserID  = "user_id"
	keyVersion = "session_version"
	keyErrors  = "inertia_errors"
	keyClear   = "inertia_clear_history"
)

func init() {
	gob.Register(inertia.ValidationErrors{})
}

// NewSessions returns the session manager, storing sessions in Postgres.
// Cookies are Secure when the deployment is reached over https.
func NewSessions(pool *pgxpool.Pool, baseURL string) *scs.SessionManager {
	m := scs.New()
	m.Store = pgxstore.NewWithCleanupInterval(pool, 10*time.Minute)
	m.IdleTimeout = IdleTimeout
	m.Lifetime = AbsoluteTimeout
	m.Cookie.Name = "kitmaker_session"
	m.Cookie.HttpOnly = true
	m.Cookie.SameSite = http.SameSiteLaxMode
	m.Cookie.Secure = strings.HasPrefix(baseURL, "https://")
	return m
}

// SignIn renews the session token (no fixation) and records the user.
func SignIn(ctx context.Context, m *scs.SessionManager, u *ent.User) error {
	if err := m.RenewToken(ctx); err != nil {
		return err
	}
	m.Put(ctx, keyUserID, u.ID)
	m.Put(ctx, keyVersion, u.SessionVersion)
	return nil
}

// SignOut destroys the current session.
func SignOut(ctx context.Context, m *scs.SessionManager) error {
	return m.Destroy(ctx)
}

type ctxKey struct{}

// UserFrom returns the signed-in user, or nil.
func UserFrom(ctx context.Context) *ent.User {
	u, _ := ctx.Value(ctxKey{}).(*ent.User)
	return u
}

// WithUser returns ctx carrying u (tests and internal callers).
func WithUser(ctx context.Context, u *ent.User) context.Context {
	return context.WithValue(ctx, ctxKey{}, u)
}

// LoadUser puts the signed-in user in the request context. A session whose
// user is gone, inactive, or signed out everywhere since is destroyed.
func LoadUser(m *scs.SessionManager, client *ent.Client) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			ctx := r.Context()
			id := m.GetString(ctx, keyUserID)
			if id == "" {
				next.ServeHTTP(w, r)
				return
			}
			u, err := client.User.Get(ctx, id)
			if err != nil || u.Status != user.StatusActive || u.SessionVersion != m.GetInt(ctx, keyVersion) {
				_ = m.Destroy(ctx)
				next.ServeHTTP(w, r)
				return
			}
			next.ServeHTTP(w, r.WithContext(WithUser(ctx, u)))
		})
	}
}

// AutoSignIn treats every request without a session as the active user with
// this email. Only `kitmaker dev` turns it on, for local debugging (see
// devCmd); sessions still work, so signing in as someone else wins.
func AutoSignIn(client *ent.Client, email string) func(http.Handler) http.Handler {
	email = NormalizeEmail(email)
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if UserFrom(r.Context()) != nil {
				next.ServeHTTP(w, r)
				return
			}
			u, err := client.User.Query().Where(user.Email(email), user.StatusEQ(user.StatusActive)).Only(r.Context())
			if err != nil {
				next.ServeHTTP(w, r)
				return
			}
			next.ServeHTTP(w, r.WithContext(WithUser(r.Context(), u)))
		})
	}
}

// RequireUser sends anyone not signed in to /login.
func RequireUser(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if UserFrom(r.Context()) == nil {
			http.Redirect(w, r, "/login", http.StatusSeeOther)
			return
		}
		next.ServeHTTP(w, r)
	})
}

// FlashProvider keeps Inertia validation errors across the redirect that
// follows a failed form post, in the session.
type FlashProvider struct{ M *scs.SessionManager }

func (f FlashProvider) FlashErrors(ctx context.Context, errs inertia.ValidationErrors) error {
	f.M.Put(ctx, keyErrors, errs)
	return nil
}

func (f FlashProvider) GetErrors(ctx context.Context) (inertia.ValidationErrors, error) {
	errs, _ := f.M.Pop(ctx, keyErrors).(inertia.ValidationErrors)
	return errs, nil
}

func (f FlashProvider) ShouldClearHistory(ctx context.Context) (bool, error) {
	return f.M.PopBool(ctx, keyClear), nil
}

func (f FlashProvider) FlashClearHistory(ctx context.Context) error {
	f.M.Put(ctx, keyClear, true)
	return nil
}
