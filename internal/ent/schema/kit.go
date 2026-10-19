package schema

import (
	"entgo.io/ent"
	"entgo.io/ent/schema/edge"
	"entgo.io/ent/schema/field"
	"entgo.io/ent/schema/index"
)

// Kit is a cloud kit being built: an outline of slots, plus included sub-kits.
// IDs are shown to users as kit_<ULID>.
//
// First slice: collection_id and registry_integration_id arrive with their
// tables.
type Kit struct {
	ent.Schema
}

func (Kit) Mixin() []ent.Mixin {
	return []ent.Mixin{IDMixin{Prefix: "kit_"}, TimeMixin{}}
}

func (Kit) Fields() []ent.Field {
	return []ent.Field{
		field.String("team_id"),
		field.String("slug").NotEmpty(),
		field.String("display_name").NotEmpty(),
		field.Text("description").Default(""),
		field.Enum("kind").Values("workload", "mixin", "set", "environment"),
		field.String("image_repository").Optional().Nillable().Comment("e.g. acme/sbx-kit-acme-shop"),
		field.String("network_preset").Optional().Nillable(),
		field.Bool("registration_enabled").Default(false),
	}
}

func (Kit) Edges() []ent.Edge {
	return []ent.Edge{
		edge.From("team", Team.Type).Ref("kits").Field("team_id").Unique().Required(),
		edge.To("slots", Slot.Type),
		edge.To("includes", KitInclude.Type),
		edge.To("included_by", KitInclude.Type),
		edge.To("origin_slots", Slot.Type),
		edge.To("goal", Goal.Type).Unique(),
		edge.To("checks", Check.Type),
	}
}

func (Kit) Indexes() []ent.Index {
	return []ent.Index{
		index.Fields("team_id", "slug").Unique(),
	}
}
