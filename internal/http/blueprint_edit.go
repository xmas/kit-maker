package http

import (
	"errors"
	"fmt"
	nethttp "net/http"
	"strings"

	"github.com/go-chi/chi/v5"
	inertia "github.com/romsar/gonertia/v2"

	"github.com/docker/kit-maker/internal/auth"
	"github.com/docker/kit-maker/internal/authz"
	"github.com/docker/kit-maker/internal/blueprints"
	"github.com/docker/kit-maker/internal/blueprints/blocktypes"
	"github.com/docker/kit-maker/internal/ent/team"
)

// back redirects to `to` with err as the validation error under key.
func (s *server) back(w nethttp.ResponseWriter, r *nethttp.Request, to, key string, err error) {
	if errors.Is(err, blueprints.ErrNotFound) {
		nethttp.NotFound(w, r)
		return
	}
	ctx := inertia.SetValidationErrors(r.Context(), inertia.ValidationErrors{key: err.Error()})
	s.inertia.Redirect(w, r.WithContext(ctx), to)
}

type teamOption struct {
	ID   string `json:"id"`
	Name string `json:"name"`
}

// teamsWith lists the teams where the signed-in user holds perm.
func (s *server) teamsWith(r *nethttp.Request, perm authz.Key) ([]teamOption, error) {
	ids, all := authz.SubjectFrom(r.Context()).TeamsWith(perm)
	q := s.Ent.Team.Query().Order(team.ByName())
	if !all {
		q = q.Where(team.IDIn(ids...))
	}
	ts, err := q.All(r.Context())
	if err != nil {
		return nil, err
	}
	out := make([]teamOption, 0, len(ts))
	for _, t := range ts {
		out = append(out, teamOption{ID: t.ID, Name: t.Name})
	}
	return out, nil
}

// blueprintNew renders the new-blueprint form.
func (s *server) blueprintNew(w nethttp.ResponseWriter, r *nethttp.Request) {
	teams, err := s.teamsWith(r, authz.BlueprintCreate)
	if err != nil {
		s.fail(w, r, "list teams", err)
		return
	}
	s.render(w, r, "Blueprints/New", inertia.Props{"teams": teams})
}

type blueprintForm struct {
	TeamID      string `json:"teamId"`
	DisplayName string `json:"displayName"`
	Slug        string `json:"slug"`
	Kind        string `json:"kind"`
	Description string `json:"description"`
	Goal        string `json:"goal"`
}

// blueprintCreate makes a blueprint and opens it in the builder.
func (s *server) blueprintCreate(w nethttp.ResponseWriter, r *nethttp.Request) {
	var f blueprintForm
	if err := decodeForm(r, &f); err != nil {
		nethttp.Error(w, "bad request", nethttp.StatusBadRequest)
		return
	}
	if f.TeamID == "" {
		s.back(w, r, "/blueprints/new", "teamId", errors.New("pick the team that owns this blueprint"))
		return
	}
	if !s.allowed(w, r, authz.BlueprintCreate, f.TeamID) {
		return
	}
	k, err := s.blueprints().Create(r.Context(), blueprints.CreateInput{
		TeamID: f.TeamID, DisplayName: f.DisplayName, Slug: f.Slug, Kind: f.Kind, Description: f.Description, Goal: f.Goal,
	})
	if err != nil {
		s.back(w, r, "/blueprints/new", "form", err)
		return
	}
	s.inertia.Redirect(w, r, "/blueprints/"+k.ID)
}

type goalForm struct {
	Goal string `json:"goal"`
}

// blueprintGoal updates the goal statement.
func (s *server) blueprintGoal(w nethttp.ResponseWriter, r *nethttp.Request) {
	blueprintID := chi.URLParam(r, "blueprintID")
	if _, ok := s.loadBlueprint(w, r, blueprintID, authz.BlueprintEdit); !ok {
		return
	}
	var f goalForm
	if err := decodeForm(r, &f); err != nil {
		nethttp.Error(w, "bad request", nethttp.StatusBadRequest)
		return
	}
	if err := s.blueprints().UpdateGoal(r.Context(), blueprintID, f.Goal); err != nil {
		s.back(w, r, "/blueprints/"+blueprintID, "goal", err)
		return
	}
	s.inertia.Redirect(w, r, "/blueprints/"+blueprintID)
}

