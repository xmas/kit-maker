package schema

import (
	"entgo.io/ent"
	"entgo.io/ent/schema/edge"
	"entgo.io/ent/schema/field"
	"entgo.io/ent/schema/index"
)

// BlueprintInclude pulls another blueprint into a parent at a channel or a
// pinned version (DESIGN.md §2.5).
type BlueprintInclude struct {
	ent.Schema
}

func (BlueprintInclude) Mixin() []ent.Mixin {
	return []ent.Mixin{IDMixin{}, TimeMixin{}}
}

func (BlueprintInclude) Fields() []ent.Field {
	return []ent.Field{
		field.String("blueprint_id"),
		field.String("child_blueprint_id"),
		field.Enum("track").Values("stable", "latest", "pinned").Default("stable"),
		field.String("pinned_version_id").Optional().Nillable().
			Comment("FK to blueprint_versions once that table exists"),
		field.Int("position").Default(0),
	}
}

func (BlueprintInclude) Edges() []ent.Edge {
	return []ent.Edge{
		edge.From("blueprint", Blueprint.Type).Ref("includes").Field("blueprint_id").Unique().Required(),
		edge.From("child_blueprint", Blueprint.Type).Ref("included_by").Field("child_blueprint_id").Unique().Required(),
	}
}

func (BlueprintInclude) Indexes() []ent.Index {
	return []ent.Index{
		index.Fields("blueprint_id", "child_blueprint_id").Unique(),
		index.Fields("child_blueprint_id"),
	}
}
