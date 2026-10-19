package kits

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"strings"

	"github.com/docker/kit-maker/internal/blocks"
	"github.com/docker/kit-maker/internal/ent"
	"github.com/docker/kit-maker/internal/ent/block"
	"github.com/docker/kit-maker/internal/ent/blocklink"
	"github.com/docker/kit-maker/internal/ent/blockversion"
	"github.com/docker/kit-maker/internal/ent/check"
	"github.com/docker/kit-maker/internal/ent/kit"
	"github.com/docker/kit-maker/internal/ent/predicate"
	"github.com/docker/kit-maker/internal/ent/slot"
	"github.com/docker/kit-maker/internal/kits/blocktypes"
)

// CreateInput is a new kit.
type CreateInput struct {
	TeamID      string
	DisplayName string
	Slug        string
	Kind        string // workload | mixin
	Description string
	Goal        string
}

// starterSlots is the outline every new kit starts with; more are added in
// the builder (and by the AI planner later, kit-maker-i9v.7).
var starterSlots = []struct{ key, label, section string }{
	{"network-access", "Network access", "network"},
}

// Create makes a kit with its goal and a starter outline.
func (s *Service) Create(ctx context.Context, in CreateInput) (*ent.Kit, error) {
	in.DisplayName = strings.TrimSpace(in.DisplayName)
	if in.DisplayName == "" {
		return nil, errors.New("give the kit a name")
	}
	if in.Slug == "" {
		in.Slug = blocks.Slugify(in.DisplayName)
	}
	if blocks.Slugify(in.Slug) != in.Slug || in.Slug == "" {
		return nil, fmt.Errorf("slug %q must be lowercase letters, digits and dashes", in.Slug)
	}
	if in.Kind != string(kit.KindWorkload) && in.Kind != string(kit.KindMixin) {
		return nil, errors.New("kind must be workload or mixin")
	}
	taken, err := s.Ent.Kit.Query().Where(kit.TeamID(in.TeamID), kit.Slug(in.Slug)).Exist(ctx)
	if err != nil {
		return nil, err
	}
	if taken {
		return nil, fmt.Errorf("this team already has a kit called %q", in.Slug)
	}
	tx, err := s.Ent.Tx(ctx)
	if err != nil {
		return nil, err
	}
	k, err := tx.Kit.Create().SetTeamID(in.TeamID).SetSlug(in.Slug).SetDisplayName(in.DisplayName).
		SetKind(kit.Kind(in.Kind)).SetDescription(strings.TrimSpace(in.Description)).Save(ctx)
	if err != nil {
		return nil, rollback(tx, err)
	}
	if g := strings.TrimSpace(in.Goal); g != "" {
		if err := tx.Goal.Create().SetKitID(k.ID).SetStatement(g).Exec(ctx); err != nil {
			return nil, rollback(tx, err)
		}
	}
	for i, st := range starterSlots {
		if err := tx.Slot.Create().SetKitID(k.ID).SetKey(st.key).SetLabel(st.label).SetSection(st.section).
			SetRequired(true).SetPosition(i).Exec(ctx); err != nil {
			return nil, rollback(tx, err)
		}
	}
	return k, tx.Commit()
}

func rollback(tx *ent.Tx, err error) error {
	_ = tx.Rollback()
	return err
}

// UpdateGoal sets (or clears) a kit's goal statement.
func (s *Service) UpdateGoal(ctx context.Context, kitID, statement string) error {
	statement = strings.TrimSpace(statement)
	g, err := s.Ent.Kit.Query().Where(kit.ID(kitID)).QueryGoal().Only(ctx)
	switch {
	case ent.IsNotFound(err) && statement == "":
		return nil
	case ent.IsNotFound(err):
		return s.Ent.Goal.Create().SetKitID(kitID).SetStatement(statement).Exec(ctx)
	case err != nil:
		return err
	case statement == "":
		return s.Ent.Goal.DeleteOne(g).Exec(ctx)
	default:
		return g.Update().SetStatement(statement).Exec(ctx)
	}
}

