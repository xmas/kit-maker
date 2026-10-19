package seed

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/docker/kit-maker/internal/ent"
	"github.com/docker/kit-maker/internal/ent/block"
	"github.com/docker/kit-maker/internal/ent/blocklink"
	"github.com/docker/kit-maker/internal/ent/blockversion"
	"github.com/docker/kit-maker/internal/ent/check"
	"github.com/docker/kit-maker/internal/ent/goal"
	"github.com/docker/kit-maker/internal/ent/kit"
	"github.com/docker/kit-maker/internal/ent/slot"
	"github.com/docker/kit-maker/internal/kits/patch"
)

type demoBlock struct {
	slug, name, typ, description string
	payload                      string
}

var demoBlocks = []demoBlock{
	{"github-egress", "GitHub egress", "network-rules", "Lets the agent reach GitHub's API and git over HTTPS, without deleting repos.", `{"runtime":{
		"allow":[
			{"_k":"gh-git","label":"Git over HTTPS","hosts":["github.com"]},
			{"_k":"gh-api","label":"API access","hosts":["api.github.com"],"methods":["GET","HEAD","POST","PATCH","PUT","DELETE"]},
			{"_k":"gh-uploads","label":"Release uploads","hosts":["uploads.github.com"],"methods":["POST","PUT"]}],
		"deny":[
			{"_k":"gh-del","label":"No repo deletes","hosts":["api.github.com"],"methods":["DELETE"],"paths":["/repos/**"]}]}}`},
	{"deny-cloud-metadata", "Deny cloud metadata", "network-rules", "Blocks cloud instance metadata endpoints.", `{"runtime":{"deny":[
		{"_k":"aws","label":"AWS / Azure metadata","hosts":["169.254.169.254"]},
		{"_k":"gcp","label":"GCP metadata","hosts":["metadata.google.internal"]}]}}`},
	{"packagist-acme", "Packagist (acme)", "network-rules", "Read-only access to acme's private Packagist.", `{"install":{"allow":[
		{"_k":"pk-install","label":"Composer installs","hosts":["repo.packagist.com"],"methods":["GET","HEAD"],"paths":["/acme/**"]}]},
		"runtime":{"allow":[
		{"_k":"pk-run","label":"Composer reads","hosts":["repo.packagist.com"],"methods":["GET","HEAD"],"paths":["/acme/**"]}]}}`},
	{"github-token", "GitHub token", "credential", "GitHub API token, injected by the sbx proxy.", `{"service":"github","phase":["runtime"],"optional":true,"description":"GitHub API access",
		"apiKey":{"name":"GH_TOKEN","proxyManaged":true,"inject":[
			{"_k":"api","domain":"api.github.com","header":"Authorization","format":"Bearer %s"}]}}`},
	{"packagist-token", "Packagist token", "credential", "Composer auth for the private Packagist, install phase only.", `{"service":"packagist","phase":["install"],"description":"Private Packagist for composer install",
		"apiKey":{"name":"COMPOSER_TOKEN","proxyManaged":true,"inject":[
			{"_k":"pk","domain":"repo.packagist.com","scheme":"basic","username":"token"}]}}`},

	// Blocks with rules in both phases and both allow and deny, so one block's
	// lines end up in several places in the merged network policy.
	{"node-toolchain", "Node toolchain", "network-rules", "npm installs and reads; no publishing from the sandbox.", `{
		"install":{
			"allow":[
				{"_k":"npm-get","label":"npm installs","hosts":["registry.npmjs.org"],"methods":["GET","HEAD"]},
				{"_k":"node-dist","label":"Node downloads","hosts":["nodejs.org"],"methods":["GET"],"paths":["/dist/**"]}],
			"deny":[
				{"_k":"npm-pub-i","label":"No publishing during install","hosts":["registry.npmjs.org"],"methods":["PUT","DELETE"]}]},
		"runtime":{
			"allow":[
				{"_k":"npm-read","label":"npm reads","hosts":["registry.npmjs.org"],"methods":["GET","HEAD"]}],
			"deny":[
				{"_k":"npm-pub","label":"No publishing","hosts":["registry.npmjs.org"],"methods":["PUT","DELETE"]},
				{"_k":"npm-login","label":"No npm login","hosts":["registry.npmjs.org"],"methods":["POST","PUT"],"paths":["/-/user/**"]}]}}`},
	{"python-toolchain", "Python toolchain", "network-rules", "pip installs from PyPI; no uploads.", `{
		"install":{
			"allow":[
				{"_k":"pypi","label":"PyPI index","hosts":["pypi.org"],"methods":["GET"]},
				{"_k":"pyfiles","label":"PyPI files","hosts":["files.pythonhosted.org"],"methods":["GET"]}]},
		"runtime":{
			"allow":[
				{"_k":"pypi-simple","label":"Index reads","hosts":["pypi.org"],"methods":["GET","HEAD"],"paths":["/simple/**"]}],
			"deny":[
				{"_k":"pypi-upload","label":"No uploads","hosts":["upload.pypi.org"]}]}}`},
	{"acme-internal", "Acme internal APIs", "network-rules", "Read and write acme's v1 API; never the admin console or deletes.", `{"runtime":{
		"allow":[
			{"_k":"acme-v1","label":"v1 API","hosts":["api.acme.internal"],"methods":["GET","POST","PATCH"],"paths":["/v1/**"]},
			{"_k":"acme-docs","label":"Internal docs","hosts":["docs.acme.internal"],"methods":["GET"]}],
		"deny":[
			{"_k":"acme-del","label":"No deletes","hosts":["api.acme.internal"],"methods":["DELETE"]},
			{"_k":"acme-admin","label":"No admin console","hosts":["admin.acme.internal"]}]}}`},
	{"error-reporting", "Error reporting", "network-rules", "Send errors to Sentry; no access to the Sentry dashboard.", `{"runtime":{
		"allow":[
			{"_k":"sentry-ingest","label":"Event ingest","hosts":["o42.ingest.sentry.io"],"methods":["POST"],"paths":["/api/**"]}],
		"deny":[
			{"_k":"sentry-ui","label":"No dashboard","hosts":["sentry.io"]}]}}`},
	{"npm-token", "npm token", "credential", "npm auth for installs and reads, injected by the sbx proxy.", `{"service":"npm","phase":["install","runtime"],"description":"npm registry auth",
		"apiKey":{"name":"NPM_TOKEN","proxyManaged":true,"inject":[
			{"_k":"npm","domain":"registry.npmjs.org","header":"Authorization","format":"Bearer %s"}]}}`},
}

