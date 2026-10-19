package schema

import (
	"entgo.io/ent"
	"entgo.io/ent/dialect"
	"entgo.io/ent/schema/edge"
	"entgo.io/ent/schema/field"

	"entgo.io/ent/schema/index"
	"github.com/docker/kit-maker/internal/kits/patch"
	"github.com/pgvector/pgvector-go"
)

// Block is a reusable library block. Its content lives in immutable
// BlockVersions. A fork is a block with a parent: it keeps inheriting the
// parent's updates through parent_overrides (DESIGN.md §2.2).
type Block struct {
	ent.Schema
}

func (Block) Mixin() []ent.Mixin {
	return []ent.Mixin{IDMixin{}, TimeMixin{}}
}

func (Block) Fields() []ent.Field {
	return []ent.Field{
		field.String("team_id"),
		field.String("type").NotEmpty().Comment("network-rules|credential|install-hook|dockerfile|agent-context|..."),
		field.String("slug").NotEmpty(),
		field.String("name").NotEmpty(),
		field.Text("description").Default(""),
		field.Strings("tags").Optional(),
		field.Enum("visibility").Values("private", "team", "public").Default("team"),
		field.String("parent_block_id").Optional().Nillable(),
		field.Enum("parent_track").Values("latest", "pinned").Optional().Nillable(),
		field.String("parent_pinned_version_id").Optional().Nillable(),
		field.JSON("parent_overrides", []patch.Op{}).Optional().
			Comment("patch ops on the parent, keyed by item _k"),
		field.String("current_version_id").Optional().Nillable(),
		field.Other("embedding", &pgvector.Vector{}).
			SchemaType(map[string]string{dialect.Postgres: "vector(1024)"}).
			Optional().Nillable().
			Comment("semantic search (phase 3); dimension follows the embedding model"),
	}
}

func (Block) Edges() []ent.Edge {
	return []ent.Edge{
		edge.From("team", Team.Type).Ref("blocks").Field("team_id").Unique().Required(),
		edge.To("forks", Block.Type).
			From("parent").Field("parent_block_id").Unique(),
		edge.To("versions", BlockVersion.Type),
		edge.To("parent_pinned_version", BlockVersion.Type).Field("parent_pinned_version_id").Unique(),
		edge.To("current_version", BlockVersion.Type).Field("current_version_id").Unique(),
		edge.To("links", BlockLink.Type),
	}
}

func (Block) Indexes() []ent.Index {
	return []ent.Index{
		index.Fields("team_id", "slug").Unique(),
		index.Fields("type"),
	}
}
