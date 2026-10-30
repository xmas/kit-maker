package lite

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"strings"

	"github.com/docker/kit-maker/internal/blueprints/blocktypes"
	"github.com/docker/kit-maker/internal/blueprints/patch"
)

// Every change to a blueprint records a revision, as internal/blueprints
// does: a snapshot after the change plus the YAML it compiled to; the first
// change also records the starting point.

func (s *Store) blueprint(id string) (*Blueprint, error) {
	bp := s.blueprints[id]
	if bp == nil {
		return nil, ErrNotFound
	}
	return bp, nil
}

func (bp *Blueprint) slot(key string) *Slot {
	for i := range bp.Slots {
		if bp.Slots[i].Key == key {
			return &bp.Slots[i]
		}
	}
	return nil
}

func (s *Store) snapshot(bp *Blueprint) Snapshot {
	snap := Snapshot{Goal: bp.Goal, Slots: []Slot{}}
	for _, sl := range sortedSlots(bp) {
		sl.BlockName, sl.Version = "", 0
		if b := s.blocks[sl.BlockID]; b != nil {
			sl.BlockName = b.Name
			if v := b.VersionByID(sl.VersionID); v != nil {
				sl.Version = v.Version
			}
		}
		snap.Slots = append(snap.Slots, sl)
	}
	return snap
}

func (s *Store) record(bp *Blueprint, action, slotKey, summary string) {
	var yamlText string
	if l, err := s.Load(bp.ID); err == nil {
		yamlText = l.Compiled.YAML
	}
	bp.RevSeq++
	r := &Revision{ID: newID("rev_"), BlueprintID: bp.ID, Seq: bp.RevSeq, Action: action, SlotKey: slotKey,
		Summary: summary, Actor: s.actorName(), Snapshot: s.snapshot(bp), YAML: yamlText, CreatedAt: now()}
	s.putRevision(r)
	s.putBlueprint(bp)
}

func (s *Store) actorName() string {
	if s.Actor.DisplayName != "" {
		return s.Actor.DisplayName
	}
	return s.Actor.Email
}

// change runs one blueprint change and records it. The blueprint is copied
// first, so a failed change leaves it as it was.
func (s *Store) change(id, action, slotKey string, fn func(bp *Blueprint) (string, error)) error {
	orig, err := s.blueprint(id)
	if err != nil {
		return err
	}
	if orig.RevSeq == 0 {
		s.record(orig, "baseline", "", "Starting point")
	}
	bp := cloneBlueprint(orig)
	s.blueprints[id] = bp
	summary, err := fn(bp)
	if err != nil {
		s.blueprints[id] = orig
		return err
	}
	s.record(bp, action, slotKey, summary)
	return nil
}

func cloneBlueprint(bp *Blueprint) *Blueprint {
	var out Blueprint
	b, _ := json.Marshal(bp)
	_ = json.Unmarshal(b, &out)
	return &out
}

func jsonCopy(m map[string]any) map[string]any {
	b, _ := json.Marshal(m)
	var out map[string]any
	_ = json.Unmarshal(b, &out)
	return out
}

func plural(n int, one, many string) string {
	if n == 1 {
		return "1 " + one
	}
	return fmt.Sprintf("%d %s", n, many)
}

// slotName is what the outline calls a slot: its block's name, or its label.
func (s *Store) slotName(bp *Blueprint, key string) string {
	sl := bp.slot(key)
	if sl == nil {
		return key
	}
	if b := s.blocks[sl.BlockID]; b != nil {
		return b.Name
	}
	return sl.Label
}

func (s *Store) linkedVersion(bp *Blueprint, key string) (string, int) {
	sl := bp.slot(key)
	if sl == nil {
		return "", 0
	}
	b := s.blocks[sl.BlockID]
	if b == nil {
		return "", 0
	}
	if v := b.VersionByID(sl.VersionID); v != nil {
		return b.Name, v.Version
	}
	return b.Name, 0
}

// --- Blueprints -------------------------------------------------------------

// CreateInput is a new blueprint.
type CreateInput struct {
	TeamID, DisplayName, Slug, Kind, Description, Goal string
}

