package http

import (
	"encoding/json"
	"errors"
	nethttp "net/http"
	"strings"

	"github.com/go-chi/chi/v5"
	inertia "github.com/romsar/gonertia/v2"

	"github.com/docker/kit-maker/internal/authz"
	"github.com/docker/kit-maker/internal/blueprints"
	"github.com/docker/kit-maker/internal/blueprints/blocktypes"
)

func (s *server) blueprints() *blueprints.Service { return &blueprints.Service{Ent: s.Ent} }

// blueprintShow renders the builder: outline, the selected slot (or the overview
// when none), live YAML and checks.
func (s *server) blueprintShow(w nethttp.ResponseWriter, r *nethttp.Request) {
	blueprintID, slotKey := chi.URLParam(r, "blueprintID"), chi.URLParam(r, "slotKey")
	l, ok := s.loadBlueprint(w, r, blueprintID, authz.BlueprintView)
	if !ok {
		return
	}
	var selected *blueprints.SlotDetail
	if slotKey != "" {
		selected = l.Slots[slotKey]
		if selected == nil {
			nethttp.NotFound(w, r)
			return
		}
	}
	sub := authz.SubjectFrom(r.Context())
	view := "blueprint"
	var history []blueprints.Revision
	if strings.HasSuffix(r.URL.Path, "/history") {
		view = "history"
		var err error
		if history, err = s.blueprints().History(r.Context(), blueprintID, ""); err != nil {
			s.fail(w, r, "history", err)
			return
		}
	}
	props := inertia.Props{
		"view":      view,
		"history":   history,
		"blueprint": l.Blueprint,
		"sections":  l.Sections,
		"compiled":  l.Compiled,
		"checks":    l.Checks,
		"selected":  selected,
		"types":     blocktypes.All(),
		"can": map[string]bool{
			"edit":    sub.Can(authz.BlueprintEdit, l.Blueprint.TeamID),
			"revert":  sub.Can("link.revert", l.Blueprint.TeamID),
			"use":     sub.Can(authz.BlockUse, l.Blueprint.TeamID),
			"ai":      sub.Can(authz.AIUse, l.Blueprint.TeamID),
			"checks":  sub.Can(authz.BlueprintChecksEdit, l.Blueprint.TeamID),
			"create":  sub.Can(authz.BlockCreate, l.Blueprint.TeamID),
			"update":  sub.Can(authz.LinkRebaseApply, l.Blueprint.TeamID),
			"history": sub.Can(authz.BlueprintHistoryView, l.Blueprint.TeamID),
		},
	}
	// Candidates fill a blank or replace the linked block (re-link).
	if selected != nil {
		teams, all := sub.TeamsWith(authz.BlockView)
		cands, err := s.blueprints().Candidates(r.Context(), selected.BlockType, teams, all)
		if err != nil {
			s.fail(w, r, "list candidates", err)
			return
		}
		props["candidates"] = cands
	}
	s.render(w, r, "Blueprints/Show", props)
}

type slotForm struct {
	Payload map[string]any `json:"payload"`
	BlockID string         `json:"blockId"`
}

// slotSave keeps the edited payload as overrides on the linked block.
func (s *server) slotSave(w nethttp.ResponseWriter, r *nethttp.Request) {
	blueprintID, slotKey := chi.URLParam(r, "blueprintID"), chi.URLParam(r, "slotKey")
	if _, ok := s.loadBlueprint(w, r, blueprintID, authz.BlueprintEdit); !ok {
		return
	}
	var f slotForm
	if err := decodeForm(r, &f); err != nil || f.Payload == nil {
		nethttp.Error(w, "bad request", nethttp.StatusBadRequest)
		return
	}
	if _, err := s.blueprints().SaveSlot(r.Context(), blueprintID, slotKey, f.Payload); err != nil {
		s.backWithError(w, r, slotURL(blueprintID, slotKey), err)
		return
	}
	s.inertia.Redirect(w, r, slotURL(blueprintID, slotKey))
}

// slotHide resolves a conflict by hiding one entry of the slot's block in
// this blueprint, then returns to the page the request came from.
func (s *server) slotHide(w nethttp.ResponseWriter, r *nethttp.Request) {
	blueprintID, slotKey := chi.URLParam(r, "blueprintID"), chi.URLParam(r, "slotKey")
	if _, ok := s.loadBlueprint(w, r, blueprintID, authz.BlueprintEdit); !ok {
		return
	}
	var f struct {
		Path string `json:"path"`
		Key  string `json:"key"`
		Back string `json:"back"`
	}
	if err := decodeForm(r, &f); err != nil || f.Path == "" || f.Key == "" {
		nethttp.Error(w, "bad request", nethttp.StatusBadRequest)
		return
	}
	back := safeNext(f.Back)
	if back == "/blueprints" || !strings.HasPrefix(back, "/blueprints/"+blueprintID) {
		back = slotURL(blueprintID, slotKey)
	}
	if err := s.blueprints().HideEntry(r.Context(), blueprintID, slotKey, f.Path, f.Key); err != nil {
		s.backWithError(w, r, back, err)
		return
	}
	s.inertia.Redirect(w, r, back)
}

