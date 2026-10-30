// Package lite is Kit Maker without a server: the blueprint engine (the
// compiler, patch ops, checks and conflicts from internal/blueprints) behind
// the same routes and Inertia props as internal/http, over plain documents
// instead of Postgres. cmd/kitwasm compiles it to WebAssembly for the
// Cloudflare edition (cloudflare/), where it runs in the browser and the
// documents live in D1 (or, with no backend, in the browser itself).
//
// A blueprint is one document: its header, goal, checks and slots, each slot
// holding its link (block, version, overrides) — the same shape as a history
// snapshot. Blocks are documents holding every version. Revisions are their
// own documents, loaded per blueprint when history needs them.
package lite

import (
	"crypto/rand"
	"encoding/json"
	"sort"
	"time"

	"github.com/oklog/ulid/v2"

	"github.com/docker/kit-maker/internal/blueprints/patch"
)

// Document kinds.
const (
	KindBlock     = "block"
	KindBlueprint = "blueprint"
	KindRevision  = "revision"
)

// Block is a library block with all its versions.
type Block struct {
	ID          string         `json:"id"`
	TeamID      string         `json:"teamId"`
	Type        string         `json:"type"`
	Slug        string         `json:"slug"`
	Name        string         `json:"name"`
	Description string         `json:"description"`
	Visibility  string         `json:"visibility"` // private | team | public
	Current     int            `json:"current"`    // current version number
	Versions    []BlockVersion `json:"versions"`   // oldest first
	CreatedAt   time.Time      `json:"createdAt"`
	UpdatedAt   time.Time      `json:"updatedAt"`
}

// BlockVersion is one immutable version of a block.
type BlockVersion struct {
	ID         string         `json:"id"`
	Version    int            `json:"version"`
	Payload    map[string]any `json:"payload"`
	Changelog  string         `json:"changelog"`
	AuthoredBy string         `json:"authoredBy"`
	By         string         `json:"by,omitempty"`
	CreatedAt  time.Time      `json:"createdAt"`
}

// CurrentVersion is the block's current version, or nil.
func (b *Block) CurrentVersion() *BlockVersion { return b.version(b.Current) }

func (b *Block) version(n int) *BlockVersion {
	for i := range b.Versions {
		if b.Versions[i].Version == n {
			return &b.Versions[i]
		}
	}
	return nil
}

// VersionByID finds a version by its ID.
func (b *Block) VersionByID(id string) *BlockVersion {
	for i := range b.Versions {
		if b.Versions[i].ID == id {
			return &b.Versions[i]
		}
	}
	return nil
}

// Blueprint is a blueprint document.
type Blueprint struct {
	ID          string    `json:"id"`
	TeamID      string    `json:"teamId"`
	Slug        string    `json:"slug"`
	DisplayName string    `json:"displayName"`
	Description string    `json:"description"`
	Kind        string    `json:"kind"`
	Goal        string    `json:"goal"`
	Slots       []Slot    `json:"slots"`
	Checks      []Check   `json:"checks"`
	RevSeq      int       `json:"revSeq"` // the newest revision's number
	CreatedAt   time.Time `json:"createdAt"`
	UpdatedAt   time.Time `json:"updatedAt"`
}

