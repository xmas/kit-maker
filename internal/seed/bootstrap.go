package seed

import (
	"context"

	"github.com/docker/kit-maker/internal/auth"
	"github.com/docker/kit-maker/internal/ent/user"
)

// BootstrapPassword gives the bootstrap Owner a password when it has none
// (KITMAKER_BOOTSTRAP_PASSWORD). An existing password is never replaced, so
// the env value only matters for the first sign-in.
func BootstrapPassword(ctx context.Context, a *auth.Service, email, password string) (bool, error) {
	u, err := a.Ent.User.Query().Where(user.Email(auth.NormalizeEmail(email))).Only(ctx)
	if err != nil {
		return false, err
	}
	if u.PasswordHash != nil {
		return false, nil
	}
	_, err = a.SetPassword(ctx, email, password)
	return err == nil, err
}
