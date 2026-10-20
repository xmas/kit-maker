package ai

import (
	"context"
	"database/sql"
	"errors"
	"time"

	"github.com/docker/kit-maker/internal/drivers"
	"github.com/docker/kit-maker/internal/ent"
	"github.com/docker/kit-maker/internal/ent/aicall"
)

// Budget is the deployment's AI budget setting (key "ai.budget"), in billable
// tokens per team per calendar month (UTC). Zero means no limit. Per-team
// overrides are keyed by team ID.
type Budget struct {
	TeamMonthlyTokens int64            `json:"teamMonthlyTokens"`
	Teams             map[string]int64 `json:"teams,omitempty"`
}

// BudgetSettingKey is the deployment setting that holds Budget.
const BudgetSettingKey = "ai.budget"

// LimitFor returns a team's monthly limit.
func (b Budget) LimitFor(teamID string) int64 {
	if v, ok := b.Teams[teamID]; ok {
		return v
	}
	return b.TeamMonthlyTokens
}

// Metered wraps a client: it refuses calls once a team is over budget and
// records every call as an ai_calls row.
type Metered struct {
	Client
	Ent *ent.Client
	// Budget loads the current budget; nil means no limits.
	Budget func(ctx context.Context) (Budget, error)
	Now    func() time.Time
}

func (m *Metered) now() time.Time {
	if m.Now != nil {
		return m.Now()
	}
	return time.Now()
}

func (m *Metered) Generate(ctx context.Context, req Request) (*Response, error) {
	return m.run(ctx, req, func() (*Response, error) { return m.Client.Generate(ctx, req) })
}

func (m *Metered) Stream(ctx context.Context, req Request, onDelta func(string) error) (*Response, error) {
	return m.run(ctx, req, func() (*Response, error) { return m.Client.Stream(ctx, req, onDelta) })
}

func (m *Metered) run(ctx context.Context, req Request, call func() (*Response, error)) (*Response, error) {
	if err := m.check(ctx, req.TeamID); err != nil {
		m.record(ctx, req, nil, err, 0)
		return nil, err
	}
	start := m.now()
	res, err := call()
	m.record(ctx, req, res, err, m.now().Sub(start))
	return res, err
}

func (m *Metered) check(ctx context.Context, teamID string) error {
	if m.Budget == nil || teamID == "" {
		return nil
	}
	b, err := m.Budget(ctx)
	if err != nil {
		return err
	}
	limit := b.LimitFor(teamID)
	if limit <= 0 {
		return nil
	}
	used, err := Used(ctx, m.Ent, teamID, m.now())
	if err != nil {
		return err
	}
	if used >= limit {
		return ErrOverBudget
	}
	return nil
}

// record writes the usage row. It uses a context that survives the request
// being cancelled mid-stream, so spend is never lost.
func (m *Metered) record(ctx context.Context, req Request, res *Response, callErr error, d time.Duration) {
	ctx = context.WithoutCancel(ctx)
	status := aicall.StatusOk
	switch {
	case errors.Is(callErr, ErrOverBudget):
		status = aicall.StatusOverBudget
	case errors.Is(callErr, ErrRefused):
		status = aicall.StatusRefused
	case callErr != nil:
		status = aicall.StatusError
	}
	c := m.Ent.AICall.Create().
		SetRoute(string(req.Route)).
		SetProvider(m.Client.Provider()).
		SetStatus(status).
		SetDurationMs(d.Milliseconds()).
		SetNillableTeamID(nilIfEmpty(req.TeamID)).
		SetNillableUserID(nilIfEmpty(req.UserID)).
		SetNillableBlueprintID(nilIfEmpty(req.BlueprintID))
	if res != nil {
		c.SetModel(res.Model).
			SetInputTokens(res.Usage.InputTokens).
			SetOutputTokens(res.Usage.OutputTokens).
			SetCacheReadTokens(res.Usage.CacheReadTokens).
			SetCacheWriteTokens(res.Usage.CacheWriteTokens)
	}
	if callErr != nil {
		c.SetError(callErr.Error())
	}
	_ = c.Exec(ctx) // usage logging must not fail the user's call
}

// Used returns a team's billable tokens since the start of now's month (UTC).
func Used(ctx context.Context, c *ent.Client, teamID string, now time.Time) (int64, error) {
	now = now.UTC()
	since := time.Date(now.Year(), now.Month(), 1, 0, 0, 0, 0, time.UTC)
	var rows []struct {
		In, Out, Read, Write sql.NullInt64
	}
	err := c.AICall.Query().
		Where(aicall.TeamID(teamID), aicall.CreatedAtGTE(since)).
		Aggregate(
			ent.As(ent.Sum(aicall.FieldInputTokens), "In"),
			ent.As(ent.Sum(aicall.FieldOutputTokens), "Out"),
			ent.As(ent.Sum(aicall.FieldCacheReadTokens), "Read"),
			ent.As(ent.Sum(aicall.FieldCacheWriteTokens), "Write"),
		).
		Scan(ctx, &rows)
	if err != nil || len(rows) == 0 {
		return 0, err
	}
	r := rows[0]
	return Usage{InputTokens: r.In.Int64, OutputTokens: r.Out.Int64, CacheReadTokens: r.Read.Int64, CacheWriteTokens: r.Write.Int64}.Billable(), nil
}

func nilIfEmpty(s string) *string {
	if s == "" {
		return nil
	}
	return &s
}

// Provider hands out a metered client for the deployment's default AI
// integration. It resolves the integration on every call, so an admin
// switching providers takes effect without a restart.
type Provider struct {
	Default     func(ctx context.Context) (drivers.Driver, drivers.Config, error)
	Ent         *ent.Client
	Budget      func(ctx context.Context) (Budget, error)
	FixturesDir string
}

// Client returns the metered client.
func (p *Provider) Client(ctx context.Context) (Client, error) {
	d, cfg, err := p.Default(ctx)
	if err != nil {
		return nil, err
	}
	c, err := New(ctx, d.Name(), cfg, p.FixturesDir)
	if err != nil {
		return nil, err
	}
	return &Metered{Client: c, Ent: p.Ent, Budget: p.Budget}, nil
}
