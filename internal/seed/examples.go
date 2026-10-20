package seed

import (
	"context"
	"embed"
	"encoding/json"
	"fmt"
	"io/fs"
	"path"
	"strings"

	"gopkg.in/yaml.v3"

	"github.com/docker/kit-maker/internal/blocks"
	"github.com/docker/kit-maker/internal/blueprints/blocktypes"
	"github.com/docker/kit-maker/internal/blueprints/patch"
	"github.com/docker/kit-maker/internal/ent"
	"github.com/docker/kit-maker/internal/ent/block"
	"github.com/docker/kit-maker/internal/ent/blockversion"
	"github.com/docker/kit-maker/internal/ent/blueprint"
	"github.com/docker/kit-maker/internal/ent/slot"
)

// The kit spec's examples (docker/sandbox-kit-spec examples/), one folder
// each: <name>.yaml, its Dockerfile, and the files that Dockerfile copies.
//
//go:embed examples
var examplesFS embed.FS

// Example is one spec example converted into a blueprint: its descriptor
// split into blocks, one per slot.
type Example struct {
	Name        string // folder name, used as the blueprint slug
	DisplayName string
	Description string
	Kind        string
	Slots       []ExampleSlot
}

// ExampleSlot is one block of an example.
type ExampleSlot struct {
	Label   string
	Type    string // blocktypes key
	Payload map[string]any
}

// Examples converts every embedded spec example.
func Examples() ([]Example, error) {
	dirs, err := fs.ReadDir(examplesFS, "examples")
	if err != nil {
		return nil, err
	}
	var out []Example
	for _, d := range dirs {
		if !d.IsDir() {
			continue
		}
		// Folders without a descriptor (a bare Dockerfile) aren't kits.
		if _, err := fs.Stat(examplesFS, path.Join("examples", d.Name(), d.Name()+".yaml")); err != nil {
			continue
		}
		ex, err := convertExample(d.Name())
		if err != nil {
			return nil, fmt.Errorf("example %s: %w", d.Name(), err)
		}
		out = append(out, ex)
	}
	return out, nil
}

func convertExample(name string) (Example, error) {
	dir := path.Join("examples", name)
	raw, err := examplesFS.ReadFile(path.Join(dir, name+".yaml"))
	if err != nil {
		return Example{}, err
	}
	var d map[string]any
	if err := yaml.Unmarshal(raw, &d); err != nil {
		return Example{}, err
	}
	d = jsonish(d).(map[string]any)
	ex := Example{Name: name, DisplayName: str(d["displayName"]), Description: str(d["description"]), Kind: str(d["kind"])}
	if ex.DisplayName == "" {
		ex.DisplayName = name
	}
	add := func(label, typ string, p map[string]any) {
		ex.Slots = append(ex.Slots, ExampleSlot{Label: label, Type: typ, Payload: p})
	}

	// Top-level fields.
	if p := pick(d, "author", "sourceUrl", "iconUrl", "licenses"); len(p) > 0 {
		add("About", "about", p)
	}
	if p := pick(d, "provides", "requires", "integrates", "conflicts"); len(p) > 0 {
		add("Dependencies", "dependencies", p)
	}
	if args, ok := d["args"].(map[string]any); ok && len(args) > 0 {
		add("Arguments", "args", map[string]any{"args": args})
	}
	switch {
	case d["build"] != nil:
		add("Dockerfile", "dockerfile", map[string]any{"build": str(d["build"])})
	case d["kits"] != nil:
		add("Included kits", "kits", map[string]any{"kits": d["kits"]})
	default:
		file := str(d["dockerfile"])
		if file == "" {
			file = name + ".dockerfile"
		}
		if b, err := examplesFS.ReadFile(path.Join(dir, file)); err == nil {
			add("Dockerfile", "dockerfile", map[string]any{"build": string(b)})
		} else if d["dockerfile"] != nil {
			return ex, fmt.Errorf("dockerfile %s: %w", file, err)
		}
	}

	// Capabilities; a group's members become blocks of their own, each
	// optional when the group was.
	caps, _ := d["capabilities"].([]any)
	for _, item := range caps {
		c, _ := item.(map[string]any)
		if g, ok := c["group"].(map[string]any); ok {
			opt, _ := g["optional"].(bool)
			members, _ := g["capabilities"].([]any)
			for _, m := range members {
				mc, _ := m.(map[string]any)
				if opt {
					mc["optional"] = true
				}
				if err := convertCapability(mc, add); err != nil {
					return ex, err
				}
			}
			continue
		}
		if err := convertCapability(c, add); err != nil {
			return ex, err
		}
	}
	return ex, nil
}

