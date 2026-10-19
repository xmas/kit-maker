package schema

import (
	"entgo.io/ent"
	"entgo.io/ent/schema/field"
	"entgo.io/ent/schema/index"
)

// Integration is one per-deployment driver configuration: a registry, a
// build runner, a git host or an AI provider (DESIGN.md §1.2). Config is
// sealed with the deployment's secrets keeper before it is stored.
type Integration struct {
	ent.Schema
}

func (Integration) Mixin() []ent.Mixin {
	return []ent.Mixin{IDMixin{}, TimeMixin{}}
}

func (Integration) Fields() []ent.Field {
	return []ent.Field{
		field.Enum("kind").Values("registry", "build_runner", "git_host", "ai"),
		field.String("driver").NotEmpty().Comment("e.g. zot, dockerhub, buildkit, anthropic, fake"),
		field.String("name").NotEmpty().Comment("what admins call it"),
		field.Bytes("config_sealed").Sensitive().Comment("JSON config sealed by internal/secrets"),
		field.Bool("is_default").Default(false),
		field.Time("verified_at").Optional().Nillable(),
		field.Text("last_error").Default("").Comment("last test failure, empty when it passed"),
	}
}

func (Integration) Indexes() []ent.Index {
	return []ent.Index{
		index.Fields("kind", "name").Unique(),
	}
}
