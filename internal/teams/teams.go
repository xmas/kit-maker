// Package teams manages teams, nesting and memberships (DESIGN.md §13).
// Only admins create teams; this slice covers manual membership.
package teams

import (
	"context"
	"fmt"
	"regexp"
	"strings"

	"github.com/docker/kit-maker/internal/auth"
	"github.com/docker/kit-maker/internal/authz"
	"github.com/docker/kit-maker/internal/ent"
	"github.com/docker/kit-maker/internal/ent/roleassignment"
	"github.com/docker/kit-maker/internal/ent/team"
	"github.com/docker/kit-maker/internal/ent/teammembership"
	"github.com/docker/kit-maker/internal/ent/user"
)

// Service manages teams and role assignments.
type Service struct{ Ent *ent.Client }

var slugRE = regexp.MustCompile(`^[a-z0-9][a-z0-9-]{0,62}$`)

// Create makes a team, optionally nested under parentSlug.
func (s *Service) Create(ctx context.Context, name, slug, parentSlug string) (*ent.Team, error) {
	if slug == "" {
		slug = slugify(name)
	}
	if !slugRE.MatchString(slug) {
		return nil, fmt.Errorf("team slug %q must be lowercase letters, digits and dashes", slug)
	}
	c := s.Ent.Team.Create().SetName(name).SetSlug(slug)
	if parentSlug != "" {
		p, err := s.Ent.Team.Query().Where(team.Slug(parentSlug)).Only(ctx)
		if err != nil {
			return nil, fmt.Errorf("parent team %s: %w", parentSlug, err)
		}
		c.SetParent(p)
	}
	return c.Save(ctx)
}

// SetParent moves a team under another (or to the top with parentSlug "").
// It refuses cycles.
func (s *Service) SetParent(ctx context.Context, slug, parentSlug string) error {
	t, err := s.Ent.Team.Query().Where(team.Slug(slug)).Only(ctx)
	if err != nil {
		return err
	}
	if parentSlug == "" {
		return t.Update().ClearParent().Exec(ctx)
	}
	p, err := s.Ent.Team.Query().Where(team.Slug(parentSlug)).Only(ctx)
	if err != nil {
		return err
	}
	for cur := p; cur != nil; {
		if cur.ID == t.ID {
			return fmt.Errorf("moving %s under %s would make a cycle", slug, parentSlug)
		}
		if cur.ParentTeamID == nil {
			break
		}
		cur, err = s.Ent.Team.Get(ctx, *cur.ParentTeamID)
		if err != nil {
			return err
		}
	}
	return t.Update().SetParent(p).Exec(ctx)
}

// AddMember gives email a team role through a manual (pinned) membership,
// replacing any earlier manual role in that team.
func (s *Service) AddMember(ctx context.Context, teamSlug, email, role string) error {
	r, ok := authz.LookupRole(role)
	if !ok || r.Scope != authz.ScopeTeam {
		return fmt.Errorf("%q is not a team role (viewer, contributor, maintainer, team_admin)", role)
	}
	t, u, err := s.teamAndUser(ctx, teamSlug, email)
	if err != nil {
		return err
	}
	tx, err := s.Ent.Tx(ctx)
	if err != nil {
		return err
	}
	if _, err := tx.TeamMembership.Delete().Where(teammembership.TeamID(t.ID), teammembership.UserID(u.ID),
		teammembership.Pinned(true)).Exec(ctx); err != nil {
		_ = tx.Rollback()
		return err
	}
	if err := tx.TeamMembership.Create().SetTeam(t).SetUser(u).SetRole(r.Key).SetPinned(true).Exec(ctx); err != nil {
		_ = tx.Rollback()
		return err
	}
	return tx.Commit()
}

// RemoveMember deletes manual memberships of email in a team.
func (s *Service) RemoveMember(ctx context.Context, teamSlug, email string) error {
	t, u, err := s.teamAndUser(ctx, teamSlug, email)
	if err != nil {
		return err
	}
	_, err = s.Ent.TeamMembership.Delete().Where(teammembership.TeamID(t.ID), teammembership.UserID(u.ID),
		teammembership.Pinned(true)).Exec(ctx)
	return err
}

// AssignDeploymentRole grants a deployment role to a user.
func (s *Service) AssignDeploymentRole(ctx context.Context, email, role string) error {
	r, ok := authz.LookupRole(role)
	if !ok || r.Scope != authz.ScopeDeployment {
		return fmt.Errorf("%q is not a deployment role (owner, admin, auditor, member)", role)
	}
	u, err := s.Ent.User.Query().Where(user.Email(auth.NormalizeEmail(email))).Only(ctx)
	if err != nil {
		return fmt.Errorf("find %s: %w", email, err)
	}
	return s.Ent.RoleAssignment.Create().SetRoleKey(r.Key).
		SetPrincipalType(roleassignment.PrincipalTypeUser).SetPrincipalID(u.ID).
		OnConflictColumns(roleassignment.FieldPrincipalType, roleassignment.FieldPrincipalID, roleassignment.FieldRoleKey).
		DoNothing().Exec(ctx)
}

// RevokeDeploymentRole removes a deployment role from a user.
func (s *Service) RevokeDeploymentRole(ctx context.Context, email, role string) error {
	u, err := s.Ent.User.Query().Where(user.Email(auth.NormalizeEmail(email))).Only(ctx)
	if err != nil {
		return err
	}
	_, err = s.Ent.RoleAssignment.Delete().Where(roleassignment.PrincipalTypeEQ(roleassignment.PrincipalTypeUser),
		roleassignment.PrincipalID(u.ID), roleassignment.RoleKey(role)).Exec(ctx)
	return err
}

func (s *Service) teamAndUser(ctx context.Context, teamSlug, email string) (*ent.Team, *ent.User, error) {
	t, err := s.Ent.Team.Query().Where(team.Slug(teamSlug)).Only(ctx)
	if err != nil {
		return nil, nil, fmt.Errorf("team %s: %w", teamSlug, err)
	}
	u, err := s.Ent.User.Query().Where(user.Email(auth.NormalizeEmail(email))).Only(ctx)
	if err != nil {
		return nil, nil, fmt.Errorf("user %s: %w", email, err)
	}
	return t, u, nil
}

func slugify(name string) string {
	var b strings.Builder
	dash := false
	for _, r := range strings.ToLower(name) {
		switch {
		case r >= 'a' && r <= 'z' || r >= '0' && r <= '9':
			b.WriteRune(r)
			dash = false
		case !dash && b.Len() > 0:
			b.WriteByte('-')
			dash = true
		}
	}
	return strings.TrimSuffix(b.String(), "-")
}
