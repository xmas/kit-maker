package blueprints

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/docker/kit-maker/internal/auth"
	"github.com/docker/kit-maker/internal/blueprints/blocktypes"
	"github.com/docker/kit-maker/internal/blueprints/patch"
	"github.com/docker/kit-maker/internal/ent"
	"github.com/docker/kit-maker/internal/ent/blocklink"
	"github.com/docker/kit-maker/internal/ent/blockversion"
	"github.com/docker/kit-maker/internal/ent/blueprint"
	"github.com/docker/kit-maker/internal/ent/blueprintrevision"
	"github.com/docker/kit-maker/internal/ent/slot"
)

// Every change to a blueprint records a revision: a full snapshot of the blueprint after
// the change, plus the YAML it compiled to. The first change also records the
// state before it ("starting point"), so even a blueprint made before history
// existed can go back to how it was. Restoring a revision is itself a change.

// Snapshot is a blueprint's restorable state.
type Snapshot struct {
	Goal  string     `json:"goal"`
	Slots []SnapSlot `json:"slots"`
}

// SnapSlot is one slot in a snapshot. BlockID is empty for an empty slot.
type SnapSlot struct {
	Key       string     `json:"key"`
	Label     string     `json:"label"`
	Section   string     `json:"section"`
	BlockType string     `json:"blockType,omitempty"`
	Required  bool       `json:"required"`
	Position  int        `json:"position"`
	BlockID   string     `json:"blockId,omitempty"`
	VersionID string     `json:"versionId,omitempty"`
	Overrides []patch.Op `json:"overrides,omitempty"`
	// Shown in history lists.
	BlockName string `json:"blockName,omitempty"`
	Version   int    `json:"version,omitempty"`
}

func (s *Service) snapshot(ctx context.Context, c *ent.Client, blueprintID string) (Snapshot, error) {
	k, err := c.Blueprint.Query().Where(blueprint.ID(blueprintID)).WithGoal().
		WithSlots(func(q *ent.SlotQuery) {
			q.Order(ent.Asc(slot.FieldPosition)).WithLink(func(l *ent.BlockLinkQuery) { l.WithBlock().WithBaseVersion() })
		}).Only(ctx)
	if ent.IsNotFound(err) {
		return Snapshot{}, ErrNotFound
	}
	if err != nil {
		return Snapshot{}, err
	}
	snap := Snapshot{Slots: []SnapSlot{}}
	if k.Edges.Goal != nil {
		snap.Goal = k.Edges.Goal.Statement
	}
	for _, sl := range k.Edges.Slots {
		ss := SnapSlot{Key: sl.Key, Label: sl.Label, Section: sl.Section, BlockType: sl.BlockType, Required: sl.Required, Position: sl.Position}
		if l := sl.Edges.Link; l != nil {
			ss.BlockID, ss.VersionID, ss.Overrides = l.BlockID, l.BaseVersionID, l.Overrides
			if l.Edges.Block != nil {
				ss.BlockName = l.Edges.Block.Name
			}
			if l.Edges.BaseVersion != nil {
				ss.Version = l.Edges.BaseVersion.Version
			}
		}
		snap.Slots = append(snap.Slots, ss)
	}
	return snap, nil
}

func (s *Service) record(ctx context.Context, blueprintID, action, slotKey, summary string) error {
	snap, err := s.snapshot(ctx, s.Ent, blueprintID)
	if err != nil {
		return err
	}
	var yamlText string
	if l, err := s.Load(ctx, blueprintID); err == nil {
		yamlText = l.Compiled.YAML
	}
	var m map[string]any
	b, _ := json.Marshal(snap)
	_ = json.Unmarshal(b, &m)
	last, err := s.Ent.BlueprintRevision.Query().Where(blueprintrevision.BlueprintID(blueprintID)).Order(ent.Desc(blueprintrevision.FieldSeq)).First(ctx)
	seq := 1
	switch {
	case err == nil:
		seq = last.Seq + 1
	case !ent.IsNotFound(err):
		return err
	}
	q := s.Ent.BlueprintRevision.Create().SetBlueprintID(blueprintID).SetSeq(seq).SetAction(action).SetSummary(summary).
		SetSnapshot(m).SetYaml(yamlText)
	if slotKey != "" {
		q.SetSlotKey(slotKey)
	}
	if u := auth.UserFrom(ctx); u != nil {
		q.SetActorUserID(u.ID)
	}
	return q.Exec(ctx)
}