// AddSlot adds a blank slot for a block type and returns its key. The type
// decides the outline section.
func (s *Service) AddSlot(ctx context.Context, kitID, label, blockType string, required bool) (string, error) {
	t, ok := blocktypes.Get(blockType)
	if !ok {
		return "", fmt.Errorf("unknown block type %q", blockType)
	}
	existing, err := s.Ent.Slot.Query().Where(slot.KitID(kitID)).All(ctx)
	if err != nil {
		return "", err
	}
	keys, labels, pos := map[string]bool{}, map[string]bool{}, 0
	for _, sl := range existing {
		keys[sl.Key] = true
		labels[sl.Label] = true
		pos = max(pos, sl.Position+1)
	}
	label = strings.TrimSpace(label)
	if label == "" {
		// Unnamed slots take the type's name, numbered when it's taken.
		label = t.Label
		for n := 2; labels[label]; n++ {
			label = fmt.Sprintf("%s %d", t.Label, n)
		}
	}
	base := blocks.Slugify(label)
	if base == "" {
		base = t.Key
	}
	key := base
	for n := 2; keys[key]; n++ {
		key = fmt.Sprintf("%s-%d", base, n)
	}
	state := slot.StateBlank
	if !required {
		state = slot.StateOptional
	}
	err = s.Ent.Slot.Create().SetKitID(kitID).SetKey(key).SetLabel(label).SetSection(t.Section).
		SetRequired(required).SetState(state).SetPosition(pos).Exec(ctx)
	return key, err
}

// RemoveSlot deletes a slot and its link. A private block made for the slot
// is deleted with it; library blocks stay in the library.
func (s *Service) RemoveSlot(ctx context.Context, kitID, slotKey string) error {
	sl, err := s.Ent.Slot.Query().Where(slot.KitID(kitID), slot.Key(slotKey)).WithLink(func(q *ent.BlockLinkQuery) { q.WithBlock() }).Only(ctx)
	if ent.IsNotFound(err) {
		return ErrNotFound
	}
	if err != nil {
		return err
	}
	tx, err := s.Ent.Tx(ctx)
	if err != nil {
		return err
	}
	if l := sl.Edges.Link; l != nil {
		if err := tx.BlockLink.DeleteOneID(l.ID).Exec(ctx); err != nil {
			return rollback(tx, err)
		}
		if b := l.Edges.Block; b != nil && b.Visibility == block.VisibilityPrivate {
			if err := deleteBlock(ctx, tx, b.ID); err != nil {
				return rollback(tx, err)
			}
		}
	}
	if err := tx.Slot.DeleteOneID(sl.ID).Exec(ctx); err != nil {
		return rollback(tx, err)
	}
	return tx.Commit()
}

func deleteBlock(ctx context.Context, tx *ent.Tx, id string) error {
	if err := tx.Block.UpdateOneID(id).ClearCurrentVersion().Exec(ctx); err != nil {
		return err
	}
	if _, err := tx.BlockVersion.Delete().Where(blockversionOf(id)).Exec(ctx); err != nil {
		return err
	}
	return tx.Block.DeleteOneID(id).Exec(ctx)
}

// sectionDefault is the block type a blank in each section is filled with.
func sectionDefault(section string) string {
	if ts := sectionTypes[section]; len(ts) > 0 {
		return ts[0]
	}
	return blocktypes.RawCapability
}

