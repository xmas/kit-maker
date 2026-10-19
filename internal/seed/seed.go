// Package seed loads a demo team, a starter block and one demo kit. It is
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

// Run seeds the database. adminEmail is the bootstrap owner.
func Run(ctx context.Context, client *ent.Client, adminEmail string) error {
	tx, err := client.Tx(ctx)
	if err != nil {
		return err
	}
	if err := run(ctx, tx.Client(), adminEmail); err != nil {
		_ = tx.Rollback()
		return err
	}
	return tx.Commit()
}

func run(ctx context.Context, c *ent.Client, adminEmail string) error {
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

	return demo(ctx, c, t, u)
}

func hash(v any) string {
	b, _ := json.Marshal(v) // map keys marshal sorted, so this is stable
	sum := sha256.Sum256(b)
	return "sha256:" + hex.EncodeToString(sum[:])
}
