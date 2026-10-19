// Package auth signs people in (DESIGN.md §12). This slice covers local
// username/password accounts with lockout, server-side sessions, and "sign
// out everywhere". OIDC, SAML and LDAP providers plug in next to it.
package auth

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/docker/kit-maker/internal/ent"
	"github.com/docker/kit-maker/internal/ent/user"
)

// Lockout policy for password sign-in.
const (
	MaxFailedLogins = 5
	LockoutDuration = 15 * time.Minute
)

var (
	// ErrBadCredentials is returned for an unknown email or a wrong password,
	// deliberately the same so sign-in doesn't reveal which accounts exist.
	ErrBadCredentials = errors.New("email or password is incorrect")
	// ErrLocked means too many failed attempts.
	ErrLocked = errors.New("too many failed attempts; try again later")
	// ErrInactive means the account is suspended or deprovisioned.
	ErrInactive = errors.New("this account is not active")
)

// Service manages local accounts.
type Service struct {
	Ent *ent.Client
	Now func() time.Time
}

func (s *Service) now() time.Time {
	if s.Now != nil {
		return s.Now()
	}
	return time.Now()
}

// NormalizeEmail lower-cases and trims an email address.
func NormalizeEmail(email string) string { return strings.ToLower(strings.TrimSpace(email)) }

// Login checks a local password and returns the user. Failures count towards
// a lockout; success resets the count.
func (s *Service) Login(ctx context.Context, email, password string) (*ent.User, error) {
	u, err := s.Ent.User.Query().Where(user.Email(NormalizeEmail(email))).Only(ctx)
	if ent.IsNotFound(err) {
		// Spend the same time as a real check.
		_, _ = VerifyPassword(dummyHash, password)
		return nil, ErrBadCredentials
	}
	if err != nil {
		return nil, err
	}
	now := s.now()
	if u.LockedUntil != nil && now.Before(*u.LockedUntil) {
		return nil, ErrLocked
	}
	if u.PasswordHash == nil || u.IsServiceAccount {
		_, _ = VerifyPassword(dummyHash, password)
		return nil, ErrBadCredentials
	}
	ok, err := VerifyPassword(*u.PasswordHash, password)
	if err != nil {
		return nil, err
	}
	if !ok {
		upd := u.Update().AddFailedLogins(1)
		if u.FailedLogins+1 >= MaxFailedLogins {
			upd.SetLockedUntil(now.Add(LockoutDuration)).SetFailedLogins(0)
		}
		if err := upd.Exec(ctx); err != nil {
			return nil, err
		}
		return nil, ErrBadCredentials
	}
	if u.Status != user.StatusActive {
		return nil, ErrInactive
	}
	return u.Update().SetFailedLogins(0).ClearLockedUntil().SetLastLoginAt(now).Save(ctx)
}

// SetPassword sets a local password and signs the user out everywhere.
func (s *Service) SetPassword(ctx context.Context, email, password string) (*ent.User, error) {
	hash, err := HashPassword(password)
	if err != nil {
		return nil, err
	}
	u, err := s.Ent.User.Query().Where(user.Email(NormalizeEmail(email))).Only(ctx)
	if err != nil {
		return nil, fmt.Errorf("find %s: %w", email, err)
	}
	return u.Update().SetPasswordHash(hash).SetFailedLogins(0).ClearLockedUntil().AddSessionVersion(1).Save(ctx)
}

// CreateUser creates a local account. An empty password leaves it without
// one (it can sign in through SSO, or get a password later).
func (s *Service) CreateUser(ctx context.Context, email, displayName, password string) (*ent.User, error) {
	email = NormalizeEmail(email)
	if !strings.Contains(email, "@") {
		return nil, fmt.Errorf("%q is not an email address", email)
	}
	c := s.Ent.User.Create().SetEmail(email).SetDisplayName(displayName)
	if password != "" {
		hash, err := HashPassword(password)
		if err != nil {
			return nil, err
		}
		c.SetPasswordHash(hash)
	}
	return c.Save(ctx)
}

// SetStatus suspends or reactivates a user. Leaving active signs them out.
func (s *Service) SetStatus(ctx context.Context, email string, status user.Status) (*ent.User, error) {
	u, err := s.Ent.User.Query().Where(user.Email(NormalizeEmail(email))).Only(ctx)
	if err != nil {
		return nil, fmt.Errorf("find %s: %w", email, err)
	}
	upd := u.Update().SetStatus(status)
	if status != user.StatusActive {
		upd.AddSessionVersion(1)
	}
	return upd.Save(ctx)
}

// SignOutEverywhere invalidates every session the user has.
func (s *Service) SignOutEverywhere(ctx context.Context, userID string) error {
	return s.Ent.User.UpdateOneID(userID).AddSessionVersion(1).Exec(ctx)
}

// dummyHash keeps timing similar when there is no hash to check.
var dummyHash = func() string {
	h, err := HashPassword("not-a-real-password-for-timing")
	if err != nil {
		panic(err)
	}
	return h
}()