// change runs one blueprint change and records it. summary is worked out after the
// change, so it can name what the change produced.
func (s *Service) change(ctx context.Context, blueprintID, action, slotKey string, fn func() (string, error)) error {
	n, err := s.Ent.BlueprintRevision.Query().Where(blueprintrevision.BlueprintID(blueprintID)).Count(ctx)
	if err != nil {
		return err
	}
	if n == 0 {
		if err := s.record(ctx, blueprintID, "baseline", "", "Starting point"); err != nil {
			return err
		}
	}
	summary, err := fn()
	if err != nil {
		return err
	}
	return s.record(ctx, blueprintID, action, slotKey, summary)
}

// slotName is what the outline calls a slot: its block's name, or its label.
func (s *Service) slotName(ctx context.Context, blueprintID, slotKey string) string {
	sl, err := s.Ent.Slot.Query().Where(slot.BlueprintID(blueprintID), slot.Key(slotKey)).
		WithLink(func(q *ent.BlockLinkQuery) { q.WithBlock().WithBaseVersion() }).Only(ctx)
	if err != nil {
		return slotKey
	}
	if l := sl.Edges.Link; l != nil && l.Edges.Block != nil {
		return l.Edges.Block.Name
	}
	return sl.Label
}

func (s *Service) linkedVersion(ctx context.Context, blueprintID, slotKey string) (string, int) {
	sl, err := s.Ent.Slot.Query().Where(slot.BlueprintID(blueprintID), slot.Key(slotKey)).
		WithLink(func(q *ent.BlockLinkQuery) { q.WithBlock().WithBaseVersion() }).Only(ctx)
	if err != nil || sl.Edges.Link == nil || sl.Edges.Link.Edges.Block == nil || sl.Edges.Link.Edges.BaseVersion == nil {
		return "", 0
	}
	return sl.Edges.Link.Edges.Block.Name, sl.Edges.Link.Edges.BaseVersion.Version
}

func plural(n int, one, many string) string {
	if n == 1 {
		return "1 " + one
	}
	return fmt.Sprintf("%d %s", n, many)
}

// --- Recorded changes -------------------------------------------------------

// Create makes a blueprint with its goal and a starter outline.
func (s *Service) Create(ctx context.Context, in CreateInput) (*ent.Blueprint, error) {
	k, err := s.create(ctx, in)
	if err != nil {
		return nil, err
	}
	return k, s.record(ctx, k.ID, "create", "", "Created the blueprint")
}

// SaveSlot stores an edited payload as overrides on the slot's block.
func (s *Service) SaveSlot(ctx context.Context, blueprintID, slotKey string, edited map[string]any) (*SlotDetail, error) {
	var d *SlotDetail
	err := s.change(ctx, blueprintID, "edit", slotKey, func() (string, error) {
		var err error
		if d, err = s.saveSlot(ctx, blueprintID, slotKey, edited); err != nil {
			return "", err
		}
		if len(d.Ops) == 0 {
			return fmt.Sprintf("Edited %s: back to %s v%d as is", s.slotName(ctx, blueprintID, slotKey), d.BlockName, d.Version), nil
		}
		return fmt.Sprintf("Edited %s (%s from %s v%d)", s.slotName(ctx, blueprintID, slotKey), plural(len(d.Ops), "edit", "edits"), d.BlockName, d.Version), nil
	})
	return d, err
}

