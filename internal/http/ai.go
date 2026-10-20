package http

import (
	"encoding/json"
	"errors"
	"fmt"
	nethttp "net/http"
	"strings"

	"github.com/go-chi/chi/v5"

	"github.com/docker/kit-maker/internal/ai"
	"github.com/docker/kit-maker/internal/auth"
	"github.com/docker/kit-maker/internal/authz"
	"github.com/docker/kit-maker/internal/blueprints"
	"github.com/docker/kit-maker/internal/blueprints/blocktypes"
	"github.com/docker/kit-maker/internal/blueprints/patch"
)

// sse writes server-sent events and flushes each one. Every wrapper in the
// middleware chain exposes Unwrap, so the response controller reaches the
// real connection.
type sse struct {
	w  nethttp.ResponseWriter
	rc *nethttp.ResponseController
}

func newSSE(w nethttp.ResponseWriter) *sse {
	h := w.Header()
	h.Set("Content-Type", "text/event-stream")
	h.Set("Cache-Control", "no-store")
	h.Set("X-Accel-Buffering", "no") // nginx and OpenShift routers
	w.WriteHeader(nethttp.StatusOK)
	return &sse{w: w, rc: nethttp.NewResponseController(w)}
}

func (s *sse) send(event string, data any) error {
	b, err := json.Marshal(data)
	if err != nil {
		return err
	}
	if _, err := fmt.Fprintf(s.w, "event: %s\ndata: %s\n\n", event, b); err != nil {
		return err
	}
	return s.rc.Flush()
}

type explainForm struct {
	Question string `json:"question"`
}

// slotExplain streams the AI's explanation of one slot.
func (s *server) slotExplain(w nethttp.ResponseWriter, r *nethttp.Request) {
	blueprintID, slotKey := chi.URLParam(r, "blueprintID"), chi.URLParam(r, "slotKey")
	l, ok := s.loadBlueprint(w, r, blueprintID, authz.BlueprintView)
	if !ok || !s.allowed(w, r, authz.AIUse, l.Blueprint.TeamID) {
		return
	}
	slot := l.Slots[slotKey]
	if slot == nil || slot.BlockSlug == "" {
		nethttp.NotFound(w, r)
		return
	}
	var f explainForm
	_ = decodeForm(r, &f) // the question is optional

	in := ai.ExplainInput{
		Blueprint: l.Blueprint.DisplayName, Goal: l.Blueprint.Goal, Slot: slot.Label,
		BlockName: slot.BlockName, BlockType: slot.BlockType, BlockDescription: slot.BlockDescription,
		Payload: slot.Effective, Question: f.Question,
	}
	for _, c := range l.Checks {
		if c.Source == slotKey {
			in.Checks = append(in.Checks, fmt.Sprintf("%s %s → expect %s: %s (%s)", c.Method, c.URL, c.Expect, c.Status, c.Why))
		}
	}
	req := ai.ExplainRequest(in)
	req.TeamID, req.BlueprintID = l.Blueprint.TeamID, l.Blueprint.ID
	if u := auth.UserFrom(r.Context()); u != nil {
		req.UserID = u.ID
	}

	if s.AI == nil {
		nethttp.Error(w, "AI is not configured for this deployment", nethttp.StatusServiceUnavailable)
		return
	}
	client, err := s.AI.Client(r.Context())
	if err != nil {
		s.Log.ErrorContext(r.Context(), "ai client", "error", err)
		nethttp.Error(w, "AI is not configured for this deployment", nethttp.StatusServiceUnavailable)
		return
	}
	out := newSSE(w)
	res, err := client.Stream(r.Context(), req, func(d string) error {
		return out.send("delta", map[string]string{"text": d})
	})
	if err != nil {
		msg := "The AI request failed. Try again in a moment."
		switch {
		case errors.Is(err, ai.ErrOverBudget):
			msg = "Your team has used its AI budget for this month."
		case errors.Is(err, ai.ErrRefused):
			msg = "The model declined to answer this one."
		}
		if r.Context().Err() == nil {
			s.Log.WarnContext(r.Context(), "ai explain", "blueprint", blueprintID, "slot", slotKey, "error", err)
			_ = out.send("error", map[string]string{"message": msg})
		}
		return
	}
	_ = out.send("done", map[string]any{"model": res.Model, "usage": res.Usage})
}

// draft asks the AI for a slot's block content. It returns the payload
// (validated for the block type, item keys kept) and the model's note; an
// empty payload with a note means the AI had nothing to offer.
func (s *server) draft(r *nethttp.Request, l *blueprints.Loaded, slot *blueprints.SlotDetail, request string) (map[string]any, string, error) {
	if s.AI == nil {
		return nil, "", errors.New("AI isn't set up for this deployment")
	}
	client, err := s.AI.Client(r.Context())
	if err != nil {
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
	req := ai.DraftRequest(ai.DraftInput{
		Blueprint: l.Blueprint.DisplayName, Goal: l.Blueprint.Goal,
		BlockType: t.Key, TypeLabel: t.Label, TypeDescription: t.Description, Capability: t.Capability,
		Current: current, Request: request,
	})
	req.TeamID, req.BlueprintID, req.UserID = l.Blueprint.TeamID, l.Blueprint.ID, userID(r)
	res, err := client.Generate(r.Context(), req)
	switch {
	case errors.Is(err, ai.ErrOverBudget):
		return nil, "", errors.New("your team has used its AI budget for this month")
	case errors.Is(err, ai.ErrRefused):
		return nil, "", errors.New("the model declined this request")
	case err != nil:
		s.Log.WarnContext(r.Context(), "ai draft", "error", err)
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

// slotDraft returns an AI draft of a slot's block as an unsaved payload;
// the editor shows it as a draft (with its YAML diff) until it's saved.
func (s *server) slotDraft(w nethttp.ResponseWriter, r *nethttp.Request) {
	blueprintID, slotKey := chi.URLParam(r, "blueprintID"), chi.URLParam(r, "slotKey")
	l, ok := s.loadBlueprint(w, r, blueprintID, authz.BlueprintEdit)
	if !ok || !s.allowed(w, r, authz.AIUse, l.Blueprint.TeamID) {
		return
	}
	slot := l.Slots[slotKey]
	var f struct {
		Prompt string `json:"prompt"`
	}
	if slot == nil || decodeForm(r, &f) != nil || strings.TrimSpace(f.Prompt) == "" {
		nethttp.Error(w, "bad request", nethttp.StatusBadRequest)
		return
	}
	out := map[string]any{}
	payload, note, err := s.draft(r, l, slot, f.Prompt)
	if err != nil {
		out["error"] = err.Error()
	} else {
		out["note"] = note
		if payload != nil {
			out["payload"] = payload
		}
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(out)
}
