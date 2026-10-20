package seed

import (
	"context"
	"fmt"
	"strings"

	"github.com/docker/kit-maker/internal/blocks"
	"github.com/docker/kit-maker/internal/blueprints/blocktypes"
	"github.com/docker/kit-maker/internal/blueprints/patch"
	"github.com/docker/kit-maker/internal/ent"
	"github.com/docker/kit-maker/internal/ent/block"
	"github.com/docker/kit-maker/internal/ent/blockversion"
	"github.com/docker/kit-maker/internal/ent/blueprint"
	"github.com/docker/kit-maker/internal/ent/slot"
)

// Sample blueprints for the kinds of sandbox profile an organisation runs
// (developer, docs-only, internal-only, isolated, ML research, ops agents,
// support, read-only validation), each built from shared library blocks so
// the same "internal services" or "deny cloud metadata" block is reused.
// Hosts use acme.internal and .example placeholders.

type profileBlock struct {
	slug, name, typ, description string
	payload                      map[string]any
}

func allow(hosts ...string) map[string]any { return map[string]any{"hosts": anys(hosts)} }

func bounded(label string, hosts []string, methods []string, paths ...string) map[string]any {
	e := map[string]any{"label": label, "hosts": anys(hosts), "methods": anys(methods)}
	if len(paths) > 0 {
		e["paths"] = anys(paths)
	}
	return e
}

func rules(phase string, allows []any, denies []any) map[string]any {
	p := map[string]any{}
	if allows != nil {
		p["allow"] = allows
	}
	if denies != nil {
		p["deny"] = denies
	}
	return map[string]any{phase: p}
}

func anys(ss []string) []any {
	out := make([]any, len(ss))
	for i, s := range ss {
		out[i] = s
	}
	return out
}

