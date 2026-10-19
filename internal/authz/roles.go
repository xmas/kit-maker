package authz

import (
	"fmt"
	"strings"
)

// Role is a named set of permissions at one scope. Built-in roles are
// read-only presets; custom roles (stored in the database) come next.
type Role struct {
	Key         string `json:"key"`
	Name        string `json:"name"`
	Scope       Scope  `json:"scope"`
	BuiltIn     bool   `json:"builtIn"`
	Description string `json:"description"`
	Permissions []Key  `json:"permissions"`
}

// Built-in role keys.
const (
	RoleOwner   = "owner"
	RoleAdmin   = "admin"
	RoleAuditor = "auditor"
	RoleMember  = "member"

	RoleViewer      = "viewer"
	RoleContributor = "contributor"
	RoleMaintainer  = "maintainer"
	RoleTeamAdmin   = "team_admin"
)

// teamPerms selects team-scope permissions matching pred.
func teamPerms(pred func(Permission) bool) []Key {
	var out []Key
	for _, p := range catalogue {
		if p.Scope == ScopeTeam && pred(p) {
			out = append(out, p.Key)
		}
	}
	return out
}

func has(k Key, prefixes ...string) bool {
	for _, p := range prefixes {
		if strings.HasPrefix(string(k), p) || string(k) == p {
			return true
		}
	}
	return false
}

var (
	viewerPerms = teamPerms(func(p Permission) bool { return p.View })

	contributorPerms = append(append([]Key{}, viewerPerms...), teamPerms(func(p Permission) bool {
		return has(p.Key, "kit.create", "kit.edit", "kit.checks.edit", "kit.subkit.include", "kit.bundle.download",
			"block.use", "block.create", "block.edit", "block.fork", "block.import", "block.propose_upstream",
			"link.", "ai.use", "ai.review.request", "approval.request", "guardrail.exception.request",
			"blueprint.download", "integration.use", "collection.create", "collection.edit", "collection.kits.manage")
	})...)

	maintainerPerms = append(append([]Key{}, contributorPerms...), teamPerms(func(p Permission) bool {
		return has(p.Key, "kit.generate", "kit.publish", "kit.promote", "kit.channel.manage", "kit.archive",
			"approval.decide.", "approval.self_justify", "block.publish_version", "block.archive",
			"build.run", "image.publish", "reference.open_pr", "reference.pin", "sandbox.revoke",
			"enrollment_token.manage", "library.hygiene.apply", "collection.archive", "ai.prompts.view")
	})...)

	teamAdminPerms = append(append([]Key{}, maintainerPerms...), teamPerms(func(p Permission) bool {
		return has(p.Key, "kit.share", "kit.transfer", "kit.policy.manage", "block.share", "collection.share",
			"collection.policy.manage", "team.edit", "team.members.manage", "team.sources.manage", "team.roles.assign")
	})...)

	adminPerms = []Key{
		TeamCreate, "team.archive", UserView, "user.invite", "user.suspend", AuthProvidersManage, "scim.manage",
		"directory_sync.manage", "callout.manage", TokenCreate, "service_account.manage", RoleManage,
		RoleAssignDeploy, IntegrationView, IntegrationManage, "integration.test", SettingsManage,
		"branding.manage", "notifications.manage", "webhook.manage", AuditView, "audit.export", "status.view",
		"guardrail.manage", "guardrail.exception.approve", "ai.budget.manage", "ai.policy.manage",
		// Admins can see and manage every team.
		TeamView, TeamEdit, TeamMembersManage, "team.sources.manage", "team.roles.assign",
	}

	auditorPerms = append(append([]Key{}, viewerPerms...), UserView, IntegrationView, AuditView, "audit.export", "status.view")
)

func allKeys() []Key {
	out := make([]Key, 0, len(catalogue))
	for _, p := range catalogue {
		out = append(out, p.Key)
	}
	return out
}

// BuiltInRoles are the read-only presets (DESIGN.md §14).
var BuiltInRoles = []Role{
	{RoleOwner, "Owner", ScopeDeployment, true, "Every permission. Break-glass.", allKeys()},
	{RoleAdmin, "Admin", ScopeDeployment, true, "Identity, integrations, teams, roles, policies, guardrails, settings.", adminPerms},
	{RoleAuditor, "Auditor", ScopeDeployment, true, "Read everything and the audit log. No writes.", auditorPerms},
	{RoleMember, "Member", ScopeDeployment, true, "Sign in and manage own tokens. Sees only what teams grant.", []Key{TokenCreate}},
	{RoleViewer, "Viewer", ScopeTeam, true, "View kits, blocks, blueprints, references, sandboxes.", viewerPerms},
	{RoleContributor, "Contributor", ScopeTeam, true, "Viewer + create and edit kits and blocks, use AI, request reviews.", contributorPerms},
	{RoleMaintainer, "Maintainer", ScopeTeam, true, "Contributor + generate, publish, promote, approve, publish shared block versions.", maintainerPerms},
	{RoleTeamAdmin, "Team admin", ScopeTeam, true, "Maintainer + membership, membership sources, sharing, team policies.", teamAdminPerms},
}

var roleByKey = func() map[string]Role {
	m := map[string]Role{}
	for _, r := range BuiltInRoles {
		for _, k := range r.Permissions {
			if _, ok := byKey[k]; !ok {
				panic(fmt.Sprintf("role %s lists unknown permission %s", r.Key, k))
			}
		}
		m[r.Key] = r
	}
	return m
}()

// LookupRole returns a built-in role by key.
func LookupRole(key string) (Role, bool) {
	r, ok := roleByKey[key]
	return r, ok
}