// HideEntry resolves a conflict by removing one entry (Key, in the list at
// path, e.g. "/runtime/allow") from a slot's block in this blueprint only:
// an override, so the block stays linked and the entry comes back on revert.
func (s *Service) HideEntry(ctx context.Context, blueprintID, slotKey, path, key string) error {
	return s.change(ctx, blueprintID, "edit", slotKey, func() (string, error) {
		l, err := s.Load(ctx, blueprintID)
		if err != nil {
			return "", err
		}
		d := l.Slots[slotKey]
		if d == nil || d.Effective == nil {
			return "", ErrNotFound
		}
		edited, _ := deepCopyJSON(d.Effective).(map[string]any)
		parent, list := edited, ""
		parts := strings.Split(strings.Trim(path, "/"), "/")
		for i, p := range parts {
			if i == len(parts)-1 {
				list = p
				break
			}
			next, _ := parent[p].(map[string]any)
			if next == nil {
				return "", fmt.Errorf("%s has no %s", d.Label, path)
			}
			parent = next
		}
		items, _ := parent[list].([]any)
		kept, what := items[:0:0], ""
		for _, it := range items {
			if m, _ := it.(map[string]any); m != nil && m[patch.KeyField] == key {
				what = describeEntry(m)
				continue
			}
			kept = append(kept, it)
		}
		if what == "" {
			return "", fmt.Errorf("that entry is no longer in %s", d.Label)
		}
		parent[list] = kept
		if _, err := s.saveSlot(ctx, blueprintID, slotKey, edited); err != nil {
			return "", err
		}
		return fmt.Sprintf("Hid %s in %s to resolve a conflict", what, d.Label), nil
	})
}

func describeEntry(m map[string]any) string {
	if l, _ := m["label"].(string); l != "" {
		return l
	}
	var hosts []string
	for _, h := range asAnySlice(m["hosts"]) {
		if s, ok := h.(string); ok {
			hosts = append(hosts, s)
		}
	}
	if len(hosts) > 0 {
		return strings.Join(hosts, ", ")
	}
	return "an entry"
}

func asAnySlice(v any) []any { l, _ := v.([]any); return l }

func deepCopyJSON(v any) any {
	b, _ := json.Marshal(v)
	var out any
	_ = json.Unmarshal(b, &out)
	return out
}

// RevertSlot drops a slot's saved edits, keeping its block version.
func (s *Service) RevertSlot(ctx context.Context, blueprintID, slotKey string) error {
	return s.change(ctx, blueprintID, "revert", slotKey, func() (string, error) {
		return "Dropped the edits to " + s.slotName(ctx, blueprintID, slotKey), s.revertSlot(ctx, blueprintID, slotKey)
	})
}

// Resync drops a slot's saved edits and moves it to its block's latest
// version: the slot becomes exactly the library block again.
func (s *Service) Resync(ctx context.Context, blueprintID, slotKey string) error {
	return s.change(ctx, blueprintID, "resync", slotKey, func() (string, error) {
		if err := s.revertSlot(ctx, blueprintID, slotKey); err != nil {
			return "", err
		}
		if err := s.updateLink(ctx, blueprintID, slotKey); err != nil {
			return "", err
		}
		name, v := s.linkedVersion(ctx, blueprintID, slotKey)
		return fmt.Sprintf("Resynced to %s v%d", name, v), nil
	})
}

// LinkBlock fills an empty slot with a library block.
func (s *Service) LinkBlock(ctx context.Context, blueprintID, slotKey, blockID string) error {
	return s.change(ctx, blueprintID, "link", slotKey, func() (string, error) {
		if err := s.linkBlock(ctx, blueprintID, slotKey, blockID); err != nil {
			return "", err
		}
		name, v := s.linkedVersion(ctx, blueprintID, slotKey)
		return fmt.Sprintf("Filled a slot with %s v%d", name, v), nil
	})
}

