package schema

import (
	"time"

	"entgo.io/ent"
	"entgo.io/ent/schema/field"
)

// DeploymentSetting is one deployment-wide setting (branding, policies,
// defaults). Values are sealed like integration configs, so a secret can live
// here too.
type DeploymentSetting struct {
	ent.Schema
}

func (DeploymentSetting) Fields() []ent.Field {
	return []ent.Field{
		field.String("id").StorageKey("key").Immutable().NotEmpty(),
		field.Bytes("value_sealed").Sensitive(),
		field.Time("updated_at").Default(time.Now).UpdateDefault(time.Now),
	}
}
