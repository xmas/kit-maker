package lite

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"sort"
	"strings"

	"github.com/go-chi/chi/v5"

	"github.com/docker/kit-maker/internal/ai"
	"github.com/docker/kit-maker/internal/blueprints/blocktypes"
	"github.com/docker/kit-maker/internal/blueprints/patch"
)

// AI runs model requests built by internal/ai's prompts. The Cloudflare
// edition forwards them to a Pages Function holding the API key.
type AI interface {
	Generate(req ai.Request) (*ai.Response, error)
	Stream(req ai.Request, onDelta func(string) error) (*ai.Response, error)
}

func (e *Engine) slotExplain(w http.ResponseWriter, r *http.Request) {
	id, key := chi.URLParam(r, "id"), chi.URLParam(r, "key")
	l, err := e.Store.Load(id)
	if err != nil {
		http.Error(w, "not found", http.StatusNotFound)
		return
	}
	slot := l.Slots[key]
	if slot == nil || slot.BlockSlug == "" {
		http.Error(w, "not found", http.StatusNotFound)
		return
	}
	model := e.Host.AI()
	if model == nil {
		http.Error(w, "AI is not configured for this deployment", http.StatusServiceUnavailable)
		return
	}
	f, _ := decode(r)
	in := ai.ExplainInput{
		Blueprint: l.Blueprint.DisplayName, Goal: l.Blueprint.Goal, Slot: slot.Label,
		BlockName: slot.BlockName, BlockType: slot.BlockType, BlockDescription: slot.BlockDescription,
		Payload: slot.Effective, Question: f.str("question"),
	}
	for _, c := range l.Checks {
		if c.Source == key {
			in.Checks = append(in.Checks, fmt.Sprintf("%s %s → expect %s: %s (%s)", c.Method, c.URL, c.Expect, c.Status, c.Why))
		}
	}
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(http.StatusOK)
	send := func(event string, data any) error {
		b, _ := json.Marshal(data)
		_, err := fmt.Fprintf(w, "event: %s\ndata: %s\n\n", event, b)
		if fl, ok := w.(http.Flusher); ok {
			fl.Flush()
		}
		return err
	}
	res, err := model.Stream(ai.ExplainRequest(in), func(d string) error { return send("delta", map[string]string{"text": d}) })
	if err != nil {
		_ = send("error", map[string]string{"message": aiMessage(err)})
		return
	}
	_ = send("done", map[string]any{"model": res.Model, "usage": res.Usage})
}

func aiMessage(err error) string {
	switch {
	case errors.Is(err, ai.ErrRefused):
		return "The model declined to answer this one."
	case errors.Is(err, ai.ErrOverBudget):
		return "This deployment has used its AI budget."
	}
	return "The AI request failed. Try again in a moment."
}

// draft asks the AI for a slot's block content: the validated payload and
// the model's note (no payload with a note means nothing to offer).
func (e *Engine) draft(l *Loaded, slot *SlotDetail, request string) (map[string]any, string, error) {
	model := e.Host.AI()
	if model == nil {
		return nil, "", errors.New("AI isn't set up for this deployment")
	}
	t, ok := blocktypes.Get(slot.BlockType)
	if !ok {
		return nil, "", fmt.Errorf("unknown block type %s", slot.BlockType)
	}
	current := slot.Effective
	if current == nil {
		current = blocktypes.Starter(t.Key, slot.Key)
	}
	res, err := model.Generate(ai.DraftRequest(ai.DraftInput{
		Blueprint: l.Blueprint.DisplayName, Goal: l.Blueprint.Goal,
		BlockType: t.Key, TypeLabel: t.Label, TypeDescription: t.Description, Capability: t.Capability,
		Current: current, Request: request,
	}))
	switch {
	case errors.Is(err, ai.ErrRefused):
		return nil, "", errors.New("the model declined this request")
	case err != nil:
		return nil, "", errors.New("the AI request failed; try again in a moment")
	}
	var ans ai.DraftAnswer
	if err := json.Unmarshal(res.JSON, &ans); err != nil {
		return nil, "", errors.New("the AI's answer wasn't readable; try again")
	}
	if strings.TrimSpace(ans.PayloadJSON) == "" {
		return nil, ans.Note, nil
	}
	var payload map[string]any
	if err := json.Unmarshal([]byte(ans.PayloadJSON), &payload); err != nil {
		return nil, "", errors.New("the AI's block wasn't valid JSON; try again")
	}
	if err := t.Validate(payload); err != nil {
		return nil, "", fmt.Errorf("the AI's block doesn't fit a %s block: %w", t.Label, err)
	}
	patch.AssignKeys(payload)
	return payload, ans.Note, nil
}

func (e *Engine) slotDraft(w http.ResponseWriter, r *http.Request) {
	id, key := chi.URLParam(r, "id"), chi.URLParam(r, "key")
	l, err := e.Store.Load(id)
	if err != nil {
		http.Error(w, "not found", http.StatusNotFound)
		return
	}
	slot := l.Slots[key]
	f, err := decode(r)
	if slot == nil || err != nil || strings.TrimSpace(f.str("prompt")) == "" {
		badRequest(w)
		return
	}
	out := map[string]any{}
	payload, note, err := e.draft(l, slot, f.str("prompt"))
	if err != nil {
		out["error"] = err.Error()
	} else {
		out["note"] = note
		if payload != nil {
			out["payload"] = payload
		}
	}
	writeJSON(w, http.StatusOK, out)
}