type addSlotForm struct {
	Label    string `json:"label"`
	Type     string `json:"type"`
	Required bool   `json:"required"`
}

// slotAdd adds a blank slot and selects it.
func (s *server) slotAdd(w nethttp.ResponseWriter, r *nethttp.Request) {
	blueprintID := chi.URLParam(r, "blueprintID")
	if _, ok := s.loadBlueprint(w, r, blueprintID, authz.BlueprintEdit); !ok {
		return
	}
	var f addSlotForm
	if err := decodeForm(r, &f); err != nil {
		nethttp.Error(w, "bad request", nethttp.StatusBadRequest)
		return
	}
	if _, ok := blocktypes.Get(f.Type); !ok {
		s.back(w, r, "/blueprints/"+blueprintID, "slot", errors.New("pick what kind of block goes in the slot"))
		return
	}
	key, err := s.blueprints().AddSlot(r.Context(), blueprintID, f.Label, f.Type, f.Required)
	if err != nil {
		s.back(w, r, "/blueprints/"+blueprintID, "slot", err)
		return
	}
	s.inertia.Redirect(w, r, slotURL(blueprintID, key))
}

// slotRemove deletes a slot.
func (s *server) slotRemove(w nethttp.ResponseWriter, r *nethttp.Request) {
	blueprintID, slotKey := chi.URLParam(r, "blueprintID"), chi.URLParam(r, "slotKey")
	if _, ok := s.loadBlueprint(w, r, blueprintID, authz.BlueprintEdit); !ok {
		return
	}
	if err := s.blueprints().RemoveSlot(r.Context(), blueprintID, slotKey); err != nil {
		s.back(w, r, slotURL(blueprintID, slotKey), "payload", err)
		return
	}
	s.inertia.Redirect(w, r, "/blueprints/"+blueprintID)
}

// slotCustom fills a blank with a new block private to this blueprint.
func (s *server) slotCustom(w nethttp.ResponseWriter, r *nethttp.Request) {
	blueprintID, slotKey := chi.URLParam(r, "blueprintID"), chi.URLParam(r, "slotKey")
	l, ok := s.loadBlueprint(w, r, blueprintID, authz.BlueprintEdit)
	if !ok || !s.allowed(w, r, authz.BlockCreate, l.Blueprint.TeamID) {
		return
	}
	var userID string
	if u := auth.UserFrom(r.Context()); u != nil {
		userID = u.ID
	}
	var f struct {
		Prompt string `json:"prompt"` // optional: draft the new block with AI
	}
	_ = decodeForm(r, &f)
	if err := s.blueprints().StartCustomBlock(r.Context(), blueprintID, slotKey, userID); err != nil {
		s.back(w, r, slotURL(blueprintID, slotKey), "payload", err)
		return
	}
	if strings.TrimSpace(f.Prompt) != "" && authz.SubjectFrom(r.Context()).Can(authz.AIUse, l.Blueprint.TeamID) {
		// The block exists either way; a failed draft leaves the starter.
		if l, err := s.blueprints().Load(r.Context(), blueprintID); err == nil && l.Slots[slotKey] != nil {
			payload, note, err := s.draft(r, l, l.Slots[slotKey], f.Prompt)
			switch {
			case err != nil:
				s.back(w, r, slotURL(blueprintID, slotKey), "payload", fmt.Errorf("started an empty block; the AI draft failed: %w", err))
				return
			case payload == nil:
				s.back(w, r, slotURL(blueprintID, slotKey), "payload", fmt.Errorf("started an empty block. %s", note))
				return
			}
			if _, err := s.blueprints().SaveSlot(r.Context(), blueprintID, slotKey, payload); err != nil {
				s.back(w, r, slotURL(blueprintID, slotKey), "payload", err)
				return
			}
		}
	}
	s.inertia.Redirect(w, r, slotURL(blueprintID, slotKey))
}

// slotUpdate moves a slot to its block's latest version.
func (s *server) slotUpdate(w nethttp.ResponseWriter, r *nethttp.Request) {
	blueprintID, slotKey := chi.URLParam(r, "blueprintID"), chi.URLParam(r, "slotKey")
	if _, ok := s.loadBlueprint(w, r, blueprintID, authz.LinkRebaseApply); !ok {
		return
	}
	if err := s.blueprints().UpdateLink(r.Context(), blueprintID, slotKey); err != nil {
		s.back(w, r, slotURL(blueprintID, slotKey), "payload", err)
		return
	}
	s.inertia.Redirect(w, r, slotURL(blueprintID, slotKey))
}

