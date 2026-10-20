package schema

import (
	"entgo.io/ent"
	"entgo.io/ent/schema/edge"
	"entgo.io/ent/schema/field"
	"entgo.io/ent/schema/index"
)

// BlueprintVersion is one published version of a blueprint: the kit image
// pushed to the registry (tags vN and latest) and the exact YAML it was
// built from (DESIGN.md §1, §5). Immutable.
type BlueprintVersion struct {
	ent.Schema
}

func (BlueprintVersion) Mixin() []ent.Mixin {
	return []ent.Mixin{IDMixin{}, CreatedMixin{}}
}

func (BlueprintVersion) Fields() []ent.Field {
	return []ent.Field{
		field.String("blueprint_id"),
		field.Int("version").Positive().Comment("1, 2, 3… per blueprint; the image tag is v<version>"),
		field.String("repository").Comment("the image name as people pull it, e.g. localhost:5001/blueprints/sbx-kit-acme-shop"),
		field.String("digest").Comment("manifest digest, sha256:…"),
		field.Text("yaml").Comment("the descriptor the image was built from"),
		field.String("published_by").Optional().Comment("user id"),
	}
}

func (BlueprintVersion) Edges() []ent.Edge {
	return []ent.Edge{
		edge.From("blueprint", Blueprint.Type).Ref("versions").Field("blueprint_id").Unique().Required(),
	}
}

func (BlueprintVersion) Indexes() []ent.Index {
	return []ent.Index{index.Fields("blueprint_id", "version").Unique()}
}
