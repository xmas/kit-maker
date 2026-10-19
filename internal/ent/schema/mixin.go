package schema

import (
	"crypto/rand"
	"time"

	"entgo.io/ent"
	"entgo.io/ent/schema/field"
	"entgo.io/ent/schema/mixin"
	"github.com/oklog/ulid/v2"
)

// NewULID returns a new ULID string, optionally prefixed (kit_, bp_, sbx_).
// Prefixed IDs are the ones shown to users (docs/database.md).
func NewULID(prefix string) string {
	return prefix + ulid.MustNew(ulid.Timestamp(time.Now()), rand.Reader).String()
}

// IDMixin gives a schema a text primary key holding a (prefixed) ULID.
type IDMixin struct {
	mixin.Schema
	Prefix string
}

func (m IDMixin) Fields() []ent.Field {
	return []ent.Field{
		field.String("id").
			Immutable().
			NotEmpty().
			DefaultFunc(func() string { return NewULID(m.Prefix) }),
	}
}

// TimeMixin adds created_at and updated_at.
type TimeMixin struct {
	mixin.Schema
}

func (TimeMixin) Fields() []ent.Field {
	return []ent.Field{
		field.Time("created_at").Immutable().Default(time.Now),
		field.Time("updated_at").Default(time.Now).UpdateDefault(time.Now),
	}
}

// CreatedMixin adds created_at only, for immutable rows.
type CreatedMixin struct {
	mixin.Schema
}

func (CreatedMixin) Fields() []ent.Field {
	return []ent.Field{
		field.Time("created_at").Immutable().Default(time.Now),
	}
}
