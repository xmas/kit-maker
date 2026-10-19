package http

import (
	"errors"
	nethttp "net/http"

	"github.com/go-chi/chi/v5"
	inertia "github.com/romsar/gonertia/v2"

	"github.com/docker/kit-maker/internal/auth"
	"github.com/docker/kit-maker/internal/authz"
	"github.com/docker/kit-maker/internal/ent/team"
	"github.com/docker/kit-maker/internal/kits"
	"github.com/docker/kit-maker/internal/kits/blocktypes"
)

// back redirects to `to` with err as the validation error under key.
func (s *server) back(w nethttp.ResponseWriter, r *nethttp.Request, to, key string, err error) {
	if errors.Is(err, kits.ErrNotFound) {
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

// kitNew renders the new-kit form.
func (s *server) kitNew(w nethttp.ResponseWriter, r *nethttp.Request) {
	teams, err := s.teamsWith(r, authz.KitCreate)
	if err != nil {
		s.fail(w, r, "list teams", err)
		return
	}
	s.render(w, r, "Kits/New", inertia.Props{"teams": teams})
}

type kitForm struct {
	TeamID      string `json:"teamId"`
	DisplayName string `json:"displayName"`
	Slug        string `json:"slug"`
	Kind        string `json:"kind"`
	Description string `json:"description"`
	Goal        string `json:"goal"`
}

// kitCreate makes a kit and opens it in the builder.
func (s *server) kitCreate(w nethttp.ResponseWriter, r *nethttp.Request) {
	var f kitForm
	if err := decodeForm(r, &f); err != nil {
		nethttp.Error(w, "bad request", nethttp.StatusBadRequest)
		return
	}
	if f.TeamID == "" {
		s.back(w, r, "/kits/new", "teamId", errors.New("pick the team that owns this kit"))
		return
	}
	if !s.allowed(w, r, authz.KitCreate, f.TeamID) {
		return
	}
	k, err := s.kits().Create(r.Context(), kits.CreateInput{
		TeamID: f.TeamID, DisplayName: f.DisplayName, Slug: f.Slug, Kind: f.Kind, Description: f.Description, Goal: f.Goal,
	})
	if err != nil {
		s.back(w, r, "/kits/new", "form", err)
		return
	}
	s.inertia.Redirect(w, r, "/kits/"+k.ID)
}

type goalForm struct {
	Goal string `json:"goal"`
}

// kitGoal updates the goal statement.
func (s *server) kitGoal(w nethttp.ResponseWriter, r *nethttp.Request) {
	kitID := chi.URLParam(r, "kitID")
	if _, ok := s.loadKit(w, r, kitID, authz.KitEdit); !ok {
		return
	}
	var f goalForm
	if err := decodeForm(r, &f); err != nil {
		nethttp.Error(w, "bad request", nethttp.StatusBadRequest)
		return
	}
	if err := s.kits().UpdateGoal(r.Context(), kitID, f.Goal); err != nil {
		s.back(w, r, "/kits/"+kitID, "goal", err)
		return
	}
	s.inertia.Redirect(w, r, "/kits/"+kitID)
}

type addSlotForm struct {
	Label    string `json:"label"`
	Type     string `json:"type"`
	Required bool   `json:"required"`
}

// slotAdd adds a blank slot and selects it.
func (s *server) slotAdd(w nethttp.ResponseWriter, r *nethttp.Request) {
	kitID := chi.URLParam(r, "kitID")
	if _, ok := s.loadKit(w, r, kitID, authz.KitEdit); !ok {
		return
	}
	var f addSlotForm
	if err := decodeForm(r, &f); err != nil {
		nethttp.Error(w, "bad request", nethttp.StatusBadRequest)
		return
	}
	if _, ok := blocktypes.Get(f.Type); !ok {
		s.back(w, r, "/kits/"+kitID, "slot", errors.New("pick what kind of block goes in the slot"))
		return
	}
	key, err := s.kits().AddSlot(r.Context(), kitID, f.Label, f.Type, f.Required)
	if err != nil {
		s.back(w, r, "/kits/"+kitID, "slot", err)
		return
	}
	s.inertia.Redirect(w, r, slotURL(kitID, key))
}

// slotRemove deletes a slot.
func (s *server) slotRemove(w nethttp.ResponseWriter, r *nethttp.Request) {
	kitID, slotKey := chi.URLParam(r, "kitID"), chi.URLParam(r, "slotKey")
	if _, ok := s.loadKit(w, r, kitID, authz.KitEdit); !ok {
		return
	}
	if err := s.kits().RemoveSlot(r.Context(), kitID, slotKey); err != nil {
		s.back(w, r, slotURL(kitID, slotKey), "payload", err)
		return
	}
	s.inertia.Redirect(w, r, "/kits/"+kitID)
}

// slotCustom fills a blank with a new block private to this kit.
func (s *server) slotCustom(w nethttp.ResponseWriter, r *nethttp.Request) {
	kitID, slotKey := chi.URLParam(r, "kitID"), chi.URLParam(r, "slotKey")
	l, ok := s.loadKit(w, r, kitID, authz.KitEdit)
	if !ok || !s.allowed(w, r, authz.BlockCreate, l.Kit.TeamID) {
		return
	}
	var userID string
	if u := auth.UserFrom(r.Context()); u != nil {
		userID = u.ID
	}
	if err := s.kits().StartCustomBlock(r.Context(), kitID, slotKey, userID); err != nil {
		s.back(w, r, slotURL(kitID, slotKey), "payload", err)
		return
	}
	s.inertia.Redirect(w, r, slotURL(kitID, slotKey))
}

// slotUpdate moves a slot to its block's latest version.
func (s *server) slotUpdate(w nethttp.ResponseWriter, r *nethttp.Request) {
	kitID, slotKey := chi.URLParam(r, "kitID"), chi.URLParam(r, "slotKey")
	if _, ok := s.loadKit(w, r, kitID, authz.LinkRebaseApply); !ok {
		return
	}
	if err := s.kits().UpdateLink(r.Context(), kitID, slotKey); err != nil {
		s.back(w, r, slotURL(kitID, slotKey), "payload", err)
		return
	}
	s.inertia.Redirect(w, r, slotURL(kitID, slotKey))
}

type checkForm struct {
	Label  string `json:"label"`
	Method string `json:"method"`
	URL    string `json:"url"`
	Expect string `json:"expect"`
}

// checkAdd adds a network check.
func (s *server) checkAdd(w nethttp.ResponseWriter, r *nethttp.Request) {
	kitID := chi.URLParam(r, "kitID")
	if _, ok := s.loadKit(w, r, kitID, authz.KitChecksEdit); !ok {
		return
	}
	var f checkForm
	if err := decodeForm(r, &f); err != nil {
		nethttp.Error(w, "bad request", nethttp.StatusBadRequest)
		return
	}
	err := s.kits().AddCheck(r.Context(), kitID, kits.CheckInput{Label: f.Label, Method: f.Method, URL: f.URL, Expect: f.Expect})
	if err != nil {
		s.back(w, r, "/kits/"+kitID, "check", err)
		return
	}
	s.inertia.Redirect(w, r, "/kits/"+kitID)
}

// checkRemove deletes a check.
func (s *server) checkRemove(w nethttp.ResponseWriter, r *nethttp.Request) {
	kitID := chi.URLParam(r, "kitID")
	if _, ok := s.loadKit(w, r, kitID, authz.KitChecksEdit); !ok {
		return
	}
	if err := s.kits().RemoveCheck(r.Context(), kitID, chi.URLParam(r, "checkID")); err != nil {
		s.back(w, r, "/kits/"+kitID, "check", err)
		return
	}
	s.inertia.Redirect(w, r, "/kits/"+kitID)
}

type relinkForm struct {
	BlockID string `json:"blockId"`
	Name    string `json:"name"`
}

// slotRelink points a filled slot at a different library block.
func (s *server) slotRelink(w nethttp.ResponseWriter, r *nethttp.Request) {
	kitID, slotKey := chi.URLParam(r, "kitID"), chi.URLParam(r, "slotKey")
	l, ok := s.loadKit(w, r, kitID, authz.KitEdit)
	if !ok || !s.allowed(w, r, authz.BlockUse, l.Kit.TeamID) {
		return
	}
	var f relinkForm
	if err := decodeForm(r, &f); err != nil || f.BlockID == "" {
		nethttp.Error(w, "bad request", nethttp.StatusBadRequest)
		return
	}
	if err := s.kits().Relink(r.Context(), kitID, slotKey, f.BlockID); err != nil {
		s.back(w, r, slotURL(kitID, slotKey), "payload", err)
		return
	}
	s.inertia.Redirect(w, r, slotURL(kitID, slotKey))
}

// slotSaveAsBlock saves a slot's block plus its edits as a new library block.
func (s *server) slotSaveAsBlock(w nethttp.ResponseWriter, r *nethttp.Request) {
	kitID, slotKey := chi.URLParam(r, "kitID"), chi.URLParam(r, "slotKey")
	l, ok := s.loadKit(w, r, kitID, authz.KitEdit)
	if !ok || !s.allowed(w, r, authz.BlockCreate, l.Kit.TeamID) {
		return
	}
	var f relinkForm
	if err := decodeForm(r, &f); err != nil {
		nethttp.Error(w, "bad request", nethttp.StatusBadRequest)
		return
	}
	if _, err := s.kits().SaveAsBlock(r.Context(), kitID, slotKey, f.Name, userID(r)); err != nil {
		s.back(w, r, slotURL(kitID, slotKey), "saveAs", err)
		return
	}
	s.inertia.Redirect(w, r, slotURL(kitID, slotKey))
}
