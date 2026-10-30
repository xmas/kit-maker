package lite

import (
	"encoding/json"
	"errors"
	"fmt"
	"regexp"
	"sort"
	"strings"
	"time"

	"github.com/docker/kit-maker/internal/blueprints/blocktypes"
	"github.com/docker/kit-maker/internal/blueprints/patch"
)

var slugRE = regexp.MustCompile(`^[a-z0-9]([a-z0-9-]{0,62}[a-z0-9])?$`)

// Slugify turns a name into a lowercase-kebab slug (as internal/blocks).
func Slugify(name string) string {
	var b strings.Builder
	dash := false
	for _, r := range strings.ToLower(strings.TrimSpace(name)) {
		switch {
		case r >= 'a' && r <= 'z', r >= '0' && r <= '9':
			b.WriteRune(r)
			dash = false
		case !dash && b.Len() > 0:
			b.WriteByte('-')
			dash = true
		}
	}
	s := strings.TrimRight(b.String(), "-")
	if len(s) > 64 {
		s = strings.TrimRight(s[:64], "-")
	}
	return s
}

func (s *Store) blockSlugTaken(teamID, slug string) bool {
	for _, b := range s.blocks {
		if b.TeamID == teamID && b.Slug == slug {
			return true
		}
	}
	return false
}

// BlockInput is a new block; the type's starter is used when Payload is nil.
type BlockInput struct {
	TeamID, Type, Name, Slug, Description, Visibility string
	Payload                                           map[string]any
}

// CreateBlock adds a block with version 1.
func (s *Store) CreateBlock(in BlockInput) (*Block, error) {
	t, ok := blocktypes.Get(in.Type)
	if !ok {
		return nil, fmt.Errorf("unknown block type %q", in.Type)
	}
	in.Name = strings.TrimSpace(in.Name)
	if in.Name == "" {
		return nil, errors.New("give the block a name")
	}
	if in.Slug == "" {
		in.Slug = Slugify(in.Name)
	}
	if !slugRE.MatchString(in.Slug) {
		return nil, fmt.Errorf("slug %q must be lowercase letters, digits and dashes", in.Slug)
	}
	if in.Visibility == "" {
		in.Visibility = "team"
	}
	if s.blockSlugTaken(in.TeamID, in.Slug) {
		return nil, fmt.Errorf("this team already has a block called %q", in.Slug)
	}
	payload := in.Payload
	if payload == nil {
		payload = blocktypes.Starter(in.Type, in.Slug)
	}
	patch.AssignKeys(payload)
	if err := t.Validate(payload); err != nil {
		return nil, err
	}
	b := &Block{ID: newID("blk_"), TeamID: in.TeamID, Type: in.Type, Slug: in.Slug, Name: in.Name,
		Description: strings.TrimSpace(in.Description), Visibility: in.Visibility, Current: 1, CreatedAt: now()}
	b.Versions = []BlockVersion{s.newVersion(1, payload, "First version.")}
	s.putBlock(b)
	return b, nil
}

func (s *Store) newVersion(n int, payload map[string]any, changelog string) BlockVersion {
	return BlockVersion{ID: newID("bv_"), Version: n, Payload: payload, Changelog: strings.TrimSpace(changelog),
		AuthoredBy: "user", By: s.actorName(), CreatedAt: now()}
}

// PublishBlock appends a version when the payload differs from the current
// one, and reports whether it did.
func (s *Store) PublishBlock(id string, payload map[string]any, changelog string) (bool, error) {
	b := s.blocks[id]
	if b == nil {
		return false, ErrNotFound
	}
	t, ok := blocktypes.Get(b.Type)
	if !ok {
		return false, fmt.Errorf("unknown block type %q", b.Type)
	}
	patch.AssignKeys(payload)
	if err := t.Validate(payload); err != nil {
		return false, err
	}
	if cv := b.CurrentVersion(); cv != nil && sameContent(cv.Payload, payload) {
		return false, nil
	}
	if changelog == "" {
		changelog = "Edited in the library."
	}
	n := 0
	for _, v := range b.Versions {
		n = max(n, v.Version)
	}
	b.Versions = append(b.Versions, s.newVersion(n+1, payload, changelog))
	b.Current = n + 1
	s.putBlock(b)
	return true, nil
}

func sameContent(a, b map[string]any) bool {
	x, _ := json.Marshal(patch.Strip(a))
	y, _ := json.Marshal(patch.Strip(b))
	return string(x) == string(y)
}

// UpdateBlockDetails changes a block's name and description.
func (s *Store) UpdateBlockDetails(id, name, description string) error {
	b := s.blocks[id]
	if b == nil {
		return ErrNotFound
	}
	name = strings.TrimSpace(name)
	if name == "" {
		return errors.New("give the block a name")
	}
	b.Name, b.Description = name, strings.TrimSpace(description)
	s.putBlock(b)
	return nil
}

// RestoreBlockVersion publishes an earlier version's payload as the newest.
func (s *Store) RestoreBlockVersion(id string, version int) error {
	b := s.blocks[id]
	if b == nil {
		return ErrNotFound
	}
	old := b.version(version)
	if old == nil {
		return ErrNotFound
	}
	created, err := s.PublishBlock(id, jsonCopy(old.Payload), fmt.Sprintf("Restored v%d.", version))
	if err != nil {
		return err
	}
	if !created {
		return fmt.Errorf("v%d is the same as the current version", version)
	}
	return nil
}

