// Package blueprints loads a blueprint for the builder: its outline (each slot's origin
// and status), effective block payloads (base ⊕ overrides), the compiled YAML
// and check results; and saves edits as overrides that keep the link to the
// library block (DESIGN.md §2.2, §2.6).
package blueprints

import (
	"context"
	"errors"
	"fmt"

	"github.com/docker/kit-maker/internal/blocks"
	"github.com/docker/kit-maker/internal/blueprints/blocktypes"
	"github.com/docker/kit-maker/internal/blueprints/checks"
	"github.com/docker/kit-maker/internal/blueprints/compiler"
	"github.com/docker/kit-maker/internal/blueprints/patch"
	"github.com/docker/kit-maker/internal/ent"
	"github.com/docker/kit-maker/internal/ent/block"
	"github.com/docker/kit-maker/internal/ent/blocklink"
	"github.com/docker/kit-maker/internal/ent/blueprint"
	"github.com/docker/kit-maker/internal/ent/check"
	"github.com/docker/kit-maker/internal/ent/slot"
)

// Origin values shown in the outline (DESIGN.md §2.6).
const (
	OriginInherited         = "inherited"
	OriginModified          = "modified"
	OriginCustom            = "custom"
	OriginFromIncluded      = "sub"
	OriginOverridesIncluded = "over"
	OriginBlank             = "blank"
)

// Service reads and edits blueprints.
type Service struct{ Ent *ent.Client }

// BlueprintInfo is the blueprint header.
type BlueprintInfo struct {
	ID          string `json:"id"`
	Slug        string `json:"slug"`
	DisplayName string `json:"displayName"`
	Description string `json:"description"`
	Kind        string `json:"kind"`
	TeamID      string `json:"teamId"`
	Team        string `json:"team"`
	Goal        string `json:"goal"`
}

// Item is one outline row.
type Item struct {
	Key       string `json:"key"`
	Label     string `json:"label"`
	Section   string `json:"section"`
	Required  bool   `json:"required"`
	Origin    string `json:"origin"`
	Edits     int    `json:"edits"`
	Status    string `json:"status,omitempty"` // blank | problem | fail
	BlockType string `json:"blockType,omitempty"`
	BlockName string `json:"blockName,omitempty"`
	BlockSlug string `json:"blockSlug,omitempty"`
	Version   int    `json:"version,omitempty"`
	Latest    int    `json:"latest,omitempty"`
	Problem   string `json:"problem,omitempty"`
}

// Section groups outline rows.
type Section struct {
	Key   string `json:"key"`
	Label string `json:"label"`
	Items []Item `json:"items"`
	// Types are the block types that can be added to this section.
	Types []blocktypes.Type `json:"types"`
}

// SlotDetail is the selected slot's editor data.
type SlotDetail struct {
	Item
	BlockDescription string         `json:"blockDescription,omitempty"`
	UsedBy           int            `json:"usedBy"`
	Base             map[string]any `json:"base,omitempty"`
	Effective        map[string]any `json:"effective,omitempty"`
	Origins          patch.Origins  `json:"origins,omitempty"`
	Ops              []patch.Op     `json:"ops,omitempty"`
	Conflicts        []patch.Op     `json:"conflicts,omitempty"`

	linkID string
}

// Loaded is a blueprint ready for the builder.
type Loaded struct {
	Blueprint BlueprintInfo          `json:"blueprint"`
	Sections  []Section              `json:"sections"`
	Compiled  compiler.Output        `json:"compiled"`
	Checks    []checks.Result        `json:"checks"`
	Slots     map[string]*SlotDetail `json:"-"`
}

// ErrNotFound means the blueprint or slot doesn't exist.
var ErrNotFound = errors.New("not found")

// Load reads, compiles and checks a blueprint.
func (s *Service) Load(ctx context.Context, blueprintID string) (*Loaded, error) {
	return s.load(ctx, blueprintID, nil, 0)
}