// StartCustomBlock fills a blank slot with a new private block, owned by the
// kit's team and visible only in this kit (origin "custom").
func (s *Service) StartCustomBlock(ctx context.Context, kitID, slotKey, userID string) error {
	sl, err := s.Ent.Slot.Query().Where(slot.KitID(kitID), slot.Key(slotKey)).WithLink().WithKit().Only(ctx)
	if ent.IsNotFound(err) {
		return ErrNotFound
	}
	if err != nil {
		return err
	}
	if sl.Edges.Link != nil {
		return fmt.Errorf("slot %s is already filled", slotKey)
	}
	k := sl.Edges.Kit
	slug := blocks.Slugify(k.Slug + "-" + sl.Key)
	for n := 2; ; n++ {
		taken, err := s.Ent.Block.Query().Where(block.TeamID(k.TeamID), block.Slug(slug)).Exist(ctx)
		if err != nil {
			return err
		}
		if !taken {
			break
		}
		slug = blocks.Slugify(fmt.Sprintf("%s-%s-%d", k.Slug, sl.Key, n))
	}
	tx, err := s.Ent.Tx(ctx)
	if err != nil {
		return err
	}
	b, err := blocks.CreateTx(ctx, tx, blocks.CreateInput{
		TeamID: k.TeamID, Type: sectionDefault(sl.Section), Name: sl.Label, Slug: slug,
		// Name the credential service and env var after the slot, not the
		// kit-prefixed block slug.
		Payload:     blocktypes.Starter(sectionDefault(sl.Section), sl.Key),
		Description: "Made for " + k.DisplayName + ".", Visibility: block.VisibilityPrivate, UserID: userID,
	})
	if err != nil {
		return rollback(tx, err)
	}
	if err := tx.BlockLink.Create().SetSlotID(sl.ID).SetBlockID(b.ID).SetBaseVersionID(*b.CurrentVersionID).Exec(ctx); err != nil {
		return rollback(tx, err)
	}
	if err := tx.Slot.UpdateOneID(sl.ID).SetState(slot.StateFilled).Exec(ctx); err != nil {
		return rollback(tx, err)
	}
	return tx.Commit()
}

// UpdateLink moves a slot to its block's current version and re-applies the
// kit's edits on top. Edits whose target the new version removed stay stored
// and show as conflicts until the next save or a revert (DESIGN.md §2.2).
func (s *Service) UpdateLink(ctx context.Context, kitID, slotKey string) error {
	sl, err := s.Ent.Slot.Query().Where(slot.KitID(kitID), slot.Key(slotKey)).
		WithLink(func(q *ent.BlockLinkQuery) { q.WithBlock() }).Only(ctx)
	if ent.IsNotFound(err) {
		return ErrNotFound
	}
	if err != nil {
		return err
	}
	l := sl.Edges.Link
	if l == nil || l.Edges.Block == nil || l.Edges.Block.CurrentVersionID == nil {
		return fmt.Errorf("slot %s has no block to update", slotKey)
	}
	if *l.Edges.Block.CurrentVersionID == l.BaseVersionID {
		return nil
	}
	return s.Ent.BlockLink.UpdateOneID(l.ID).SetBaseVersionID(*l.Edges.Block.CurrentVersionID).
		SetRebaseState(blocklink.RebaseStateClean).Exec(ctx)
}

// CheckInput is a new network check.
type CheckInput struct {
	Label  string
	Method string
	URL    string
	Expect string // reach | blocked
}

// AddCheck appends a network check to a kit.
func (s *Service) AddCheck(ctx context.Context, kitID string, in CheckInput) error {
	in.Label, in.URL = strings.TrimSpace(in.Label), strings.TrimSpace(in.URL)
	in.Method = strings.ToUpper(strings.TrimSpace(in.Method))
	if in.Method == "" {
		in.Method = "GET"
	}
	u, err := url.Parse(in.URL)
	if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" {
		return errors.New("the check needs a full http(s) URL")
	}
	if in.Expect != "reach" && in.Expect != "blocked" {
		return errors.New("expect must be reach or blocked")
	}
	if in.Label == "" {
		in.Label = in.Method + " " + u.Host + u.Path
	}
	n, err := s.Ent.Check.Query().Where(check.KitID(kitID)).Count(ctx)
	if err != nil {
		return err
	}
	return s.Ent.Check.Create().SetKitID(kitID).SetLabel(in.Label).SetType(check.TypeNetwork).
		SetSpec(map[string]any{"url": in.URL, "method": in.Method}).SetExpect(check.Expect(in.Expect)).
		SetPosition(n).Exec(ctx)
}

