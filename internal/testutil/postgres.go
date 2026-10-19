// Package testutil starts real backing services for tests (DESIGN.md §11.5:
// no SQLite divergence on jsonb or pgvector).
package testutil

import (
	"context"
	"database/sql"
	"io"
	"log/slog"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/jackc/pgx/v5/stdlib"
	"github.com/testcontainers/testcontainers-go"
	"github.com/testcontainers/testcontainers-go/modules/postgres"
	"github.com/testcontainers/testcontainers-go/wait"

	"github.com/docker/kit-maker/internal/dbmigrate"
)

// PostgresImage matches the Compose stack and the Helm chart's expectations.
const PostgresImage = "pgvector/pgvector:pg16"

// DB is a migrated, throwaway Postgres.
type DB struct {
	URL  string
	Pool *pgxpool.Pool
	SQL  *sql.DB
}

// StartPostgres starts Postgres+pgvector, applies all migrations, and stops
// it when the test ends.
func StartPostgres(t testing.TB) *DB {
	t.Helper()
	ctx := context.Background()
	pg, err := postgres.Run(ctx, PostgresImage,
		postgres.WithDatabase("kitmaker"),
		postgres.WithUsername("kitmaker"),
		postgres.WithPassword("kitmaker"),
		testcontainers.WithWaitStrategy(
			wait.ForLog("database system is ready to accept connections").
				WithOccurrence(2).WithStartupTimeout(time.Minute)),
	)
	testcontainers.CleanupContainer(t, pg)
	if err != nil {
		t.Fatalf("start postgres: %v", err)
	}
	url, err := pg.ConnectionString(ctx, "sslmode=disable")
	if err != nil {
		t.Fatal(err)
	}
	pool, err := pgxpool.New(ctx, url)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(pool.Close)
	db := stdlib.OpenDBFromPool(pool)
	if err := dbmigrate.Up(ctx, db, pool, Logger()); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	return &DB{URL: url, Pool: pool, SQL: db}
}

// Logger discards output; tests that care about logs make their own.
func Logger() *slog.Logger {
	return slog.New(slog.NewTextHandler(io.Discard, nil))
}
