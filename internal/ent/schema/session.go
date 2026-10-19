package schema

import (
	"entgo.io/ent"
	"entgo.io/ent/dialect/entsql"
	"entgo.io/ent/schema"
	"entgo.io/ent/schema/field"
	"entgo.io/ent/schema/index"
)

// Session is the scs session store table (alexedwards/scs/pgxstore). It is
// declared here so Atlas manages it with everything else; the app reads and
// writes it only through scs.
type Session struct {
	ent.Schema
}

func (Session) Annotations() []schema.Annotation {
	return []schema.Annotation{entsql.Annotation{Table: "sessions"}}
}

func (Session) Fields() []ent.Field {
	return []ent.Field{
		field.String("id").StorageKey("token").Immutable().NotEmpty(),
		field.Bytes("data"),
		field.Time("expiry"),
	}
}

func (Session) Indexes() []ent.Index {
	return []ent.Index{
		index.Fields("expiry").StorageKey("sessions_expiry_idx"),
	}
}
