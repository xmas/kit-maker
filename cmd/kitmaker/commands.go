package main

import (
	"context"
	"fmt"
	"net/url"
	"os"
	"strings"

	"github.com/spf13/cobra"
	"golang.org/x/sync/errgroup"

	"github.com/docker/kit-maker/internal/app"
	"github.com/docker/kit-maker/internal/auth"
	"github.com/docker/kit-maker/internal/dbmigrate"
	kmhttp "github.com/docker/kit-maker/internal/http"
	"github.com/docker/kit-maker/internal/jobs"
	"github.com/docker/kit-maker/internal/seed"
	"github.com/docker/kit-maker/web"
)

func serveCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "serve",
		Short: "Serve the web UI and APIs",
		RunE: func(cmd *cobra.Command, _ []string) error {
			a, err := open(cmd.Context())
			if err != nil {
				return err
			}
			defer a.Close()
			return serve(cmd.Context(), a, "")
		},
	}
}

func workerCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "worker",
		Short: "Run background jobs and the scheduler",
		RunE: func(cmd *cobra.Command, _ []string) error {
			a, err := open(cmd.Context())
			if err != nil {
				return err
			}
			defer a.Close()
			return work(cmd.Context(), a)
		},
	}
}

func migrateCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "migrate",
		Short: "Apply database migrations (embedded in the binary)",
		RunE: func(cmd *cobra.Command, _ []string) error {
			// Only the database: the Helm pre-upgrade Job has no storage access.
			cfg, err := app.ConfigFromEnv()
			if err != nil {
				return err
			}
			pool, db, err := app.OpenDB(cmd.Context(), cfg.DatabaseURL)
			if err != nil {
				return err
			}
			defer pool.Close()
			return dbmigrate.Up(cmd.Context(), db, pool, app.NewLogger(cfg.LogFormat, cfg.LogLevel))
		},
	}
}

func seedCmd() *cobra.Command {
	var admin string
	var samples bool
	c := &cobra.Command{
		Use:   "seed",
		Short: "Create the bootstrap Owner and team, and load the starter blueprints and blocks",
		RunE: func(cmd *cobra.Command, _ []string) error {
			a, err := open(cmd.Context())
			if err != nil {
				return err
			}
			defer a.Close()
			return runSeed(cmd.Context(), a, admin, samples)
		},
	}
	c.Flags().StringVar(&admin, "admin-email", envOr("KITMAKER_BOOTSTRAP_ADMIN", "admin@kitmaker.local"), "bootstrap owner email")
	c.Flags().BoolVar(&samples, "samples", os.Getenv("KITMAKER_SEED_SAMPLES") != "false",
		"load the starter library, spec example and sample blueprints (env KITMAKER_SEED_SAMPLES=false to skip)")
	return c
}

func devCmd() *cobra.Command {
	var noMigrate, noSeed bool
	var devUser string
	c := &cobra.Command{
		Use:   "dev",
		Short: "Migrate, seed, then serve and work in one process (for a laptop)",
		RunE: func(cmd *cobra.Command, _ []string) error {
			ctx := cmd.Context()
			a, err := open(ctx)
			if err != nil {
				return err
			}
			defer a.Close()
			if !noMigrate {
				if err := dbmigrate.Up(ctx, a.DB, a.Pool, a.Log); err != nil {
					return err
				}
			}
			if !noSeed {
				if err := runSeed(ctx, a, envOr("KITMAKER_BOOTSTRAP_ADMIN", "admin@kitmaker.local"), os.Getenv("KITMAKER_SEED_SAMPLES") != "false"); err != nil {
					return err
				}
			}
			if devUser != "" {
				if err := localOnly(a.Config.BaseURL); err != nil {
					return fmt.Errorf("--dev-user: %w", err)
				}
				a.Log.Warn("signing in every request as the dev user; no password needed", "user", devUser)
			}
			g, ctx := errgroup.WithContext(ctx)
			g.Go(func() error { return serve(ctx, a, devUser) })
			g.Go(func() error { return work(ctx, a) })
			return g.Wait()
		},
	}
	c.Flags().BoolVar(&noMigrate, "no-migrate", false, "skip migrations")
	c.Flags().BoolVar(&noSeed, "no-seed", false, "skip seeding")
	c.Flags().StringVar(&devUser, "dev-user", os.Getenv("KITMAKER_DEV_USER"),
		"sign in every request as this account, no password (local URLs only; env KITMAKER_DEV_USER)")
	return c
}

