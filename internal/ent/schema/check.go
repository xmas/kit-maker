package schema

import (
	"entgo.io/ent"
	"entgo.io/ent/schema/edge"
	"entgo.io/ent/schema/field"
	"entgo.io/ent/schema/index"
)

// Goal is a blueprint's purpose in plain language; checks are derived from it.
type Goal struct {
	ent.Schema
}

func (Goal) Mixin() []ent.Mixin {
	return []ent.Mixin{IDMixin{}, TimeMixin{}}
}

func (Goal) Fields() []ent.Field {
	return []ent.Field{
		field.String("blueprint_id").Unique(),
		field.Text("statement").NotEmpty(),
		field.JSON("sources", map[string]any{}).Optional().Comment("repo, kit yaml, dockerfile, sbx-scan"),
	}
}

func (Goal) Edges() []ent.Edge {
	return []ent.Edge{
		edge.From("blueprint", Blueprint.Type).Ref("goal").Field("blueprint_id").Unique().Required(),
	}
}

// Check is one testable expectation of a blueprint (DESIGN.md §2).
type Check struct {
	ent.Schema
}

func (Check) Mixin() []ent.Mixin {
	return []ent.Mixin{IDMixin{}, TimeMixin{}}
}

func (Check) Fields() []ent.Field {
	return []ent.Field{
		field.String("blueprint_id"),
		field.String("label").NotEmpty(),
		field.Enum("type").Values("network", "credential", "tool", "setup", "agent", "manual"),
		field.JSON("spec", map[string]any{}).Comment("network: {url, method}"),
		field.Enum("expect").Values("reach", "blocked", "runs", "present"),
		field.Int("position").Default(0),
		field.Enum("authored_by").Values("user", "ai").Default("user"),
	}
}

func (Check) Edges() []ent.Edge {
	return []ent.Edge{
		edge.From("blueprint", Blueprint.Type).Ref("checks").Field("blueprint_id").Unique().Required(),
	}
}

func (Check) Indexes() []ent.Index {
	return []ent.Index{index.Fields("blueprint_id", "position")}
}
