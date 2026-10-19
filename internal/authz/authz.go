package authz

import (
	"context"
	"slices"
	"time"

	"github.com/docker/kit-maker/internal/ent"
	"github.com/docker/kit-maker/internal/ent/roleassignment"
	"github.com/docker/kit-maker/internal/ent/team"
	"github.com/docker/kit-maker/internal/ent/teammembership"
)

// Subject is what one user may do, resolved once per request.
type Subject struct {
	UserID string
	// deployment holds permissions from deployment roles.
	deployment map[Key]bool
	// teams maps team ID → permissions held there (direct membership).
	teams map[string]map[Key]bool
	// viewTeams are ancestors of member teams: nested teams inherit view
	// permissions from their parents (DESIGN.md §13).
	viewTeams map[string]bool
	// DeploymentRoles and TeamRoles are for display.
	DeploymentRoles []string
	TeamRoles       map[string]string
}

// Authorizer resolves subjects.
type Authorizer struct {
	Ent *ent.Client
	Now func() time.Time
}

// Load resolves the permissions of userID: deployment roles held directly or
// through a team, team memberships (minus exclusions and expired rows), and
// view access inherited from parent teams.
func (a *Authorizer) Load(ctx context.Context, userID string) (*Subject, error) {
	now := time.Now()
	if a.Now != nil {
		now = a.Now()
	}
	s := &Subject{UserID: userID, deployment: map[Key]bool{}, teams: map[string]map[Key]bool{},
		viewTeams: map[string]bool{}, TeamRoles: map[string]string{}}

	rows, err := a.Ent.TeamMembership.Query().Where(teammembership.UserID(userID)).All(ctx)
	if err != nil {
		return nil, err
	}
	excluded := map[string]bool{}
	var memberTeams []string
	for _, m := range rows {
		if m.IsExcluded {
			excluded[m.TeamID] = true
		}
	}
	for _, m := range rows {
		if m.IsExcluded || excluded[m.TeamID] || (m.ExpiresAt != nil && m.ExpiresAt.Before(now)) {
			continue
		}
		r, ok := LookupRole(m.Role)
		if !ok || r.Scope != ScopeTeam {
			continue // unknown keys grant nothing (DESIGN.md §14)
		}
		perms := s.teams[m.TeamID]
		if perms == nil {
			perms = map[Key]bool{}
			s.teams[m.TeamID] = perms
			memberTeams = append(memberTeams, m.TeamID)
		}
		for _, k := range r.Permissions {
			perms[k] = true
		}
		if rank(r.Key) > rank(s.TeamRoles[m.TeamID]) {
			s.TeamRoles[m.TeamID] = r.Key
		}
	}

	assignments, err := a.Ent.RoleAssignment.Query().Where(roleassignment.Or(
		roleassignment.And(roleassignment.PrincipalTypeEQ(roleassignment.PrincipalTypeUser), roleassignment.PrincipalID(userID)),
		roleassignment.And(roleassignment.PrincipalTypeEQ(roleassignment.PrincipalTypeTeam), roleassignment.PrincipalIDIn(memberTeams...)),
	)).All(ctx)
	if err != nil {
		return nil, err
	}
	for _, as := range assignments {
		r, ok := LookupRole(as.RoleKey)
		if !ok || r.Scope != ScopeDeployment {
			continue
		}
		if !slices.Contains(s.DeploymentRoles, r.Key) {
			s.DeploymentRoles = append(s.DeploymentRoles, r.Key)
		}
		for _, k := range r.Permissions {
			s.deployment[k] = true
		}
	}

	// Walk up from each member team: ancestors grant view-only access.
	parents, err := a.parents(ctx)
	if err != nil {
		return nil, err
	}
	for _, t := range memberTeams {
		for p := parents[t]; p != ""; p = parents[p] {
			if s.viewTeams[p] {
				break
			}
			s.viewTeams[p] = true
		}
	}
	return s, nil
}

func (a *Authorizer) parents(ctx context.Context) (map[string]string, error) {
	teams, err := a.Ent.Team.Query().Select(team.FieldID, team.FieldParentTeamID).All(ctx)
	if err != nil {
		return nil, err
	}
	out := make(map[string]string, len(teams))
	for _, t := range teams {
		if t.ParentTeamID != nil {
			out[t.ID] = *t.ParentTeamID
		}
	}
	return out, nil
}

func rank(role string) int {
	switch role {
	case RoleViewer:
		return 1
	case RoleContributor:
		return 2
	case RoleMaintainer:
		return 3
	case RoleTeamAdmin:
		return 4
	}
	return 0
}

// Can reports whether the subject holds perm. For team-scope permissions,
// ownerTeamID is the team that owns the object; deployment roles that carry
// team-scope permissions (Owner, Auditor's views) apply to every team.
func (s *Subject) Can(perm Key, ownerTeamID string) bool {
	if s == nil {
		return false
	}
	if s.deployment[perm] {
		return true
	}
	if MustScope(perm) == ScopeDeployment {
		return false
	}
	if s.teams[ownerTeamID][perm] {
		return true
	}
	if s.viewTeams[ownerTeamID] {
		p, _ := Lookup(perm)
		return p.View
	}
	return false
}

// CanAnywhere reports whether the subject holds perm in at least one team
// (or deployment-wide). It guards list and create routes before a specific
// owner team is known.
func (s *Subject) CanAnywhere(perm Key) bool {
	if s == nil {
		return false
	}
	if s.deployment[perm] {
		return true
	}
	if MustScope(perm) == ScopeDeployment {
		return false
	}
	for _, perms := range s.teams {
		if perms[perm] {
			return true
		}
	}
	if len(s.viewTeams) > 0 {
		p, _ := Lookup(perm)
		return p.View
	}
	return false
}

// TeamsWith returns the team IDs where the subject holds perm, for SQL list
// filtering. all=true means every team (a deployment role grants it).
func (s *Subject) TeamsWith(perm Key) (ids []string, all bool) {
	if s.deployment[perm] {
		return nil, true
	}
	for id, perms := range s.teams {
		if perms[perm] {
			ids = append(ids, id)
		}
	}
	if p, _ := Lookup(perm); p.View {
		for id := range s.viewTeams {
			if !slices.Contains(ids, id) {
				ids = append(ids, id)
			}
		}
	}
	slices.Sort(ids)
	return ids, false
}

type subjectKey struct{}

// WithSubject stores s in ctx.
func WithSubject(ctx context.Context, s *Subject) context.Context {
	return context.WithValue(ctx, subjectKey{}, s)
}

// SubjectFrom returns the request's subject, or nil.
func SubjectFrom(ctx context.Context) *Subject {
	s, _ := ctx.Value(subjectKey{}).(*Subject)
	return s
}
