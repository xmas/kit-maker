package schema

import (
	"entgo.io/ent"
	"entgo.io/ent/schema/edge"
	"entgo.io/ent/schema/field"
	"entgo.io/ent/schema/index"
)

// BlueprintRevision is a point in a blueprint's history: a full snapshot
// (goal, slots, links and their edits) taken after each change, plus the
// YAML it compiled to. Any revision can be restored, for the whole
// blueprint or one slot.
type BlueprintRevision struct {
	ent.Schema
}

func (BlueprintRevision) Mixin() []ent.Mixin {
	return []ent.Mixin{IDMixin{}, CreatedMixin{}}
}

func (BlueprintRevision) Fields() []ent.Field {
	return []ent.Field{
		field.String("blueprint_id"),
		field.Int("seq").Positive().Comment("1, 2, 3… per blueprint"),
		field.String("actor_user_id").Optional().Nillable(),
		field.String("action").Comment("baseline, create, edit, link, relink, unlink, add-slot, remove-slot, update, resync, revert, goal, save-as-block, custom-block, restore"),
		field.String("slot_key").Optional().Comment("the slot the change was about, if any"),
		field.Text("summary"),
		field.JSON("snapshot", map[string]any{}),
		field.Text("yaml").Default(""),
	}
}

func (BlueprintRevision) Edges() []ent.Edge {
	return []ent.Edge{
		edge.From("blueprint", Blueprint.Type).Ref("revisions").Field("blueprint_id").Unique().Required(),
		edge.From("actor", User.Type).Ref("blueprint_revisions").Field("actor_user_id").Unique(),
	}
}

func (BlueprintRevision) Indexes() []ent.Index {
	return []ent.Index{
		index.Fields("blueprint_id", "seq").Unique(),
		index.Fields("blueprint_id", "slot_key"),
	}
}