// Preview compiles the blueprint as if one slot's content were draft, without
// saving anything: the editor uses it to show unsaved edits in the YAML.
func (s *Service) Preview(ctx context.Context, blueprintID, slotKey string, draft map[string]any) (compiler.Output, error) {
	patch.AssignKeys(draft)
	l, err := s.load(ctx, blueprintID, map[string]map[string]any{slotKey: draft}, 0)
	if err != nil {
		return compiler.Output{}, err
	}
	if l.Slots[slotKey] == nil {
		return compiler.Output{}, ErrNotFound
	}
	return l.Compiled, nil
}

// load is Load with optional drafts replacing slots' effective content.
func (s *Service) load(ctx context.Context, blueprintID string, drafts map[string]map[string]any, version int) (*Loaded, error) {
	k, err := s.Ent.Blueprint.Query().Where(blueprint.ID(blueprintID)).
		WithTeam().WithGoal().
		WithChecks(func(q *ent.CheckQuery) { q.Order(ent.Asc(check.FieldPosition)) }).
		WithSlots(func(q *ent.SlotQuery) {
			q.Order(ent.Asc(slot.FieldPosition)).WithLink(func(l *ent.BlockLinkQuery) {
				l.WithBaseVersion().WithBlock(func(b *ent.BlockQuery) { b.WithCurrentVersion() })
			})
		}).
		Only(ctx)
	if ent.IsNotFound(err) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}

	l := &Loaded{
		Blueprint: BlueprintInfo{ID: k.ID, Slug: k.Slug, DisplayName: k.DisplayName, Description: k.Description,
			Kind: string(k.Kind), TeamID: k.TeamID},
		Slots: map[string]*SlotDetail{},
	}
	if k.Edges.Team != nil {
		l.Blueprint.Team = k.Edges.Team.Name
	}
	if k.Edges.Goal != nil {
		l.Blueprint.Goal = k.Edges.Goal.Statement
	}

	usedBy, err := s.usedBy(ctx, k.Edges.Slots)
	if err != nil {
		return nil, err
	}

	in := compiler.Input{Blueprint: compiler.Blueprint{Slug: k.Slug, DisplayName: k.DisplayName, Description: k.Description, Kind: string(k.Kind), Version: version}}
	labels := map[string]string{}
	var order []*SlotDetail
	for _, sl := range k.Edges.Slots {
		d := &SlotDetail{Item: Item{Key: sl.Key, Label: sl.Label, Section: sl.Section, Required: sl.Required, Origin: OriginBlank, BlockType: SlotType(sl)}}
		labels[sl.Key] = sl.Label
		cs := compiler.Slot{Key: sl.Key, Label: sl.Label, Section: sl.Section, Required: sl.Required}
		if link := sl.Edges.Link; link != nil && link.Edges.BaseVersion != nil && link.Edges.Block != nil {
			b, base := link.Edges.Block, link.Edges.BaseVersion
			eff, origins, conflicts := patch.Apply(base.Payload, link.Overrides)
			edits := len(link.Overrides)
			if draft, ok := drafts[sl.Key]; ok {
				eff, edits = draft, len(patch.Diff(base.Payload, draft))
			}
			d.linkID = link.ID
			d.BlockType, d.BlockName, d.BlockSlug, d.Version = b.Type, b.Name, b.Slug, base.Version
			if cv := b.Edges.CurrentVersion; cv != nil {
				d.Latest = cv.Version
			}
			d.BlockDescription = b.Description
			d.UsedBy = usedBy[b.ID]
			d.Base, d.Effective, d.Origins, d.Ops, d.Conflicts = base.Payload, eff, origins, link.Overrides, conflicts
			d.Edits = len(link.Overrides)
			switch {
			case b.Visibility == block.VisibilityPrivate:
				d.Origin = OriginCustom
			case d.Edits > 0:
				d.Origin = OriginModified
			default:
				d.Origin = OriginInherited
			}
			if sl.OriginBlueprintID != nil {
				d.Origin = OriginFromIncluded
			}
			if sl.OverridesSlotID != nil {
				d.Origin = OriginOverridesIncluded
			}
			cs.Block = &compiler.Block{Name: b.Name, Slug: b.Slug, Type: b.Type, Version: base.Version, Edits: edits, Payload: eff}
			// Mark the blueprint's edits in the YAML (not for a blueprint-only block,
			// where everything is the blueprint's own).
			if b.Visibility != block.VisibilityPrivate {
				cs.Block.Base = base.Payload
			}
		} else if sl.Required {
			d.Status = "blank"
		}
		in.Slots = append(in.Slots, cs)
		l.Slots[sl.Key] = d
		order = append(order, d)
	}

	l.Compiled = compiler.Compile(in)
	for _, p := range l.Compiled.Problems {
		if d := l.Slots[p.Slot]; d != nil {
			d.Status, d.Problem = "problem", p.Message
		}
	}
	for _, c := range l.Compiled.Conflicts {
		for _, key := range c.Slots {
			if d := l.Slots[key]; d != nil && d.Status == "" {
				d.Status = "conflict"
			}
		}
	}

	var list []checks.Check
	for _, c := range k.Edges.Checks {
		url, _ := c.Spec["url"].(string)
		method, _ := c.Spec["method"].(string)
		list = append(list, checks.Check{ID: c.ID, Label: c.Label, Type: string(c.Type), URL: url, Method: method, Expect: string(c.Expect)})
	}
	l.Checks = checks.Evaluate(l.Compiled.Policy, list, labels)

	// Every section is listed, empty or not, so the outline always shows
	// what a blueprint can hold and where to add it.
	sections := map[string]*Section{}
	for _, sec := range blocktypes.Sections {
		types := blocktypes.ForSection(sec.Key)
		if types == nil {
			types = []blocktypes.Type{}
		}
		sections[sec.Key] = &Section{Key: sec.Key, Label: sec.Label, Items: []Item{}, Types: types}
	}
	for _, d := range order {
		sec := sections[d.Section]
		if sec == nil {
			sec = &Section{Key: d.Section, Label: d.Section, Types: []blocktypes.Type{}}
			sections[d.Section] = sec
		}
		sec.Items = append(sec.Items, d.Item)
	}
	for _, sec := range blocktypes.Sections {
		l.Sections = append(l.Sections, *sections[sec.Key])
	}
	return l, nil
}

