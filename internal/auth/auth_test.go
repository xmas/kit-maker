package auth_test

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"entgo.io/ent/dialect"
	entsql "entgo.io/ent/dialect/sql"

	"github.com/docker/kit-maker/internal/auth"
	"github.com/docker/kit-maker/internal/ent"
	"github.com/docker/kit-maker/internal/ent/user"
	"github.com/docker/kit-maker/internal/testutil"
)

func TestPasswordHash(t *testing.T) {
	h, err := auth.HashPassword("correct-horse-battery")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(h, "$argon2id$v=19$m=65536,t=3,p=2$") {
		t.Fatalf("unexpected PHC string %q", h)
	}
	if ok, err := auth.VerifyPassword(h, "correct-horse-battery"); err != nil || !ok {
		t.Fatalf("verify good: %v %v", ok, err)
	}
	if ok, _ := auth.VerifyPassword(h, "correct-horse-batterz"); ok {
		t.Fatal("verify bad password succeeded")
	}
	if _, err := auth.HashPassword("short"); !errors.Is(err, auth.ErrWeakPassword) {
		t.Fatalf("short password: %v", err)
	}
}

func TestLoginAndLockout(t *testing.T) {
	ctx := context.Background()
	db := testutil.StartPostgres(t)
	client := ent.NewClient(ent.Driver(entsql.OpenDB(dialect.Postgres, db.SQL)))
	now := time.Date(2026, 10, 17, 9, 0, 0, 0, time.UTC)
	s := &auth.Service{Ent: client, Now: func() time.Time { return now }}

	if _, err := s.CreateUser(ctx, " Sam@Acme.com ", "Sam", "correct-horse-battery"); err != nil {
		t.Fatal(err)
	}

	u, err := s.Login(ctx, "sam@acme.com", "correct-horse-battery")
	if err != nil || u.LastLoginAt == nil {
		t.Fatalf("login: %v %+v", err, u)
	}

	for i := 0; i < auth.MaxFailedLogins; i++ {
		if _, err := s.Login(ctx, "sam@acme.com", "wrong-wrong-wrong"); !errors.Is(err, auth.ErrBadCredentials) {
			t.Fatalf("attempt %d: %v", i, err)
		}
	}
	if _, err := s.Login(ctx, "sam@acme.com", "correct-horse-battery"); !errors.Is(err, auth.ErrLocked) {
		t.Fatalf("want locked, got %v", err)
	}
	now = now.Add(auth.LockoutDuration + time.Second)
	if _, err := s.Login(ctx, "sam@acme.com", "correct-horse-battery"); err != nil {
		t.Fatalf("after lockout: %v", err)
	}

	before := u.SessionVersion
	u, err = s.SetPassword(ctx, "sam@acme.com", "another-long-password")
	if err != nil || u.SessionVersion != before+1 {
		t.Fatalf("set password should bump session version: %v %d→%d", err, before, u.SessionVersion)
	}

	if _, err := s.SetStatus(ctx, "sam@acme.com", user.StatusSuspended); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Login(ctx, "sam@acme.com", "another-long-password"); !errors.Is(err, auth.ErrInactive) {
		t.Fatalf("suspended: %v", err)
	}
}
