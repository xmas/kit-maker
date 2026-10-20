package schema

import (
	"entgo.io/ent"
	"entgo.io/ent/schema/edge"
	"entgo.io/ent/schema/field"
	"entgo.io/ent/schema/index"
)

// Blueprint is the design for a Docker Sandboxes kit: an outline of slots,
// plus included blueprints (DESIGN.md §2). IDs are shown to users as
// bp_<ULID>.
//
// First slice: collection_id and registry_integration_id arrive with their
// tables.
type Blueprint struct {
	ent.Schema
}

func (Blueprint) Mixin() []ent.Mixin {
	return []ent.Mixin{IDMixin{Prefix: "bp_"}, TimeMixin{}}
}

func (Blueprint) Fields() []ent.Field {
	return []ent.Field{
		field.String("team_id"),
		field.String("slug").NotEmpty(),
		field.String("display_name").NotEmpty(),
		field.Text("description").Default(""),
		field.Enum("kind").Values("workload", "mixin", "set", "environment"), // the spec's kit kind
		field.String("image_repository").Optional().Nillable().Comment("e.g. acme/sbx-kit-acme-shop"),
		field.String("network_preset").Optional().Nillable(),
		field.Bool("registration_enabled").Default(false),
	}
}

func (Blueprint) Edges() []ent.Edge {
	return []ent.Edge{
		edge.From("team", Team.Type).Ref("blueprints").Field("team_id").Unique().Required(),
		edge.To("slots", Slot.Type),
		edge.To("includes", BlueprintInclude.Type),
		edge.To("included_by", BlueprintInclude.Type),
		edge.To("origin_slots", Slot.Type),
		edge.To("goal", Goal.Type).Unique(),
		edge.To("checks", Check.Type),
		edge.To("revisions", BlueprintRevision.Type),
		edge.To("versions", BlueprintVersion.Type),
	}
}

func (Blueprint) Indexes() []ent.Index {
	return []ent.Index{
		index.Fields("team_id", "slug").Unique(),
	}
}
