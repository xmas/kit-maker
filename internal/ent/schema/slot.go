package schema

import (
	"entgo.io/ent"
	"entgo.io/ent/schema/edge"
	"entgo.io/ent/schema/field"
	"entgo.io/ent/schema/index"
)

// Slot is a place in a blueprint's outline: blank, suggested, filled or
// optional (DESIGN.md §2). A slot from an included blueprint has
// origin_blueprint_id set; a parent slot
// that replaces or patches it has overrides_slot_id set.
type Slot struct {
	ent.Schema
}

func (Slot) Mixin() []ent.Mixin {
	return []ent.Mixin{IDMixin{}, TimeMixin{}}
}

func (Slot) Fields() []ent.Field {
	return []ent.Field{
		field.String("blueprint_id"),
		field.String("origin_blueprint_id").Optional().Nillable(),
		field.String("overrides_slot_id").Optional().Nillable(),
		field.String("key").NotEmpty(),
		field.String("label").Default(""),
		field.String("section").NotEmpty(),
		field.String("block_type").Default("").Comment("the block type that fills it (blocktypes key); blank slots show and start this type"),
		field.Enum("state").Values("blank", "suggested", "filled", "optional").Default("blank"),
		field.Bool("required").Default(false),
		field.Int("position").Default(0),
		field.Bool("optional_capability").Default(false),
		field.String("group_name").Optional().Nillable(),
	}
}

func (Slot) Edges() []ent.Edge {
	return []ent.Edge{
		edge.From("blueprint", Blueprint.Type).Ref("slots").Field("blueprint_id").Unique().Required(),
		edge.From("origin_blueprint", Blueprint.Type).Ref("origin_slots").Field("origin_blueprint_id").Unique(),
		edge.To("overridden_by", Slot.Type).Unique().
			From("overrides").Field("overrides_slot_id").Unique(),
		edge.To("link", BlockLink.Type).Unique(),
	}
}

func (Slot) Indexes() []ent.Index {
	return []ent.Index{
		index.Fields("blueprint_id", "key").Unique(),
	}
}
