package schema

import (
	"entgo.io/ent"
	"entgo.io/ent/schema/edge"
	"entgo.io/ent/schema/field"
	"entgo.io/ent/schema/index"
)

// KitInclude pulls a sub-kit into a parent kit at a channel or a pinned rev
// (DESIGN.md §2.5).
type KitInclude struct {
	ent.Schema
}

func (KitInclude) Mixin() []ent.Mixin {
	return []ent.Mixin{IDMixin{}, TimeMixin{}}
}

func (KitInclude) Fields() []ent.Field {
	return []ent.Field{
		field.String("kit_id"),
		field.String("child_kit_id"),
		field.Enum("track").Values("stable", "latest", "pinned").Default("stable"),
		field.String("pinned_blueprint_id").Optional().Nillable().
			Comment("FK to blueprints once that table exists"),
		field.Int("position").Default(0),
	}
}

func (KitInclude) Edges() []ent.Edge {
	return []ent.Edge{
		edge.From("kit", Kit.Type).Ref("includes").Field("kit_id").Unique().Required(),
		edge.From("child_kit", Kit.Type).Ref("included_by").Field("child_kit_id").Unique().Required(),
	}
}

func (KitInclude) Indexes() []ent.Index {
	return []ent.Index{
		index.Fields("kit_id", "child_kit_id").Unique(),
		index.Fields("child_kit_id"),
	}
}