// typeFor maps a capability type to the block type that holds it.
var typeFor = func() map[string]string {
	m := map[string]string{}
	for _, t := range blocktypes.All() {
		if t.Emit == "capability" && t.Key != "oauth-credential" {
			m[t.Capability] = t.Key
		}
	}
	return m
}()

func convertCapability(c map[string]any, add func(label, typ string, p map[string]any)) error {
	typ := str(c["type"])
	cfg, _ := c["config"].(map[string]any)
	if cfg == nil {
		cfg = map[string]any{}
	}
	entry := func(p map[string]any) map[string]any {
		if opt, _ := c["optional"].(bool); opt {
			p["optional"] = true
		}
		if desc := str(c["description"]); desc != "" {
			p["description"] = desc
		}
		return p
	}
	label := func(fallback string) string {
		if n := str(c["name"]); n != "" {
			return n
		}
		return fallback
	}
	switch typ {
	case "com.docker.sandbox/network-policy@1", "com.docker.sandbox/network-policy@2":
		add(label("Network"), blocktypes.NetworkRules, networkPayload(cfg))
	case "com.docker.sandbox/credential@1":
		phase := cfg["phase"]
		if s, ok := phase.(string); ok {
			phase = []any{s}
		}
		if _, oauth := cfg["oauth"]; oauth || cfg["apiKey"] == nil {
			add(label("Credential: "+str(cfg["service"])), "oauth-credential", entry(cfg))
			return nil
		}
		p := entry(map[string]any{"service": cfg["service"], "phase": phase, "apiKey": cfg["apiKey"]})
		add(label("Credential: "+str(cfg["service"])), blocktypes.Credential, p)
	case "com.docker.sandbox/lifecycle@1":
		for _, part := range []struct{ key, typ, label string }{
			{"install", "install-steps", "Install steps"},
			{"startup", "startup-steps", "Startup steps"},
			{"files", "files", "Files"},
			{"interactive", "interactive", "Interactive launch"},
		} {
			if v, ok := cfg[part.key]; ok && v != nil {
				add(part.label, part.typ, map[string]any{part.key: v})
			}
		}
	default:
		key, known := typeFor[typ]
		if !known {
			add(label(typ), blocktypes.RawCapability, entry(map[string]any{"type": typ, "config": cfg}))
			return nil
		}
		t, _ := blocktypes.Get(key)
		add(label(t.Label), key, entry(cfg))
	}
	return nil
}

// networkPayload turns either network-policy version's config into a
// network-rules payload: every entry as {hosts, methods, paths}.
func networkPayload(cfg map[string]any) map[string]any {
	out := map[string]any{}
	for _, phase := range []string{"install", "runtime"} {
		pc, _ := cfg[phase].(map[string]any)
		if pc == nil {
			continue
		}
		rules := map[string]any{}
		for _, list := range []string{"allow", "deny"} {
			items, _ := pc[list].([]any)
			var entries []any
			for _, it := range items {
				switch e := it.(type) {
				case string:
					entries = append(entries, map[string]any{"hosts": []any{e}})
				case map[string]any:
					entries = append(entries, e)
				}
			}
			if entries != nil {
				rules[list] = entries
			}
		}
		out[phase] = rules
	}
	return out
}

