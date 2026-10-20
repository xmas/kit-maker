package http

import (
	"errors"
	nethttp "net/http"

	"github.com/go-chi/chi/v5"
	inertia "github.com/romsar/gonertia/v2"

	"github.com/docker/kit-maker/internal/auth"
	"github.com/docker/kit-maker/internal/authz"
	"github.com/docker/kit-maker/internal/blocks"
	"github.com/docker/kit-maker/internal/blueprints/blocktypes"
)

func (s *server) blocks() *blocks.Service { return &blocks.Service{Ent: s.Ent} }

func userID(r *nethttp.Request) string {
	if u := auth.UserFrom(r.Context()); u != nil {
		return u.ID
	}
	return ""
}

// blocksIndex lists the library.
func (s *server) blocksIndex(w nethttp.ResponseWriter, r *nethttp.Request) {
	teams, all := authz.SubjectFrom(r.Context()).TeamsWith(authz.BlockView)
	list, err := s.blocks().List(r.Context(), teams, all)
	if err != nil {
		s.fail(w, r, "list blocks", err)
		return
	}
	createTeams, err := s.teamsWith(r, authz.BlockCreate)
	if err != nil {
		s.fail(w, r, "list teams", err)
		return
	}
	s.render(w, r, "Blocks/Index", inertia.Props{"blocks": list, "types": blocktypes.All(), "abilities": blocktypes.Abilities, "teams": createTeams})
}

type blockForm struct {
	TeamID      string         `json:"teamId"`
	Type        string         `json:"type"`
	Name        string         `json:"name"`
	Slug        string         `json:"slug"`
	Description string         `json:"description"`
	Payload     map[string]any `json:"payload"`
	Changelog   string         `json:"changelog"`
}

// blockCreate adds a block to the library and opens it.
func (s *server) blockCreate(w nethttp.ResponseWriter, r *nethttp.Request) {
	var f blockForm
	if err := decodeForm(r, &f); err != nil {
		nethttp.Error(w, "bad request", nethttp.StatusBadRequest)
		return
	}
	if f.TeamID == "" {
		s.back(w, r, "/blocks", "teamId", errors.New("pick the team that owns this block"))
		return
	}
	if !s.allowed(w, r, authz.BlockCreate, f.TeamID) {
		return
	}
	b, err := s.blocks().Create(r.Context(), blocks.CreateInput{
		TeamID: f.TeamID, Type: f.Type, Name: f.Name, Slug: f.Slug, Description: f.Description, UserID: userID(r),
	})
	if err != nil {
		s.back(w, r, "/blocks", "form", err)
		return
	}
	s.inertia.Redirect(w, r, "/blocks/"+b.ID)
}

// loadBlock loads a block and checks perm against its owner team.
func (s *server) loadBlock(w nethttp.ResponseWriter, r *nethttp.Request, perm authz.Key) (*blocks.Detail, bool) {
	d, err := s.blocks().Get(r.Context(), chi.URLParam(r, "blockID"))
	if errors.Is(err, blocks.ErrNotFound) {
		nethttp.NotFound(w, r)
		return nil, false
	}
	if err != nil {
		s.fail(w, r, "load block", err)
		return nil, false
	}
	// A public block (the starter library) is viewable by anyone who can
	// view blocks somewhere; changing it still needs its owner team's rights.
	if perm == authz.BlockView && d.Visibility == "public" && authz.SubjectFrom(r.Context()).CanAnywhere(authz.BlockView) {
		return d, true
	}
	if !s.allowed(w, r, perm, d.TeamID) {
		return nil, false
	}
	return d, true
}

// blockShow renders one block with its editor, history and uses.
func (s *server) blockShow(w nethttp.ResponseWriter, r *nethttp.Request) {
	d, ok := s.loadBlock(w, r, authz.BlockView)
	if !ok {
		return
	}
	sub := authz.SubjectFrom(r.Context())
	bt, _ := blocktypes.Get(d.Type)
	s.render(w, r, "Blocks/Show", inertia.Props{
		"block":     d,
		"blockType": bt,
		"can": map[string]bool{
			"edit":    sub.Can(authz.BlockEdit, d.TeamID),
			"publish": sub.Can(authz.BlockPublishVersion, d.TeamID),
		},
	})
}

// blockUpdate changes a block's name and description.
func (s *server) blockUpdate(w nethttp.ResponseWriter, r *nethttp.Request) {
	d, ok := s.loadBlock(w, r, authz.BlockEdit)
	if !ok {
		return
	}
	var f blockForm
	if err := decodeForm(r, &f); err != nil {
		nethttp.Error(w, "bad request", nethttp.StatusBadRequest)
		return
	}
	if err := s.blocks().UpdateDetails(r.Context(), d.ID, f.Name, f.Description); err != nil {
		s.back(w, r, "/blocks/"+d.ID, "details", err)
		return
	}
	s.inertia.Redirect(w, r, "/blocks/"+d.ID)
}

// blockPublish saves an edited payload as the block's next version.
func (s *server) blockPublish(w nethttp.ResponseWriter, r *nethttp.Request) {
	d, ok := s.loadBlock(w, r, authz.BlockPublishVersion)
	if !ok {
		return
	}
	var f blockForm
	if err := decodeForm(r, &f); err != nil || f.Payload == nil {
		nethttp.Error(w, "bad request", nethttp.StatusBadRequest)
		return
	}
	if _, _, err := s.blocks().Publish(r.Context(), d.ID, f.Payload, f.Changelog, userID(r)); err != nil {
		s.back(w, r, "/blocks/"+d.ID, "payload", err)
		return
	}
	s.inertia.Redirect(w, r, "/blocks/"+d.ID)
}