// Relink switches a filled slot to another block.
func (s *Service) Relink(ctx context.Context, blueprintID, slotKey, blockID string) error {
	before := s.slotName(ctx, blueprintID, slotKey)
	return s.change(ctx, blueprintID, "relink", slotKey, func() (string, error) {
		if err := s.relink(ctx, blueprintID, slotKey, blockID); err != nil {
			return "", err
		}
		name, v := s.linkedVersion(ctx, blueprintID, slotKey)
		return fmt.Sprintf("Switched %s to %s v%d", before, name, v), nil
	})
}

// Unlink empties a slot.
func (s *Service) Unlink(ctx context.Context, blueprintID, slotKey string) error {
	before := s.slotName(ctx, blueprintID, slotKey)
	return s.change(ctx, blueprintID, "unlink", slotKey, func() (string, error) {
		return "Emptied the slot that held " + before, s.unlink(ctx, blueprintID, slotKey)
	})
}

// UpdateGoal sets (or clears) the goal statement.
func (s *Service) UpdateGoal(ctx context.Context, blueprintID, statement string) error {
	return s.change(ctx, blueprintID, "goal", "", func() (string, error) {
		return "Changed the goal", s.updateGoal(ctx, blueprintID, statement)
	})
}

// AddSlot adds an empty slot for a block type and returns its key.
func (s *Service) AddSlot(ctx context.Context, blueprintID, label, blockType string, required bool) (string, error) {
	var key string
	err := s.change(ctx, blueprintID, "add-slot", "", func() (string, error) {
		var err error
		key, err = s.addSlot(ctx, blueprintID, label, blockType, required)
		t, _ := blocktypes.Get(blockType)
		return fmt.Sprintf("Added an empty %s slot", t.Label), err
	})
	return key, err
}

// RemoveSlot deletes a slot.
func (s *Service) RemoveSlot(ctx context.Context, blueprintID, slotKey string) error {
	before := s.slotName(ctx, blueprintID, slotKey)
	return s.change(ctx, blueprintID, "remove-slot", slotKey, func() (string, error) {
		return "Removed " + before, s.removeSlot(ctx, blueprintID, slotKey)
	})
}

// StartCustomBlock fills an empty slot with a new block only this blueprint uses.
func (s *Service) StartCustomBlock(ctx context.Context, blueprintID, slotKey, userID string) error {
	return s.change(ctx, blueprintID, "custom-block", slotKey, func() (string, error) {
		if err := s.startCustomBlock(ctx, blueprintID, slotKey, userID); err != nil {
			return "", err
		}
		return "Started a new block, " + s.slotName(ctx, blueprintID, slotKey), nil
	})
}

// UpdateLink moves a slot to its block's latest version, keeping its edits.
func (s *Service) UpdateLink(ctx context.Context, blueprintID, slotKey string) error {
	return s.change(ctx, blueprintID, "update", slotKey, func() (string, error) {
		if err := s.updateLink(ctx, blueprintID, slotKey); err != nil {
			return "", err
		}
		name, v := s.linkedVersion(ctx, blueprintID, slotKey)
		return fmt.Sprintf("Updated %s to v%d", name, v), nil
	})
}

// SaveAsBlock makes a library block from a slot's block plus its edits.
func (s *Service) SaveAsBlock(ctx context.Context, blueprintID, slotKey, name, userID string) (*ent.Block, error) {
	before := s.slotName(ctx, blueprintID, slotKey)
	var b *ent.Block
	err := s.change(ctx, blueprintID, "save-as-block", slotKey, func() (string, error) {
		var err error
		if b, err = s.saveAsBlock(ctx, blueprintID, slotKey, name, userID); err != nil {
			return "", err
		}
		return fmt.Sprintf("Saved %s with its edits as a new block, %s", before, b.Name), nil
	})
	return b, err
}

// --- History and restore ----------------------------------------------------

// Revision is one entry in a blueprint's history.
type Revision struct {
	ID        string    `json:"id"`
	Seq       int       `json:"seq"`
	Action    string    `json:"action"`
	SlotKey   string    `json:"slotKey,omitempty"`
	Summary   string    `json:"summary"`
	Actor     string    `json:"actor,omitempty"`
	CreatedAt time.Time `json:"createdAt"`
	// Slot is the slot's state at this point, when history is for one slot.
	Slot *SnapSlot `json:"slot,omitempty"`
	// Current marks the newest revision (the blueprint as it is now).
	Current bool `json:"current,omitempty"`
}