// examples seeds every spec example as a blueprint, once each. Their parts
// go into the library as blocks (identical parts are shared), so the
// examples double as a starter library.
func examples(ctx context.Context, c *ent.Client, t *ent.Team, u *ent.User) error {
	exs, err := Examples()
	if err != nil {
		return err
	}
	shared := map[string]*ent.Block{} // type + payload hash → block
	for _, ex := range exs {
		exists, err := c.Blueprint.Query().Where(blueprint.TeamID(t.ID), blueprint.Slug(ex.Name)).Exist(ctx)
		if err != nil {
			return err
		}
		if exists {
			continue
		}
		kind := blueprint.Kind(ex.Kind)
		if kind.String() == "" || blueprint.KindValidator(kind) != nil {
			kind = blueprint.KindMixin
		}
		bp, err := c.Blueprint.Create().SetTeam(t).SetSlug(ex.Name).SetDisplayName(ex.DisplayName).
			SetDescription(ex.Description).SetKind(kind).Save(ctx)
		if err != nil {
			return fmt.Errorf("example %s: %w", ex.Name, err)
		}
		if ex.Description != "" {
			if err := c.Goal.Create().SetBlueprint(bp).SetStatement(ex.Description).Exec(ctx); err != nil {
				return err
			}
		}
		keys := map[string]bool{}
		for i, s := range ex.Slots {
			bt, _ := blocktypes.Get(s.Type)
			b, v, err := exampleBlock(ctx, c, t, u, ex, s, shared)
			if err != nil {
				return fmt.Errorf("example %s, %s: %w", ex.Name, s.Label, err)
			}
			key := blocks.Slugify(s.Label)
			for n := 2; keys[key]; n++ {
				key = fmt.Sprintf("%s-%d", blocks.Slugify(s.Label), n)
			}
			keys[key] = true
			sl, err := c.Slot.Create().SetBlueprint(bp).SetKey(key).SetLabel(s.Label).SetSection(bt.Section).
				SetBlockType(s.Type).SetRequired(true).SetState(slot.StateFilled).SetPosition(i).Save(ctx)
			if err != nil {
				return err
			}
			if err := c.BlockLink.Create().SetSlot(sl).SetBlock(b).SetBaseVersion(v).
				SetOverridesHash(patch.Hash(nil)).Exec(ctx); err != nil {
				return err
			}
		}
	}
	return nil
}

// exampleBlock finds or creates the library block for one example part.
func exampleBlock(ctx context.Context, c *ent.Client, t *ent.Team, u *ent.User, ex Example, s ExampleSlot, shared map[string]*ent.Block) (*ent.Block, *ent.BlockVersion, error) {
	id := s.Type + " " + hash(s.Payload)
	if b, ok := shared[id]; ok {
		return b, b.Edges.CurrentVersion, nil
	}
	name := ex.DisplayName + " · " + s.Label
	slug := blocks.Slugify(ex.Name + "-" + s.Label)
	b, err := c.Block.Query().Where(block.TeamID(t.ID), block.Slug(slug)).WithCurrentVersion().Only(ctx)
	if err == nil && b.Edges.CurrentVersion != nil {
		shared[id] = b
		return b, b.Edges.CurrentVersion, nil
	}
	if !ent.IsNotFound(err) && err != nil {
		return nil, nil, err
	}
	payload := s.Payload
	patch.AssignKeys(payload)
	b, err = c.Block.Create().SetTeam(t).SetType(s.Type).SetSlug(slug).SetName(name).
		SetDescription("From the kit spec's " + ex.Name + " example.").SetVisibility(block.VisibilityTeam).Save(ctx)
	if err != nil {
		return nil, nil, err
	}
	v, err := c.BlockVersion.Create().SetBlock(b).SetVersion(1).SetPayload(payload).SetPayloadHash(hash(payload)).
		SetSchemaVersion(SpecVersion).SetChangelog("Converted from the kit spec example " + ex.Name + ".").
		SetAuthoredBy(blockversion.AuthoredByUser).SetAcceptedBy(u).Save(ctx)
	if err != nil {
		return nil, nil, err
	}
	if b, err = b.Update().SetCurrentVersion(v).Save(ctx); err != nil {
		return nil, nil, err
	}
	b.Edges.CurrentVersion = v
	shared[id] = b
	return b, v, nil
}

// jsonish makes decoded YAML look like decoded JSON (map[string]any,
// float64 numbers), the shape payloads are stored and compared in.
func jsonish(v any) any {
	b, _ := json.Marshal(normalizeYAML(v))
	var out any
	_ = json.Unmarshal(b, &out)
	return out
}

func normalizeYAML(v any) any {
	switch t := v.(type) {
	case map[string]any:
		for k, x := range t {
			t[k] = normalizeYAML(x)
		}
		return t
	case map[any]any:
		m := map[string]any{}
		for k, x := range t {
			m[fmt.Sprint(k)] = normalizeYAML(x)
		}
		return m
	case []any:
		for i, x := range t {
			t[i] = normalizeYAML(x)
		}
		return t
	}
	return v
}

func pick(d map[string]any, keys ...string) map[string]any {
	out := map[string]any{}
	for _, k := range keys {
		if v, ok := d[k]; ok && v != nil {
			out[k] = v
		}
	}
	return out
}

func str(v any) string {
	s, _ := v.(string)
	return strings.TrimSpace(s)
}
