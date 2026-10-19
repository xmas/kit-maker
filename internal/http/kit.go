package http

import (
	"encoding/json"
	"errors"
	nethttp "net/http"

	"github.com/go-chi/chi/v5"
	inertia "github.com/romsar/gonertia/v2"

	"github.com/docker/kit-maker/internal/authz"
	"github.com/docker/kit-maker/internal/kits"
	"github.com/docker/kit-maker/internal/kits/blocktypes"
)

func (s *server) kits() *kits.Service { return &kits.Service{Ent: s.Ent} }

// kitShow renders the builder: outline, the selected slot (or the overview
// when none), live YAML and checks.
func (s *server) kitShow(w nethttp.ResponseWriter, r *nethttp.Request) {
	kitID, slotKey := chi.URLParam(r, "kitID"), chi.URLParam(r, "slotKey")
	l, ok := s.loadKit(w, r, kitID, authz.KitView)
	if !ok {
		return
	}
	var selected *kits.SlotDetail
	if slotKey != "" {
		selected = l.Slots[slotKey]
		if selected == nil {
			nethttp.NotFound(w, r)
			return
		}
	}
	sub := authz.SubjectFrom(r.Context())
	props := inertia.Props{
		"kit":      l.Kit,
		"sections": l.Sections,
		"compiled": l.Compiled,
		"checks":   l.Checks,
		"selected": selected,
		"types":    blocktypes.All(),
		"can": map[string]bool{
			"edit":   sub.Can(authz.KitEdit, l.Kit.TeamID),
			"revert": sub.Can("link.revert", l.Kit.TeamID),
			"use":    sub.Can(authz.BlockUse, l.Kit.TeamID),
			"ai":     sub.Can(authz.AIUse, l.Kit.TeamID),
			"checks": sub.Can(authz.KitChecksEdit, l.Kit.TeamID),
			"create": sub.Can(authz.BlockCreate, l.Kit.TeamID),
			"update": sub.Can(authz.LinkRebaseApply, l.Kit.TeamID),
		},
	}
	// Candidates fill a blank or replace the linked block (re-link).
	if selected != nil {
		teams, all := sub.TeamsWith(authz.BlockView)
		cands, err := s.kits().Candidates(r.Context(), selected.Section, teams, all)
		if err != nil {
			s.fail(w, r, "list candidates", err)
			return
		}
		props["candidates"] = cands
	}
	s.render(w, r, "Kits/Show", props)
}

type slotForm struct {
	Payload map[string]any `json:"payload"`
	BlockID string         `json:"blockId"`
}

// slotSave keeps the edited payload as overrides on the linked block.
func (s *server) slotSave(w nethttp.ResponseWriter, r *nethttp.Request) {
	kitID, slotKey := chi.URLParam(r, "kitID"), chi.URLParam(r, "slotKey")
	if _, ok := s.loadKit(w, r, kitID, authz.KitEdit); !ok {
		return
	}
	var f slotForm
	if err := decodeForm(r, &f); err != nil || f.Payload == nil {
		nethttp.Error(w, "bad request", nethttp.StatusBadRequest)
		return
	}
	if _, err := s.kits().SaveSlot(r.Context(), kitID, slotKey, f.Payload); err != nil {
		s.backWithError(w, r, slotURL(kitID, slotKey), err)
		return
	}
	s.inertia.Redirect(w, r, slotURL(kitID, slotKey))
}

// slotRevert drops a slot's edits.
func (s *server) slotRevert(w nethttp.ResponseWriter, r *nethttp.Request) {
	kitID, slotKey := chi.URLParam(r, "kitID"), chi.URLParam(r, "slotKey")
	if _, ok := s.loadKit(w, r, kitID, "link.revert"); !ok {
		return
	}
	if err := s.kits().RevertSlot(r.Context(), kitID, slotKey); err != nil {
		s.backWithError(w, r, slotURL(kitID, slotKey), err)
		return
	}
	s.inertia.Redirect(w, r, slotURL(kitID, slotKey))
}

// slotLink fills a blank slot with a library block.
func (s *server) slotLink(w nethttp.ResponseWriter, r *nethttp.Request) {
	kitID, slotKey := chi.URLParam(r, "kitID"), chi.URLParam(r, "slotKey")
	if _, ok := s.loadKit(w, r, kitID, authz.BlockUse); !ok {
		return
	}
	var f slotForm
	if err := decodeForm(r, &f); err != nil || f.BlockID == "" {
		nethttp.Error(w, "bad request", nethttp.StatusBadRequest)
		return
	}
	if err := s.kits().LinkBlock(r.Context(), kitID, slotKey, f.BlockID); err != nil {
		s.backWithError(w, r, slotURL(kitID, slotKey), err)
		return
	}
	s.inertia.Redirect(w, r, slotURL(kitID, slotKey))
}

// slotUnlink empties a slot.
func (s *server) slotUnlink(w nethttp.ResponseWriter, r *nethttp.Request) {
	kitID, slotKey := chi.URLParam(r, "kitID"), chi.URLParam(r, "slotKey")
	if _, ok := s.loadKit(w, r, kitID, authz.KitEdit); !ok {
		return
	}
	if err := s.kits().Unlink(r.Context(), kitID, slotKey); err != nil {
		s.backWithError(w, r, slotURL(kitID, slotKey), err)
		return
	}
	s.inertia.Redirect(w, r, slotURL(kitID, slotKey))
}

// loadKit loads a kit and checks perm against its owner team.
func (s *server) loadKit(w nethttp.ResponseWriter, r *nethttp.Request, kitID string, perm authz.Key) (*kits.Loaded, bool) {
	l, err := s.kits().Load(r.Context(), kitID)
	if errors.Is(err, kits.ErrNotFound) {
		nethttp.NotFound(w, r)
		return nil, false
	}
	if err != nil {
		s.fail(w, r, "load kit", err)
		return nil, false
	}
	if !s.allowed(w, r, perm, l.Kit.TeamID) {
		return nil, false
	}
	return l, true
}

func (s *server) backWithError(w nethttp.ResponseWriter, r *nethttp.Request, to string, err error) {
	if errors.Is(err, kits.ErrNotFound) {
		nethttp.NotFound(w, r)
		return
	}
	ctx := inertia.SetValidationErrors(r.Context(), inertia.ValidationErrors{"payload": err.Error()})
	s.inertia.Redirect(w, r.WithContext(ctx), to)
}

func slotURL(kitID, slotKey string) string { return "/kits/" + kitID + "/slots/" + slotKey }

// slotPreview compiles the kit with the slot's unsaved draft and returns the
// compiled output as JSON (the editor's live YAML diff). Nothing is saved.
func (s *server) slotPreview(w nethttp.ResponseWriter, r *nethttp.Request) {
	kitID, slotKey := chi.URLParam(r, "kitID"), chi.URLParam(r, "slotKey")
	if _, ok := s.loadKit(w, r, kitID, authz.KitEdit); !ok {
		return
	}
	var f slotForm
	if err := decodeForm(r, &f); err != nil || f.Payload == nil {
		nethttp.Error(w, "bad request", nethttp.StatusBadRequest)
		return
	}
	out, err := s.kits().Preview(r.Context(), kitID, slotKey, f.Payload)
	if errors.Is(err, kits.ErrNotFound) {
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