// localOnly refuses a base URL that isn't this machine, so the dev user can
// never sign people in on a deployment someone else can reach.
func localOnly(baseURL string) error {
	u, err := url.Parse(baseURL)
	if err != nil {
		return err
	}
	switch h := u.Hostname(); {
	case h == "localhost", h == "127.0.0.1", h == "::1", strings.HasSuffix(h, ".localhost"):
		return nil
	default:
		return fmt.Errorf("only for a local base URL, not %q", baseURL)
	}
}

func serve(ctx context.Context, a *app.App, devUser string) error {
	client, err := jobs.NewClient(a.Pool, a.Log, false)
	if err != nil {
		return err
	}
	h, err := kmhttp.NewHandler(kmhttp.Deps{
		Log:         a.Log,
		DB:          a.DB,
		Ent:         a.Ent,
		Bucket:      a.Bucket,
		Jobs:        client,
		Assets:      web.Dist(),
		ViteHotFile: a.Config.ViteHotFile,
		Sessions:    auth.NewSessions(a.Pool, a.Config.BaseURL),
		Auth:        &auth.Service{Ent: a.Ent},
		AI:          a.AI,
		Settings:    a.Settings,
		DevUser:     devUser,
	})
	if err != nil {
		return err
	}
	return kmhttp.Serve(ctx, a.Config.HTTPAddr, h, a.Log)
}

func work(ctx context.Context, a *app.App) error {
	client, err := jobs.NewClient(a.Pool, a.Log, true)
	if err != nil {
		return err
	}
	if err := client.Start(ctx); err != nil {
		return fmt.Errorf("start worker: %w", err)
	}
	a.Log.Info("worker started")
	<-ctx.Done()
	return client.Stop(context.WithoutCancel(ctx))
}

func runSeed(ctx context.Context, a *app.App, admin string, samples bool) error {
	if err := seed.RunWith(ctx, a.Ent, admin, seed.Options{Samples: samples}); err != nil {
		return fmt.Errorf("seed: %w", err)
	}
	if pw := os.Getenv("KITMAKER_BOOTSTRAP_PASSWORD"); pw != "" {
		set, err := seed.BootstrapPassword(ctx, &auth.Service{Ent: a.Ent}, admin, pw)
		if err != nil {
			return err
		}
		if set {
			a.Log.Info("bootstrap password set", "admin", admin)
		}
	}
	if err := seed.Integrations(ctx, a.Settings, seed.LocalStack{
		RegistryHost:     os.Getenv("KITMAKER_REGISTRY_URL"),
		RegistryPullHost: os.Getenv("KITMAKER_REGISTRY_PULL_HOST"),
		RegistryInsecure: os.Getenv("KITMAKER_REGISTRY_INSECURE") != "false",
		BuildKitAddr:     os.Getenv("KITMAKER_BUILDKIT_ADDR"),
	}); err != nil {
		return err
	}
	// Show the queue working end to end: the worker logs this.
	client, err := jobs.NewClient(a.Pool, a.Log, false)
	if err != nil {
		return err
	}
	if _, err := client.Insert(ctx, jobs.EchoArgs{Message: "seeded"}, nil); err != nil {
		return fmt.Errorf("enqueue echo: %w", err)
	}
	a.Log.Info("seed complete", "admin", admin)
	return nil
}

func envOr(key, def string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return def
}