// BlockSummary is one row of the library.
type BlockSummary struct {
	ID          string    `json:"id"`
	Name        string    `json:"name"`
	Slug        string    `json:"slug"`
	Type        string    `json:"type"`
	TypeLabel   string    `json:"typeLabel"`
	Section     string    `json:"section"`
	Description string    `json:"description"`
	TeamID      string    `json:"teamId"`
	Team        string    `json:"team"`
	Visibility  string    `json:"visibility"`
	Version     int       `json:"version"`
	UsedBy      int       `json:"usedBy"`
	UpdatedAt   time.Time `json:"updatedAt"`
	Abilities   []string  `json:"abilities"`
	Text        string    `json:"text"`
}

func (s *Store) summarize(b *Block, usedBy map[string]int) BlockSummary {
	sm := BlockSummary{ID: b.ID, Name: b.Name, Slug: b.Slug, Type: b.Type, Description: b.Description,
		TeamID: b.TeamID, Team: s.Team.Name, Visibility: b.Visibility, UsedBy: usedBy[b.ID], UpdatedAt: b.UpdatedAt}
	if t, ok := blocktypes.Get(b.Type); ok {
		sm.TypeLabel, sm.Section = t.Label, t.Section
	}
	if cv := b.CurrentVersion(); cv != nil {
		sm.Version = cv.Version
		sm.Abilities = blocktypes.AbilitiesOf(b.Type, cv.Payload)
		sm.Text = SearchText(cv.Payload)
		if cv.CreatedAt.After(sm.UpdatedAt) {
			sm.UpdatedAt = cv.CreatedAt
		}
	}
	return sm
}

// ListBlocks returns the library (every block but blueprint-only ones).
func (s *Store) ListBlocks() []BlockSummary {
	usedBy := s.usedBy()
	out := []BlockSummary{}
	for _, b := range s.sortedBlocks() {
		if b.Visibility != "private" {
			out = append(out, s.summarize(b, usedBy))
		}
	}
	return out
}

// VersionInfo is one entry in a block's history.
type VersionInfo struct {
	Version    int       `json:"version"`
	Changelog  string    `json:"changelog"`
	AuthoredBy string    `json:"authoredBy"`
	By         string    `json:"by,omitempty"`
	CreatedAt  time.Time `json:"createdAt"`
}

// Use is one blueprint slot linking a block.
type Use struct {
	BlueprintID string `json:"blueprintId"`
	Blueprint   string `json:"blueprint"`
	SlotKey     string `json:"slotKey"`
	SlotLabel   string `json:"slotLabel"`
	Version     int    `json:"version"`
	Edits       int    `json:"edits"`
}

// BlockDetail is a block with its payload, history and uses.
type BlockDetail struct {
	BlockSummary
	Payload  map[string]any `json:"payload"`
	Versions []VersionInfo  `json:"versions"`
	UsedIn   []Use          `json:"usedIn"`
}

// GetBlock loads one block.
func (s *Store) GetBlock(id string) (*BlockDetail, error) {
	b := s.blocks[id]
	if b == nil {
		return nil, ErrNotFound
	}
	d := &BlockDetail{BlockSummary: s.summarize(b, s.usedBy()), Payload: map[string]any{}}
	if cv := b.CurrentVersion(); cv != nil {
		d.Payload = cv.Payload
	}
	for i := len(b.Versions) - 1; i >= 0; i-- {
		v := b.Versions[i]
		d.Versions = append(d.Versions, VersionInfo{Version: v.Version, Changelog: v.Changelog, AuthoredBy: v.AuthoredBy, By: v.By, CreatedAt: v.CreatedAt})
	}
	bps := make([]*Blueprint, 0, len(s.blueprints))
	for _, bp := range s.blueprints {
		bps = append(bps, bp)
	}
	sort.Slice(bps, func(i, j int) bool { return bps[i].DisplayName < bps[j].DisplayName })
	for _, bp := range bps {
		for _, sl := range sortedSlots(bp) {
			if sl.BlockID != id {
				continue
			}
			u := Use{BlueprintID: bp.ID, Blueprint: bp.DisplayName, SlotKey: sl.Key, SlotLabel: sl.Label, Edits: len(sl.Overrides)}
			if v := b.VersionByID(sl.VersionID); v != nil {
				u.Version = v.Version
			}
			d.UsedIn = append(d.UsedIn, u)
		}
	}
	return d, nil
}

// SearchText flattens a payload's values (not item keys) for search.
func SearchText(p map[string]any) string {
	var b strings.Builder
	var walk func(v any)
	walk = func(v any) {
		if b.Len() > 4000 {
			return
		}
		switch t := v.(type) {
		case map[string]any:
			keys := make([]string, 0, len(t))
			for k := range t {
				if k != "_k" {
					keys = append(keys, k)
				}
			}
			sort.Strings(keys)
			for _, k := range keys {
				walk(t[k])
			}
		case []any:
			for _, x := range t {
				walk(x)
			}
		case string:
			b.WriteString(t)
			b.WriteByte(' ')
		case float64, bool:
			fmt.Fprint(&b, t, " ")
		}
	}
	walk(p)
	out := b.String()
	if len(out) > 4000 {
		out = out[:4000]
	}
	return strings.TrimSpace(out)
}
