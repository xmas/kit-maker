package schema

import (
	"entgo.io/ent"
	"entgo.io/ent/schema/edge"
	"entgo.io/ent/schema/field"
	"entgo.io/ent/schema/index"
)

// TeamMembership is one user's membership of one team (docs/database.md
// team_memberships; DESIGN.md §6 calls it team_user). A user can hold several
// rows per team once membership sources land, so (team, user) is not unique.
//
// First slice: role is a plain key until the roles table exists (then
// role_id), and source_id arrives with team_member_sources.
type TeamMembership struct {
	ent.Schema
}

func (TeamMembership) Mixin() []ent.Mixin {
	return []ent.Mixin{IDMixin{}, TimeMixin{}}
}

func (TeamMembership) Fields() []ent.Field {
	return []ent.Field{
		field.String("team_id"),
		field.String("user_id"),
		field.String("role").NotEmpty().Comment("team-scope role key, e.g. maintainer"),
		field.Bool("pinned").Default(false).Comment("manual add over external sources"),
		// Go name differs so it doesn't clash with the upsert builder's SetExcluded.
		field.Bool("is_excluded").StorageKey("excluded").Default(false).
			Comment("local deny, wins over every source"),
		field.Time("expires_at").Optional().Nillable().Comment("TTL for callout/claim rows"),
	}
}

func (TeamMembership) Edges() []ent.Edge {
	return []ent.Edge{
		edge.From("team", Team.Type).Ref("memberships").Field("team_id").Unique().Required(),
		edge.From("user", User.Type).Ref("memberships").Field("user_id").Unique().Required(),
	}
}

func (TeamMembership) Indexes() []ent.Index {
	return []ent.Index{
		index.Fields("team_id", "user_id"),
		index.Fields("user_id"),
	}
}
