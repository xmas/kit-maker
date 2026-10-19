// Package app holds bootstrap config and wiring. Env vars cover bootstrap
// only (DESIGN.md §11.1); drivers are configured in the admin UI later.
package app

import (
	"errors"
	"os"
)

type Config struct {
	// HTTPAddr is where `serve` listens.
	HTTPAddr string
	// BaseURL is the URL the deployment is reached at.
	BaseURL string
	// DatabaseURL is a Postgres connection URL.
	DatabaseURL string
	// StorageURL is a gocloud.dev/blob URL (see internal/drivers/storage).
	StorageURL string
	// SecretsURL is a gocloud.dev/secrets keeper URL (see internal/secrets).
	SecretsURL string
	// LogFormat is "json" (default) or "text".
	LogFormat string
	// LogLevel is debug, info, warn or error.
	LogLevel string
	// ViteHotFile is the file the Vite dev server writes its URL to. When it
	// exists, pages load assets from Vite (hot reload) instead of the embedded
	// build.
	ViteHotFile string
	// AIFixturesDir adds recorded AI fixtures on top of the built-in ones for
	// the fake AI driver (internal/ai).
	AIFixturesDir string
}

// ConfigFromEnv reads KITMAKER_* variables.
func ConfigFromEnv() (Config, error) {
	c := Config{
		HTTPAddr:      env("KITMAKER_HTTP_ADDR", ":8080"),
		BaseURL:       env("KITMAKER_BASE_URL", "http://localhost:8080"),
		DatabaseURL:   env("KITMAKER_DATABASE_URL", ""),
		StorageURL:    env("KITMAKER_STORAGE_URL", ""),
		SecretsURL:    env("KITMAKER_SECRETS_URL", ""),
		LogFormat:     env("KITMAKER_LOG_FORMAT", "json"),
		LogLevel:      env("KITMAKER_LOG_LEVEL", "info"),
		ViteHotFile:   env("KITMAKER_VITE_HOT_FILE", "web/hot"),
		AIFixturesDir: env("KITMAKER_AI_FIXTURES", ""),
	}
	if c.DatabaseURL == "" {
		return c, errors.New("missing required env: KITMAKER_DATABASE_URL")
	}
	return c, nil
}

// RequireStorage reports whether object storage is configured. Every command
// except migrate needs it.
func (c Config) RequireStorage() error {
	if c.StorageURL == "" {
		return errors.New("missing required env: KITMAKER_STORAGE_URL")
	}
	return nil
}

func env(key, def string) string {
	if v, ok := os.LookupEnv(key); ok && v != "" {
		return v
	}
	return def
}