func (s *Service) usedBy(ctx context.Context, slots []*ent.Slot) (map[string]int, error) {
	var ids []string
	for _, sl := range slots {
		if sl.Edges.Link != nil {
			ids = append(ids, sl.Edges.Link.BlockID)
		}
	}
	out := map[string]int{}
	if len(ids) == 0 {
		return out, nil
	}
	links, err := s.Ent.BlockLink.Query().Where(blocklink.BlockIDIn(ids...)).All(ctx)
	if err != nil {
		return nil, err
	}
	for _, l := range links {
		out[l.BlockID]++
	}
	return out, nil
}

// saveSlot stores the edited effective payload of a filled slot as override
// ops on its linked block version: the link stays, and library updates can
// rebase underneath (DESIGN.md §2.2).
func (s *Service) saveSlot(ctx context.Context, blueprintID, slotKey string, edited map[string]any) (*SlotDetail, error) {
	l, err := s.Load(ctx, blueprintID)
	if err != nil {
		return nil, err
	}
	d := l.Slots[slotKey]
	if d == nil {
		return nil, ErrNotFound
	}
	if d.linkID == "" {
		return nil, fmt.Errorf("slot %s is blank: link a block before editing", slotKey)
	}
	t, ok := blocktypes.Get(d.BlockType)
	if !ok {
		return nil, fmt.Errorf("unknown block type %s", d.BlockType)
	}
	patch.AssignKeys(edited)
	if err := t.Validate(edited); err != nil {
		return nil, err
	}
	ops := patch.Diff(d.Base, edited)
	if err := s.Ent.BlockLink.UpdateOneID(d.linkID).SetOverrides(ops).SetOverridesHash(patch.Hash(ops)).Exec(ctx); err != nil {
		return nil, err
	}
	d.Ops = ops
	return d, nil
}

// revertSlot drops a slot's overrides, back to the library version.
func (s *Service) revertSlot(ctx context.Context, blueprintID, slotKey string) error {
	l, err := s.Load(ctx, blueprintID)
	if err != nil {
		return err
	}
	d := l.Slots[slotKey]
	if d == nil || d.linkID == "" {
		return ErrNotFound
	}
	return s.Ent.BlockLink.UpdateOneID(d.linkID).ClearOverrides().SetOverridesHash("").Exec(ctx)
}

