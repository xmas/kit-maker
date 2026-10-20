package http

import (
	nethttp "net/http"
	"strings"

	inertia "github.com/romsar/gonertia/v2"

	"github.com/docker/kit-maker/internal/authz"
	"github.com/docker/kit-maker/internal/ent"
	"github.com/docker/kit-maker/internal/ent/blueprint"
	"github.com/docker/kit-maker/internal/ent/slot"
)

type blueprintRow struct {
	ID          string `json:"id"`
	Slug        string `json:"slug"`
	DisplayName string `json:"displayName"`
	Kind        string `json:"kind"`
	Team        string `json:"team"`
	Slots       int    `json:"slots"`
	Blanks      int    `json:"blanks"`
}

// blueprintsIndex renders the placeholder Blueprints page.
func (s *server) blueprintsIndex(w nethttp.ResponseWriter, r *nethttp.Request) {
	teams, all := authz.SubjectFrom(r.Context()).TeamsWith(authz.BlueprintView)
	q := s.Ent.Blueprint.Query()
	if !all {
		q = q.Where(blueprint.TeamIDIn(teams...))
	}
	blueprints, err := q.
		WithTeam().
		WithSlots(func(q *ent.SlotQuery) { q.Select(slot.FieldBlueprintID, slot.FieldState, slot.FieldRequired) }).
		Order(ent.Asc(blueprint.FieldDisplayName)).
		All(r.Context())
	if err != nil {
		s.Log.ErrorContext(r.Context(), "list blueprints", "error", err)
		nethttp.Error(w, "could not load blueprints", nethttp.StatusInternalServerError)
		return
	}
	rows := make([]blueprintRow, 0, len(blueprints))
	for _, k := range blueprints {
		row := blueprintRow{ID: k.ID, Slug: k.Slug, DisplayName: k.DisplayName, Kind: string(k.Kind), Slots: len(k.Edges.Slots)}
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
	if err := s.inertia.Render(w, r, "Blueprints/Index", inertia.Props{"blueprints": rows}); err != nil {
		s.Log.ErrorContext(r.Context(), "render blueprints", "error", err)
		nethttp.Error(w, "could not render page", nethttp.StatusInternalServerError)
	}
}

// kitsRedirect sends links from before the rename (/kits/kit_…) to the same
// page under /blueprints, with the bp_ ID the migration gave the blueprint.
func (s *server) kitsRedirect(w nethttp.ResponseWriter, r *nethttp.Request) {
	rest := strings.TrimPrefix(r.URL.Path, "/kits")
	if strings.HasPrefix(rest, "/kit_") {
		rest = "/bp_" + strings.TrimPrefix(rest, "/kit_")
	}
	to := "/blueprints" + rest
	if r.URL.RawQuery != "" {
		to += "?" + r.URL.RawQuery
	}
	nethttp.Redirect(w, r, to, nethttp.StatusMovedPermanently)
}
