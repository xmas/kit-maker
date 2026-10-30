package lite

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strconv"
	"strings"

	"github.com/go-chi/chi/v5"

	"github.com/docker/kit-maker/internal/blueprints/blocktypes"
)

// Modes say where documents are kept.
const (
	ModeShared = "shared" // D1 through the Pages Functions; sign-in required
	ModeLocal  = "local"  // this browser only (no backend deployed)
)

// Host is what the engine asks of its surroundings: the backend for sign-in
// and history, and AI when it's set up.
type Host interface {
	Login(email, password string) (*User, error)
	Logout() error
	// Revisions returns a blueprint's stored revisions.
	Revisions(blueprintID string) ([]Doc, error)
	// AI runs a model request; nil when AI isn't set up.
	AI() AI
}

// Engine serves Kit Maker's routes from a Store.
type Engine struct {
	Store *Store
	Host  Host
	Mode  string
	User  *User // nil when signed out
	// Version is the Inertia asset version.
	Version string

	flash  map[string]string // validation errors for the next page
	router chi.Router
}

// NewEngine builds the router.
func NewEngine(store *Store, host Host, mode string) *Engine {
	e := &Engine{Store: store, Host: host, Mode: mode}
	r := chi.NewRouter()
	r.Get("/login", e.loginPage)
	r.Post("/login", e.login)
	r.Group(func(r chi.Router) {
		r.Use(e.requireUser)
		r.Get("/", func(w http.ResponseWriter, r *http.Request) { e.redirect(w, r, "/blueprints") })
		r.Post("/logout", e.logout)
		r.Get("/blueprints", e.blueprintsIndex)
		r.Get("/blueprints/new", e.blueprintNew)
		r.Post("/blueprints", e.blueprintCreate)
		r.Get("/blueprints/{id}", e.blueprintShow)
		r.Get("/blueprints/{id}/history", e.blueprintShow)
		r.Get("/blueprints/{id}/slots/{key}", e.blueprintShow)
		r.Put("/blueprints/{id}/goal", e.blueprintGoal)
		r.Post("/blueprints/{id}/slots", e.slotAdd)
		r.Delete("/blueprints/{id}/slots/{key}", e.slotRemove)
		r.Put("/blueprints/{id}/slots/{key}", e.slotSave)
		r.Post("/blueprints/{id}/slots/{key}/preview", e.slotPreview)
		r.Post("/blueprints/{id}/slots/{key}/revert", e.slotAction(func(id, key string, _ form) error { return e.Store.RevertSlot(id, key) }))
		r.Post("/blueprints/{id}/slots/{key}/resync", e.slotAction(func(id, key string, _ form) error { return e.Store.Resync(id, key) }))
		r.Post("/blueprints/{id}/slots/{key}/update", e.slotAction(func(id, key string, _ form) error { return e.Store.UpdateLink(id, key) }))
		r.Post("/blueprints/{id}/slots/{key}/unlink", e.slotAction(func(id, key string, _ form) error { return e.Store.Unlink(id, key) }))
		r.Post("/blueprints/{id}/slots/{key}/link", e.slotAction(e.linkOrRelink(e.Store.LinkBlock)))
		r.Post("/blueprints/{id}/slots/{key}/relink", e.slotAction(e.linkOrRelink(e.Store.Relink)))
		r.Post("/blueprints/{id}/slots/{key}/custom", e.slotCustom)
		r.Post("/blueprints/{id}/slots/{key}/save-as-block", e.slotSaveAsBlock)
		r.Post("/blueprints/{id}/slots/{key}/hide", e.slotHide)
		r.Post("/blueprints/{id}/slots/{key}/explain", e.slotExplain)
		r.Post("/blueprints/{id}/slots/{key}/draft", e.slotDraft)
		r.Post("/blueprints/{id}/checks", e.checkAdd)
		r.Delete("/blueprints/{id}/checks/{checkID}", e.checkRemove)
		r.Get("/blueprints/{id}/history.json", e.historyJSON)
		r.Get("/blueprints/{id}/revisions/{rev}/yaml", e.revisionYAML)
		r.Post("/blueprints/{id}/revisions/{rev}/restore", e.revisionRestore)
		r.Get("/blueprints/{id}/run", e.runInfoJSON)
		r.Post("/blueprints/{id}/publish", e.publish)
		r.Get("/blueprints/{id}/download", e.download)
		r.Get("/blueprints/{id}/sbxenv.yaml", e.sbxenv)
		r.Get("/blocks", e.blocksIndex)
		r.Post("/blocks", e.blockCreate)
		r.Post("/blocks/search", e.blockSearch)
		r.Get("/blocks/{blockID}", e.blockShow)
		r.Put("/blocks/{blockID}", e.blockUpdate)
		r.Post("/blocks/{blockID}/versions", e.blockPublish)
		r.Post("/blocks/{blockID}/versions/{version}/restore", e.blockRestoreVersion)
	})
	r.NotFound(func(w http.ResponseWriter, r *http.Request) { e.notFound(w, r) })
	e.router = r
	return e
}

