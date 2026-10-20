package seed

import (
	"context"
	"testing"

	"entgo.io/ent/dialect"
	entsql "entgo.io/ent/dialect/sql"

	"github.com/docker/kit-maker/internal/dbmigrate"
	"github.com/docker/kit-maker/internal/ent"
	"github.com/docker/kit-maker/internal/ent/block"
	"github.com/docker/kit-maker/internal/testutil"
)

// The seed ships the starter content with every release: off with Samples
// false, public to every team, and refreshed (one new version) when a
// release changes a starter block.
func TestSeedStarterContent(t *testing.T) {
	db := testutil.StartPostgres(t)
	ctx := context.Background()
	if err := dbmigrate.Up(ctx, db.SQL, db.Pool, testutil.Logger()); err != nil {
		t.Fatal(err)
	}
	c := ent.NewClient(ent.Driver(entsql.OpenDB(dialect.Postgres, db.SQL)))

	if err := RunWith(ctx, c, "owner@example.com", Options{Samples: false}); err != nil {
		t.Fatal(err)
	}
	if n := c.Blueprint.Query().CountX(ctx); n != 0 {
		t.Fatalf("samples off: %d blueprints", n)
	}

	if err := Run(ctx, c, "owner@example.com"); err != nil {
		t.Fatal(err)
	}
	if n := c.Blueprint.Query().CountX(ctx); n < 40 {
		t.Fatalf("starter blueprints: %d", n)
	}
	if n := c.Block.Query().Where(block.VisibilityNEQ(block.VisibilityPublic), block.VisibilityNEQ(block.VisibilityPrivate)).CountX(ctx); n != 0 {
		t.Errorf("%d starter blocks aren't public", n)
	}
	// Seeds from before starter blocks were public made them team-only; a
	// re-seed (an upgrade) makes every one public, and adds no blocks.
	before := c.Block.Query().CountX(ctx)
	c.Block.Update().Where(block.VisibilityEQ(block.VisibilityPublic)).SetVisibility(block.VisibilityTeam).ExecX(ctx)
	if err := Run(ctx, c, "owner@example.com"); err != nil {
		t.Fatal(err)
	}
	if n := c.Block.Query().Where(block.VisibilityEQ(block.VisibilityTeam)).CountX(ctx); n != 0 {
		t.Errorf("after re-seed, %d starter blocks are still team-only", n)
	}
	if after := c.Block.Query().CountX(ctx); after != before {
		t.Errorf("re-seed changed the block count: %d → %d", before, after)
	}

	// A release that changes a starter block publishes it as a new version.
	saved := profileBlocks
	defer func() { profileBlocks = saved }()
	profileBlocks = append([]profileBlock(nil), saved...)
	for i, pb := range profileBlocks {
		if pb.slug == "standard-resources" {
			profileBlocks[i].payload = map[string]any{"cpu": 4, "memory": "8GiB"}
		}
	}
	for range 2 { // the second run finds nothing new
		if err := Run(ctx, c, "owner@example.com"); err != nil {
			t.Fatal(err)
		}
	}
	b := c.Block.Query().Where(block.Slug("standard-resources")).WithCurrentVersion().OnlyX(ctx)
	if v := b.Edges.CurrentVersion; v.Version != 2 || v.Payload["cpu"] != float64(4) {
		t.Fatalf("refreshed block: v%d %v", v.Version, v.Payload)
	}
}