// slotRevert drops a slot's edits.
func (s *server) slotRevert(w nethttp.ResponseWriter, r *nethttp.Request) {
	blueprintID, slotKey := chi.URLParam(r, "blueprintID"), chi.URLParam(r, "slotKey")
	if _, ok := s.loadBlueprint(w, r, blueprintID, "link.revert"); !ok {
		return
	}
	if err := s.blueprints().RevertSlot(r.Context(), blueprintID, slotKey); err != nil {
		s.backWithError(w, r, slotURL(blueprintID, slotKey), err)
		return
	}
	s.inertia.Redirect(w, r, slotURL(blueprintID, slotKey))
}

// slotLink fills a blank slot with a library block.
func (s *server) slotLink(w nethttp.ResponseWriter, r *nethttp.Request) {
	blueprintID, slotKey := chi.URLParam(r, "blueprintID"), chi.URLParam(r, "slotKey")
	if _, ok := s.loadBlueprint(w, r, blueprintID, authz.BlockUse); !ok {
		return
	}
	var f slotForm
	if err := decodeForm(r, &f); err != nil || f.BlockID == "" {
		nethttp.Error(w, "bad request", nethttp.StatusBadRequest)
		return
	}
	if err := s.usableBlock(r, f.BlockID); err != nil {
		s.backWithError(w, r, slotURL(blueprintID, slotKey), err)
		return
	}
	if err := s.blueprints().LinkBlock(r.Context(), blueprintID, slotKey, f.BlockID); err != nil {
		s.backWithError(w, r, slotURL(blueprintID, slotKey), err)
		return
	}
	s.inertia.Redirect(w, r, slotURL(blueprintID, slotKey))
}

// usableBlock reports whether this person may link a block: public ones
// (the starter library), or shared blocks of a team they can view. Private
// blocks belong to one blueprint.
func (s *server) usableBlock(r *nethttp.Request, blockID string) error {
	b, err := s.blocks().Get(r.Context(), blockID)
	if err != nil || b.Visibility == "private" ||
		(b.Visibility != "public" && !authz.SubjectFrom(r.Context()).Can(authz.BlockView, b.TeamID)) {
		return errors.New("that block isn't available to you")
	}
	return nil
}

// slotUnlink empties a slot.
func (s *server) slotUnlink(w nethttp.ResponseWriter, r *nethttp.Request) {
	blueprintID, slotKey := chi.URLParam(r, "blueprintID"), chi.URLParam(r, "slotKey")
	if _, ok := s.loadBlueprint(w, r, blueprintID, authz.BlueprintEdit); !ok {
		return
	}
	if err := s.blueprints().Unlink(r.Context(), blueprintID, slotKey); err != nil {
		s.backWithError(w, r, slotURL(blueprintID, slotKey), err)
		return
	}
	s.inertia.Redirect(w, r, slotURL(blueprintID, slotKey))
}

// loadBlueprint loads a blueprint and checks perm against its owner team.
func (s *server) loadBlueprint(w nethttp.ResponseWriter, r *nethttp.Request, blueprintID string, perm authz.Key) (*blueprints.Loaded, bool) {
	l, err := s.blueprints().Load(r.Context(), blueprintID)
	if errors.Is(err, blueprints.ErrNotFound) {
		nethttp.NotFound(w, r)
		return nil, false
	}
	if err != nil {
		s.fail(w, r, "load blueprint", err)
		return nil, false
	}
	if !s.allowed(w, r, perm, l.Blueprint.TeamID) {
		return nil, false
	}
	return l, true
}

func (s *server) backWithError(w nethttp.ResponseWriter, r *nethttp.Request, to string, err error) {
	if errors.Is(err, blueprints.ErrNotFound) {
		nethttp.NotFound(w, r)
		return
	}
	ctx := inertia.SetValidationErrors(r.Context(), inertia.ValidationErrors{"payload": err.Error()})
	s.inertia.Redirect(w, r.WithContext(ctx), to)
}

func slotURL(blueprintID, slotKey string) string {
	return "/blueprints/" + blueprintID + "/slots/" + slotKey
}

// slotPreview compiles the blueprint with the slot's unsaved draft and returns the
// compiled output as JSON (the editor's live YAML diff). Nothing is saved.
func (s *server) slotPreview(w nethttp.ResponseWriter, r *nethttp.Request) {
	blueprintID, slotKey := chi.URLParam(r, "blueprintID"), chi.URLParam(r, "slotKey")
	if _, ok := s.loadBlueprint(w, r, blueprintID, authz.BlueprintEdit); !ok {
		return
	}
	var f slotForm
	if err := decodeForm(r, &f); err != nil || f.Payload == nil {
		nethttp.Error(w, "bad request", nethttp.StatusBadRequest)
		return
	}
	out, err := s.blueprints().Preview(r.Context(), blueprintID, slotKey, f.Payload)
	if errors.Is(err, blueprints.ErrNotFound) {
		nethttp.NotFound(w, r)
		return
	}
	if err != nil {
		s.fail(w, r, "preview", err)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "no-store")
	_ = json.NewEncoder(w).Encode(map[string]any{"compiled": out})
}
