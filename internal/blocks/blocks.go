// Package blocks manages the block library: creating blocks, editing their
// details and publishing immutable versions (DESIGN.md §2.1–2.2). Blueprints link
// a block version and keep their own edits as overrides (internal/blueprints).
package blocks

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"regexp"
	"sort"
	"strings"
	"time"

	"github.com/docker/kit-maker/internal/blueprints/blocktypes"
	"github.com/docker/kit-maker/internal/blueprints/patch"
	"github.com/docker/kit-maker/internal/ent"
	"github.com/docker/kit-maker/internal/ent/block"
	"github.com/docker/kit-maker/internal/ent/blocklink"
	"github.com/docker/kit-maker/internal/ent/blockversion"
)

// SpecVersion is the sandbox-kit-spec release new versions are written for.
const SpecVersion = "v3.0.0-m.8"

// ErrNotFound means the block doesn't exist.
var ErrNotFound = errors.New("block not found")

var slugRE = regexp.MustCompile(`^[a-z0-9]([a-z0-9-]{0,62}[a-z0-9])?$`)

// Slugify turns a name into a lowercase-kebab slug.
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

// PayloadHash is the stable hash stored on a version (map keys marshal sorted).
func PayloadHash(p map[string]any) string {
	b, _ := json.Marshal(p)
	sum := sha256.Sum256(b)
	return "sha256:" + hex.EncodeToString(sum[:])
}

// Service reads and writes library blocks.
type Service struct {
	Ent *ent.Client
}

// CreateInput is a new block. Payload is optional: the type's starter is
// used when it's nil.
type CreateInput struct {
	TeamID      string
	Type        string
	Name        string
	Slug        string
	Description string
	Visibility  block.Visibility
	Payload     map[string]any
	UserID      string
}

// Create adds a block with version 1.
func (s *Service) Create(ctx context.Context, in CreateInput) (*ent.Block, error) {
	return create(ctx, s.Ent, in)
}

// Create is Service.Create for callers holding a client.
func Create(ctx context.Context, c *ent.Client, in CreateInput) (*ent.Block, error) {
	return create(ctx, c, in)
}

// CreateTx is Create inside the caller's transaction.
func CreateTx(ctx context.Context, tx *ent.Tx, in CreateInput) (*ent.Block, error) {
	return create(ctx, tx.Client(), in)
}

func create(ctx context.Context, c *ent.Client, in CreateInput) (*ent.Block, error) {
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
		in.Visibility = block.VisibilityTeam
	}
	taken, err := c.Block.Query().Where(block.TeamID(in.TeamID), block.Slug(in.Slug)).Exist(ctx)
	if err != nil {
		return nil, err
	}
	if taken {
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
	b, err := c.Block.Create().SetTeamID(in.TeamID).SetType(in.Type).SetSlug(in.Slug).SetName(in.Name).
		SetDescription(strings.TrimSpace(in.Description)).SetVisibility(in.Visibility).Save(ctx)
	if err != nil {
		return nil, err
	}
	v, err := newVersion(ctx, c, b.ID, 1, payload, "First version.", in.UserID)
	if err != nil {
		return nil, err
	}
	return b.Update().SetCurrentVersion(v).Save(ctx)
}

func newVersion(ctx context.Context, c *ent.Client, blockID string, n int, payload map[string]any, changelog, userID string) (*ent.BlockVersion, error) {
	q := c.BlockVersion.Create().SetBlockID(blockID).SetVersion(n).SetPayload(payload).
		SetPayloadHash(PayloadHash(payload)).SetSchemaVersion(SpecVersion).
		SetChangelog(strings.TrimSpace(changelog)).SetAuthoredBy(blockversion.AuthoredByUser)
	if userID != "" {
		q.SetAcceptedByUserID(userID)
	}
	return q.Save(ctx)
}