var profileBlocks = []profileBlock{
	{"acme-internal-services", "Acme internal services", blocktypes.NetworkRules, "Source control, tracker, wiki and chat on the internal network.",
		rules("runtime", []any{allow("gitlab.acme.internal", "jira.acme.internal", "wiki.acme.internal", "chat.acme.internal")}, nil)},
	{"package-registries", "Package registries", blocktypes.NetworkRules, "npm, PyPI and the Go proxy, while dependencies install.",
		rules("install", []any{allow("registry.npmjs.org"), allow("pypi.org", "files.pythonhosted.org"), allow("proxy.golang.org", "sum.golang.org")}, nil)},
	{"model-apis", "Model APIs", blocktypes.NetworkRules, "Hosted model endpoints and the internal inference gateway.",
		rules("runtime", []any{allow("api.anthropic.com"), allow("api.openai.com"), allow("inference.acme.internal")}, nil)},
	{"research-sources", "Research sources", blocktypes.NetworkRules, "Papers, model hubs and framework documentation, read-only.",
		rules("runtime", []any{
			bounded("Papers", []string{"arxiv.org", "export.arxiv.org"}, []string{"GET", "HEAD"}),
			bounded("Model hub", []string{"huggingface.co"}, []string{"GET", "HEAD"}),
			bounded("Framework docs", []string{"pytorch.org", "docs.python.org"}, []string{"GET", "HEAD"}),
		}, nil)},
	{"vendor-docs", "Vendor documentation", blocktypes.NetworkRules, "The EDA vendor's documentation site, read-only.",
		rules("runtime", []any{bounded("Docs", []string{"docs.eda-vendor.example"}, []string{"GET", "HEAD"})}, nil)},
	{"observability", "Observability", blocktypes.NetworkRules, "Dashboards, metrics and logs for the platform team.",
		rules("runtime", []any{allow("grafana.acme.internal", "prometheus.acme.internal", "logs.acme.internal")}, nil)},
	{"cluster-api", "Cluster API", blocktypes.NetworkRules, "The platform's Kubernetes API, for SRE work.",
		rules("runtime", []any{allow("k8s.acme.internal")}, nil)},
	{"dc-inventory-readonly", "Datacenter inventory (read-only)", blocktypes.NetworkRules, "Host and datacenter inventory APIs, GET only.",
		rules("runtime", []any{
			bounded("Inventory", []string{"inventory.acme.internal"}, []string{"GET"}, "/api/v1/**"),
			bounded("DCIM", []string{"dcim.acme.internal"}, []string{"GET"}, "/api/**"),
		}, nil)},
	{"tickets-readonly", "Tickets (read-only)", blocktypes.NetworkRules, "Read tickets and reports from the tracker; no edits.",
		rules("runtime", []any{bounded("Read tickets", []string{"jira.acme.internal"}, []string{"GET"}, "/rest/api/**")},
			[]any{bounded("No ticket edits", []string{"jira.acme.internal"}, []string{"POST", "PUT", "PATCH", "DELETE"})})},
	{"chamber-api", "Chamber API", blocktypes.NetworkRules, "The one service an isolated chamber may call.",
		rules("runtime", []any{bounded("Chamber jobs", []string{"chamber.acme.internal"}, []string{"GET", "POST"}, "/jobs/**")}, nil)},
	{"integration-readonly", "Integration hub (read-only)", blocktypes.NetworkRules, "Validation reads against the integration hub's cloud API.",
		rules("runtime", []any{bounded("Read", []string{"api.integration-hub.example"}, []string{"GET", "HEAD"}, "/api/**")},
			[]any{bounded("No writes", []string{"api.integration-hub.example"}, []string{"POST", "PUT", "PATCH", "DELETE"})})},
	{"jira-token", "Jira token", blocktypes.Credential, "Read-only Jira API token, injected by the sbx proxy.",
		map[string]any{"service": "jira", "phase": []any{"runtime"}, "apiKey": map[string]any{"name": "JIRA_TOKEN", "proxyManaged": true,
			"inject": []any{map[string]any{"domain": "jira.acme.internal", "header": "Authorization", "format": "Bearer %s"}}}}},
	{"anthropic-key", "Anthropic API key", blocktypes.Credential, "Model API key, injected by the sbx proxy.",
		map[string]any{"service": "anthropic", "phase": []any{"runtime"}, "optional": true, "apiKey": map[string]any{"name": "ANTHROPIC_API_KEY", "proxyManaged": true,
			"inject": []any{map[string]any{"domain": "api.anthropic.com", "header": "x-api-key", "format": "%s"}}}}},
	{"standard-resources", "Standard resources", "resources", "2 CPUs and 8 GiB, enough for most development.",
		map[string]any{"cpu": 2, "memory": "8GiB"}},
	{"gpu-resources", "GPU resources", "resources", "8 CPUs, 32 GiB and every GPU, for training and evaluation.",
		map[string]any{"cpu": 8, "memory": "32GiB", "gpu": "all"}},
	{"package-cache", "Package cache", "volume", "Keeps downloaded packages across restarts.",
		map[string]any{"path": "/home/agent/.cache", "size": "20GiB"}},
	{"readonly-guidance", "Read-only guidance", "agent-context", "Tells the agent this profile only reads.",
		map[string]any{"directory": "/home/agent", "filename": "AGENTS.md",
			"content": "This sandbox is read-only toward every service it can reach. Never create, edit or delete anything; report what you find instead."}},
	{"report-guidance", "Report guidance", "agent-context", "How support reports are written.",
		map[string]any{"directory": "/home/agent", "filename": "AGENTS.md",
			"content": "Summarise tickets with: impact, timeline, root cause (if known) and next steps. Quote ticket keys. Don't speculate about customers."}},
	{"host-git-identity", "Host git identity", "git-identity", "Commits carry the developer's own name and email.", map[string]any{}},
	{"ubuntu-base", "Ubuntu 24.04 (public)", "dockerfile", "The official ubuntu:24.04 image (no Docker Hardened Images login) with the platform floor sbx expects: bash, curl, git, CA certificates and the agent user.",
		map[string]any{"build": ubuntuBase}},
}

// ubuntuBase is a workload root filesystem from the official Ubuntu image,
// which pulls without a login (the spec examples' dhi.io bases need one).
// It adds the platform floor a workload base should carry (spec §3.5).
const ubuntuBase = `# syntax=docker/dockerfile:1
# Official Ubuntu (Docker Official Image), not Docker Hardened Images, so it
# pulls without a login. Adds the platform floor sbx expects of a workload
# (kit spec §3.5): bash, curl, git, a CA store, and the non-root agent user
# (uid 1000, home /home/agent). Ubuntu's own uid-1000 user is replaced.
FROM ubuntu:24.04
RUN apt-get update \
 && apt-get install -y --no-install-recommends bash ca-certificates curl git \
 && rm -rf /var/lib/apt/lists/* \
 && userdel --remove ubuntu \
 && useradd --create-home --uid 1000 --shell /bin/bash agent \
 && install -d -o agent -g agent /home/agent/workspace
USER agent
WORKDIR /home/agent/workspace
CMD ["bash", "-l"]
`