// ServeHTTP handles one request. The caller runs requests one at a time.
func (e *Engine) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	e.Store.Actor = User{}
	if e.User != nil {
		e.Store.Actor = *e.User
	}
	e.router.ServeHTTP(w, r)
}

// --- Inertia ----------------------------------------------------------------

type props map[string]any

func (e *Engine) render(w http.ResponseWriter, r *http.Request, component string, p props) {
	errs := e.flash
	e.flash = nil
	if errs == nil {
		errs = map[string]string{}
	}
	p["errors"] = errs
	var who any
	if e.User != nil {
		who = map[string]string{"id": e.User.ID, "email": e.User.Email, "displayName": e.User.DisplayName}
	}
	p["auth"] = map[string]any{"user": who, "mode": e.Mode}
	page := map[string]any{"component": component, "props": p, "url": r.URL.RequestURI(), "version": e.Version}
	w.Header().Set("X-Inertia", "true")
	w.Header().Set("Vary", "X-Inertia")
	writeJSON(w, http.StatusOK, page)
}

// redirect answers 303, which Inertia follows with a GET for every method.
func (e *Engine) redirect(w http.ResponseWriter, _ *http.Request, to string) {
	w.Header().Set("Location", to)
	w.WriteHeader(http.StatusSeeOther)
}

// back redirects with err as the validation error under key.
func (e *Engine) back(w http.ResponseWriter, r *http.Request, to, key string, err error) {
	if errors.Is(err, ErrNotFound) {
		e.notFound(w, r)
		return
	}
	e.flash = map[string]string{key: err.Error()}
	e.redirect(w, r, to)
}

func (e *Engine) notFound(w http.ResponseWriter, r *http.Request) {
	if r.Header.Get("X-Inertia") == "" && r.Method != http.MethodGet {
		http.Error(w, "not found", http.StatusNotFound)
		return
	}
	e.render(w, r, "Error", props{"status": 404, "title": "Not found", "message": "There's nothing at this address. It may have been removed."})
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

// form is a decoded request body.
type form map[string]any

func (f form) str(k string) string {
	switch v := f[k].(type) {
	case string:
		return v
	case nil:
		return ""
	default:
		return fmt.Sprint(v)
	}
}

func (f form) bool(k string) bool { b, _ := f[k].(bool); return b }

func (f form) obj(k string) map[string]any { m, _ := f[k].(map[string]any); return m }

func decode(r *http.Request) (form, error) {
	f := form{}
	if r.Body == nil {
		return f, nil
	}
	err := json.NewDecoder(http.MaxBytesReader(nil, r.Body, 4<<20)).Decode(&f)
	if err != nil && err.Error() == "EOF" {
		err = nil
	}
	return f, err
}

func badRequest(w http.ResponseWriter) { http.Error(w, "bad request", http.StatusBadRequest) }

// --- Sign-in ----------------------------------------------------------------

func (e *Engine) requireUser(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if e.User == nil {
			e.redirect(w, r, "/login?next="+url.QueryEscape(r.URL.RequestURI()))
			return
		}
		next.ServeHTTP(w, r)
	})
}

func safeNext(next string) string {
	u, err := url.Parse(next)
	if next == "" || err != nil || u.IsAbs() || u.Host != "" || !strings.HasPrefix(u.Path, "/") || strings.HasPrefix(next, "//") {
		return "/blueprints"
	}
	return u.RequestURI()
}

func (e *Engine) loginPage(w http.ResponseWriter, r *http.Request) {
	if e.User != nil {
		e.redirect(w, r, "/blueprints")
		return
	}
	e.render(w, r, "Auth/Login", props{
		"next":      safeNext(r.URL.Query().Get("next")),
		"providers": []map[string]string{{"key": "local", "label": "Email and password"}},
	})
}

func (e *Engine) login(w http.ResponseWriter, r *http.Request) {
	f, err := decode(r)
	if err != nil {
		badRequest(w)
		return
	}
	u, err := e.Host.Login(f.str("email"), f.str("password"))
	if err != nil {
		e.flash = map[string]string{"email": err.Error()}
		e.redirect(w, r, "/login?next="+url.QueryEscape(safeNext(f.str("next"))))
		return
	}
	e.User = u
	e.redirect(w, r, safeNext(f.str("next")))
}