// Slot is one slot of a blueprint (a history SnapSlot plus nothing).
type Slot struct {
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

// Check is a network check on a blueprint.
type Check struct {
	ID     string `json:"id"`
	Label  string `json:"label"`
	Type   string `json:"type"`
	URL    string `json:"url"`
	Method string `json:"method"`
	Expect string `json:"expect"`
}

// Snapshot is a blueprint's restorable state, as stored in a revision.
type Snapshot struct {
	Goal  string `json:"goal"`
	Slots []Slot `json:"slots"`
}

// Revision is one recorded change to a blueprint.
type Revision struct {
	ID          string    `json:"id"`
	BlueprintID string    `json:"blueprintId"`
	Seq         int       `json:"seq"`
	Action      string    `json:"action"`
	SlotKey     string    `json:"slotKey,omitempty"`
	Summary     string    `json:"summary"`
	Actor       string    `json:"actor,omitempty"`
	Snapshot    Snapshot  `json:"snapshot"`
	YAML        string    `json:"yaml"`
	CreatedAt   time.Time `json:"createdAt"`
}

// Team is the team that owns everything in this edition.
type Team struct {
	ID   string `json:"id"`
	Name string `json:"name"`
}

// User is the person using the app.
type User struct {
	ID          string `json:"id"`
	Email       string `json:"email"`
	DisplayName string `json:"displayName"`
}

// Doc is a stored document.
type Doc struct {
	Kind   string          `json:"kind"`
	ID     string          `json:"id"`
	Parent string          `json:"parent,omitempty"` // a revision's blueprint
	Rev    int             `json:"rev"`              // the store's version of it, 0 for starter content
	Body   json.RawMessage `json:"body"`
}

// Store holds the documents in memory and remembers which changed.
type Store struct {
	Team Team
	// Actor is who is making changes (set per request).
	Actor      User
	blocks     map[string]*Block
	blueprints map[string]*Blueprint
	revisions  map[string][]*Revision // by blueprint, oldest first; nil = not loaded
	revs       map[string]int         // kind/id → stored rev
	dirty      map[string]Doc         // kind/id → doc to write
}

// NewStore makes an empty store for team.
func NewStore(team Team) *Store {
	return &Store{Team: team, blocks: map[string]*Block{}, blueprints: map[string]*Blueprint{},
		revisions: map[string][]*Revision{}, revs: map[string]int{}, dirty: map[string]Doc{}}
}

// LoadDocs adds or replaces documents (starter content, then stored ones).
// Revisions add to their blueprint's history.
func (s *Store) LoadDocs(docs []Doc) error {
	for _, d := range docs {
		s.revs[d.Kind+"/"+d.ID] = d.Rev
		switch d.Kind {
		case KindBlock:
			var b Block
			if err := json.Unmarshal(d.Body, &b); err != nil {
				return err
			}
			s.blocks[b.ID] = &b
		case KindBlueprint:
			var bp Blueprint
			if err := json.Unmarshal(d.Body, &bp); err != nil {
				return err
			}
			s.blueprints[bp.ID] = &bp
		case KindRevision:
			var r Revision
			if err := json.Unmarshal(d.Body, &r); err != nil {
				return err
			}
			s.addRevision(&r)
		}
	}
	return nil
}

// LoadRevisions sets a blueprint's history (all of it).
func (s *Store) LoadRevisions(blueprintID string, docs []Doc) error {
	s.revisions[blueprintID] = []*Revision{}
	return s.LoadDocs(docs)
}

// RevisionsLoaded reports whether a blueprint's history is in memory.
func (s *Store) RevisionsLoaded(blueprintID string) bool { return s.revisions[blueprintID] != nil }

func (s *Store) addRevision(r *Revision) {
	list := s.revisions[r.BlueprintID]
	for i, x := range list {
		if x.ID == r.ID {
			list[i] = r
			return
		}
	}
	list = append(list, r)
	sort.Slice(list, func(i, j int) bool { return list[i].Seq < list[j].Seq })
	s.revisions[r.BlueprintID] = list
}

// TakeWrites returns the changed documents and forgets them.
func (s *Store) TakeWrites() []Doc {
	out := make([]Doc, 0, len(s.dirty))
	for _, d := range s.dirty {
		out = append(out, d)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Kind+out[i].ID < out[j].Kind+out[j].ID })
	s.dirty = map[string]Doc{}
	return out
}

// Saved records the revs the backend gave written documents.
func (s *Store) Saved(revs map[string]int) {
	for k, v := range revs {
		s.revs[k] = v
	}
}

func (s *Store) mark(kind, id, parent string, v any) {
	b, _ := json.Marshal(v)
	key := kind + "/" + id
	s.dirty[key] = Doc{Kind: kind, ID: id, Parent: parent, Rev: s.revs[key], Body: b}
}

func (s *Store) putBlock(b *Block) {
	b.UpdatedAt = now()
	s.blocks[b.ID] = b
	s.mark(KindBlock, b.ID, "", b)
}

func (s *Store) putBlueprint(bp *Blueprint) {
	bp.UpdatedAt = now()
	s.blueprints[bp.ID] = bp
	s.mark(KindBlueprint, bp.ID, "", bp)
}

func (s *Store) putRevision(r *Revision) {
	s.addRevision(r)
	s.mark(KindRevision, r.ID, r.BlueprintID, r)
}

func newID(prefix string) string {
	return prefix + ulid.MustNew(ulid.Timestamp(time.Now()), rand.Reader).String()
}

// now is replaceable in tests.
var now = func() time.Time { return time.Now().UTC() }