// Publish appends a version when the payload differs from the current one.
// It reports whether a version was created.
func (s *Service) Publish(ctx context.Context, blockID string, payload map[string]any, changelog, userID string) (*ent.BlockVersion, bool, error) {
	b, err := s.Ent.Block.Query().Where(block.ID(blockID)).WithCurrentVersion().Only(ctx)
	if ent.IsNotFound(err) {
		return nil, false, ErrNotFound
	}
	if err != nil {
		return nil, false, err
	}
	t, ok := blocktypes.Get(b.Type)
	if !ok {
		return nil, false, fmt.Errorf("unknown block type %q", b.Type)
	}
	patch.AssignKeys(payload)
	if err := t.Validate(payload); err != nil {
		return nil, false, err
	}
	// Item keys are bookkeeping, so a save that only differs in _k (a client
	// that dropped them) is not a new version.
	if cv := b.Edges.CurrentVersion; cv != nil && sameContent(cv.Payload, payload) {
		return cv, false, nil
	}
	tx, err := s.Ent.Tx(ctx)
	if err != nil {
		return nil, false, err
	}
	n, err := tx.BlockVersion.Query().Where(blockversion.BlockID(blockID)).Count(ctx)
	if err != nil {
		return nil, false, rollback(tx, err)
	}
	if changelog == "" {
		changelog = "Edited in the library."
	}
	// The unique (block_id, version) index stops two saves taking one number.
	v, err := newVersion(ctx, tx.Client(), blockID, n+1, payload, changelog, userID)
	if ent.IsConstraintError(err) {
		return nil, false, rollback(tx, errors.New("someone else saved this block just now: reload and try again"))
	}
	if err != nil {
		return nil, false, rollback(tx, err)
	}
	if err := tx.Block.UpdateOneID(blockID).SetCurrentVersion(v).Exec(ctx); err != nil {
		return nil, false, rollback(tx, err)
	}
	return v, true, tx.Commit()
}

func sameContent(a, b map[string]any) bool {
	x, _ := json.Marshal(patch.Strip(a))
	y, _ := json.Marshal(patch.Strip(b))
	return string(x) == string(y)
}

func rollback(tx *ent.Tx, err error) error {
	_ = tx.Rollback()
	return err
}

// UpdateDetails changes a block's name and description (not versioned).
func (s *Service) UpdateDetails(ctx context.Context, blockID, name, description string) error {
	name = strings.TrimSpace(name)
	if name == "" {
		return errors.New("give the block a name")
	}
	err := s.Ent.Block.UpdateOneID(blockID).SetName(name).SetDescription(strings.TrimSpace(description)).Exec(ctx)
	if ent.IsNotFound(err) {
		return ErrNotFound
	}
	return err
}

// Summary is one row of the library.
type Summary struct {
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
	// Abilities are what the current version lets a sandbox do
	// (blocktypes.Abilities keys), for filtering the library.
	Abilities []string `json:"abilities"`
	// Text is the current version's values (hosts, commands, services…),
	// so search finds a block by what's in it.
	Text string `json:"text"`
}

func summarize(b *ent.Block) Summary {
	s := Summary{ID: b.ID, Name: b.Name, Slug: b.Slug, Type: b.Type, Description: b.Description,
		TeamID: b.TeamID, Visibility: string(b.Visibility), UsedBy: len(b.Edges.Links), UpdatedAt: b.UpdatedAt}
	if t, ok := blocktypes.Get(b.Type); ok {
		s.TypeLabel, s.Section = t.Label, t.Section
	}
	if b.Edges.Team != nil {
		s.Team = b.Edges.Team.Name
	}
	if cv := b.Edges.CurrentVersion; cv != nil {
		s.Version = cv.Version
		s.Abilities = blocktypes.AbilitiesOf(b.Type, cv.Payload)
		s.Text = SearchText(cv.Payload)
		if cv.CreatedAt.After(s.UpdatedAt) {
			s.UpdatedAt = cv.CreatedAt
		}
	}
	return s
}

// List returns the shared blocks owned by teamIDs (or every team when all).
// Private blocks belong to one blueprint and show there instead.
func (s *Service) List(ctx context.Context, teamIDs []string, all bool) ([]Summary, error) {
	q := s.Ent.Block.Query().Where(block.VisibilityNEQ(block.VisibilityPrivate)).
		WithTeam().WithCurrentVersion().WithLinks().Order(ent.Asc(block.FieldName))
	if !all {
		q = q.Where(block.TeamIDIn(teamIDs...))
	}
	bs, err := q.All(ctx)
	if err != nil {
		return nil, err
	}
	out := make([]Summary, 0, len(bs))
	for _, b := range bs {
		out = append(out, summarize(b))
	}
	return out, nil
}

