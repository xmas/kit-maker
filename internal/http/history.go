package http

import (
	"errors"
	nethttp "net/http"
	"strconv"

	"github.com/go-chi/chi/v5"

	"github.com/docker/kit-maker/internal/authz"
	"github.com/docker/kit-maker/internal/blocks"
	"github.com/docker/kit-maker/internal/kits"
)

// slotResync drops a slot's saved edits and moves it to the block's latest version.
func (s *server) slotResync(w nethttp.ResponseWriter, r *nethttp.Request) {
	kitID, slotKey := chi.URLParam(r, "kitID"), chi.URLParam(r, "slotKey")
	l, ok := s.loadKit(w, r, kitID, "link.revert")
	if !ok || !s.allowed(w, r, authz.LinkRebaseApply, l.Kit.TeamID) {
		return
	}
	if err := s.kits().Resync(r.Context(), kitID, slotKey); err != nil {
		s.back(w, r, slotURL(kitID, slotKey), "payload", err)
		return
	}
	s.inertia.Redirect(w, r, slotURL(kitID, slotKey))
}

// kitHistoryJSON lists revisions (optionally for one slot) as JSON.
func (s *server) kitHistoryJSON(w nethttp.ResponseWriter, r *nethttp.Request) {
	kitID := chi.URLParam(r, "kitID")
	if _, ok := s.loadKit(w, r, kitID, authz.KitHistoryView); !ok {
		return
	}
	revs, err := s.kits().History(r.Context(), kitID, r.URL.Query().Get("slot"))
	if err != nil {
		s.fail(w, r, "history", err)
		return
	}
	w.Header().Set("Cache-Control", "no-store")
	writeJSON(w, nethttp.StatusOK, map[string]any{"revisions": revs})
}

// revisionYAML returns the YAML the kit had at a revision.
func (s *server) revisionYAML(w nethttp.ResponseWriter, r *nethttp.Request) {
	kitID := chi.URLParam(r, "kitID")
	if _, ok := s.loadKit(w, r, kitID, authz.KitHistoryView); !ok {
		return
	}
	y, err := s.kits().RevisionYAML(r.Context(), kitID, chi.URLParam(r, "revID"))
	if errors.Is(err, kits.ErrNotFound) {
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

// revisionRestore restores the whole kit, or one slot, to a revision.
func (s *server) revisionRestore(w nethttp.ResponseWriter, r *nethttp.Request) {
	kitID, revID := chi.URLParam(r, "kitID"), chi.URLParam(r, "revID")
	if _, ok := s.loadKit(w, r, kitID, authz.KitEdit); !ok {
		return
	}
	var f restoreForm
	_ = decodeForm(r, &f)
	if f.SlotKey != "" {
		if err := s.kits().RestoreSlot(r.Context(), kitID, revID, f.SlotKey); err != nil {
			s.back(w, r, slotURL(kitID, f.SlotKey), "payload", err)
			return
		}
		s.inertia.Redirect(w, r, slotURL(kitID, f.SlotKey))
		return
	}
	if err := s.kits().RestoreKit(r.Context(), kitID, revID); err != nil {
		s.back(w, r, "/kits/"+kitID+"/history", "restore", err)
		return
	}
	s.inertia.Redirect(w, r, "/kits/"+kitID+"/history")
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