// History lists a blueprint's revisions, newest first. With slotKey, only the
// revisions that changed that slot (or restored the whole blueprint), each with
// the slot's state at that point.
func (s *Service) History(ctx context.Context, blueprintID, slotKey string) ([]Revision, error) {
	q := s.Ent.BlueprintRevision.Query().Where(blueprintrevision.BlueprintID(blueprintID)).WithActor().Order(ent.Desc(blueprintrevision.FieldSeq))
	if slotKey != "" {
		q = q.Where(blueprintrevision.Or(blueprintrevision.SlotKey(slotKey),
			blueprintrevision.And(blueprintrevision.SlotKeyIsNil(), blueprintrevision.ActionIn("restore", "baseline", "create"))))
	}
	rows, err := q.All(ctx)
	if err != nil {
		return nil, err
	}
	latest, _ := s.Ent.BlueprintRevision.Query().Where(blueprintrevision.BlueprintID(blueprintID)).Aggregate(ent.Max(blueprintrevision.FieldSeq)).Int(ctx)
	out := make([]Revision, 0, len(rows))
	for _, r := range rows {
		rv := Revision{ID: r.ID, Seq: r.Seq, Action: r.Action, SlotKey: r.SlotKey, Summary: r.Summary, CreatedAt: r.CreatedAt, Current: r.Seq == latest}
		if u := r.Edges.Actor; u != nil {
			rv.Actor = u.DisplayName
			if rv.Actor == "" {
				rv.Actor = u.Email
			}
		}
		if slotKey != "" {
			snap := decodeSnapshot(r.Snapshot)
			for i := range snap.Slots {
				if snap.Slots[i].Key == slotKey {
					rv.Slot = &snap.Slots[i]
				}
			}
		}
		out = append(out, rv)
	}
	return out, nil
}

// RevisionYAML returns the YAML the blueprint compiled to at a revision.
func (s *Service) RevisionYAML(ctx context.Context, blueprintID, revID string) (string, error) {
	r, err := s.Ent.BlueprintRevision.Query().Where(blueprintrevision.BlueprintID(blueprintID), blueprintrevision.ID(revID)).Only(ctx)
	if ent.IsNotFound(err) {
		return "", ErrNotFound
	}
	if err != nil {
		return "", err
	}
	return r.Yaml, nil
}

func decodeSnapshot(m map[string]any) Snapshot {
	var snap Snapshot
	b, _ := json.Marshal(m)
	_ = json.Unmarshal(b, &snap)
	return snap
}

func (s *Service) revision(ctx context.Context, blueprintID, revID string) (*ent.BlueprintRevision, Snapshot, error) {
	r, err := s.Ent.BlueprintRevision.Query().Where(blueprintrevision.BlueprintID(blueprintID), blueprintrevision.ID(revID)).Only(ctx)
	if ent.IsNotFound(err) {
		return nil, Snapshot{}, ErrNotFound
	}
	if err != nil {
		return nil, Snapshot{}, err
	}
	return r, decodeSnapshot(r.Snapshot), nil
}

// RestoreBlueprint puts the whole blueprint (goal, slots, their blocks and edits) back
// the way it was at a revision.
func (s *Service) RestoreBlueprint(ctx context.Context, blueprintID, revID string) error {
	r, snap, err := s.revision(ctx, blueprintID, revID)
	if err != nil {
		return err
	}
	return s.change(ctx, blueprintID, "restore", "", func() (string, error) {
		tx, err := s.Ent.Tx(ctx)
		if err != nil {
			return "", err
		}
		if err := applySnapshot(ctx, tx, blueprintID, snap); err != nil {
			return "", rollback(tx, err)
		}
		return fmt.Sprintf("Restored the blueprint to #%d (%s)", r.Seq, r.Summary), tx.Commit()
	})
}

