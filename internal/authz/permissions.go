// Package authz is Kit Maker's RBAC (DESIGN.md §14). Every feature, route,
// job and AI action is gated by a permission from this registry; roles are
// named sets of permissions at deployment or team scope.
package authz

import (
	"fmt"
	"sort"
)

// Key names a permission, e.g. "blueprint.promote".
type Key string

// Scope says where a permission applies.
type Scope string

const (
	// ScopeDeployment permissions apply to the whole deployment and can only
	// be held through deployment roles.
	ScopeDeployment Scope = "deployment"
	// ScopeTeam permissions apply to what a team owns (or what is shared
	// with it).
	ScopeTeam Scope = "team"
)

// Permission describes one permission for the role editor, token scopes and
// docs.
type Permission struct {
	Key         Key    `json:"key"`
	Area        string `json:"area"`
	Scope       Scope  `json:"scope"`
	Description string `json:"description"`
	// View marks read-only permissions, which nested child teams inherit
	// from their parents (DESIGN.md §13).
	View bool `json:"view,omitempty"`
}

// Route guards that are not permissions.
const (
	// Public routes need no sign-in (login, health probes).
	Public Key = "@public"
	// SignedIn routes need a session but no permission (sign out, own account).
	SignedIn Key = "@signed-in"
)

// Permission keys used in code. The catalogue below is the full list.
const (
	BlueprintView          Key = "blueprint.view"
	BlueprintCreate        Key = "blueprint.create"
	BlueprintEdit          Key = "blueprint.edit"
	BlueprintArchive       Key = "blueprint.archive"
	BlueprintTransfer      Key = "blueprint.transfer"
	BlueprintShare         Key = "blueprint.share"
	BlueprintChecksEdit    Key = "blueprint.checks.edit"
	BlueprintInclude       Key = "blueprint.include"
	BlueprintHistoryView   Key = "blueprint.history.view"
	BlueprintDownload      Key = "blueprint.download"
	BlueprintPublish       Key = "blueprint.publish"
	BlueprintPromote       Key = "blueprint.promote"
	BlueprintChannelManage Key = "blueprint.channel.manage"
	BlueprintVersionView   Key = "blueprint.version.view"

	BlockView           Key = "block.view"
	BlockUse            Key = "block.use"
	AIUse               Key = "ai.use"
	LinkRebaseApply     Key = "link.rebase.apply"
	BlockCreate         Key = "block.create"
	BlockEdit           Key = "block.edit"
	BlockPublishVersion Key = "block.publish_version"
	BlockFork           Key = "block.fork"

	TeamView          Key = "team.view"
	TeamCreate        Key = "team.create"
	TeamEdit          Key = "team.edit"
	TeamMembersManage Key = "team.members.manage"

	UserView            Key = "user.view"
	RoleManage          Key = "role.manage"
	RoleAssignDeploy    Key = "role.assign.deployment"
	IntegrationView     Key = "integration.view"
	IntegrationManage   Key = "integration.manage"
	SettingsManage      Key = "settings.manage"
	AuditView           Key = "audit.view"
	TokenCreate         Key = "token.create"
	AuthProvidersManage Key = "auth.providers.manage"
)

