package schema

import (
	"entgo.io/ent"
	"entgo.io/ent/schema/edge"
	"entgo.io/ent/schema/field"
)

// Team is the unit of ownership: every blueprint, block and integration
// has an owner team. Teams nest, with view-only inheritance (DESIGN.md §13).
type Team struct {
	ent.Schema
}

func (Team) Mixin() []ent.Mixin {
	return []ent.Mixin{IDMixin{}, TimeMixin{}}
}

func (Team) Fields() []ent.Field {
	return []ent.Field{
		field.String("parent_team_id").Optional().Nillable(),
		field.String("name").NotEmpty(),
		field.String("slug").NotEmpty().Unique(),
	}
}

func (Team) Edges() []ent.Edge {
	return []ent.Edge{
		edge.To("children", Team.Type).
			From("parent").Field("parent_team_id").Unique(),
		edge.To("memberships", TeamMembership.Type),
		edge.To("blocks", Block.Type),
		edge.To("blueprints", Blueprint.Type),
	}
}
