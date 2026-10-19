package schema

import (
	"entgo.io/ent"
	"entgo.io/ent/schema/edge"
	"entgo.io/ent/schema/field"
)

// User is a person or a service account. Identities (local, OIDC, SAML, LDAP)
// link to one user; they arrive with auth in a later slice.
type User struct {
	ent.Schema
}

func (User) Mixin() []ent.Mixin {
	return []ent.Mixin{IDMixin{}, TimeMixin{}}
}

func (User) Fields() []ent.Field {
	return []ent.Field{
		field.String("email").NotEmpty().Unique(),
		field.String("display_name").Default(""),
		field.String("password_hash").Optional().Nillable().Sensitive().
			Comment("argon2id, local accounts only"),
		field.Enum("status").Values("active", "suspended", "deprovisioned").Default("active"),
		field.Bool("is_service_account").Default(false),
		field.Time("last_login_at").Optional().Nillable(),
		field.Int("failed_logins").Default(0).Comment("consecutive failed password sign-ins"),
		field.Time("locked_until").Optional().Nillable().Comment("password sign-in locked until"),
		field.Int("session_version").Default(1).
			Comment("bumped to sign the user out everywhere; sessions carry the version they started with"),
	}
}

func (User) Edges() []ent.Edge {
	return []ent.Edge{
		edge.To("memberships", TeamMembership.Type),
		edge.To("accepted_block_versions", BlockVersion.Type),
		edge.To("kit_revisions", KitRevision.Type),
	}
}