func (e *Engine) logout(w http.ResponseWriter, r *http.Request) {
	if e.Mode == ModeLocal { // nobody to sign out
		e.redirect(w, r, "/blueprints")
		return
	}
	_ = e.Host.Logout()
	e.User = nil
	e.redirect(w, r, "/login")
}

// --- Blueprints -------------------------------------------------------------

type blueprintRow struct {
	ID          string `json:"id"`
	Slug        string `json:"slug"`
	DisplayName string `json:"displayName"`
	Kind        string `json:"kind"`
	Team        string `json:"team"`
	Slots       int    `json:"slots"`
	Blanks      int    `json:"blanks"`
}

func (e *Engine) blueprintsIndex(w http.ResponseWriter, r *http.Request) {
	rows := []blueprintRow{}
	for _, bp := range e.Store.sortedBlueprints() {
		row := blueprintRow{ID: bp.ID, Slug: bp.Slug, DisplayName: bp.DisplayName, Kind: bp.Kind, Team: e.Store.Team.Name, Slots: len(bp.Slots)}
		for _, sl := range bp.Slots {
			if sl.Required && sl.BlockID == "" {
				row.Blanks++
			}
		}
		rows = append(rows, row)
	}
	e.render(w, r, "Blueprints/Index", props{"blueprints": rows})
}

func (e *Engine) teams() []Team { return []Team{e.Store.Team} }

func (e *Engine) blueprintNew(w http.ResponseWriter, r *http.Request) {
	e.render(w, r, "Blueprints/New", props{"teams": e.teams()})
}

func (e *Engine) blueprintCreate(w http.ResponseWriter, r *http.Request) {
	f, err := decode(r)
	if err != nil {
		badRequest(w)
		return
	}
	if f.str("teamId") == "" {
		e.back(w, r, "/blueprints/new", "teamId", errors.New("pick the team that owns this blueprint"))
		return
	}
	bp, err := e.Store.Create(CreateInput{TeamID: f.str("teamId"), DisplayName: f.str("displayName"), Slug: f.str("slug"),
		Kind: f.str("kind"), Description: f.str("description"), Goal: f.str("goal")})
	if err != nil {
		e.back(w, r, "/blueprints/new", "form", err)
		return
	}
	e.redirect(w, r, "/blueprints/"+bp.ID)
}

func slotURL(id, key string) string { return "/blueprints/" + id + "/slots/" + key }

// can is what the signed-in person may do; everyone signed in is an editor
// in this edition.
func (e *Engine) can() map[string]bool {
	ai := e.Host.AI() != nil
	return map[string]bool{"edit": true, "revert": true, "use": true, "ai": ai, "checks": true, "create": true, "update": true, "history": true}
}

func (e *Engine) blueprintShow(w http.ResponseWriter, r *http.Request) {
	id, key := chi.URLParam(r, "id"), chi.URLParam(r, "key")
	l, err := e.Store.Load(id)
	if err != nil {
		e.notFound(w, r)
		return
	}
	var selected *SlotDetail
	if key != "" {
		if selected = l.Slots[key]; selected == nil {
			e.notFound(w, r)
			return
		}
	}
	view := "blueprint"
	var history []RevisionInfo
	if strings.HasSuffix(r.URL.Path, "/history") {
		view = "history"
		if err := e.ensureRevisions(id); err != nil {
			e.fail(w, r, err)
			return
		}
		history = e.Store.History(id, "")
	}
	p := props{
		"view": view, "history": history, "blueprint": l.Blueprint, "sections": l.Sections, "compiled": l.Compiled,
		"checks": l.Checks, "selected": selected, "types": blocktypes.All(), "can": e.can(),
	}
	if selected != nil {
		p["candidates"] = e.Store.Candidates(selected.BlockType)
	}
	e.render(w, r, "Blueprints/Show", p)
}

func (e *Engine) fail(w http.ResponseWriter, _ *http.Request, err error) {
	http.Error(w, "something went wrong: "+err.Error(), http.StatusInternalServerError)
}

// ensureRevisions loads a blueprint's history from the backend once.
func (e *Engine) ensureRevisions(id string) error {
	if e.Store.RevisionsLoaded(id) {
		return nil
	}
	docs, err := e.Host.Revisions(id)
	if err != nil {
		return err
	}
	// Keep revisions recorded in this session that the backend may not
	// have returned yet.
	pending := e.Store.revisions[id]
	if err := e.Store.LoadRevisions(id, docs); err != nil {
		return err
	}
	for _, rv := range pending {
		e.Store.addRevision(rv)
	}
	return nil
}

