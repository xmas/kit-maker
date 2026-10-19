package http

import (
	"encoding/json"
	"errors"
	nethttp "net/http"
	"net/url"
	"strings"

	inertia "github.com/romsar/gonertia/v2"

	"github.com/docker/kit-maker/internal/auth"
)

type loginForm struct {
	Email    string `json:"email"`
	Password string `json:"password"`
	Next     string `json:"next"`
}

// loginPage renders the sign-in screen. Signed-in visitors go to their kits.
func (s *server) loginPage(w nethttp.ResponseWriter, r *nethttp.Request) {
	if auth.UserFrom(r.Context()) != nil {
		nethttp.Redirect(w, r, "/kits", nethttp.StatusSeeOther)
		return
	}
	s.render(w, r, "Auth/Login", inertia.Props{
		"next":      safeNext(r.URL.Query().Get("next")),
		"providers": []map[string]string{{"key": "local", "label": "Email and password"}},
	})
}

// login checks the password and starts a session.
func (s *server) login(w nethttp.ResponseWriter, r *nethttp.Request) {
	var f loginForm
	if err := decodeForm(r, &f); err != nil {
		nethttp.Error(w, "bad request", nethttp.StatusBadRequest)
		return
	}
	u, err := s.Auth.Login(r.Context(), f.Email, f.Password)
	if err != nil {
		msg := err.Error()
		if !errors.Is(err, auth.ErrBadCredentials) && !errors.Is(err, auth.ErrLocked) && !errors.Is(err, auth.ErrInactive) {
			s.Log.ErrorContext(r.Context(), "login", "error", err)
			msg = "sign-in failed; try again"
		}
		s.Log.InfoContext(r.Context(), "login failed", "email", auth.NormalizeEmail(f.Email), "reason", msg)
		ctx := inertia.SetValidationErrors(r.Context(), inertia.ValidationErrors{"email": msg})
		s.inertia.Redirect(w, r.WithContext(ctx), "/login?next="+url.QueryEscape(safeNext(f.Next)))
		return
	}
	if err := auth.SignIn(r.Context(), s.Sessions, u); err != nil {
		s.fail(w, r, "start session", err)
		return
	}
	s.Log.InfoContext(r.Context(), "login", "user", u.ID)
	s.inertia.Redirect(w, r, safeNext(f.Next))
}

// logout ends the current session.
func (s *server) logout(w nethttp.ResponseWriter, r *nethttp.Request) {
	_ = auth.SignOut(r.Context(), s.Sessions)
	s.inertia.Redirect(w, r, "/login")
}

// signOutEverywhere invalidates every session of the current user.
func (s *server) signOutEverywhere(w nethttp.ResponseWriter, r *nethttp.Request) {
	u := auth.UserFrom(r.Context())
	if err := s.Auth.SignOutEverywhere(r.Context(), u.ID); err != nil {
		s.fail(w, r, "sign out everywhere", err)
		return
	}
	_ = auth.SignOut(r.Context(), s.Sessions)
	s.inertia.Redirect(w, r, "/login")
}

// safeNext keeps post-login redirects on this site.
func safeNext(next string) string {
	u, err := url.Parse(next)
	if next == "" || err != nil || u.IsAbs() || u.Host != "" || !strings.HasPrefix(u.Path, "/") || strings.HasPrefix(next, "//") {
		return "/kits"
	}
	return u.RequestURI()
}

// decodeForm reads an Inertia JSON body or a classic form post.
func decodeForm(r *nethttp.Request, v any) error {
	if strings.HasPrefix(r.Header.Get("Content-Type"), "application/json") {
		return json.NewDecoder(nethttp.MaxBytesReader(nil, r.Body, 1<<20)).Decode(v)
	}
	if err := r.ParseForm(); err != nil {
		return err
	}
	b, err := json.Marshal(flatten(r.PostForm))
	if err != nil {
		return err
	}
	return json.Unmarshal(b, v)
}

func flatten(v url.Values) map[string]string {
	out := map[string]string{}
	for k := range v {
		out[k] = v.Get(k)
	}
	return out
}