// Create makes a blueprint with its goal and a starter "Network access" slot.
func (s *Store) Create(in CreateInput) (*Blueprint, error) {
	in.DisplayName = strings.TrimSpace(in.DisplayName)
	if in.DisplayName == "" {
		return nil, errors.New("give the blueprint a name")
	}
	if in.Slug == "" {
		in.Slug = Slugify(in.DisplayName)
	}
	if Slugify(in.Slug) != in.Slug || in.Slug == "" {
		return nil, fmt.Errorf("slug %q must be lowercase letters, digits and dashes", in.Slug)
	}
	if in.Kind != "workload" && in.Kind != "mixin" {
		return nil, errors.New("kind must be workload or mixin")
	}
	for _, bp := range s.blueprints {
		if bp.TeamID == in.TeamID && bp.Slug == in.Slug {
			return nil, fmt.Errorf("this team already has a blueprint called %q", in.Slug)
		}
	}
	t, _ := blocktypes.Get(blocktypes.NetworkRules)
	bp := &Blueprint{ID: newID("bp_"), TeamID: in.TeamID, Slug: in.Slug, DisplayName: in.DisplayName, Kind: in.Kind,
		Description: strings.TrimSpace(in.Description), Goal: strings.TrimSpace(in.Goal), Checks: []Check{},
		Slots:     []Slot{{Key: "network-access", Label: "Network access", Section: t.Section, BlockType: t.Key, Required: true}},
		CreatedAt: now()}
	s.blueprints[bp.ID] = bp
	s.record(bp, "create", "", "Created the blueprint")
	return bp, nil
}

// UpdateGoal sets (or clears) the goal statement.
func (s *Store) UpdateGoal(id, statement string) error {
	return s.change(id, "goal", "", func(bp *Blueprint) (string, error) {
		bp.Goal = strings.TrimSpace(statement)
		return "Changed the goal", nil
	})
}

// AddSlot adds an empty slot for a block type and returns its key.
func (s *Store) AddSlot(id, label, blockType string, required bool) (string, error) {
	t, ok := blocktypes.Get(blockType)
	if !ok {
		return "", fmt.Errorf("unknown block type %q", blockType)
	}
	var key string
	err := s.change(id, "add-slot", "", func(bp *Blueprint) (string, error) {
		keys, labels, pos := map[string]bool{}, map[string]bool{}, 0
		for _, sl := range bp.Slots {
			keys[sl.Key] = true
			labels[sl.Label] = true
			pos = max(pos, sl.Position+1)
		}
		label = strings.TrimSpace(label)
		if label == "" {
			label = t.Label
			for n := 2; labels[label]; n++ {
				label = fmt.Sprintf("%s %d", t.Label, n)
			}
		}
		base := Slugify(label)
		if base == "" {
			base = t.Key
		}
		key = base
		for n := 2; keys[key]; n++ {
			key = fmt.Sprintf("%s-%d", base, n)
		}
		bp.Slots = append(bp.Slots, Slot{Key: key, Label: label, Section: t.Section, BlockType: t.Key, Required: required, Position: pos})
		return fmt.Sprintf("Added an empty %s slot", t.Label), nil
	})
	return key, err
}

// RemoveSlot deletes a slot. A block made only for it stays (unlinked), so
// history can bring it back.
func (s *Store) RemoveSlot(id, key string) error {
	bp, err := s.blueprint(id)
	if err != nil {
		return err
	}
	if bp.slot(key) == nil {
		return ErrNotFound
	}
	before := s.slotName(bp, key)
	return s.change(id, "remove-slot", key, func(bp *Blueprint) (string, error) {
		kept := bp.Slots[:0]
		for _, sl := range bp.Slots {
			if sl.Key != key {
				kept = append(kept, sl)
			}
		}
		bp.Slots = kept
		return "Removed " + before, nil
	})
}

func (s *Store) filledSlot(bp *Blueprint, key string) (*Slot, *Block, *BlockVersion, error) {
	sl := bp.slot(key)
	if sl == nil {
		return nil, nil, nil, ErrNotFound
	}
	b := s.blocks[sl.BlockID]
	if sl.BlockID == "" || b == nil {
		return sl, nil, nil, fmt.Errorf("slot %s is blank: link a block before editing", key)
	}
	v := b.VersionByID(sl.VersionID)
	if v == nil {
		return sl, b, nil, fmt.Errorf("slot %s links a block version that no longer exists", key)
	}
	return sl, b, v, nil
}

// SaveSlot stores an edited payload as override ops on the slot's block.
func (s *Store) SaveSlot(id, key string, edited map[string]any) error {
	return s.change(id, "edit", key, func(bp *Blueprint) (string, error) {
		ops, b, v, err := s.saveSlot(bp, key, edited)
		if err != nil {
			return "", err
		}
		if len(ops) == 0 {
			return fmt.Sprintf("Edited %s: back to %s v%d as is", b.Name, b.Name, v.Version), nil
		}
		return fmt.Sprintf("Edited %s (%s from %s v%d)", b.Name, plural(len(ops), "edit", "edits"), b.Name, v.Version), nil
	})
}