type demoSlot struct {
	key, label, section string
	required            bool
	block               string     // linked block slug, "" = blank
	overrides           []patch.Op // edits kept in this kit
}

var demoSlots = []demoSlot{
	{"github-egress", "GitHub egress", "network", true, "github-egress", []patch.Op{
		{Op: "replace", Path: "/runtime/allow", Key: "gh-api", Value: map[string]any{"label": "API reads", "hosts": []any{"api.github.com"}, "methods": []any{"GET", "HEAD"}}},
		{Op: "add", Path: "/runtime/allow", After: "gh-api", Value: map[string]any{"_k": "acme-writes", "label": "API writes to acme/*", "hosts": []any{"api.github.com"}, "methods": []any{"POST", "PATCH", "PUT"}, "paths": []any{"/repos/acme/**"}}},
	}},
	{"deny-metadata", "Deny cloud metadata", "network", true, "deny-cloud-metadata", nil},
	{"packagist", "Private Packagist", "network", true, "", nil},
	{"github-token", "GitHub token", "credentials", true, "github-token", nil},
	{"packagist-token", "Packagist token", "credentials", true, "", nil},
	{"install-deps", "Install dependencies", "setup", false, "", nil},
}

type demoCheck struct {
	label, method, url, expect string
}

var demoChecks = []demoCheck{
	{"Read the acme/shop repo", "GET", "https://api.github.com/repos/acme/shop", "reach"},
	{"Update a pull request on acme/shop", "PATCH", "https://api.github.com/repos/acme/shop/pulls/42", "reach"},
	{"No writes outside acme/*", "POST", "https://api.github.com/repos/torvalds/linux/issues", "blocked"},
	{"composer install from the private Packagist", "GET", "https://repo.packagist.com/acme/packages.json", "reach"},
	{"Cloud metadata is blocked", "GET", "http://169.254.169.254/latest/meta-data/", "blocked"},
	{"Never post to Slack", "POST", "https://slack.com/api/chat.postMessage", "blocked"},
}

