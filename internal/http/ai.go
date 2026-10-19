package http

import (
	"encoding/json"
	"errors"
	"fmt"
	nethttp "net/http"

	"github.com/go-chi/chi/v5"

	"github.com/docker/kit-maker/internal/ai"
	"github.com/docker/kit-maker/internal/auth"
	"github.com/docker/kit-maker/internal/authz"
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
	kitID, slotKey := chi.URLParam(r, "kitID"), chi.URLParam(r, "slotKey")
	l, ok := s.loadKit(w, r, kitID, authz.KitView)
	if !ok || !s.allowed(w, r, authz.AIUse, l.Kit.TeamID) {
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
		Kit: l.Kit.DisplayName, Goal: l.Kit.Goal, Slot: slot.Label,
		BlockName: slot.BlockName, BlockType: slot.BlockType, BlockDescription: slot.BlockDescription,
		Payload: slot.Effective, Question: f.Question,
	}
	for _, c := range l.Checks {
		if c.Source == slotKey {
			in.Checks = append(in.Checks, fmt.Sprintf("%s %s → expect %s: %s (%s)", c.Method, c.URL, c.Expect, c.Status, c.Why))
		}
	}
	req := ai.ExplainRequest(in)
	req.TeamID, req.KitID = l.Kit.TeamID, l.Kit.ID
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
			s.Log.WarnContext(r.Context(), "ai explain", "kit", kitID, "slot", slotKey, "error", err)
			_ = out.send("error", map[string]string{"message": msg})
		}
		return
	}
	_ = out.send("done", map[string]any{"model": res.Model, "usage": res.Usage})
}