func (s *Store) saveSlot(bp *Blueprint, key string, edited map[string]any) ([]patch.Op, *Block, *BlockVersion, error) {
	sl, b, v, err := s.filledSlot(bp, key)
	if err != nil {
		return nil, nil, nil, err
	}
	t, ok := blocktypes.Get(b.Type)
	if !ok {
		return nil, nil, nil, fmt.Errorf("unknown block type %s", b.Type)
	}
	patch.AssignKeys(edited)
	if err := t.Validate(edited); err != nil {
		return nil, nil, nil, err
	}
	sl.Overrides = patch.Diff(v.Payload, edited)
	return sl.Overrides, b, v, nil
}

// HideEntry removes one entry (key, in the list at path) from a slot's
// block in this blueprint only, to resolve a conflict.
func (s *Store) HideEntry(id, key, path, entryKey string) error {
	return s.change(id, "edit", key, func(bp *Blueprint) (string, error) {
		l, err := s.Load(id)
		if err != nil {
			return "", err
		}
		d := l.Slots[key]
		if d == nil || d.Effective == nil {
			return "", ErrNotFound
		}
		edited := jsonCopy(d.Effective)
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
			if m, _ := it.(map[string]any); m != nil && m[patch.KeyField] == entryKey {
				what = describeEntry(m)
				continue
			}
			kept = append(kept, it)
		}
		if what == "" {
			return "", fmt.Errorf("that entry is no longer in %s", d.Label)
		}
		parent[list] = kept
		if _, _, _, err := s.saveSlot(bp, key, edited); err != nil {
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
	list, _ := m["hosts"].([]any)
	for _, h := range list {
		if s, ok := h.(string); ok {
			hosts = append(hosts, s)
		}
	}
	if len(hosts) > 0 {
		return strings.Join(hosts, ", ")
	}
	return "an entry"
}

// RevertSlot drops a slot's saved edits, keeping its block version.
func (s *Store) RevertSlot(id, key string) error {
	return s.change(id, "revert", key, func(bp *Blueprint) (string, error) {
		sl := bp.slot(key)
		if sl == nil || sl.BlockID == "" {
			return "", ErrNotFound
		}
		sl.Overrides = nil
		return "Dropped the edits to " + s.slotName(bp, key), nil
	})
}

// Resync drops a slot's edits and moves it to its block's latest version.
func (s *Store) Resync(id, key string) error {
	return s.change(id, "resync", key, func(bp *Blueprint) (string, error) {
		sl := bp.slot(key)
		if sl == nil || sl.BlockID == "" {
			return "", ErrNotFound
		}
		sl.Overrides = nil
		if err := s.updateLink(bp, key); err != nil {
			return "", err
		}
		name, v := s.linkedVersion(bp, key)
		return fmt.Sprintf("Resynced to %s v%d", name, v), nil
	})
}

func (s *Store) updateLink(bp *Blueprint, key string) error {
	sl := bp.slot(key)
	b := s.blocks[sl.BlockID]
	if b == nil || b.CurrentVersion() == nil {
		return fmt.Errorf("slot %s has no block to update", key)
	}
	sl.VersionID = b.CurrentVersion().ID
	return nil
}

// UpdateLink moves a slot to its block's latest version, keeping its edits.
func (s *Store) UpdateLink(id, key string) error {
	return s.change(id, "update", key, func(bp *Blueprint) (string, error) {
		if sl := bp.slot(key); sl == nil || sl.BlockID == "" {
			return "", fmt.Errorf("slot %s has no block to update", key)
		}
		if err := s.updateLink(bp, key); err != nil {
			return "", err
		}
		name, v := s.linkedVersion(bp, key)
		return fmt.Sprintf("Updated %s to v%d", name, v), nil
	})
}

// LinkBlock fills an empty slot with a library block at its current version.
func (s *Store) LinkBlock(id, key, blockID string) error {
	return s.change(id, "link", key, func(bp *Blueprint) (string, error) {
		sl := bp.slot(key)
		if sl == nil {
			return "", ErrNotFound
		}
		if sl.BlockID != "" {
			return "", fmt.Errorf("slot %s is already filled", key)
		}
		if err := s.link(sl, blockID); err != nil {
			return "", err
		}
		name, v := s.linkedVersion(bp, key)
		return fmt.Sprintf("Filled a slot with %s v%d", name, v), nil
	})
}

func (s *Store) link(sl *Slot, blockID string) error {
	b := s.blocks[blockID]
	if b == nil {
		return ErrNotFound
	}
	cv := b.CurrentVersion()
	if cv == nil {
		return fmt.Errorf("block %s has no published version", b.Slug)
	}
	sl.BlockID, sl.VersionID, sl.Overrides = b.ID, cv.ID, nil
	return nil
}

// Relink switches a filled slot to another block; its edits are dropped.
func (s *Store) Relink(id, key, blockID string) error {
	bp, err := s.blueprint(id)
	if err != nil {
		return err
	}
	before := s.slotName(bp, key)
	return s.change(id, "relink", key, func(bp *Blueprint) (string, error) {
		sl := bp.slot(key)
		if sl == nil {
			return "", ErrNotFound
		}
		if sl.BlockID != blockID {
			if err := s.link(sl, blockID); err != nil {
				return "", err
			}
		}
		name, v := s.linkedVersion(bp, key)
		return fmt.Sprintf("Switched %s to %s v%d", before, name, v), nil
	})
}

// Unlink empties a slot.
func (s *Store) Unlink(id, key string) error {
	bp, err := s.blueprint(id)
	if err != nil {
		return err
	}
	before := s.slotName(bp, key)
	return s.change(id, "unlink", key, func(bp *Blueprint) (string, error) {
		sl := bp.slot(key)
		if sl == nil {
			return "", ErrNotFound
		}
		sl.BlockID, sl.VersionID, sl.Overrides = "", "", nil
		return "Emptied the slot that held " + before, nil
	})
}

// StartCustomBlock fills an empty slot with a new block only this
// blueprint uses.
func (s *Store) StartCustomBlock(id, key string) error {
	return s.change(id, "custom-block", key, func(bp *Blueprint) (string, error) {
		sl := bp.slot(key)
		if sl == nil {
			return "", ErrNotFound
		}
		if sl.BlockID != "" {
			return "", fmt.Errorf("slot %s is already filled", key)
		}
		slug := Slugify(bp.Slug + "-" + sl.Key)
		for n := 2; s.blockSlugTaken(bp.TeamID, slug); n++ {
			slug = Slugify(fmt.Sprintf("%s-%s-%d", bp.Slug, sl.Key, n))
		}
		b, err := s.CreateBlock(BlockInput{TeamID: bp.TeamID, Type: slotType(*sl), Name: sl.Label, Slug: slug,
			Payload:     blocktypes.Starter(slotType(*sl), sl.Key),
			Description: "Made for " + bp.DisplayName + ".", Visibility: "private"})
		if err != nil {
			return "", err
		}
		if err := s.link(sl, b.ID); err != nil {
			return "", err
		}
		return "Started a new block, " + b.Name, nil
	})
}

// SaveAsBlock makes a library block from a slot's block plus its edits and
// links the slot to it.
func (s *Store) SaveAsBlock(id, key, name string) error {
	l, err := s.Load(id)
	if err != nil {
		return err
	}
	d := l.Slots[key]
	if d == nil {
		return ErrNotFound
	}
	if !d.linked {
		return fmt.Errorf("slot %s is empty", key)
	}
	before := d.BlockName
	return s.change(id, "save-as-block", key, func(bp *Blueprint) (string, error) {
		desc := fmt.Sprintf("Saved from %s, based on %s v%d.", l.Blueprint.DisplayName, d.BlockName, d.Version)
		if d.Origin == OriginCustom {
			desc = "Saved from " + l.Blueprint.DisplayName + "."
		}
		b, err := s.CreateBlock(BlockInput{TeamID: bp.TeamID, Type: d.BlockType, Name: name, Description: desc, Payload: jsonCopy(d.Effective)})
		if err != nil {
			return "", err
		}
		if err := s.link(bp.slot(key), b.ID); err != nil {
			return "", err
		}
		return fmt.Sprintf("Saved %s with its edits as a new block, %s", before, b.Name), nil
	})
}

// CheckInput is a new network check.
type CheckInput struct{ Label, Method, URL, Expect string }

// AddCheck appends a network check (not recorded in history, as before).
func (s *Store) AddCheck(id string, in CheckInput) error {
	bp, err := s.blueprint(id)
	if err != nil {
		return err
	}
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
	bp.Checks = append(bp.Checks, Check{ID: newID("chk_"), Label: in.Label, Type: "network", URL: in.URL, Method: in.Method, Expect: in.Expect})
	s.putBlueprint(bp)
	return nil
}

// RemoveCheck deletes one of a blueprint's checks.
func (s *Store) RemoveCheck(id, checkID string) error {
	bp, err := s.blueprint(id)
	if err != nil {
		return err
	}
	kept := bp.Checks[:0]
	for _, c := range bp.Checks {
		if c.ID != checkID {
			kept = append(kept, c)
		}
	}
	if len(kept) == len(bp.Checks) {
		return ErrNotFound
	}
	bp.Checks = kept
	s.putBlueprint(bp)
	return nil
}

// --- History ----------------------------------------------------------------

// RevisionInfo is one entry in a blueprint's history list.
type RevisionInfo struct {
	ID        string `json:"id"`
	Seq       int    `json:"seq"`
	Action    string `json:"action"`
	SlotKey   string `json:"slotKey,omitempty"`
	Summary   string `json:"summary"`
	Actor     string `json:"actor,omitempty"`
	CreatedAt string `json:"createdAt"`
	Slot      *Slot  `json:"slot,omitempty"`
	Current   bool   `json:"current,omitempty"`
}

// History lists a blueprint's revisions, newest first; with slotKey, only
// those that changed that slot (or the whole blueprint).
func (s *Store) History(id, slotKey string) []RevisionInfo {
	list := s.revisions[id]
	latest := 0
	for _, r := range list {
		latest = max(latest, r.Seq)
	}
	out := []RevisionInfo{}
	for i := len(list) - 1; i >= 0; i-- {
		r := list[i]
		if slotKey != "" && r.SlotKey != slotKey && !(r.SlotKey == "" && (r.Action == "restore" || r.Action == "baseline" || r.Action == "create")) {
			continue
		}
		ri := RevisionInfo{ID: r.ID, Seq: r.Seq, Action: r.Action, SlotKey: r.SlotKey, Summary: r.Summary, Actor: r.Actor,
			CreatedAt: r.CreatedAt.Format("2006-01-02T15:04:05.000Z07:00"), Current: r.Seq == latest}
		if slotKey != "" {
			for j := range r.Snapshot.Slots {
				if r.Snapshot.Slots[j].Key == slotKey {
					ri.Slot = &r.Snapshot.Slots[j]
				}
			}
		}
		out = append(out, ri)
	}
	return out
}

func (s *Store) revision(id, revID string) (*Revision, error) {
	for _, r := range s.revisions[id] {
		if r.ID == revID {
			return r, nil
		}
	}
	return nil, ErrNotFound
}

// RevisionYAML returns the YAML a blueprint compiled to at a revision.
func (s *Store) RevisionYAML(id, revID string) (string, error) {
	r, err := s.revision(id, revID)
	if err != nil {
		return "", err
	}
	return r.YAML, nil
}

// RestoreBlueprint puts the whole blueprint back the way it was at a revision.
func (s *Store) RestoreBlueprint(id, revID string) error {
	r, err := s.revision(id, revID)
	if err != nil {
		return err
	}
	return s.change(id, "restore", "", func(bp *Blueprint) (string, error) {
		for _, ss := range r.Snapshot.Slots {
			if err := s.checkSnapSlot(ss); err != nil {
				return "", err
			}
		}
		bp.Goal = r.Snapshot.Goal
		bp.Slots = append([]Slot(nil), r.Snapshot.Slots...)
		return fmt.Sprintf("Restored the blueprint to #%d (%s)", r.Seq, r.Summary), nil
	})
}

// RestoreSlot puts one slot back the way it was at a revision.
func (s *Store) RestoreSlot(id, revID, key string) error {
	r, err := s.revision(id, revID)
	if err != nil {
		return err
	}
	var ss *Slot
	for i := range r.Snapshot.Slots {
		if r.Snapshot.Slots[i].Key == key {
			ss = &r.Snapshot.Slots[i]
		}
	}
	if ss == nil {
		return fmt.Errorf("that point in history has no %s slot", key)
	}
	return s.change(id, "restore", key, func(bp *Blueprint) (string, error) {
		if err := s.checkSnapSlot(*ss); err != nil {
			return "", err
		}
		if sl := bp.slot(key); sl != nil {
			*sl = *ss
		} else {
			bp.Slots = append(bp.Slots, *ss)
		}
		what := ss.Label
		if ss.BlockName != "" {
			what = ss.BlockName
		}
		return fmt.Sprintf("Restored %s to #%d", what, r.Seq), nil
	})
}

func (s *Store) checkSnapSlot(ss Slot) error {
	if ss.BlockID == "" {
		return nil
	}
	if b := s.blocks[ss.BlockID]; b == nil || b.VersionByID(ss.VersionID) == nil {
		return errors.New("a block version this point in history used no longer exists")
	}
	return nil
}
