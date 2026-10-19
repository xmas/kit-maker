package schema

import (
	"entgo.io/ent"
	"entgo.io/ent/schema/field"
	"entgo.io/ent/schema/index"
)

// RoleAssignment grants a deployment-scope role to a user or to every member
// of a team (e.g. Security → Auditor). Team-scope roles live on
// team_memberships instead (DESIGN.md §14).
type RoleAssignment struct {
	ent.Schema
}

func (RoleAssignment) Mixin() []ent.Mixin {
	return []ent.Mixin{IDMixin{}, TimeMixin{}}
}

func (RoleAssignment) Fields() []ent.Field {
	return []ent.Field{
		field.String("role_key").NotEmpty().Comment("built-in or custom deployment role key"),
		field.Enum("principal_type").Values("user", "team"),
		field.String("principal_id").NotEmpty(),
	}
}

func (RoleAssignment) Indexes() []ent.Index {
	return []ent.Index{
		index.Fields("principal_type", "principal_id", "role_key").Unique(),
	}
}
