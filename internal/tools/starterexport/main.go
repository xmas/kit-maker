// Command starterexport writes the starter content every release ships
// (internal/seed: the block library, the spec examples and the sample
// blueprints) as documents for the Cloudflare edition (internal/lite):
//
//	go run ./internal/tools/starterexport cloudflare/public/starter.json
//
// It seeds a throwaway pgvector container (testcontainers), so it needs
// Docker. IDs are derived from slugs (blk_<slug>, bp_<slug>, …) so they stay
// the same from one release to the next: blueprints people saved keep
// pointing at the same blocks.
package main

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"log/slog"
	"os"
	"sort"
	"strings"
	"time"

	"entgo.io/ent/dialect"
	entsql "entgo.io/ent/dialect/sql"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/jackc/pgx/v5/stdlib"
	"github.com/testcontainers/testcontainers-go"
	"github.com/testcontainers/testcontainers-go/modules/postgres"
	"github.com/testcontainers/testcontainers-go/wait"

	"github.com/docker/kit-maker/internal/dbmigrate"
	"github.com/docker/kit-maker/internal/ent"
	"github.com/docker/kit-maker/internal/ent/blockversion"
	"github.com/docker/kit-maker/internal/ent/blueprintrevision"
	"github.com/docker/kit-maker/internal/ent/check"
	"github.com/docker/kit-maker/internal/ent/slot"
	"github.com/docker/kit-maker/internal/lite"
	"github.com/docker/kit-maker/internal/seed"
)

// Starter is the file's shape.
type Starter struct {
	Team lite.Team  `json:"team"`
	Docs []lite.Doc `json:"docs"`
}

func main() {
	if len(os.Args) != 2 {
		log.Fatal("usage: starterexport <out.json>")
	}
	if err := run(context.Background(), os.Args[1]); err != nil {
		log.Fatal(err)
	}
}

func run(ctx context.Context, out string) error {
	pg, err := postgres.Run(ctx, "pgvector/pgvector:pg16",
		postgres.WithDatabase("kitmaker"), postgres.WithUsername("kitmaker"), postgres.WithPassword("kitmaker"),
		testcontainers.WithWaitStrategy(wait.ForLog("database system is ready to accept connections").
			WithOccurrence(2).WithStartupTimeout(time.Minute)),
	)
	if err != nil {
		return fmt.Errorf("start database: %w", err)
	}
	defer func() { _ = testcontainers.TerminateContainer(pg) }()
	url, err := pg.ConnectionString(ctx, "sslmode=disable")
	if err != nil {
		return err
	}
	pool, err := pgxpool.New(ctx, url)
	if err != nil {
		return err
	}
	defer pool.Close()
	db := stdlib.OpenDBFromPool(pool)
	if err := dbmigrate.Up(ctx, db, pool, slog.New(slog.NewTextHandler(io.Discard, nil))); err != nil {
		return fmt.Errorf("migrate: %w", err)
	}
	c := ent.NewClient(ent.Driver(entsql.OpenDB(dialect.Postgres, db)))
	if err := seed.Run(ctx, c, "admin@kitmaker.local"); err != nil {
		return fmt.Errorf("seed: %w", err)
	}
	st, err := export(ctx, c)
	if err != nil {
		return err
	}
	b, err := json.Marshal(st)
	if err != nil {
		return err
	}
	if err := os.WriteFile(out, b, 0o644); err != nil {
		return err
	}
	fmt.Printf("wrote %s: %d documents, %d KB\n", out, len(st.Docs), len(b)/1024)
	return nil
}