func (e *Engine) blueprintGoal(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	f, err := decode(r)
	if err != nil {
		badRequest(w)
		return
	}
	if err := e.Store.UpdateGoal(id, f.str("goal")); err != nil {
		e.back(w, r, "/blueprints/"+id, "goal", err)
		return
	}
	e.redirect(w, r, "/blueprints/"+id)
}

func (e *Engine) slotAdd(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	f, err := decode(r)
	if err != nil {
		badRequest(w)
		return
	}
	if _, ok := blocktypes.Get(f.str("type")); !ok {
		e.back(w, r, "/blueprints/"+id, "slot", errors.New("pick what kind of block goes in the slot"))
		return
	}
	key, err := e.Store.AddSlot(id, f.str("label"), f.str("type"), f.bool("required"))
	if err != nil {
		e.back(w, r, "/blueprints/"+id, "slot", err)
		return
	}
	e.redirect(w, r, slotURL(id, key))
}

func (e *Engine) slotRemove(w http.ResponseWriter, r *http.Request) {
	id, key := chi.URLParam(r, "id"), chi.URLParam(r, "key")
	if err := e.Store.RemoveSlot(id, key); err != nil {
		e.back(w, r, slotURL(id, key), "payload", err)
		return
	}
	e.redirect(w, r, "/blueprints/"+id)
}

func (e *Engine) slotSave(w http.ResponseWriter, r *http.Request) {
	id, key := chi.URLParam(r, "id"), chi.URLParam(r, "key")
	f, err := decode(r)
	if err != nil || f.obj("payload") == nil {
		badRequest(w)
		return
	}
	if err := e.Store.SaveSlot(id, key, f.obj("payload")); err != nil {
		e.back(w, r, slotURL(id, key), "payload", err)
		return
	}
	e.redirect(w, r, slotURL(id, key))
}

// slotAction runs a change to one slot and returns to it.
func (e *Engine) slotAction(fn func(id, key string, f form) error) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		id, key := chi.URLParam(r, "id"), chi.URLParam(r, "key")
		f, err := decode(r)
		if err != nil {
			badRequest(w)
			return
		}
		if err := fn(id, key, f); err != nil {
			e.back(w, r, slotURL(id, key), "payload", err)
			return
		}
		e.redirect(w, r, slotURL(id, key))
	}
}

func (e *Engine) linkOrRelink(fn func(id, key, blockID string) error) func(id, key string, f form) error {
	return func(id, key string, f form) error {
		blockID := f.str("blockId")
		if b := e.Store.blocks[blockID]; b == nil || b.Visibility == "private" {
			return errors.New("that block isn't available to you")
		}
		return fn(id, key, blockID)
	}
}

func (e *Engine) slotSaveAsBlock(w http.ResponseWriter, r *http.Request) {
	id, key := chi.URLParam(r, "id"), chi.URLParam(r, "key")
	f, err := decode(r)
	if err != nil {
		badRequest(w)
		return
	}
	if err := e.Store.SaveAsBlock(id, key, f.str("name")); err != nil {
		e.back(w, r, slotURL(id, key), "saveAs", err)
		return
	}
	e.redirect(w, r, slotURL(id, key))
}

func (e *Engine) slotHide(w http.ResponseWriter, r *http.Request) {
	id, key := chi.URLParam(r, "id"), chi.URLParam(r, "key")
	f, err := decode(r)
	if err != nil || f.str("path") == "" || f.str("key") == "" {
		badRequest(w)
		return
	}
	back := safeNext(f.str("back"))
	if back == "/blueprints" || !strings.HasPrefix(back, "/blueprints/"+id) {
		back = slotURL(id, key)
	}
	if err := e.Store.HideEntry(id, key, f.str("path"), f.str("key")); err != nil {
		e.back(w, r, back, "payload", err)
		return
	}
	e.redirect(w, r, back)
}

func (e *Engine) slotPreview(w http.ResponseWriter, r *http.Request) {
	id, key := chi.URLParam(r, "id"), chi.URLParam(r, "key")
	f, err := decode(r)
	if err != nil || f.obj("payload") == nil {
		badRequest(w)
		return
	}
	out, err := e.Store.Preview(id, key, f.obj("payload"))
	if err != nil {
		http.Error(w, "not found", http.StatusNotFound)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"compiled": out})
}

func (e *Engine) checkAdd(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	f, err := decode(r)
	if err != nil {
		badRequest(w)
		return
	}
	if err := e.Store.AddCheck(id, CheckInput{Label: f.str("label"), Method: f.str("method"), URL: f.str("url"), Expect: f.str("expect")}); err != nil {
		e.back(w, r, "/blueprints/"+id, "check", err)
		return
	}
	e.redirect(w, r, "/blueprints/"+id)
}

