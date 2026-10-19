// Package settings stores per-deployment integrations and settings, sealed by
// the deployment's secrets keeper (DESIGN.md §1.2).
package settings

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/docker/kit-maker/internal/drivers"
	"github.com/docker/kit-maker/internal/ent"
	"github.com/docker/kit-maker/internal/ent/integration"
	"github.com/docker/kit-maker/internal/secrets"
)

// Service reads and writes integrations and deployment settings.
type Service struct {
	Ent    *ent.Client
	Keeper *secrets.Keeper
}

// Integration is the safe, redacted view of one integration.
type Integration struct {
	ID         string         `json:"id"`
	Kind       drivers.Kind   `json:"kind"`
	Driver     string         `json:"driver"`
	Label      string         `json:"label"`
	Name       string         `json:"name"`
	IsDefault  bool           `json:"isDefault"`
	VerifiedAt *time.Time     `json:"verifiedAt,omitempty"`
	LastError  string         `json:"lastError,omitempty"`
	Config     drivers.Config `json:"config"`
}

// SaveInput is what admins (UI or CLI) submit.
type SaveInput struct {
	Kind      drivers.Kind
	Driver    string
	Name      string
	Config    drivers.Config
	IsDefault bool
	// Test runs the driver's live test before saving; a failure blocks the
	// save unless the driver can't be tested from here yet.
	Test bool
}

// Save validates, optionally tests, seals and upserts an integration by
// (kind, name). Making it the default clears the previous default of that kind.
func (s *Service) Save(ctx context.Context, in SaveInput) (*Integration, error) {
	if !drivers.ValidKind(in.Kind) {
		return nil, fmt.Errorf("unknown integration kind %q", in.Kind)
	}
	d, err := drivers.Get(in.Kind, in.Driver)
	if err != nil {
		return nil, err
	}
	if in.Name == "" {
		in.Name = in.Driver
	}
	if in.Config == nil {
		in.Config = drivers.Config{}
	}
	if err := d.Validate(in.Config); err != nil {
		return nil, err
	}
	var verified *time.Time
	if in.Test {
		switch err := d.Test(ctx, in.Config); {
		case err == nil:
			now := time.Now()
			verified = &now
		case errors.Is(err, drivers.ErrTestUnavailable):
		default:
			return nil, fmt.Errorf("test failed: %w", err)
		}
	}
	sealed, err := s.Keeper.SealJSON(ctx, in.Config)
	if err != nil {
		return nil, err
	}

	tx, err := s.Ent.Tx(ctx)
	if err != nil {
		return nil, err
	}
	row, err := upsert(ctx, tx.Client(), in, sealed, verified)
	if err == nil && in.IsDefault {
		err = tx.Integration.Update().
			Where(integration.KindEQ(integration.Kind(in.Kind)), integration.IDNEQ(row.ID)).
			SetIsDefault(false).Exec(ctx)
	}
	if err != nil {
		_ = tx.Rollback()
		return nil, err
	}
	if err := tx.Commit(); err != nil {
		return nil, err
	}
	return s.view(row, d, in.Config), nil
}

func upsert(ctx context.Context, c *ent.Client, in SaveInput, sealed []byte, verified *time.Time) (*ent.Integration, error) {
	existing, err := c.Integration.Query().
		Where(integration.KindEQ(integration.Kind(in.Kind)), integration.Name(in.Name)).
		Only(ctx)
	switch {
	case ent.IsNotFound(err):
		return c.Integration.Create().
			SetKind(integration.Kind(in.Kind)).SetDriver(in.Driver).SetName(in.Name).
			SetConfigSealed(sealed).SetIsDefault(in.IsDefault).
			SetNillableVerifiedAt(verified).
			Save(ctx)
	case err != nil:
		return nil, err
	}
	u := existing.Update().SetDriver(in.Driver).SetConfigSealed(sealed).SetIsDefault(in.IsDefault).SetLastError("")
	if verified != nil {
		u.SetVerifiedAt(*verified)
	} else {
		u.ClearVerifiedAt()
	}
	return u.Save(ctx)
}

// List returns every integration, redacted.
func (s *Service) List(ctx context.Context) ([]Integration, error) {
	rows, err := s.Ent.Integration.Query().
		Order(ent.Asc(integration.FieldKind), ent.Asc(integration.FieldName)).
		All(ctx)
	if err != nil {
		return nil, err
	}
	out := make([]Integration, 0, len(rows))
	for _, r := range rows {
		d, cfg, err := s.open(ctx, r)
		if err != nil {
			return nil, err
		}
		out = append(out, *s.view(r, d, cfg))
	}
	return out, nil
}

// Test runs the live test for an integration and records the result.
func (s *Service) Test(ctx context.Context, id string) (*Integration, error) {
	r, err := s.Ent.Integration.Get(ctx, id)
	if err != nil {
		return nil, err
	}
	d, cfg, err := s.open(ctx, r)
	if err != nil {
		return nil, err
	}
	u := r.Update()
	switch terr := d.Test(ctx, cfg); {
	case terr == nil:
		u.SetVerifiedAt(time.Now()).SetLastError("")
	case errors.Is(terr, drivers.ErrTestUnavailable):
		u.SetLastError(terr.Error())
	default:
		u.ClearVerifiedAt().SetLastError(terr.Error())
	}
	r, err = u.Save(ctx)
	if err != nil {
		return nil, err
	}
	return s.view(r, d, cfg), nil
}

// Default returns the default integration of kind with its opened config.
func (s *Service) Default(ctx context.Context, kind drivers.Kind) (drivers.Driver, drivers.Config, error) {
	r, err := s.Ent.Integration.Query().
		Where(integration.KindEQ(integration.Kind(kind)), integration.IsDefault(true)).
		Only(ctx)
	if err != nil {
		return nil, nil, fmt.Errorf("no default %s integration: %w", kind, err)
	}
	return s.open(ctx, r)
}

func (s *Service) open(ctx context.Context, r *ent.Integration) (drivers.Driver, drivers.Config, error) {
	d, err := drivers.Get(drivers.Kind(r.Kind), r.Driver)
	if err != nil {
		return nil, nil, err
	}
	var cfg drivers.Config
	if err := s.Keeper.OpenJSON(ctx, r.ConfigSealed, &cfg); err != nil {
		return nil, nil, fmt.Errorf("integration %s: %w", r.Name, err)
	}
	return d, cfg, nil
}

func (s *Service) view(r *ent.Integration, d drivers.Driver, cfg drivers.Config) *Integration {
	return &Integration{
		ID: r.ID, Kind: drivers.Kind(r.Kind), Driver: r.Driver, Label: d.Label(), Name: r.Name,
		IsDefault: r.IsDefault, VerifiedAt: r.VerifiedAt, LastError: r.LastError,
		Config: drivers.Redact(d, cfg),
	}
}

// GetSetting opens a deployment setting into v. It reports false when unset.
func (s *Service) GetSetting(ctx context.Context, key string, v any) (bool, error) {
	r, err := s.Ent.DeploymentSetting.Get(ctx, key)
	if ent.IsNotFound(err) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	return true, s.Keeper.OpenJSON(ctx, r.ValueSealed, v)
}

// SetSetting seals v and stores it under key.
func (s *Service) SetSetting(ctx context.Context, key string, v any) error {
	sealed, err := s.Keeper.SealJSON(ctx, v)
	if err != nil {
		return err
	}
	return s.Ent.DeploymentSetting.Create().SetID(key).SetValueSealed(sealed).
		OnConflictColumns("key").UpdateNewValues().Exec(ctx)
}