type checkForm struct {
	Label  string `json:"label"`
	Method string `json:"method"`
	URL    string `json:"url"`
	Expect string `json:"expect"`
}

// checkAdd adds a network check.
func (s *server) checkAdd(w nethttp.ResponseWriter, r *nethttp.Request) {
	blueprintID := chi.URLParam(r, "blueprintID")
	if _, ok := s.loadBlueprint(w, r, blueprintID, authz.BlueprintChecksEdit); !ok {
		return
	}
	var f checkForm
	if err := decodeForm(r, &f); err != nil {
		nethttp.Error(w, "bad request", nethttp.StatusBadRequest)
		return
	}
	err := s.blueprints().AddCheck(r.Context(), blueprintID, blueprints.CheckInput{Label: f.Label, Method: f.Method, URL: f.URL, Expect: f.Expect})
	if err != nil {
		s.back(w, r, "/blueprints/"+blueprintID, "check", err)
		return
	}
	s.inertia.Redirect(w, r, "/blueprints/"+blueprintID)
}

// checkRemove deletes a check.
func (s *server) checkRemove(w nethttp.ResponseWriter, r *nethttp.Request) {
	blueprintID := chi.URLParam(r, "blueprintID")
	if _, ok := s.loadBlueprint(w, r, blueprintID, authz.BlueprintChecksEdit); !ok {
		return
	}
	if err := s.blueprints().RemoveCheck(r.Context(), blueprintID, chi.URLParam(r, "checkID")); err != nil {
		s.back(w, r, "/blueprints/"+blueprintID, "check", err)
		return
	}
	s.inertia.Redirect(w, r, "/blueprints/"+blueprintID)
}

type relinkForm struct {
	BlockID string `json:"blockId"`
	Name    string `json:"name"`
}

// slotRelink points a filled slot at a different library block.
func (s *server) slotRelink(w nethttp.ResponseWriter, r *nethttp.Request) {
	blueprintID, slotKey := chi.URLParam(r, "blueprintID"), chi.URLParam(r, "slotKey")
	l, ok := s.loadBlueprint(w, r, blueprintID, authz.BlueprintEdit)
	if !ok || !s.allowed(w, r, authz.BlockUse, l.Blueprint.TeamID) {
		return
	}
	var f relinkForm
	if err := decodeForm(r, &f); err != nil || f.BlockID == "" {
		nethttp.Error(w, "bad request", nethttp.StatusBadRequest)
		return
	}
	if err := s.usableBlock(r, f.BlockID); err != nil {
		s.back(w, r, slotURL(blueprintID, slotKey), "payload", err)
		return
	}
	if err := s.blueprints().Relink(r.Context(), blueprintID, slotKey, f.BlockID); err != nil {
		s.back(w, r, slotURL(blueprintID, slotKey), "payload", err)
		return
	}
	s.inertia.Redirect(w, r, slotURL(blueprintID, slotKey))
}

// slotSaveAsBlock saves a slot's block plus its edits as a new library block.
func (s *server) slotSaveAsBlock(w nethttp.ResponseWriter, r *nethttp.Request) {
	blueprintID, slotKey := chi.URLParam(r, "blueprintID"), chi.URLParam(r, "slotKey")
	l, ok := s.loadBlueprint(w, r, blueprintID, authz.BlueprintEdit)
	if !ok || !s.allowed(w, r, authz.BlockCreate, l.Blueprint.TeamID) {
		return
	}
	var f relinkForm
	if err := decodeForm(r, &f); err != nil {
		nethttp.Error(w, "bad request", nethttp.StatusBadRequest)
		return
	}
	if _, err := s.blueprints().SaveAsBlock(r.Context(), blueprintID, slotKey, f.Name, userID(r)); err != nil {
		s.back(w, r, slotURL(blueprintID, slotKey), "saveAs", err)
		return
	}
	s.inertia.Redirect(w, r, slotURL(blueprintID, slotKey))
}
