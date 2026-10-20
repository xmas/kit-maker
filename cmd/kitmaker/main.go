// Command kitmaker is the single Kit Maker binary:
// serve | worker | migrate | seed | dev.
package main

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"syscall"

	"github.com/spf13/cobra"

	"github.com/docker/kit-maker/internal/app"
)

// version is set at build time (-ldflags "-X main.version=…").
var version = "dev"

func main() {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	if err := rootCmd().ExecuteContext(ctx); err != nil {
		fmt.Fprintln(os.Stderr, "kitmaker:", err)
		os.Exit(1)
	}
}

func rootCmd() *cobra.Command {
	root := &cobra.Command{
		Use:           "kitmaker",
		Short:         "Build blueprints for Docker Sandboxes kits from reusable blocks",
		Version:       version,
		SilenceUsage:  true,
		SilenceErrors: true,
	}
	root.AddCommand(serveCmd(), workerCmd(), migrateCmd(), seedCmd(), devCmd(),
		secretsCmd(), integrationCmd(), aiCmd(), userCmd(), teamCmd(), roleCmd())
	return root
}

// open loads config from the environment and connects to the backing services.
func open(ctx context.Context) (*app.App, error) {
	cfg, err := app.ConfigFromEnv()
	if err != nil {
		return nil, err
	}
	return app.New(ctx, cfg, app.NewLogger(cfg.LogFormat, cfg.LogLevel))
}