// demo seeds the starter blocks and the Acme Shop kit. It upgrades a kit
// seeded by the phase-0 skeleton in place.
func demo(ctx context.Context, c *ent.Client, t *ent.Team, u *ent.User) error {
	versions := map[string]*ent.BlockVersion{}
	blocks := map[string]*ent.Block{}
	for _, db := range demoBlocks {
		b, v, err := ensureBlock(ctx, c, t, u, db)
		if err != nil {
			return fmt.Errorf("block %s: %w", db.slug, err)
		}
		blocks[db.slug], versions[db.slug] = b, v
	}
	if err := polyglot(ctx, c, t, blocks, versions); err != nil {
		return fmt.Errorf("polyglot kit: %w", err)
	}

	k, err := c.Kit.Query().Where(kit.TeamID(t.ID), kit.Slug("acme-shop")).Only(ctx)
	switch {
	case ent.IsNotFound(err):
		k, err = c.Kit.Create().SetTeam(t).SetSlug("acme-shop").SetDisplayName("Acme Shop").
			SetKind(kit.KindMixin).SetImageRepository("acme/sbx-kit-acme-shop").Save(ctx)
		if err != nil {
			return fmt.Errorf("kit: %w", err)
		}
	case err != nil:
		return err
	}
	if upgraded, err := c.Slot.Query().Where(slot.KitID(k.ID), slot.Key("deny-metadata")).Exist(ctx); err != nil {
		return err
	} else if upgraded {
		return nil
	}

	// Rebuild the outline (phase-0 skeleton kits had a placeholder layout).
	old, err := c.Slot.Query().Where(slot.KitID(k.ID)).IDs(ctx)
	if err != nil {
		return err
	}
	if _, err := c.BlockLink.Delete().Where(blocklink.SlotIDIn(old...)).Exec(ctx); err != nil {
		return err
	}
	if _, err := c.Slot.Delete().Where(slot.KitID(k.ID)).Exec(ctx); err != nil {
		return err
	}
	if err := k.Update().SetKind(kit.KindMixin).
		SetDescription("Claude Code for the acme/shop Laravel monorepo: tests, GitHub, private Packagist.").Exec(ctx); err != nil {
		return err
	}
	for i, ds := range demoSlots {
		state := slot.StateBlank
		switch {
		case ds.block != "":
			state = slot.StateFilled
		case !ds.required:
			state = slot.StateOptional
		}
		sl, err := c.Slot.Create().SetKit(k).SetKey(ds.key).SetLabel(ds.label).SetSection(ds.section).
			SetRequired(ds.required).SetState(state).SetPosition(i).Save(ctx)
		if err != nil {
			return fmt.Errorf("slot %s: %w", ds.key, err)
		}
		if ds.block == "" {
			continue
		}
		if err := c.BlockLink.Create().SetSlot(sl).SetBlock(blocks[ds.block]).SetBaseVersion(versions[ds.block]).
			SetOverrides(ds.overrides).SetOverridesHash(patch.Hash(ds.overrides)).Exec(ctx); err != nil {
			return fmt.Errorf("link %s: %w", ds.key, err)
		}
	}

	if _, err := c.Goal.Delete().Where(goal.KitID(k.ID)).Exec(ctx); err != nil {
		return err
	}
	if err := c.Goal.Create().SetKit(k).SetStatement("A Claude Code sandbox for our Laravel monorepo (acme/shop). " +
		"It should run the test suite, push branches to GitHub and install packages from our private Packagist. " +
		"It must never reach production or post to Slack.").Exec(ctx); err != nil {
		return err
	}
	if _, err := c.Check.Delete().Where(check.KitID(k.ID)).Exec(ctx); err != nil {
		return err
	}
	for i, dc := range demoChecks {
		if err := c.Check.Create().SetKit(k).SetLabel(dc.label).SetType(check.TypeNetwork).
			SetSpec(map[string]any{"url": dc.url, "method": dc.method}).
			SetExpect(check.Expect(dc.expect)).SetPosition(i).Exec(ctx); err != nil {
			return err
		}
	}
	return nil
}