// slotCustom fills a blank with a new block private to this blueprint,
// drafted by AI when a prompt is given.
func (e *Engine) slotCustom(w http.ResponseWriter, r *http.Request) {
	id, key := chi.URLParam(r, "id"), chi.URLParam(r, "key")
	f, _ := decode(r)
	if err := e.Store.StartCustomBlock(id, key); err != nil {
		e.back(w, r, slotURL(id, key), "payload", err)
		return
	}
	if prompt := strings.TrimSpace(f.str("prompt")); prompt != "" && e.Host.AI() != nil {
		if l, err := e.Store.Load(id); err == nil && l.Slots[key] != nil {
			payload, note, err := e.draft(l, l.Slots[key], prompt)
			switch {
			case err != nil:
				e.back(w, r, slotURL(id, key), "payload", fmt.Errorf("started an empty block; the AI draft failed: %w", err))
				return
			case payload == nil:
				e.back(w, r, slotURL(id, key), "payload", fmt.Errorf("started an empty block. %s", note))
				return
			}
			if err := e.Store.SaveSlot(id, key, payload); err != nil {
				e.back(w, r, slotURL(id, key), "payload", err)
				return
			}
		}
	}
	e.redirect(w, r, slotURL(id, key))
}

type searchResult struct {
	Source  string           `json:"source"`
	Message string           `json:"message,omitempty"`
	Matches []ai.SearchMatch `json:"matches"`
}

// blockSearch finds library blocks for a plain-language need, with AI when
// it's set up and keyword ranking otherwise.
func (e *Engine) blockSearch(w http.ResponseWriter, r *http.Request) {
	f, err := decode(r)
	if err != nil || strings.TrimSpace(f.str("q")) == "" {
		badRequest(w)
		return
	}
	list := e.Store.ListBlocks()
	if t := f.str("type"); t != "" {
		kept := list[:0]
		for _, b := range list {
			if b.Type == t {
				kept = append(kept, b)
			}
		}
		list = kept
	}
	res := searchResult{Source: "ai", Matches: []ai.SearchMatch{}}
	if model := e.Host.AI(); model == nil {
		res.Source, res.Message = "keyword", "AI isn't set up for this deployment, so these are keyword matches."
	} else {
		res.Matches, res.Message = aiSearch(model, f.str("q"), list)
		if len(res.Matches) == 0 {
			res.Source = "keyword"
			if res.Message == "" {
				res.Message = "The AI found nothing to recommend, so these are keyword matches."
			}
		}
	}
	if res.Source == "keyword" {
		res.Matches = keywordSearch(f.str("q"), list)
	}
	writeJSON(w, http.StatusOK, res)
}

func aiSearch(model AI, query string, list []BlockSummary) ([]ai.SearchMatch, string) {
	items := make([]ai.SearchItem, 0, len(list))
	known := map[string]bool{}
	for _, b := range list {
		items = append(items, ai.SearchItem{ID: b.ID, Name: b.Name, Type: b.TypeLabel, Description: b.Description, Abilities: b.Abilities, Content: b.Text})
		known[b.ID] = true
	}
	resp, err := model.Generate(ai.SearchRequest(query, items))
	if err != nil {
		return nil, "The AI request failed, so these are keyword matches."
	}
	var out struct {
		Matches []ai.SearchMatch `json:"matches"`
	}
	_ = json.Unmarshal(resp.JSON, &out)
	kept := out.Matches[:0]
	for _, m := range out.Matches {
		if known[m.ID] { // never trust ids the model made up
			kept = append(kept, m)
		}
	}
	return kept, ""
}

// keywordSearch ranks blocks by how many query words they contain, name
// hits first.
func keywordSearch(query string, list []BlockSummary) []ai.SearchMatch {
	words := strings.Fields(strings.ToLower(query))
	type scored struct {
		m ai.SearchMatch
		n int
	}
	var hits []scored
	for _, b := range list {
		name := strings.ToLower(b.Name + " " + b.Slug)
		rest := strings.ToLower(b.Description + " " + b.TypeLabel + " " + strings.Join(b.Abilities, " ") + " " + b.Text)
		n, found := 0, []string{}
		for _, w := range words {
			switch {
			case strings.Contains(name, w):
				n += 3
				found = append(found, w)
			case strings.Contains(rest, w):
				n++
				found = append(found, w)
			}
		}
		if n > 0 {
			hits = append(hits, scored{ai.SearchMatch{ID: b.ID, Reason: "Mentions " + strings.Join(found, ", ") + "."}, n})
		}
	}
	sort.SliceStable(hits, func(i, j int) bool { return hits[i].n > hits[j].n })
	out := make([]ai.SearchMatch, 0, len(hits))
	for i, h := range hits {
		if i == 10 {
			break
		}
		out = append(out, h.m)
	}
	return out
}
