package schema

import (
	"entgo.io/ent"
	"entgo.io/ent/schema/edge"
	"entgo.io/ent/schema/field"

	"github.com/docker/kit-maker/internal/kits/patch"
)

// BlockLink fills a slot with a library block plus local overrides. The kit
// uses base ⊕ overrides, and base_version_id records the version the
// overrides were last rebased onto (DESIGN.md §2.2).
type BlockLink struct {
	ent.Schema
}

func (BlockLink) Mixin() []ent.Mixin {
	return []ent.Mixin{IDMixin{}, TimeMixin{}}
}

func (BlockLink) Fields() []ent.Field {
	return []ent.Field{
		field.String("slot_id").Unique(),
		field.String("block_id"),
		field.Enum("track").Values("latest", "pinned").Default("latest"),
		field.String("pinned_version_id").Optional().Nillable(),
		field.String("base_version_id"),
		field.JSON("overrides", []patch.Op{}).Optional().
			Comment("set|unset|add|remove|replace by item _k"),
		field.String("overrides_hash").Default(""),
		field.Enum("rebase_state").Values("clean", "update_available", "conflict").Default("clean"),
		field.JSON("pending_rebase", map[string]any{}).Optional(),
	}
}

func (BlockLink) Edges() []ent.Edge {
	return []ent.Edge{
		edge.From("slot", Slot.Type).Ref("link").Field("slot_id").Unique().Required(),
		edge.From("block", Block.Type).Ref("links").Field("block_id").Unique().Required(),
		edge.To("base_version", BlockVersion.Type).Field("base_version_id").Unique().Required(),
		edge.To("pinned_version", BlockVersion.Type).Field("pinned_version_id").Unique(),
	}
}
