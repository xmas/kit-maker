package http

import (
	nethttp "net/http"

	inertia "github.com/romsar/gonertia/v2"

	"github.com/docker/kit-maker/internal/authz"
	"github.com/docker/kit-maker/internal/ent"
	"github.com/docker/kit-maker/internal/ent/kit"
	"github.com/docker/kit-maker/internal/ent/slot"
)

type kitRow struct {
	ID          string `json:"id"`
	Slug        string `json:"slug"`
	DisplayName string `json:"displayName"`
	Kind        string `json:"kind"`
	Team        string `json:"team"`
	Slots       int    `json:"slots"`
	Blanks      int    `json:"blanks"`
}

// kitsIndex renders the placeholder Kits page.
func (s *server) kitsIndex(w nethttp.ResponseWriter, r *nethttp.Request) {
	teams, all := authz.SubjectFrom(r.Context()).TeamsWith(authz.KitView)
	q := s.Ent.Kit.Query()
	if !all {
		q = q.Where(kit.TeamIDIn(teams...))
	}
	kits, err := q.
		WithTeam().
		WithSlots(func(q *ent.SlotQuery) { q.Select(slot.FieldKitID, slot.FieldState, slot.FieldRequired) }).
		Order(ent.Asc(kit.FieldDisplayName)).
		All(r.Context())
	if err != nil {
		s.Log.ErrorContext(r.Context(), "list kits", "error", err)
		nethttp.Error(w, "could not load kits", nethttp.StatusInternalServerError)
		return
	}
	rows := make([]kitRow, 0, len(kits))
	for _, k := range kits {
		row := kitRow{ID: k.ID, Slug: k.Slug, DisplayName: k.DisplayName, Kind: string(k.Kind), Slots: len(k.Edges.Slots)}
		if k.Edges.Team != nil {
			row.Team = k.Edges.Team.Name
		}
		for _, sl := range k.Edges.Slots {
			if sl.Required && sl.State == slot.StateBlank {
				row.Blanks++
			}
		}
		rows = append(rows, row)
	}
	if err := s.inertia.Render(w, r, "Kits/Index", inertia.Props{"kits": rows}); err != nil {
		s.Log.ErrorContext(r.Context(), "render kits", "error", err)
		nethttp.Error(w, "could not render page", nethttp.StatusInternalServerError)
	}
}
