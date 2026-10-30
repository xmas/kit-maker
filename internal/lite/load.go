package lite

import (
	"errors"
	"sort"

	"github.com/docker/kit-maker/internal/blueprints/blocktypes"
	"github.com/docker/kit-maker/internal/blueprints/checks"
	"github.com/docker/kit-maker/internal/blueprints/compiler"
	"github.com/docker/kit-maker/internal/blueprints/patch"
)

// Origin values shown in the outline (DESIGN.md §2.6).
const (
	OriginInherited = "inherited"
	OriginModified  = "modified"
	OriginCustom    = "custom"
	OriginBlank     = "blank"
)

// ErrNotFound means the blueprint, slot or block doesn't exist.
var ErrNotFound = errors.New("not found")

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
	Status    string `json:"status,omitempty"` // blank | problem | conflict
	BlockType string `json:"blockType,omitempty"`
	BlockName string `json:"blockName,omitempty"`
	BlockSlug string `json:"blockSlug,omitempty"`
	Version   int    `json:"version,omitempty"`
	Latest    int    `json:"latest,omitempty"`
	Problem   string `json:"problem,omitempty"`
}

// Section groups outline rows.
type Section struct {
	Key   string            `json:"key"`
	Label string            `json:"label"`
	Items []Item            `json:"items"`
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

	linked bool
}

// Loaded is a blueprint ready for the builder.
type Loaded struct {
	Blueprint BlueprintInfo          `json:"blueprint"`
	Sections  []Section              `json:"sections"`
	Compiled  compiler.Output        `json:"compiled"`
	Checks    []checks.Result        `json:"checks"`
	Slots     map[string]*SlotDetail `json:"-"`
}

// slotType is the block type a slot takes.
func slotType(sl Slot) string {
	if sl.BlockType != "" {
		return sl.BlockType
	}
	return sectionDefault(sl.Section)
}

func sectionDefault(section string) string {
	switch section {
	case "network":
		return blocktypes.NetworkRules
	case "credentials":
		return blocktypes.Credential
	}
	return blocktypes.RawCapability
}

// Load reads, compiles and checks a blueprint.
func (s *Store) Load(id string) (*Loaded, error) { return s.load(id, nil, 0) }

// Preview compiles the blueprint as if one slot held draft.
func (s *Store) Preview(id, slotKey string, draft map[string]any) (compiler.Output, error) {
	patch.AssignKeys(draft)
	l, err := s.load(id, map[string]map[string]any{slotKey: draft}, 0)
	if err != nil {
		return compiler.Output{}, err
	}
	if l.Slots[slotKey] == nil {
		return compiler.Output{}, ErrNotFound
	}
	return l.Compiled, nil
}

// sortedSlots returns a blueprint's slots by position.
func sortedSlots(bp *Blueprint) []Slot {
	out := append([]Slot(nil), bp.Slots...)
	sort.SliceStable(out, func(i, j int) bool { return out[i].Position < out[j].Position })
	return out
}

func (s *Store) load(id string, drafts map[string]map[string]any, version int) (*Loaded, error) {
	bp := s.blueprints[id]
	if bp == nil {
		return nil, ErrNotFound
	}
	l := &Loaded{
		Blueprint: BlueprintInfo{ID: bp.ID, Slug: bp.Slug, DisplayName: bp.DisplayName, Description: bp.Description,
			Kind: bp.Kind, TeamID: bp.TeamID, Team: s.Team.Name, Goal: bp.Goal},
		Slots: map[string]*SlotDetail{},
	}
	usedBy := s.usedBy()
	in := compiler.Input{Blueprint: compiler.Blueprint{Slug: bp.Slug, DisplayName: bp.DisplayName, Description: bp.Description, Kind: bp.Kind, Version: version}}
	labels := map[string]string{}
	var order []*SlotDetail
	for _, sl := range sortedSlots(bp) {
		d := &SlotDetail{Item: Item{Key: sl.Key, Label: sl.Label, Section: sl.Section, Required: sl.Required, Origin: OriginBlank, BlockType: slotType(sl)}}
		labels[sl.Key] = sl.Label
		cs := compiler.Slot{Key: sl.Key, Label: sl.Label, Section: sl.Section, Required: sl.Required}
		b := s.blocks[sl.BlockID]
		var base *BlockVersion
		if b != nil {
			base = b.VersionByID(sl.VersionID)
		}
		if sl.BlockID != "" && base != nil {
			eff, origins, conflicts := patch.Apply(base.Payload, sl.Overrides)
			edits := len(sl.Overrides)
			if draft, ok := drafts[sl.Key]; ok {
				eff, edits = draft, len(patch.Diff(base.Payload, draft))
			}
			d.linked = true
			d.BlockType, d.BlockName, d.BlockSlug, d.Version = b.Type, b.Name, b.Slug, base.Version
			d.Latest = b.Current
			d.BlockDescription = b.Description
			d.UsedBy = usedBy[b.ID]
			d.Base, d.Effective, d.Origins, d.Ops, d.Conflicts = base.Payload, eff, origins, sl.Overrides, conflicts
			d.Edits = len(sl.Overrides)
			switch {
			case b.Visibility == "private":
				d.Origin = OriginCustom
			case d.Edits > 0:
				d.Origin = OriginModified
			default:
				d.Origin = OriginInherited
			}
			cs.Block = &compiler.Block{Name: b.Name, Slug: b.Slug, Type: b.Type, Version: base.Version, Edits: edits, Payload: eff}
			if b.Visibility != "private" {
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
	for _, c := range bp.Checks {
		list = append(list, checks.Check{ID: c.ID, Label: c.Label, Type: c.Type, URL: c.URL, Method: c.Method, Expect: c.Expect})
	}
	l.Checks = checks.Evaluate(l.Compiled.Policy, list, labels)

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

// usedBy counts the slots linking each block.
func (s *Store) usedBy() map[string]int {
	out := map[string]int{}
	for _, bp := range s.blueprints {
		for _, sl := range bp.Slots {
			if sl.BlockID != "" {
				out[sl.BlockID]++
			}
		}
	}
	return out
}

// Candidate is a library block that can fill a slot.
type Candidate struct {
	ID          string `json:"id"`
	Name        string `json:"name"`
	Slug        string `json:"slug"`
	Type        string `json:"type"`
	Description string `json:"description"`
	Version     int    `json:"version"`
	UsedBy      int    `json:"usedBy"`
	Text        string `json:"text"`
}

// Candidates lists library blocks of a type, by name.
func (s *Store) Candidates(blockType string) []Candidate {
	usedBy := s.usedBy()
	out := []Candidate{}
	for _, b := range s.sortedBlocks() {
		cv := b.CurrentVersion()
		if b.Type != blockType || b.Visibility == "private" || cv == nil {
			continue
		}
		out = append(out, Candidate{ID: b.ID, Name: b.Name, Slug: b.Slug, Type: b.Type, Description: b.Description,
			Version: cv.Version, UsedBy: usedBy[b.ID], Text: SearchText(cv.Payload)})
	}
	return out
}

func (s *Store) sortedBlocks() []*Block {
	out := make([]*Block, 0, len(s.blocks))
	for _, b := range s.blocks {
		out = append(out, b)
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Name != out[j].Name {
			return out[i].Name < out[j].Name
		}
		return out[i].ID < out[j].ID
	})
	return out
}