// Version is one entry in a block's history.
type Version struct {
	Version    int       `json:"version"`
	Changelog  string    `json:"changelog"`
	AuthoredBy string    `json:"authoredBy"`
	By         string    `json:"by,omitempty"`
	CreatedAt  time.Time `json:"createdAt"`
}

// Use is one blueprint slot linking the block.
type Use struct {
	BlueprintID string `json:"blueprintId"`
	Blueprint   string `json:"blueprint"`
	SlotKey     string `json:"slotKey"`
	SlotLabel   string `json:"slotLabel"`
	Version     int    `json:"version"`
	Edits       int    `json:"edits"`
}

// Detail is a block with its current payload, history and uses.
type Detail struct {
	Summary
	Payload  map[string]any `json:"payload"`
	Versions []Version      `json:"versions"`
	UsedIn   []Use          `json:"usedIn"`
}

// Get loads one block.
func (s *Service) Get(ctx context.Context, id string) (*Detail, error) {
	b, err := s.Ent.Block.Query().Where(block.ID(id)).WithTeam().WithCurrentVersion().WithLinks().
		WithVersions(func(q *ent.BlockVersionQuery) {
			q.Order(ent.Desc(blockversion.FieldVersion)).WithAcceptedBy()
		}).Only(ctx)
	if ent.IsNotFound(err) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	d := &Detail{Summary: summarize(b), Payload: map[string]any{}}
	if cv := b.Edges.CurrentVersion; cv != nil {
		d.Payload = cv.Payload
	}
	for _, v := range b.Edges.Versions {
		e := Version{Version: v.Version, Changelog: v.Changelog, AuthoredBy: string(v.AuthoredBy), CreatedAt: v.CreatedAt}
		if u := v.Edges.AcceptedBy; u != nil {
			e.By = u.DisplayName
			if e.By == "" {
				e.By = u.Email
			}
		}
		d.Versions = append(d.Versions, e)
	}
	links, err := s.Ent.BlockLink.Query().Where(blocklink.BlockID(id)).
		WithBaseVersion().WithSlot(func(q *ent.SlotQuery) { q.WithBlueprint() }).All(ctx)
	if err != nil {
		return nil, err
	}
	for _, l := range links {
		sl := l.Edges.Slot
		if sl == nil || sl.Edges.Blueprint == nil {
			continue
		}
		u := Use{BlueprintID: sl.BlueprintID, Blueprint: sl.Edges.Blueprint.DisplayName, SlotKey: sl.Key, SlotLabel: sl.Label, Edits: len(l.Overrides)}
		if l.Edges.BaseVersion != nil {
			u.Version = l.Edges.BaseVersion.Version
		}
		d.UsedIn = append(d.UsedIn, u)
	}
	return d, nil
}

// TeamOf returns a block's owner team (for permission checks).
func (s *Service) TeamOf(ctx context.Context, id string) (string, error) {
	b, err := s.Ent.Block.Query().Where(block.ID(id)).Select(block.FieldTeamID).Only(ctx)
	if ent.IsNotFound(err) {
		return "", ErrNotFound
	}
	if err != nil {
		return "", err
	}
	return b.TeamID, nil
}

// RestoreVersion publishes an earlier version's payload as the newest
// version, so blueprints following the block can take it like any update.
func (s *Service) RestoreVersion(ctx context.Context, blockID string, version int, userID string) (*ent.BlockVersion, error) {
	old, err := s.Ent.BlockVersion.Query().Where(blockversion.BlockID(blockID), blockversion.Version(version)).Only(ctx)
	if ent.IsNotFound(err) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	v, created, err := s.Publish(ctx, blockID, old.Payload, fmt.Sprintf("Restored v%d.", version), userID)
	if err != nil {
		return nil, err
	}
	if !created {
		return nil, fmt.Errorf("v%d is the same as the current version", version)
	}
	return v, nil
}

// SearchText flattens a payload's values (not item keys) into one string
// for search, capped so a large Dockerfile doesn't swamp the page.
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
	s := b.String()
	if len(s) > 4000 {
		s = s[:4000]
	}
	return strings.TrimSpace(s)
}