// RemoveCheck deletes one of a kit's checks.
func (s *Service) RemoveCheck(ctx context.Context, kitID, checkID string) error {
	n, err := s.Ent.Check.Delete().Where(check.KitID(kitID), check.ID(checkID)).Exec(ctx)
	if err == nil && n == 0 {
		return ErrNotFound
	}
	return err
}

func blockversionOf(blockID string) predicate.BlockVersion { return blockversion.BlockID(blockID) }

// Relink points a filled slot at a different library block (its current
// version). The kit's edits belonged to the old block, so they are dropped;
// save them as a new block first to keep them. A private block left behind
// is deleted.
func (s *Service) Relink(ctx context.Context, kitID, slotKey, blockID string) error {
	sl, err := s.Ent.Slot.Query().Where(slot.KitID(kitID), slot.Key(slotKey)).
		WithLink(func(q *ent.BlockLinkQuery) { q.WithBlock() }).Only(ctx)
	if ent.IsNotFound(err) {
		return ErrNotFound
	}
	if err != nil {
		return err
	}
	l := sl.Edges.Link
	if l == nil {
		return s.LinkBlock(ctx, kitID, slotKey, blockID)
	}
	if l.BlockID == blockID {
		return nil
	}
	b, err := s.Ent.Block.Query().Where(block.ID(blockID)).Only(ctx)
	if err != nil {
		return err
	}
	if b.CurrentVersionID == nil {
		return fmt.Errorf("block %s has no published version", b.Slug)
	}
	return s.swapLink(ctx, l, b.ID, *b.CurrentVersionID)
}

// swapLink moves a link to another block version, clears its edits and
// deletes the old block when it was private to this kit.
func (s *Service) swapLink(ctx context.Context, l *ent.BlockLink, blockID, versionID string) error {
	tx, err := s.Ent.Tx(ctx)
	if err != nil {
		return err
	}
	if err := tx.BlockLink.UpdateOneID(l.ID).SetBlockID(blockID).SetBaseVersionID(versionID).
		ClearOverrides().SetOverridesHash("").SetRebaseState(blocklink.RebaseStateClean).Exec(ctx); err != nil {
		return rollback(tx, err)
	}
	if old := l.Edges.Block; old != nil && old.Visibility == block.VisibilityPrivate && old.ID != blockID {
		if err := deleteBlock(ctx, tx, old.ID); err != nil {
			return rollback(tx, err)
		}
	}
	return tx.Commit()
}

// SaveAsBlock turns a slot's effective content (the block plus this kit's
// edits) into a new shared library block, and links the slot to it with no
// edits left over.
func (s *Service) SaveAsBlock(ctx context.Context, kitID, slotKey, name, userID string) (*ent.Block, error) {
	l, err := s.Load(ctx, kitID)
	if err != nil {
		return nil, err
	}
	d := l.Slots[slotKey]
	if d == nil {
		return nil, ErrNotFound
	}
	if d.linkID == "" {
		return nil, fmt.Errorf("slot %s is empty", slotKey)
	}
	link, err := s.Ent.BlockLink.Query().Where(blocklink.ID(d.linkID)).WithBlock().Only(ctx)
	if err != nil {
		return nil, err
	}
	desc := fmt.Sprintf("Saved from %s, based on %s v%d.", l.Kit.DisplayName, d.BlockName, d.Version)
	if d.Origin == OriginCustom {
		desc = "Saved from " + l.Kit.DisplayName + "."
	}
	b, err := blocks.Create(ctx, s.Ent, blocks.CreateInput{
		TeamID: l.Kit.TeamID, Type: d.BlockType, Name: name, Description: desc,
		Payload: jsonCopy(d.Effective), UserID: userID,
	})
	if err != nil {
		return nil, err
	}
	if err := s.swapLink(ctx, link, b.ID, *b.CurrentVersionID); err != nil {
		return nil, err
	}
	return b, nil
}

func jsonCopy(m map[string]any) map[string]any {
	b, _ := json.Marshal(m)
	var out map[string]any
	_ = json.Unmarshal(b, &out)
	return out
}