var catalogue = []Permission{
	// Blueprints
	{BlueprintView, "Blueprints", ScopeTeam, "View blueprints, their outline, YAML and checks", true},
	{BlueprintCreate, "Blueprints", ScopeTeam, "Create blueprints", false},
	{BlueprintEdit, "Blueprints", ScopeTeam, "Edit blueprint drafts: slots, links, overrides", false},
	{BlueprintArchive, "Blueprints", ScopeTeam, "Archive blueprints", false},
	{BlueprintTransfer, "Blueprints", ScopeTeam, "Move a blueprint to another owner team", false},
	{BlueprintShare, "Blueprints", ScopeTeam, "Share blueprints with other teams", false},
	{BlueprintChecksEdit, "Blueprints", ScopeTeam, "Edit a blueprint's goal and checks", false},
	{BlueprintInclude, "Blueprints", ScopeTeam, "Include other blueprints", false},
	{BlueprintHistoryView, "Blueprints", ScopeTeam, "View a blueprint's change history", true},
	{BlueprintDownload, "Blueprints", ScopeTeam, "Download blueprint versions (repo reference or file)", false},
	// Versions & channels
	{BlueprintPublish, "Versions & channels", ScopeTeam, "Publish a blueprint version", false},
	{BlueprintPromote, "Versions & channels", ScopeTeam, "Promote a version to stable", false},
	{BlueprintChannelManage, "Versions & channels", ScopeTeam, "Create and move channels", false},
	{BlueprintVersionView, "Versions & channels", ScopeTeam, "View published versions and their provenance", true},
	// Approvals
	{"approval.request", "Approvals", ScopeTeam, "Request approvals and AI reviews", false},
	{"approval.decide.widening", "Approvals", ScopeTeam, "Approve permission-widening changes", false},
	{"approval.decide.promote", "Approvals", ScopeTeam, "Approve promotion to stable", false},
	{"approval.decide.shared_block", "Approvals", ScopeTeam, "Approve shared block versions", false},
	{"approval.self_justify", "Approvals", ScopeTeam, "Self-approve with a written justification when no one else can", false},
	{"blueprint.policy.manage", "Approvals", ScopeTeam, "Set a blueprint's approval policy", false},
	// Collections
	{"collection.view", "Collections", ScopeTeam, "View collections", true},
	{"collection.create", "Collections", ScopeTeam, "Create collections", false},
	{"collection.edit", "Collections", ScopeTeam, "Rename and describe collections", false},
	{"collection.blueprints.manage", "Collections", ScopeTeam, "Move blueprints in and out of collections", false},
	{"collection.policy.manage", "Collections", ScopeTeam, "Set a collection's approval policy", false},
	{"collection.share", "Collections", ScopeTeam, "Share collections", false},
	{"collection.archive", "Collections", ScopeTeam, "Archive collections", false},
	// Blocks
	{BlockView, "Blocks", ScopeTeam, "View library blocks", true},
	{BlockUse, "Blocks", ScopeTeam, "Link blocks into blueprints", false},
	{BlockCreate, "Blocks", ScopeTeam, "Create library blocks", false},
	{BlockEdit, "Blocks", ScopeTeam, "Edit block drafts", false},
	{BlockPublishVersion, "Blocks", ScopeTeam, "Publish a new version of a shared block", false},
	{BlockFork, "Blocks", ScopeTeam, "Fork blocks into the library", false},
	{"block.share", "Blocks", ScopeTeam, "Share blocks", false},
	{"block.archive", "Blocks", ScopeTeam, "Archive blocks", false},
	{"block.import", "Blocks", ScopeTeam, "Import kits and repos into blocks", false},
	{"library.hygiene.apply", "Blocks", ScopeTeam, "Apply AI library clean-ups", false},
	// Inheritance
	{LinkRebaseApply, "Inheritance", ScopeTeam, "Rebase a blueprint's edits onto a new block version", false},
	{"link.conflict.resolve", "Inheritance", ScopeTeam, "Resolve rebase conflicts", false},
	{"link.revert", "Inheritance", ScopeTeam, "Revert a linked block to the library version", false},
	{"block.propose_upstream", "Inheritance", ScopeTeam, "Propose a blueprint's edits as a new block version", false},
	// Guardrails
	{"guardrail.view", "Guardrails", ScopeTeam, "View guardrails that apply", true},
	{"guardrail.manage", "Guardrails", ScopeDeployment, "Define mandatory guardrails", false},
	{"guardrail.exception.request", "Guardrails", ScopeTeam, "Request a guardrail exception", false},
	{"guardrail.exception.approve", "Guardrails", ScopeDeployment, "Approve guardrail exceptions", false},
	// Build & publish
	{"build.run", "Build & publish", ScopeTeam, "Run blueprint image builds", false},
	{"build.logs.view", "Build & publish", ScopeTeam, "View build logs", true},
	{"image.publish", "Build & publish", ScopeTeam, "Push blueprint images to the registry", false},
	// References & sandboxes
	{"reference.view", "References & sandboxes", ScopeTeam, "View which repos reference a blueprint", true},
	{"reference.open_pr", "References & sandboxes", ScopeTeam, "Open pull requests that add or update references", false},
	{"reference.pin", "References & sandboxes", ScopeTeam, "Change a repo's pin or channel", false},
	{"sandbox.view", "References & sandboxes", ScopeTeam, "View registered sandboxes", true},
	{"sandbox.revoke", "References & sandboxes", ScopeTeam, "Revoke sandbox registrations", false},
	{"enrollment_token.manage", "References & sandboxes", ScopeTeam, "Manage sandbox enrollment tokens", false},
	// AI
	{AIUse, "AI", ScopeTeam, "Use AI drafting and Ask AI", false},
	{"ai.review.request", "AI", ScopeTeam, "Request an AI review", false},
	{"ai.review.view", "AI", ScopeTeam, "View AI review verdicts", true},
	{"ai.prompts.view", "AI", ScopeTeam, "View the prompts and responses behind AI suggestions", false},
	{"ai.budget.view", "AI", ScopeTeam, "View AI usage and budgets", true},
	{"ai.budget.manage", "AI", ScopeDeployment, "Set AI budgets", false},
	{"ai.policy.manage", "AI", ScopeDeployment, "Set AI policy", false},
	// Teams
	{TeamView, "Teams", ScopeTeam, "View a team and its members", true},
	{TeamCreate, "Teams", ScopeDeployment, "Create teams (admins only)", false},
	{TeamEdit, "Teams", ScopeTeam, "Rename a team and change its parent", false},
	{"team.archive", "Teams", ScopeDeployment, "Archive teams", false},
	{TeamMembersManage, "Teams", ScopeTeam, "Add and remove members, pin and exclude", false},
	{"team.sources.manage", "Teams", ScopeTeam, "Configure membership sources", false},
	{"team.roles.assign", "Teams", ScopeTeam, "Assign team roles", false},
	// Identity & access
	{UserView, "Identity & access", ScopeDeployment, "View all accounts", false},
	{"user.invite", "Identity & access", ScopeDeployment, "Invite people", false},
	{"user.suspend", "Identity & access", ScopeDeployment, "Suspend and reactivate accounts", false},
	{AuthProvidersManage, "Identity & access", ScopeDeployment, "Configure sign-in providers", false},
	{"scim.manage", "Identity & access", ScopeDeployment, "Configure SCIM provisioning", false},
	{"directory_sync.manage", "Identity & access", ScopeDeployment, "Configure directory sync", false},
	{"callout.manage", "Identity & access", ScopeDeployment, "Configure the membership callout", false},
	{TokenCreate, "Identity & access", ScopeDeployment, "Create personal API tokens", false},
	{"service_account.manage", "Identity & access", ScopeDeployment, "Manage service accounts", false},
	{RoleManage, "Identity & access", ScopeDeployment, "Create and edit custom roles", false},
	{RoleAssignDeploy, "Identity & access", ScopeDeployment, "Assign deployment roles", false},
	// Integrations
	{IntegrationView, "Integrations", ScopeDeployment, "View integrations", false},
	{"integration.use", "Integrations", ScopeTeam, "Use integrations when publishing", false},
	{IntegrationManage, "Integrations", ScopeDeployment, "Add, change and remove integrations", false},
	{"integration.test", "Integrations", ScopeDeployment, "Test integrations", false},
	// Platform
	{SettingsManage, "Platform", ScopeDeployment, "Change deployment settings", false},
	{"branding.manage", "Platform", ScopeDeployment, "Change branding", false},
	{"notifications.manage", "Platform", ScopeDeployment, "Configure notifications", false},
	{"webhook.manage", "Platform", ScopeDeployment, "Configure outbound webhooks", false},
	{AuditView, "Platform", ScopeDeployment, "View the audit log", false},
	{"audit.export", "Platform", ScopeDeployment, "Export the audit log", false},
	{"backup.run", "Platform", ScopeDeployment, "Run backups", false},
	{"backup.restore", "Platform", ScopeDeployment, "Restore backups", false},
	{"status.view", "Platform", ScopeDeployment, "View system status", false},
}

var byKey = func() map[Key]Permission {
	m := make(map[Key]Permission, len(catalogue))
	for _, p := range catalogue {
		if _, dup := m[p.Key]; dup {
			panic(fmt.Sprintf("permission %s listed twice", p.Key))
		}
		m[p.Key] = p
	}
	return m
}()

// All returns the catalogue, sorted by area then key.
func All() []Permission {
	out := append([]Permission(nil), catalogue...)
	sort.SliceStable(out, func(i, j int) bool {
		if out[i].Area != out[j].Area {
			return out[i].Area < out[j].Area
		}
		return out[i].Key < out[j].Key
	})
	return out
}

// Lookup returns the permission for key.
func Lookup(k Key) (Permission, bool) {
	p, ok := byKey[k]
	return p, ok
}

// MustScope returns the scope of a known permission and panics otherwise:
// guarding a route with an unknown permission is a programming error.
func MustScope(k Key) Scope {
	p, ok := byKey[k]
	if !ok {
		panic(fmt.Sprintf("unknown permission %q", k))
	}
	return p.Scope
}