// RestoreSlot puts one slot back the way it was at a revision; the rest of
// the blueprint is left as it is.
func (s *Service) RestoreSlot(ctx context.Context, blueprintID, revID, slotKey string) error {
	r, snap, err := s.revision(ctx, blueprintID, revID)
	if err != nil {
		return err
	}
	var ss *SnapSlot
	for i := range snap.Slots {
		if snap.Slots[i].Key == slotKey {
			ss = &snap.Slots[i]
		}
	}
	if ss == nil {
		return fmt.Errorf("that point in history has no %s slot", slotKey)
	}
	return s.change(ctx, blueprintID, "restore", slotKey, func() (string, error) {
		tx, err := s.Ent.Tx(ctx)
		if err != nil {
			return "", err
		}
		if err := applySlot(ctx, tx, blueprintID, *ss); err != nil {
			return "", rollback(tx, err)
		}
		what := ss.Label
		if ss.BlockName != "" {
			what = ss.BlockName
		}
		return fmt.Sprintf("Restored %s to #%d", what, r.Seq), tx.Commit()
	})
}

func applySnapshot(ctx context.Context, tx *ent.Tx, blueprintID string, snap Snapshot) error {
	if err := setGoal(ctx, tx.Client(), blueprintID, snap.Goal); err != nil {
		return err
	}
	keep := map[string]bool{}
	for _, ss := range snap.Slots {
		keep[ss.Key] = true
		if err := applySlot(ctx, tx, blueprintID, ss); err != nil {
			return err
		}
	}
	current, err := tx.Slot.Query().Where(slot.BlueprintID(blueprintID)).All(ctx)
	if err != nil {
		return err
	}
	for _, sl := range current {
		if keep[sl.Key] {
			continue
		}
		if _, err := tx.BlockLink.Delete().Where(blocklink.SlotID(sl.ID)).Exec(ctx); err != nil {
			return err
		}
		if err := tx.Slot.DeleteOneID(sl.ID).Exec(ctx); err != nil {
			return err
		}
	}
	return nil
}

func applySlot(ctx context.Context, tx *ent.Tx, blueprintID string, ss SnapSlot) error {
	state := slot.StateFilled
	switch {
	case ss.BlockID != "":
	case ss.Required:
		state = slot.StateBlank
	default:
		state = slot.StateOptional
	}
	sl, err := tx.Slot.Query().Where(slot.BlueprintID(blueprintID), slot.Key(ss.Key)).Only(ctx)
	switch {
	case ent.IsNotFound(err):
		sl, err = tx.Slot.Create().SetBlueprintID(blueprintID).SetKey(ss.Key).SetLabel(ss.Label).SetSection(ss.Section).
			SetBlockType(ss.BlockType).SetRequired(ss.Required).SetPosition(ss.Position).SetState(state).Save(ctx)
		if err != nil {
			return err
		}
	case err != nil:
		return err
	default:
		if err := sl.Update().SetLabel(ss.Label).SetSection(ss.Section).SetBlockType(ss.BlockType).SetRequired(ss.Required).
			SetPosition(ss.Position).SetState(state).Exec(ctx); err != nil {
			return err
		}
	}
	if _, err := tx.BlockLink.Delete().Where(blocklink.SlotID(sl.ID)).Exec(ctx); err != nil {
		return err
	}
	if ss.BlockID == "" {
		return nil
	}
	exists, err := tx.BlockVersion.Query().Where(blockversion.ID(ss.VersionID), blockversion.BlockID(ss.BlockID)).Exist(ctx)
	if err != nil {
		return err
	}
	if !exists {
		return errors.New("a block version this point in history used no longer exists")
	}
	return tx.BlockLink.Create().SetSlotID(sl.ID).SetBlockID(ss.BlockID).SetBaseVersionID(ss.VersionID).
		SetOverrides(ss.Overrides).SetOverridesHash(patch.Hash(ss.Overrides)).Exec(ctx)
}
