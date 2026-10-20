// Package seed loads a demo team, a starter block and one demo blueprint. It is
// idempotent. The full starter library from the spec examples comes in
// phase 3.
package seed

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"github.com/docker/kit-maker/internal/ent"
	"github.com/docker/kit-maker/internal/ent/roleassignment"
	"github.com/docker/kit-maker/internal/ent/team"
	"github.com/docker/kit-maker/internal/ent/teammembership"
	"github.com/docker/kit-maker/internal/ent/user"
)

// SpecVersion is the sandbox-kit-spec release the seed payloads target.
const SpecVersion = "v3.0.0-m.8"

// Options choose what a seed loads.
type Options struct {
	// Samples loads the starter content that ships with every release: the
	// starter block library (public to every team), every kit spec example
	// as a blueprint, the sample profile blueprints and the demos. Re-running
	// on upgrade refreshes starter blocks whose content changed.
	Samples bool
}

// Run seeds the database with the starter content. adminEmail is the
// bootstrap owner.
func Run(ctx context.Context, client *ent.Client, adminEmail string) error {
	return RunWith(ctx, client, adminEmail, Options{Samples: true})
}

// RunWith seeds the bootstrap team and Owner, and the starter content when
// opts.Samples is set.
func RunWith(ctx context.Context, client *ent.Client, adminEmail string, opts Options) error {
	tx, err := client.Tx(ctx)
	if err != nil {
		return err
	}
	if err := run(ctx, tx.Client(), adminEmail, opts); err != nil {
		_ = tx.Rollback()
		return err
	}
	return tx.Commit()
}

func run(ctx context.Context, c *ent.Client, adminEmail string, opts Options) error {
	t, err := c.Team.Query().Where(team.Slug("platform")).Only(ctx)
	if ent.IsNotFound(err) {
		t, err = c.Team.Create().SetName("Platform").SetSlug("platform").Save(ctx)
	}
	if err != nil {
		return fmt.Errorf("team: %w", err)
	}

	adminEmail = strings.ToLower(strings.TrimSpace(adminEmail))
	u, err := c.User.Query().Where(user.Email(adminEmail)).Only(ctx)
	if ent.IsNotFound(err) {
		u, err = c.User.Create().SetEmail(adminEmail).SetDisplayName("Admin").Save(ctx)
		if err == nil {
			err = c.TeamMembership.Create().SetTeam(t).SetUser(u).SetRole("team_admin").SetPinned(true).Exec(ctx)
		}
	}
	if err != nil {
		return fmt.Errorf("admin user: %w", err)
	}
	// The bootstrap account is the deployment's break-glass Owner.
	err = c.RoleAssignment.Create().SetRoleKey("owner").
		SetPrincipalType(roleassignment.PrincipalTypeUser).SetPrincipalID(u.ID).
		OnConflictColumns(roleassignment.FieldPrincipalType, roleassignment.FieldPrincipalID, roleassignment.FieldRoleKey).
		DoNothing().Exec(ctx)
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return fmt.Errorf("owner role: %w", err)
	}
	// Seeds from before team roles existed used "owner" as a team role.
	if _, err := c.TeamMembership.Update().Where(teammembership.Role("owner")).SetRole("team_admin").Save(ctx); err != nil {
		return err
	}

	if !opts.Samples {
		return nil
	}
	return demo(ctx, c, t, u)
}

func hash(v any) string {
	b, _ := json.Marshal(v) // map keys marshal sorted, so this is stable
	sum := sha256.Sum256(b)
	return "sha256:" + hex.EncodeToString(sum[:])
}