func (e *Engine) checkRemove(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	if err := e.Store.RemoveCheck(id, chi.URLParam(r, "checkID")); err != nil {
		e.back(w, r, "/blueprints/"+id, "check", err)
		return
	}
	e.redirect(w, r, "/blueprints/"+id)
}

// --- History ----------------------------------------------------------------

func (e *Engine) historyJSON(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	if _, err := e.Store.blueprint(id); err != nil {
		http.Error(w, "not found", http.StatusNotFound)
		return
	}
	if err := e.ensureRevisions(id); err != nil {
		e.fail(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"revisions": e.Store.History(id, r.URL.Query().Get("slot"))})
}

func (e *Engine) revisionYAML(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	if err := e.ensureRevisions(id); err != nil {
		e.fail(w, r, err)
		return
	}
	y, err := e.Store.RevisionYAML(id, chi.URLParam(r, "rev"))
	if err != nil {
		http.Error(w, "not found", http.StatusNotFound)
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"yaml": y})
}

func (e *Engine) revisionRestore(w http.ResponseWriter, r *http.Request) {
	id, rev := chi.URLParam(r, "id"), chi.URLParam(r, "rev")
	f, _ := decode(r)
	if err := e.ensureRevisions(id); err != nil {
		e.fail(w, r, err)
		return
	}
	if key := f.str("slotKey"); key != "" {
		if err := e.Store.RestoreSlot(id, rev, key); err != nil {
			e.back(w, r, slotURL(id, key), "payload", err)
			return
		}
		e.redirect(w, r, slotURL(id, key))
		return
	}
	if err := e.Store.RestoreBlueprint(id, rev); err != nil {
		e.back(w, r, "/blueprints/"+id+"/history", "restore", err)
		return
	}
	e.redirect(w, r, "/blueprints/"+id+"/history")
}

// --- Blocks -----------------------------------------------------------------

func (e *Engine) blocksIndex(w http.ResponseWriter, r *http.Request) {
	e.render(w, r, "Blocks/Index", props{"blocks": e.Store.ListBlocks(), "types": blocktypes.All(), "abilities": blocktypes.Abilities, "teams": e.teams()})
}

func (e *Engine) blockCreate(w http.ResponseWriter, r *http.Request) {
	f, err := decode(r)
	if err != nil {
		badRequest(w)
		return
	}
	if f.str("teamId") == "" {
		e.back(w, r, "/blocks", "teamId", errors.New("pick the team that owns this block"))
		return
	}
	b, err := e.Store.CreateBlock(BlockInput{TeamID: f.str("teamId"), Type: f.str("type"), Name: f.str("name"), Slug: f.str("slug"), Description: f.str("description")})
	if err != nil {
		e.back(w, r, "/blocks", "form", err)
		return
	}
	e.redirect(w, r, "/blocks/"+b.ID)
}

func (e *Engine) blockShow(w http.ResponseWriter, r *http.Request) {
	d, err := e.Store.GetBlock(chi.URLParam(r, "blockID"))
	if err != nil {
		e.notFound(w, r)
		return
	}
	bt, _ := blocktypes.Get(d.Type)
	e.render(w, r, "Blocks/Show", props{"block": d, "blockType": bt, "can": map[string]bool{"edit": true, "publish": true}})
}

func (e *Engine) blockUpdate(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "blockID")
	f, err := decode(r)
	if err != nil {
		badRequest(w)
		return
	}
	if err := e.Store.UpdateBlockDetails(id, f.str("name"), f.str("description")); err != nil {
		e.back(w, r, "/blocks/"+id, "details", err)
		return
	}
	e.redirect(w, r, "/blocks/"+id)
}

func (e *Engine) blockPublish(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "blockID")
	f, err := decode(r)
	if err != nil || f.obj("payload") == nil {
		badRequest(w)
		return
	}
	if _, err := e.Store.PublishBlock(id, f.obj("payload"), f.str("changelog")); err != nil {
		e.back(w, r, "/blocks/"+id, "payload", err)
		return
	}
	e.redirect(w, r, "/blocks/"+id)
}

func (e *Engine) blockRestoreVersion(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "blockID")
	n, err := strconv.Atoi(chi.URLParam(r, "version"))
	if err != nil {
		e.notFound(w, r)
		return
	}
	if err := e.Store.RestoreBlockVersion(id, n); err != nil {
		e.back(w, r, "/blocks/"+id, "payload", err)
		return
	}
	e.redirect(w, r, "/blocks/"+id)
}
