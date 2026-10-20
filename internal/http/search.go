package http

import (
	"encoding/json"
	"errors"
	nethttp "net/http"
	"sort"
	"strings"

	"github.com/docker/kit-maker/internal/ai"
	"github.com/docker/kit-maker/internal/authz"
	"github.com/docker/kit-maker/internal/blocks"
)

type searchResult struct {
	// Source says who ranked the matches: "ai", or "keyword" when AI isn't
	// available or found nothing usable (Message says why).
	Source  string           `json:"source"`
	Message string           `json:"message,omitempty"`
	Matches []ai.SearchMatch `json:"matches"`
}

// blockSearch finds library blocks for a plain-language need ("read Jira
// but never write"). The model ranks them and says why; without AI (or
// with an empty answer) it falls back to keyword ranking and says so.
func (s *server) blockSearch(w nethttp.ResponseWriter, r *nethttp.Request) {
	var f struct {
		Query string `json:"q"`
		Type  string `json:"type"`
	}
	if err := decodeForm(r, &f); err != nil || strings.TrimSpace(f.Query) == "" {
		nethttp.Error(w, "bad request", nethttp.StatusBadRequest)
		return
	}
	sub := authz.SubjectFrom(r.Context())
	teams, all := sub.TeamsWith(authz.BlockView)
	list, err := s.blocks().List(r.Context(), teams, all)
	if err != nil {
		s.fail(w, r, "list blocks", err)
		return
	}
	if f.Type != "" {
		kept := list[:0]
		for _, b := range list {
			if b.Type == f.Type {
				kept = append(kept, b)
			}
		}
		list = kept
	}

	res := searchResult{Source: "ai", Matches: []ai.SearchMatch{}}
	switch {
	case !sub.CanAnywhere(authz.AIUse):
		res.Source, res.Message = "keyword", "You don't have AI access, so these are keyword matches."
	case s.AI == nil:
		res.Source, res.Message = "keyword", "AI isn't set up for this deployment, so these are keyword matches."
	default:
		res.Matches, res.Message = s.aiSearch(r, f.Query, list)
		if len(res.Matches) == 0 {
			res.Source = "keyword"
			if res.Message == "" {
				res.Message = "The AI found nothing to recommend, so these are keyword matches."
			}
		}
	}
	if res.Source == "keyword" {
		res.Matches = keywordSearch(f.Query, list)
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(res)
}

func (s *server) aiSearch(r *nethttp.Request, query string, list []blocks.Summary) ([]ai.SearchMatch, string) {
	client, err := s.AI.Client(r.Context())
	if err != nil {
		return nil, "AI isn't set up for this deployment, so these are keyword matches."
	}
	items := make([]ai.SearchItem, 0, len(list))
	known := map[string]bool{}
	for _, b := range list {
		items = append(items, ai.SearchItem{ID: b.ID, Name: b.Name, Type: b.TypeLabel, Description: b.Description, Abilities: b.Abilities, Content: b.Text})
		known[b.ID] = true
	}
	req := ai.SearchRequest(query, items)
	req.UserID = userID(r)
	resp, err := client.Generate(r.Context(), req)
	if err != nil {
		s.Log.WarnContext(r.Context(), "ai search", "error", err)
		if errors.Is(err, ai.ErrOverBudget) {
			return nil, "Your team has used its AI budget for this month, so these are keyword matches."
		}
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
func keywordSearch(query string, list []blocks.Summary) []ai.SearchMatch {
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