type profile struct {
	slug, name, kind, description, goal string
	blocks                              []string // profileBlocks slugs, or demo block slugs (github-egress, deny-cloud-metadata, github-token)
}

var profiles = []profile{
	{"developer", "Developer", "mixin", "General developer work: source control, packages and internal tools.",
		"Everyday development: clone and push code, install packages, read the tracker and wiki. No cloud metadata, nothing outside the approved destinations.",
		[]string{"github-egress", "package-registries", "acme-internal-services", "deny-cloud-metadata", "github-token", "standard-resources", "package-cache", "host-git-identity"}},
	{"vendor-docs-profile", "Vendor documentation", "mixin", "Read the EDA vendor's documentation and nothing else.",
		"Look things up in the vendor's documentation while working on designs.",
		[]string{"vendor-docs", "deny-cloud-metadata", "readonly-guidance"}},
	{"platform-sre", "Platform dev and SRE", "mixin", "Platform development and on-call: cluster, observability and code.",
		"Develop and operate the platform: read dashboards and logs, talk to the cluster API, change code.",
		[]string{"github-egress", "package-registries", "acme-internal-services", "observability", "cluster-api", "deny-cloud-metadata", "github-token", "standard-resources", "host-git-identity"}},
	{"internal-only", "Internal only", "mixin", "Internal services only; nothing on the public internet.",
		"Work only with internal services; no public internet, no cloud metadata.",
		[]string{"acme-internal-services", "deny-cloud-metadata", "standard-resources"}},
	{"isolated-chambers", "Isolated chambers", "mixin", "Sealed workflows that may only reach the chamber API.",
		"Run chamber jobs with no other network access at all.",
		[]string{"chamber-api", "deny-cloud-metadata", "standard-resources"}},
	{"ml-research", "ML research", "mixin", "Call models and read papers and documentation.",
		"Research with models: call model APIs, read papers, model hubs and framework docs, install packages, use GPUs.",
		[]string{"model-apis", "research-sources", "package-registries", "deny-cloud-metadata", "anthropic-key", "gpu-resources", "package-cache"}},
	{"dc-ops-agent", "Datacenter ops agent", "mixin", "An agent that reads host and datacenter inventory.",
		"Retrieve host and datacenter data from inventory systems, read-only.",
		[]string{"dc-inventory-readonly", "deny-cloud-metadata", "readonly-guidance"}},
	{"support-analysis", "Support ticket analysis", "mixin", "Analyse tickets and write reports for support reviews.",
		"Read tickets, analyse them with a model and write review reports. Never edit tickets.",
		[]string{"tickets-readonly", "model-apis", "deny-cloud-metadata", "jira-token", "anthropic-key", "report-guidance"}},
	{"integration-validation", "Read-only integration validation", "mixin", "Bounded, operator-triggered read-only checks of an integration.",
		"Validate the integration hub's cloud integration by reading only; any write is blocked.",
		[]string{"integration-readonly", "deny-cloud-metadata", "readonly-guidance"}},
}

