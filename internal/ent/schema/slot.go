package schema

import (
	"entgo.io/ent"
	"entgo.io/ent/schema/edge"
	"entgo.io/ent/schema/field"
	"entgo.io/ent/schema/index"
)

// Slot is a place in a kit's outline: blank, suggested, filled or optional
// (DESIGN.md §2). A slot from a sub-kit has origin_kit_id set; a parent slot
// that replaces or patches it has overrides_slot_id set.
type Slot struct {
	ent.Schema
}

func (Slot) Mixin() []ent.Mixin {
	return []ent.Mixin{IDMixin{}, TimeMixin{}}
}

func (Slot) Fields() []ent.Field {
	return []ent.Field{
		field.String("kit_id"),
		field.String("origin_kit_id").Optional().Nillable(),
		field.String("overrides_slot_id").Optional().Nillable(),
		field.String("key").NotEmpty(),
		field.String("label").Default(""),
		field.String("section").NotEmpty(),
		field.Enum("state").Values("blank", "suggested", "filled", "optional").Default("blank"),
		field.Bool("required").Default(false),
		field.Int("position").Default(0),
		field.Bool("optional_capability").Default(false),
		field.String("group_name").Optional().Nillable(),
	}
}

func (Slot) Edges() []ent.Edge {
	return []ent.Edge{
		edge.From("kit", Kit.Type).Ref("slots").Field("kit_id").Unique().Required(),
		edge.From("origin_kit", Kit.Type).Ref("origin_slots").Field("origin_kit_id").Unique(),
		edge.To("overridden_by", Slot.Type).Unique().
			From("overrides").Field("overrides_slot_id").Unique(),
		edge.To("link", BlockLink.Type).Unique(),
	}
}

func (Slot) Indexes() []ent.Index {
	return []ent.Index{
		index.Fields("kit_id", "key").Unique(),
	}
}
