package schema

import (
	"entgo.io/ent"
	"entgo.io/ent/schema/field"
	"entgo.io/ent/schema/index"
)

// AICall records one model call: who, where, which route, and the tokens it
// used. Budgets sum these rows (DESIGN.md §3 cost guard).
type AICall struct {
	ent.Schema
}

func (AICall) Mixin() []ent.Mixin {
	return []ent.Mixin{IDMixin{}, CreatedMixin{}}
}

func (AICall) Fields() []ent.Field {
	return []ent.Field{
		field.String("team_id").Optional().Nillable(),
		field.String("user_id").Optional().Nillable(),
		field.String("blueprint_id").Optional().Nillable(),
		field.String("route"),
		field.String("provider").Comment("integration driver: fake, anthropic, bedrock, vertex"),
		field.String("model").Optional().Comment("model that answered (may be a fallback)"),
		field.Int64("input_tokens").Default(0),
		field.Int64("output_tokens").Default(0),
		field.Int64("cache_read_tokens").Default(0),
		field.Int64("cache_write_tokens").Default(0),
		field.Enum("status").Values("ok", "refused", "error", "over_budget"),
		field.Text("error").Optional(),
		field.Int64("duration_ms").Default(0),
	}
}

func (AICall) Indexes() []ent.Index {
	return []ent.Index{
		index.Fields("team_id", "created_at"),
	}
}
