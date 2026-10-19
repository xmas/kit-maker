package app

import (
	"context"
	"database/sql"
	"fmt"
	"log/slog"
	"os"
	"strings"

	"entgo.io/ent/dialect"
	entsql "entgo.io/ent/dialect/sql"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/jackc/pgx/v5/stdlib"
	"gocloud.dev/blob"

	"github.com/docker/kit-maker/internal/ai"
	"github.com/docker/kit-maker/internal/drivers"
	"github.com/docker/kit-maker/internal/drivers/storage"
	"github.com/docker/kit-maker/internal/ent"
	"github.com/docker/kit-maker/internal/secrets"
	"github.com/docker/kit-maker/internal/settings"
)

// App holds the long-lived dependencies shared by every command.
type App struct {
	Config Config
	Log    *slog.Logger
	// Pool is the one connection pool; River uses it directly, and DB wraps
	// it for database/sql users (Ent, Atlas).
	Pool   *pgxpool.Pool
	DB     *sql.DB
	Ent    *ent.Client
	Bucket *blob.Bucket
	// Keeper seals integration configs and settings.
	Keeper   *secrets.Keeper
	Settings *settings.Service
	// AI hands out metered clients for the default AI integration.
	AI *ai.Provider
}

// OpenDB opens the connection pool and a database/sql view of it.
func OpenDB(ctx context.Context, url string) (*pgxpool.Pool, *sql.DB, error) {
	pool, err := pgxpool.New(ctx, url)
	if err != nil {
		return nil, nil, fmt.Errorf("open database: %w", err)
	}
	return pool, stdlib.OpenDBFromPool(pool), nil
}

// New connects to Postgres and object storage.
func New(ctx context.Context, cfg Config, log *slog.Logger) (*App, error) {
	if err := cfg.RequireStorage(); err != nil {
		return nil, err
	}
	pool, db, err := OpenDB(ctx, cfg.DatabaseURL)
	if err != nil {
		return nil, err
	}
	bucket, err := storage.Open(ctx, cfg.StorageURL)
	if err != nil {
		pool.Close()
		return nil, err
	}
	keeper, err := secrets.Open(ctx, cfg.SecretsURL)
	if err != nil {
		_ = bucket.Close()
		pool.Close()
		return nil, err
	}
	client := ent.NewClient(ent.Driver(entsql.OpenDB(dialect.Postgres, db)))
	st := &settings.Service{Ent: client, Keeper: keeper}
	return &App{
		Config:   cfg,
		Log:      log,
		Pool:     pool,
		DB:       db,
		Ent:      client,
		Bucket:   bucket,
		Keeper:   keeper,
		Settings: st,
		AI:       NewAIProvider(client, st, cfg.AIFixturesDir),
	}, nil
}

// NewAIProvider wires the AI provider to the default AI integration and the
// "ai.budget" deployment setting.
func NewAIProvider(client *ent.Client, st *settings.Service, fixturesDir string) *ai.Provider {
	return &ai.Provider{
		Default: func(ctx context.Context) (drivers.Driver, drivers.Config, error) {
			return st.Default(ctx, drivers.KindAI)
		},
		Ent: client,
		Budget: func(ctx context.Context) (ai.Budget, error) {
			var b ai.Budget
			_, err := st.GetSetting(ctx, ai.BudgetSettingKey, &b)
			return b, err
		},
		FixturesDir: fixturesDir,
	}
}

func (a *App) Close() {
	_ = a.Keeper.Close()
	_ = a.Bucket.Close()
	_ = a.Ent.Close() // closes DB
	a.Pool.Close()
}

// NewLogger returns the process logger: JSON to stdout unless format is "text".
func NewLogger(format, level string) *slog.Logger {
	var lvl slog.Level
	if err := lvl.UnmarshalText([]byte(strings.ToUpper(level))); err != nil {
		lvl = slog.LevelInfo
	}
	opts := &slog.HandlerOptions{Level: lvl}
	if format == "text" {
		return slog.New(slog.NewTextHandler(os.Stdout, opts))
	}
	return slog.New(slog.NewJSONHandler(os.Stdout, opts))
}