// Candidate is a library block that can fill a blank slot.
type Candidate struct {
	ID          string `json:"id"`
	Name        string `json:"name"`
	Slug        string `json:"slug"`
	Type        string `json:"type"`
	Description string `json:"description"`
	Version     int    `json:"version"`
	UsedBy      int    `json:"usedBy"`
	Text        string `json:"text"` // the block's values, for search
}

// SlotType is the block type a slot takes: the one it was added for, or
// (slots from before types were stored) its section's first type.
func SlotType(sl *ent.Slot) string {
	if sl.BlockType != "" {
		return sl.BlockType
	}
	return sectionDefault(sl.Section)
}

// Candidates lists library blocks of a block type, owned by teamIDs (or any
// team when all is true).
func (s *Service) Candidates(ctx context.Context, blockType string, teamIDs []string, all bool) ([]Candidate, error) {
	q := s.Ent.Block.Query().Where(block.Type(blockType), block.VisibilityNEQ(block.VisibilityPrivate)).
		WithCurrentVersion().WithLinks().Order(ent.Asc(block.FieldName))
	if !all { // the teams' own blocks, plus public ones (the starter library)
		q = q.Where(block.Or(block.TeamIDIn(teamIDs...), block.VisibilityEQ(block.VisibilityPublic)))
	}
	bs, err := q.All(ctx)
	if err != nil {
		return nil, err
	}
	out := make([]Candidate, 0, len(bs))
	for _, b := range bs {
		if b.Edges.CurrentVersion == nil {
			continue
		}
		out = append(out, Candidate{ID: b.ID, Name: b.Name, Slug: b.Slug, Type: b.Type, Description: b.Description,
			Version: b.Edges.CurrentVersion.Version, UsedBy: len(b.Edges.Links), Text: blocks.SearchText(b.Edges.CurrentVersion.Payload)})
	}
	return out, nil
}

// linkBlock fills a blank slot with a library block at its current version.
func (s *Service) linkBlock(ctx context.Context, blueprintID, slotKey, blockID string) error {
	sl, err := s.Ent.Slot.Query().Where(slot.BlueprintID(blueprintID), slot.Key(slotKey)).WithLink().Only(ctx)
	if ent.IsNotFound(err) {
		return ErrNotFound
	}
	if err != nil {
		return err
	}
	if sl.Edges.Link != nil {
		return fmt.Errorf("slot %s is already filled", slotKey)
	}
	b, err := s.Ent.Block.Query().Where(block.ID(blockID)).WithCurrentVersion().Only(ctx)
	if err != nil {
		return err
	}
	if b.Edges.CurrentVersion == nil {
		return fmt.Errorf("block %s has no published version", b.Slug)
	}
	tx, err := s.Ent.Tx(ctx)
	if err != nil {
		return err
	}
	if err := tx.BlockLink.Create().SetSlotID(sl.ID).SetBlockID(b.ID).SetBaseVersionID(b.Edges.CurrentVersion.ID).Exec(ctx); err != nil {
		_ = tx.Rollback()
		return err
	}
	if err := tx.Slot.UpdateOneID(sl.ID).SetState(slot.StateFilled).Exec(ctx); err != nil {
		_ = tx.Rollback()
		return err
	}
	return tx.Commit()
}

// unlink empties a slot, removing its link and any edits.
func (s *Service) unlink(ctx context.Context, blueprintID, slotKey string) error {
	sl, err := s.Ent.Slot.Query().Where(slot.BlueprintID(blueprintID), slot.Key(slotKey)).Only(ctx)
	if ent.IsNotFound(err) {
		return ErrNotFound
	}
	if err != nil {
		return err
	}
	if _, err := s.Ent.BlockLink.Delete().Where(blocklink.SlotID(sl.ID)).Exec(ctx); err != nil {
		return err
	}
	state := slot.StateBlank
	if !sl.Required {
		state = slot.StateOptional
	}
	return sl.Update().SetState(state).Exec(ctx)
}
