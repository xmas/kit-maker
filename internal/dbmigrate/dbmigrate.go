// Package dbmigrate applies the embedded Atlas migrations (the app schema in
// public) and River's own migrations (in the river schema).
package dbmigrate

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"io/fs"
	"log/slog"
	"path"
	"strings"

	"ariga.io/atlas/sql/migrate"
	"ariga.io/atlas/sql/postgres"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/riverqueue/river/riverdriver/riverpgxv5"
	"github.com/riverqueue/river/rivermigrate"

	"github.com/docker/kit-maker/internal/jobs"
	"github.com/docker/kit-maker/migrations"
)

// Up applies every pending migration. Atlas runs first: on a fresh database
// it requires a clean realm, and River's schema would make it dirty.
func Up(ctx context.Context, db *sql.DB, pool *pgxpool.Pool, log *slog.Logger) error {
	if err := upAtlas(ctx, db, log); err != nil {
		return err
	}
	return upRiver(ctx, pool, log)
}

func upAtlas(ctx context.Context, db *sql.DB, log *slog.Logger) error {
	dir, err := embeddedDir()
	if err != nil {
		return err
	}
	drv, err := postgres.Open(db)
	if err != nil {
		return fmt.Errorf("open atlas driver: %w", err)
	}
	rrw := &revisions{db: db}
	if err := rrw.init(ctx); err != nil {
		return fmt.Errorf("create revisions table: %w", err)
	}
	ex, err := migrate.NewExecutor(drv, dir, rrw,
		migrate.WithLogger(atlasLogger{log}),
		migrate.WithOperatorVersion("kitmaker"),
	)
	if err != nil {
		return err
	}
	switch err := ex.ExecuteN(ctx, 0); {
	case errors.Is(err, migrate.ErrNoPendingFiles):
		log.Info("schema is up to date")
	case err != nil:
		return fmt.Errorf("apply migrations: %w", err)
	}
	return nil
}

func upRiver(ctx context.Context, pool *pgxpool.Pool, log *slog.Logger) error {
	if _, err := pool.Exec(ctx, "CREATE SCHEMA IF NOT EXISTS "+jobs.Schema); err != nil {
		return fmt.Errorf("create river schema: %w", err)
	}
	m, err := rivermigrate.New(riverpgxv5.New(pool), &rivermigrate.Config{Schema: jobs.Schema, Logger: log})
	if err != nil {
		return err
	}
	res, err := m.Migrate(ctx, rivermigrate.DirectionUp, nil)
	if err != nil {
		return fmt.Errorf("apply river migrations: %w", err)
	}
	for _, v := range res.Versions {
		log.Info("applied river migration", "version", v.Version, "name", v.Name)
	}
	return nil
}

// ErrBehind is returned by Check when the database is missing migrations.
var ErrBehind = errors.New("database schema is behind the binary")

// Check reports whether the newest embedded migration has been applied. It is
// cheap enough for a readiness probe.
func Check(ctx context.Context, db *sql.DB) error {
	latest, err := latestVersion()
	if err != nil {
		return err
	}
	var applied sql.NullString
	err = db.QueryRowContext(ctx,
		`SELECT max(version) FROM `+revisionSchema+`.`+revisionTable+` WHERE applied = total`).Scan(&applied)
	if err != nil {
		return fmt.Errorf("%w: %v", ErrBehind, err)
	}
	if applied.String < latest {
		return fmt.Errorf("%w: applied %q, want %q", ErrBehind, applied.String, latest)
	}
	return nil
}

func embeddedDir() (*migrate.MemDir, error) {
	dir := migrate.OpenMemDir("kitmaker")
	entries, err := fs.ReadDir(migrations.FS, ".")
	if err != nil {
		return nil, err
	}
	for _, e := range entries {
		if e.IsDir() || (path.Ext(e.Name()) != ".sql" && e.Name() != migrate.HashFileName) {
			continue
		}
		b, err := fs.ReadFile(migrations.FS, e.Name())
		if err != nil {
			return nil, err
		}
		if err := dir.WriteFile(e.Name(), b); err != nil {
			return nil, err
		}
	}
	return dir, nil
}

func latestVersion() (string, error) {
	entries, err := fs.Glob(migrations.FS, "*.sql")
	if err != nil || len(entries) == 0 {
		return "", fmt.Errorf("no embedded migrations: %v", err)
	}
	last := entries[len(entries)-1] // fs.Glob returns names in lexical order
	v, _, _ := strings.Cut(last, "_")
	return v, nil
}

type atlasLogger struct{ log *slog.Logger }

func (l atlasLogger) Log(e migrate.LogEntry) {
	switch e := e.(type) {
	case migrate.LogFile:
		l.log.Info("applying migration", "version", e.File.Version(), "desc", e.File.Desc())
	case migrate.LogError:
		l.log.Error("migration failed", "error", e.Error, "stmt", e.SQL)
	case migrate.LogDone:
		l.log.Info("migrations applied")
	}
}