// ensureBlock creates a block, or adds a new version when the current one
// differs from the demo payload (e.g. phase-0 seeds used a draft format).
func ensureBlock(ctx context.Context, c *ent.Client, t *ent.Team, u *ent.User, db demoBlock) (*ent.Block, *ent.BlockVersion, error) {
	var payload map[string]any
	if err := json.Unmarshal([]byte(db.payload), &payload); err != nil {
		return nil, nil, err
	}
	b, err := c.Block.Query().Where(block.TeamID(t.ID), block.Slug(db.slug)).WithCurrentVersion().Only(ctx)
	if ent.IsNotFound(err) {
		b, err = c.Block.Create().SetTeam(t).SetType(db.typ).SetSlug(db.slug).SetName(db.name).
			SetDescription(db.description).SetVisibility(block.VisibilityTeam).Save(ctx)
	}
	if err != nil {
		return nil, nil, err
	}
	if cv := b.Edges.CurrentVersion; cv != nil && cv.PayloadHash == hash(payload) {
		return b, cv, nil
	}
	n, err := c.BlockVersion.Query().Where(blockversion.BlockID(b.ID)).Count(ctx)
	if err != nil {
		return nil, nil, err
	}
	v, err := c.BlockVersion.Create().SetBlock(b).SetVersion(n + 1).SetPayload(payload).SetPayloadHash(hash(payload)).
		SetSchemaVersion(SpecVersion).SetChangelog("Starter library.").
		SetAuthoredBy(blockversion.AuthoredByUser).SetAcceptedBy(u).Save(ctx)
	if err != nil {
		return nil, nil, err
	}
	if err := b.Update().SetCurrentVersion(v).SetName(db.name).SetDescription(db.description).Exec(ctx); err != nil {
		return nil, nil, err
	}
	return b, v, nil
}

// polyglotSlots mixes toolchain blocks so each one's install/runtime and
// allow/deny rules interleave with the others' in the merged network policy:
// a slot's lines are split across several places in the YAML.
var polyglotSlots = []demoSlot{
	{"node", "Node toolchain", "network", true, "node-toolchain", nil},
	{"python", "Python toolchain", "network", true, "python-toolchain", nil},
	{"github", "GitHub egress", "network", true, "github-egress", nil},
	{"acme-internal", "Acme internal APIs", "network", true, "acme-internal", []patch.Op{
		{Op: "add", Path: "/runtime/deny", After: "acme-del", Value: map[string]any{"_k": "acme-prod", "label": "No prod writes",
			"hosts": []any{"api.acme.internal"}, "methods": []any{"POST", "PATCH"}, "paths": []any{"/v1/prod/**"}}},
	}},
	{"metadata", "Deny cloud metadata", "network", true, "deny-cloud-metadata", nil},
	{"errors", "Error reporting", "network", true, "error-reporting", nil},
	{"npm-token", "npm token", "credentials", true, "npm-token", nil},
	{"github-token", "GitHub token", "credentials", true, "github-token", nil},
}

// polyglot seeds the "Polyglot CI" kit once; after that it is the user's.
func polyglot(ctx context.Context, c *ent.Client, t *ent.Team, blocks map[string]*ent.Block, versions map[string]*ent.BlockVersion) error {
	exists, err := c.Kit.Query().Where(kit.TeamID(t.ID), kit.Slug("polyglot-ci")).Exist(ctx)
	if err != nil || exists {
		return err
	}
	// The seed already runs in a transaction (seed.Run).
	k, err := c.Kit.Create().SetTeam(t).SetSlug("polyglot-ci").SetDisplayName("Polyglot CI").SetKind(kit.KindWorkload).
		SetDescription("Demo: blocks with install and runtime, allow and deny rules, merged into one policy.").Save(ctx)
	if err != nil {
		return err
	}
	if err := c.Goal.Create().SetKit(k).SetStatement("A CI agent for acme's Node and Python services. It installs packages, " +
		"reports errors to Sentry and calls acme's v1 API. It must never publish packages, touch the admin console or write to prod.").Exec(ctx); err != nil {
		return err
	}
	for i, ds := range polyglotSlots {
		sl, err := c.Slot.Create().SetKit(k).SetKey(ds.key).SetLabel(ds.label).SetSection(ds.section).
			SetRequired(ds.required).SetState(slot.StateFilled).SetPosition(i).Save(ctx)
		if err != nil {
			return fmt.Errorf("slot %s: %w", ds.key, err)
		}
		if err := c.BlockLink.Create().SetSlot(sl).SetBlock(blocks[ds.block]).SetBaseVersion(versions[ds.block]).
			SetOverrides(ds.overrides).SetOverridesHash(patch.Hash(ds.overrides)).Exec(ctx); err != nil {
			return fmt.Errorf("link %s: %w", ds.key, err)
		}
	}
	return nil
}
