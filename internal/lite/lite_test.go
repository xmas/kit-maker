package lite

import (
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
)

type fakeHost struct{ revs map[string][]Doc }

func (fakeHost) Login(email, password string) (*User, error) {
	if password != "correct horse battery" {
		return nil, errors.New("wrong email or password")
	}
	return &User{ID: "u1", Email: email}, nil
}
func (fakeHost) Logout() error                       { return nil }
func (h fakeHost) Revisions(id string) ([]Doc, error) { return h.revs[id], nil }
func (fakeHost) AI() AI                              { return nil }

func starter(t *testing.T) *Store {
	t.Helper()
	b, err := os.ReadFile("../../cloudflare/public/starter.json")
	if err != nil {
		t.Skip("no starter.json (go run ./internal/tools/starterexport cloudflare/public/starter.json)")
	}
	var st struct {
		Team Team  `json:"team"`
		Docs []Doc `json:"docs"`
	}
	if err := json.Unmarshal(b, &st); err != nil {
		t.Fatal(err)
	}
	s := NewStore(st.Team)
	if err := s.LoadDocs(st.Docs); err != nil {
		t.Fatal(err)
	}
	return s
}

type page struct {
	Component string         `json:"component"`
	Props     map[string]any `json:"props"`
	URL       string         `json:"url"`
}

// do runs one request, following a redirect like Inertia does.
func do(t *testing.T, e *Engine, method, url string, body any) (*httptest.ResponseRecorder, page) {
	t.Helper()
	var r *http.Request
	if body != nil {
		b, _ := json.Marshal(body)
		r = httptest.NewRequest(method, url, strings.NewReader(string(b)))
		r.Header.Set("Content-Type", "application/json")
	} else {
		r = httptest.NewRequest(method, url, nil)
	}
	r.Header.Set("X-Inertia", "true")
	w := httptest.NewRecorder()
	e.ServeHTTP(w, r)
	if w.Code == http.StatusSeeOther {
		return do(t, e, http.MethodGet, w.Header().Get("Location"), nil)
	}
	var p page
	if strings.HasPrefix(w.Header().Get("Content-Type"), "application/json") {
		_ = json.Unmarshal(w.Body.Bytes(), &p)
	}
	return w, p
}

func TestEveryStarterBlueprintLoads(t *testing.T) {
	s := starter(t)
	for _, bp := range s.sortedBlueprints() {
		l, err := s.Load(bp.ID)
		if err != nil {
			t.Fatalf("%s: %v", bp.Slug, err)
		}
		blank := false
		for _, d := range l.Slots {
			blank = blank || d.Status == "blank"
		}
		if !l.Compiled.Valid && !blank {
			t.Errorf("%s doesn't compile: %s", bp.Slug, l.Compiled.Error)
		}
	}
}

func TestSignInAndEdit(t *testing.T) {
	s := starter(t)
	e := NewEngine(s, fakeHost{}, ModeShared)

	_, p := do(t, e, "GET", "/blueprints", nil)
	if p.Component != "Auth/Login" {
		t.Fatalf("signed out: got %s", p.Component)
	}
	_, p = do(t, e, "POST", "/login", map[string]any{"email": "a@example.com", "password": "nope", "next": "/blocks"})
	if p.Component != "Auth/Login" || p.Props["errors"].(map[string]any)["email"] == nil {
		t.Fatalf("bad password: %+v", p)
	}
	_, p = do(t, e, "POST", "/login", map[string]any{"email": "a@example.com", "password": "correct horse battery", "next": "/blocks"})
	if p.Component != "Blocks/Index" {
		t.Fatalf("after sign-in: %s", p.Component)
	}
	s.TakeWrites()

	_, p = do(t, e, "GET", "/blueprints/bp_acme-shop", nil)
	if p.Component != "Blueprints/Show" || p.Props["compiled"].(map[string]any)["yaml"] == "" {
		t.Fatalf("show: %+v", p.Component)
	}

	// Edit the GitHub egress slot: drop one runtime rule.
	l, _ := s.Load("bp_acme-shop")
	var key string
	for k, d := range l.Slots {
		if d.BlockSlug == "github-egress" {
			key = k
		}
	}
	if key == "" {
		t.Fatal("no github-egress slot")
	}
	eff := jsonCopy(l.Slots[key].Effective)
	rt := eff["runtime"].(map[string]any)
	allow := rt["allow"].([]any)
	rt["allow"] = allow[1:]
	w, p := do(t, e, "PUT", slotURL("bp_acme-shop", key), map[string]any{"payload": eff})
	if w.Code != 200 || p.Props["selected"] == nil {
		t.Fatalf("save: %d %v", w.Code, p.Props["errors"])
	}
	writes := s.TakeWrites()
	kinds := map[string]int{}
	for _, d := range writes {
		kinds[d.Kind]++
	}
	if kinds[KindBlueprint] != 1 || kinds[KindRevision] != 2 { // starting point + the edit
		t.Fatalf("writes: %v", kinds)
	}
	_, p = do(t, e, "GET", "/blueprints/bp_acme-shop/history", nil)
	if h, _ := p.Props["history"].([]any); len(h) != 2 {
		t.Fatalf("history: %v", p.Props["history"])
	}

	// Add a slot, start a block in it, save it to the library.
	_, p = do(t, e, "POST", "/blueprints/bp_acme-shop/slots", map[string]any{"type": "network-rules", "required": true})
	if !strings.Contains(p.URL, "/slots/network-rules") {
		t.Fatalf("add slot went to %s", p.URL)
	}
	_, p = do(t, e, "POST", "/blueprints/bp_acme-shop/slots/network-rules/custom", map[string]any{})
	if sel := p.Props["selected"].(map[string]any); sel["origin"] != OriginCustom {
		t.Fatalf("custom: %v", sel["origin"])
	}
	_, p = do(t, e, "POST", "/blueprints/bp_acme-shop/slots/network-rules/save-as-block", map[string]any{"name": "Shop extras"})
	if sel := p.Props["selected"].(map[string]any); sel["blockName"] != "Shop extras" {
		t.Fatalf("save as block: %v %v", sel["blockName"], p.Props["errors"])
	}

	// Restore the starting point.
	revs := s.History("bp_acme-shop", "")
	first := revs[len(revs)-1]
	_, p = do(t, e, "POST", "/blueprints/bp_acme-shop/revisions/"+first.ID+"/restore", map[string]any{})
	if l, _ := s.Load("bp_acme-shop"); l.Slots["network-rules"] != nil {
		t.Fatal("restore kept the added slot")
	}

	// A new blueprint.
	_, p = do(t, e, "POST", "/blueprints", map[string]any{"teamId": s.Team.ID, "displayName": "Test kit", "kind": "workload"})
	if p.Component != "Blueprints/Show" {
		t.Fatalf("create: %s %v", p.Component, p.Props["errors"])
	}

	// Downloads.
	w, _ = do(t, e, "GET", "/blueprints/bp_acme-shop/sbxenv.yaml", nil)
	if !strings.Contains(w.Body.String(), "kits:\n  - ./acme-shop") {
		t.Fatalf("sbxenv: %s", w.Body.String())
	}
}