// profileSamples seeds the sample profile blueprints once each, creating
// their shared blocks on first use. demoBlocks are the demo seed's blocks
// (github-egress, deny-cloud-metadata, github-token…), reused here.
func profileSamples(ctx context.Context, c *ent.Client, t *ent.Team, u *ent.User, demoBlocks map[string]*ent.Block, demoVersions map[string]*ent.BlockVersion) error {
	byslug := map[string]profileBlock{}
	for _, b := range profileBlocks {
		byslug[b.slug] = b
	}
	for _, p := range profiles {
		exists, err := c.Blueprint.Query().Where(blueprint.TeamID(t.ID), blueprint.Slug(p.slug)).Exist(ctx)
		if err != nil {
			return err
		}
		if exists {
			continue
		}
		bp, err := c.Blueprint.Create().SetTeam(t).SetSlug(p.slug).SetDisplayName(p.name).SetDescription(p.description).
			SetKind(blueprint.Kind(p.kind)).Save(ctx)
		if err != nil {
			return fmt.Errorf("profile %s: %w", p.slug, err)
		}
		if err := c.Goal.Create().SetBlueprint(bp).SetStatement(p.goal).Exec(ctx); err != nil {
			return err
		}
		for i, slug := range p.blocks {
			b, v := demoBlocks[slug], demoVersions[slug]
			if b == nil {
				pb, ok := byslug[slug]
				if !ok {
					return fmt.Errorf("profile %s: unknown block %s", p.slug, slug)
				}
				if b, v, err = sharedBlock(ctx, c, t, u, pb); err != nil {
					return fmt.Errorf("profile %s, block %s: %w", p.slug, slug, err)
				}
			}
			bt, _ := blocktypes.Get(b.Type)
			sl, err := c.Slot.Create().SetBlueprint(bp).SetKey(slug).SetLabel(b.Name).SetSection(bt.Section).
				SetBlockType(b.Type).SetRequired(true).SetState(slot.StateFilled).SetPosition(i).Save(ctx)
			if err != nil {
				return err
			}
			if err := c.BlockLink.Create().SetSlot(sl).SetBlock(b).SetBaseVersion(v).SetOverridesHash(patch.Hash(nil)).Exec(ctx); err != nil {
				return err
			}
		}
	}
	// Library blocks no profile uses yet (the Ubuntu base) exist too.
	for _, pb := range profileBlocks {
		if demoBlocks[pb.slug] == nil {
			if _, _, err := sharedBlock(ctx, c, t, u, pb); err != nil {
				return fmt.Errorf("block %s: %w", pb.slug, err)
			}
		}
	}
	return nil
}

// sharedBlock returns the library block for slug, creating it (version 1)
// the first time. Item keys are derived from content, so they're stable.
func sharedBlock(ctx context.Context, c *ent.Client, t *ent.Team, u *ent.User, pb profileBlock) (*ent.Block, *ent.BlockVersion, error) {
	b, err := c.Block.Query().Where(block.TeamID(t.ID), block.Slug(pb.slug)).WithCurrentVersion().Only(ctx)
	if err == nil && b.Edges.CurrentVersion != nil {
		return b, b.Edges.CurrentVersion, nil
	}
	if err != nil && !ent.IsNotFound(err) {
		return nil, nil, err
	}
	payload := pb.payload
	stableKeys(payload)
	if b == nil {
		b, err = c.Block.Create().SetTeam(t).SetType(pb.typ).SetSlug(pb.slug).SetName(pb.name).
			SetDescription(pb.description).SetVisibility(block.VisibilityTeam).Save(ctx)
		if err != nil {
			return nil, nil, err
		}
	}
	v, err := c.BlockVersion.Create().SetBlock(b).SetVersion(1).SetPayload(payload).SetPayloadHash(hash(payload)).
		SetSchemaVersion(SpecVersion).SetChangelog("Sample profile library.").
		SetAuthoredBy(blockversion.AuthoredByUser).SetAcceptedBy(u).Save(ctx)
	if err != nil {
		return nil, nil, err
	}
	if b, err = b.Update().SetCurrentVersion(v).Save(ctx); err != nil {
		return nil, nil, err
	}
	return b, v, nil
}

// stableKeys gives list items without one a "_k" derived from their content.
func stableKeys(v any) {
	switch t := v.(type) {
	case map[string]any:
		for _, x := range t {
			stableKeys(x)
		}
	case []any:
		seen := map[string]bool{}
		for _, it := range t {
			m, ok := it.(map[string]any)
			if !ok {
				continue
			}
			stableKeys(m)
			if _, has := m[patch.KeyField]; has {
				continue
			}
			base := blocks.Slugify(fmt.Sprint(m["label"], " ", strings.Join(strs(m["hosts"]), " "), " ", m["domain"]))
			k := base
			for n := 2; seen[k]; n++ {
				k = fmt.Sprintf("%s-%d", base, n)
			}
			seen[k] = true
			m[patch.KeyField] = k
		}
	}
}

func strs(v any) []string {
	list, _ := v.([]any)
	var out []string
	for _, x := range list {
		if s, ok := x.(string); ok {
			out = append(out, s)
		}
	}
	return out
}
