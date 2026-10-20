package http

import (
	"errors"
	nethttp "net/http"
	"strconv"

	"github.com/go-chi/chi/v5"

	"github.com/docker/kit-maker/internal/authz"
	"github.com/docker/kit-maker/internal/blocks"
	"github.com/docker/kit-maker/internal/blueprints"
)

// slotResync drops a slot's saved edits and moves it to the block's latest version.
func (s *server) slotResync(w nethttp.ResponseWriter, r *nethttp.Request) {
	blueprintID, slotKey := chi.URLParam(r, "blueprintID"), chi.URLParam(r, "slotKey")
	l, ok := s.loadBlueprint(w, r, blueprintID, "link.revert")
	if !ok || !s.allowed(w, r, authz.LinkRebaseApply, l.Blueprint.TeamID) {
		return
	}
	if err := s.blueprints().Resync(r.Context(), blueprintID, slotKey); err != nil {
		s.back(w, r, slotURL(blueprintID, slotKey), "payload", err)
		return
	}
	s.inertia.Redirect(w, r, slotURL(blueprintID, slotKey))
}

// blueprintHistoryJSON lists revisions (optionally for one slot) as JSON.
func (s *server) blueprintHistoryJSON(w nethttp.ResponseWriter, r *nethttp.Request) {
	blueprintID := chi.URLParam(r, "blueprintID")
	if _, ok := s.loadBlueprint(w, r, blueprintID, authz.BlueprintHistoryView); !ok {
		return
	}
	revs, err := s.blueprints().History(r.Context(), blueprintID, r.URL.Query().Get("slot"))
	if err != nil {
		s.fail(w, r, "history", err)
		return
	}
	w.Header().Set("Cache-Control", "no-store")
	writeJSON(w, nethttp.StatusOK, map[string]any{"revisions": revs})
}

// revisionYAML returns the YAML the blueprint had at a revision.
func (s *server) revisionYAML(w nethttp.ResponseWriter, r *nethttp.Request) {
	blueprintID := chi.URLParam(r, "blueprintID")
	if _, ok := s.loadBlueprint(w, r, blueprintID, authz.BlueprintHistoryView); !ok {
		return
	}
	y, err := s.blueprints().RevisionYAML(r.Context(), blueprintID, chi.URLParam(r, "revID"))
	if errors.Is(err, blueprints.ErrNotFound) {
		nethttp.NotFound(w, r)
		return
	}
	if err != nil {
		s.fail(w, r, "revision yaml", err)
		return
	}
	w.Header().Set("Cache-Control", "no-store")
	writeJSON(w, nethttp.StatusOK, map[string]string{"yaml": y})
}

type restoreForm struct {
	SlotKey string `json:"slotKey"`
}

// revisionRestore restores the whole blueprint, or one slot, to a revision.
func (s *server) revisionRestore(w nethttp.ResponseWriter, r *nethttp.Request) {
	blueprintID, revID := chi.URLParam(r, "blueprintID"), chi.URLParam(r, "revID")
	if _, ok := s.loadBlueprint(w, r, blueprintID, authz.BlueprintEdit); !ok {
		return
	}
	var f restoreForm
	_ = decodeForm(r, &f)
	if f.SlotKey != "" {
		if err := s.blueprints().RestoreSlot(r.Context(), blueprintID, revID, f.SlotKey); err != nil {
			s.back(w, r, slotURL(blueprintID, f.SlotKey), "payload", err)
			return
		}
		s.inertia.Redirect(w, r, slotURL(blueprintID, f.SlotKey))
		return
	}
	if err := s.blueprints().RestoreBlueprint(r.Context(), blueprintID, revID); err != nil {
		s.back(w, r, "/blueprints/"+blueprintID+"/history", "restore", err)
		return
	}
	s.inertia.Redirect(w, r, "/blueprints/"+blueprintID+"/history")
}

// blockRestoreVersion publishes an old version of a block as its newest.
func (s *server) blockRestoreVersion(w nethttp.ResponseWriter, r *nethttp.Request) {
	d, ok := s.loadBlock(w, r, authz.BlockPublishVersion)
	if !ok {
		return
	}
	n, err := strconv.Atoi(chi.URLParam(r, "version"))
	if err != nil {
		nethttp.NotFound(w, r)
		return
	}
	if _, err := s.blocks().RestoreVersion(r.Context(), d.ID, n, userID(r)); err != nil {
		if errors.Is(err, blocks.ErrNotFound) {
			nethttp.NotFound(w, r)
			return
		}
		s.back(w, r, "/blocks/"+d.ID, "payload", err)
		return
	}
	s.inertia.Redirect(w, r, "/blocks/"+d.ID)
}
