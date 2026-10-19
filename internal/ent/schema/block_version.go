package schema

import (
	"entgo.io/ent"
	"entgo.io/ent/schema/edge"
	"entgo.io/ent/schema/field"
	"entgo.io/ent/schema/index"
)

// BlockVersion is one immutable version of a block's payload. Every field is
// immutable, so Ent generates no setters to change a version after insert.
type BlockVersion struct {
	ent.Schema
}

func (BlockVersion) Mixin() []ent.Mixin {
	return []ent.Mixin{IDMixin{}, CreatedMixin{}}
}

func (BlockVersion) Fields() []ent.Field {
	return []ent.Field{
		field.String("block_id").Immutable(),
		field.Int("version").Immutable().Positive(),
		field.JSON("payload", map[string]any{}).Immutable().
			Comment("list items carry stable _k keys"),
		field.String("payload_hash").Immutable().NotEmpty(),
		field.String("schema_version").Immutable().NotEmpty().Comment("vendored spec commit or tag"),
		field.Text("changelog").Immutable().Default(""),
		field.Enum("authored_by").Values("user", "ai").Immutable(),
		field.String("accepted_by_user_id").Immutable().Optional().Nillable(),
		field.String("ai_suggestion_id").Immutable().Optional().Nillable().
			Comment("FK to ai_suggestions once that table exists"),
	}
}

func (BlockVersion) Edges() []ent.Edge {
	return []ent.Edge{
		edge.From("block", Block.Type).Ref("versions").Field("block_id").Unique().Required().Immutable(),
		edge.From("accepted_by", User.Type).Ref("accepted_block_versions").
			Field("accepted_by_user_id").Unique().Immutable(),
		edge.From("current_of", Block.Type).Ref("current_version"),
		edge.From("pinned_by_forks", Block.Type).Ref("parent_pinned_version"),
		edge.From("base_of_links", BlockLink.Type).Ref("base_version"),
		edge.From("pinned_by_links", BlockLink.Type).Ref("pinned_version"),
	}
}

func (BlockVersion) Indexes() []ent.Index {
	return []ent.Index{
		index.Fields("block_id", "version").Unique(),
	}
}
