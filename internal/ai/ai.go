// Package ai is Kit Maker's model client (DESIGN.md §3). Callers build a
// Request for a route; the route picks the effort. Clients exist for the
// Anthropic API, Bedrock and Vertex, plus a fake that replays fixtures so tests
// and local stacks need no key. Metered wraps any client with budgets and usage
// rows.
package ai

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
)

// Model is the default model for every route (DESIGN.md §3).
const Model = "claude-opus-5-5"

// Route names a place in the product that calls the model.
type Route string

const (
	RoutePing         Route = "ping"         // integration test
	RouteExplain      Route = "explain"      // field and block help
	RouteAutocomplete Route = "autocomplete" // hosts, paths, env names
	RouteFill         Route = "fill"         // options for a blank
	RouteEdit         Route = "edit"         // Ask AI on a block
	RouteChecks       Route = "checks"       // goal → checks
	RoutePlan         Route = "plan"         // goal + repo → outline
	RouteReview       Route = "review"       // rev review, AI approver
	RouteCurate       Route = "curate"       // library hygiene
	RouteImport       Route = "import"       // YAML/repo → blocks
	RouteTroubleshoot Route = "troubleshoot" // sbx errors → fix
)

// Effort is the output_config effort level.
type Effort string

const (
	EffortLow    Effort = "low"
	EffortMedium Effort = "medium"
	EffortHigh   Effort = "high"
)

var routeEffort = map[Route]Effort{
	RoutePing:         EffortLow,
	RouteExplain:      EffortLow,
	RouteAutocomplete: EffortLow,
	RouteFill:         EffortMedium,
	RouteEdit:         EffortMedium,
	RouteChecks:       EffortMedium,
	RouteImport:       EffortMedium,
	RouteTroubleshoot: EffortMedium,
	RouteCurate:       EffortMedium,
	RoutePlan:         EffortHigh,
	RouteReview:       EffortHigh,
}

// EffortFor returns the effort a route runs at.
func EffortFor(r Route) Effort {
	if e, ok := routeEffort[r]; ok {
		return e
	}
	return EffortMedium
}

// Request is one model call. System and Context are cached prefixes: System
// is the frozen product prefix (spec excerpts, schemas, block catalogue) at a
// 1h TTL; Context is slower-changing per-team material (library summaries) at
// the default TTL. Prompt is the per-request content after the breakpoints.
type Request struct {
	Route   Route
	System  []string
	Context []string
	Prompt  string
	// Schema, when set, is the JSON Schema for structured output; the answer
	// arrives in Response.JSON.
	Schema    map[string]any
	MaxTokens int64

	// Attribution for usage rows and budgets.
	TeamID string
	UserID string
	KitID  string
}

func (r Request) maxTokens() int64 {
	if r.MaxTokens > 0 {
		return r.MaxTokens
	}
	return 4096
}

// Key is a stable hash of the request content, used to name fixtures.
func (r Request) Key() string {
	b, _ := json.Marshal(struct {
		System, Context []string
		Prompt          string
		Schema          map[string]any
	}{r.System, r.Context, r.Prompt, r.Schema})
	h := sha256.Sum256(b)
	return string(r.Route) + "-" + hex.EncodeToString(h[:6])
}

// Usage is the token count of one call.
type Usage struct {
	InputTokens      int64 `json:"inputTokens"`
	OutputTokens     int64 `json:"outputTokens"`
	CacheReadTokens  int64 `json:"cacheReadTokens"`
	CacheWriteTokens int64 `json:"cacheWriteTokens"`
}

// Billable weighs tokens for budgets: cache reads count a tenth.
func (u Usage) Billable() int64 {
	return u.InputTokens + u.CacheWriteTokens + u.OutputTokens + u.CacheReadTokens/10
}

// Response is the model's answer.
type Response struct {
	Text       string          `json:"text"`
	JSON       json.RawMessage `json:"json,omitempty"`
	Model      string          `json:"model"`
	StopReason string          `json:"stopReason"`
	Usage      Usage           `json:"usage"`
}

// Client calls a model. Stream calls onDelta with text as it arrives and
// returns the full response at the end.
type Client interface {
	Provider() string
	Generate(ctx context.Context, req Request) (*Response, error)
	Stream(ctx context.Context, req Request, onDelta func(string) error) (*Response, error)
}

var (
	// ErrRefused means the model (and any fallback) declined to answer.
	ErrRefused = errors.New("ai: the model declined this request")
	// ErrOverBudget means the team has used its AI budget for the month.
	ErrOverBudget = errors.New("ai: monthly AI budget used up for this team")
	// ErrNoFixture means the fake client has no recorded answer.
	ErrNoFixture = errors.New("ai: no fixture for this request")
)
