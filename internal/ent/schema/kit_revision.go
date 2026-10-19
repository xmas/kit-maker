package schema

import (
	"entgo.io/ent"
	"entgo.io/ent/schema/edge"
	"entgo.io/ent/schema/field"
	"entgo.io/ent/schema/index"
)

// KitRevision is a point in a kit's history: a full snapshot of the kit
// (goal, slots, links and their edits) taken after each change, plus the
// YAML it compiled to. Any revision can be restored, for the whole kit or
// one slot.
type KitRevision struct {
	ent.Schema
}

func (KitRevision) Mixin() []ent.Mixin {
	return []ent.Mixin{IDMixin{}, CreatedMixin{}}
}

func (KitRevision) Fields() []ent.Field {
	return []ent.Field{
		field.String("kit_id"),
		field.Int("seq").Positive().Comment("1, 2, 3… per kit"),
		field.String("actor_user_id").Optional().Nillable(),
		field.String("action").Comment("baseline, create, edit, link, relink, unlink, add-slot, remove-slot, update, resync, revert, goal, save-as-block, custom-block, restore"),
		field.String("slot_key").Optional().Comment("the slot the change was about, if any"),
		field.Text("summary"),
		field.JSON("snapshot", map[string]any{}),
		field.Text("yaml").Default(""),
	}
}

func (KitRevision) Edges() []ent.Edge {
	return []ent.Edge{
		edge.From("kit", Kit.Type).Ref("revisions").Field("kit_id").Unique().Required(),
		edge.From("actor", User.Type).Ref("kit_revisions").Field("actor_user_id").Unique(),
	}
}

func (KitRevision) Indexes() []ent.Index {
	return []ent.Index{
		index.Fields("kit_id", "seq").Unique(),
		index.Fields("kit_id", "slot_key"),
	}
}