func export(ctx context.Context, c *ent.Client) (*Starter, error) {
	team, err := c.Team.Query().Only(ctx)
	if err != nil {
		return nil, err
	}
	ids := map[string]string{team.ID: "team_" + team.Slug}
	st := &Starter{Team: lite.Team{ID: ids[team.ID], Name: team.Name}}

	blocks, err := c.Block.Query().WithVersions(func(q *ent.BlockVersionQuery) {
		q.Order(ent.Asc(blockversion.FieldVersion))
	}).WithCurrentVersion().All(ctx)
	if err != nil {
		return nil, err
	}
	var docs []any
	for _, b := range blocks {
		ids[b.ID] = "blk_" + b.Slug
		lb := lite.Block{ID: b.ID, TeamID: b.TeamID, Type: b.Type, Slug: b.Slug, Name: b.Name, Description: b.Description,
			Visibility: string(b.Visibility), CreatedAt: b.CreatedAt.UTC(), UpdatedAt: b.UpdatedAt.UTC()}
		if b.Edges.CurrentVersion != nil {
			lb.Current = b.Edges.CurrentVersion.Version
		}
		for _, v := range b.Edges.Versions {
			ids[v.ID] = fmt.Sprintf("bv_%s_v%d", b.Slug, v.Version)
			lb.Versions = append(lb.Versions, lite.BlockVersion{ID: v.ID, Version: v.Version, Payload: v.Payload,
				Changelog: v.Changelog, AuthoredBy: string(v.AuthoredBy), CreatedAt: v.CreatedAt.UTC()})
		}
		docs = append(docs, doc{lite.KindBlock, b.ID, "", lb})
	}

	bps, err := c.Blueprint.Query().WithGoal().
		WithChecks(func(q *ent.CheckQuery) { q.Order(ent.Asc(check.FieldPosition)) }).
		WithSlots(func(q *ent.SlotQuery) {
			q.Order(ent.Asc(slot.FieldPosition)).WithLink(func(l *ent.BlockLinkQuery) { l.WithBlock().WithBaseVersion() })
		}).All(ctx)
	if err != nil {
		return nil, err
	}
	for _, k := range bps {
		ids[k.ID] = "bp_" + k.Slug
		bp := lite.Blueprint{ID: k.ID, TeamID: k.TeamID, Slug: k.Slug, DisplayName: k.DisplayName, Description: k.Description,
			Kind: string(k.Kind), Slots: []lite.Slot{}, Checks: []lite.Check{}, CreatedAt: k.CreatedAt.UTC(), UpdatedAt: k.UpdatedAt.UTC()}
		if k.Edges.Goal != nil {
			bp.Goal = k.Edges.Goal.Statement
		}
		for i, ch := range k.Edges.Checks {
			ids[ch.ID] = fmt.Sprintf("chk_%s_%d", k.Slug, i+1)
			u, _ := ch.Spec["url"].(string)
			m, _ := ch.Spec["method"].(string)
			bp.Checks = append(bp.Checks, lite.Check{ID: ch.ID, Label: ch.Label, Type: string(ch.Type), URL: u, Method: m, Expect: string(ch.Expect)})
		}
		for _, sl := range k.Edges.Slots {
			ls := lite.Slot{Key: sl.Key, Label: sl.Label, Section: sl.Section, BlockType: sl.BlockType, Required: sl.Required, Position: sl.Position}
			if l := sl.Edges.Link; l != nil {
				ls.BlockID, ls.VersionID, ls.Overrides = l.BlockID, l.BaseVersionID, l.Overrides
			}
			bp.Slots = append(bp.Slots, ls)
		}
		revs, err := c.BlueprintRevision.Query().Where(blueprintrevision.BlueprintID(k.ID)).
			Order(ent.Asc(blueprintrevision.FieldSeq)).All(ctx)
		if err != nil {
			return nil, err
		}
		for _, r := range revs {
			ids[r.ID] = fmt.Sprintf("rev_%s_%d", k.Slug, r.Seq)
			var snap lite.Snapshot
			raw, _ := json.Marshal(r.Snapshot)
			_ = json.Unmarshal(raw, &snap)
			docs = append(docs, doc{lite.KindRevision, r.ID, k.ID, lite.Revision{ID: r.ID, BlueprintID: k.ID, Seq: r.Seq,
				Action: r.Action, SlotKey: r.SlotKey, Summary: r.Summary, Snapshot: snap, YAML: r.Yaml, CreatedAt: r.CreatedAt.UTC()}})
			bp.RevSeq = r.Seq
		}
		docs = append(docs, doc{lite.KindBlueprint, k.ID, "", bp})
	}

	// Swap every ID for its stable one: they are whole JSON strings, so a
	// quoted replace can't touch anything else.
	pairs := make([]string, 0, 2*len(ids))
	for old, stable := range ids {
		pairs = append(pairs, `"`+old+`"`, `"`+stable+`"`)
	}
	swap := strings.NewReplacer(pairs...)
	for _, d := range docs {
		dd := d.(doc)
		body, err := json.Marshal(dd.body)
		if err != nil {
			return nil, err
		}
		st.Docs = append(st.Docs, lite.Doc{Kind: dd.kind, ID: ids[dd.id], Parent: ids[dd.parent], Body: json.RawMessage(swap.Replace(string(body)))})
	}
	sort.Slice(st.Docs, func(i, j int) bool {
		if st.Docs[i].Kind != st.Docs[j].Kind {
			return st.Docs[i].Kind < st.Docs[j].Kind
		}
		return st.Docs[i].ID < st.Docs[j].ID
	})
	return st, nil
}

type doc struct {
	kind, id, parent string
	body             any
}
