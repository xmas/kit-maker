// Command migratediff writes a new versioned migration to migrations/ from
// the difference between the Ent schema and the migration directory.
//
// It replays the directory on a throwaway pgvector container (testcontainers),
// so it needs Docker but not the Atlas CLI:
//
//	go run ./internal/tools/migratediff add_widgets
package main

import (
	"context"
	"database/sql"
	"fmt"
	"log"
	"os"
	"time"

	"ariga.io/atlas/sql/migrate"
	"entgo.io/ent/dialect"
	"entgo.io/ent/dialect/sql/schema"
	"github.com/jackc/pgx/v5/stdlib"
	"github.com/testcontainers/testcontainers-go"
	"github.com/testcontainers/testcontainers-go/modules/postgres"
	"github.com/testcontainers/testcontainers-go/wait"

	entmigrate "github.com/docker/kit-maker/internal/ent/migrate"
)

const dirPath = "migrations"

// Ent opens the dev database by URL scheme through database/sql.
func init() { sql.Register("postgres", stdlib.GetDefaultDriver()) }

func main() {
	if len(os.Args) != 2 {
		log.Fatal("usage: migratediff <name>")
	}
	if err := run(context.Background(), os.Args[1]); err != nil {
		log.Fatal(err)
	}
}

func run(ctx context.Context, name string) error {
	pg, err := postgres.Run(ctx, "pgvector/pgvector:pg16",
		postgres.WithDatabase("dev"),
		testcontainers.WithWaitStrategy(wait.ForListeningPort("5432/tcp").WithStartupTimeout(time.Minute)),
	)
	if err != nil {
		return fmt.Errorf("start dev database: %w", err)
	}
	defer func() { _ = testcontainers.TerminateContainer(pg) }()

	url, err := pg.ConnectionString(ctx, "sslmode=disable", "search_path=public")
	if err != nil {
		return err
	}

	dir, err := migrate.NewLocalDir(dirPath)
	if err != nil {
		return err
	}
	// Re-hash first so hand-written files (e.g. extensions) are accepted.
	sum, err := dir.Checksum()
	if err != nil {
		return err
	}
	if err := migrate.WriteSumFile(dir, sum); err != nil {
		return err
	}

	err = schema.Diff(ctx, url, name, entmigrate.Tables,
		schema.WithDir(dir),
		schema.WithMigrationMode(schema.ModeReplay),
		schema.WithDialect(dialect.Postgres),
		schema.WithFormatter(migrate.DefaultFormatter),
		schema.WithDropColumn(true),
		schema.WithDropIndex(true),
	)
	if err != nil {
		return fmt.Errorf("diff: %w", err)
	}
	return nil
}
