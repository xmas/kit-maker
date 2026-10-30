// Package blocktypes defines the kinds of library block and how each is
// checked (DESIGN.md §2.1). The compiler turns them into kit capabilities;
// the UI picks a form by Key.
package blocktypes

import (
	"fmt"
	"regexp"
	"slices"
	"strings"
)

// Type describes one block type.
type Type struct {
	Key         string `json:"key"`
	Label       string `json:"label"`
	Section     string `json:"section"`
	Description string `json:"description"`
	// Capability is the kit capability it compiles into; empty for the
	// top-level fields (Emit "top") and for raw capabilities, which name
	// their own type.
	Capability string `json:"capability,omitempty"`
	// Emit says how the compiler places it (see compiler.Compile):
	//   network    merged into the one network-policy@2
	//   lifecycle  one list (Lifecycle) of the one lifecycle@1
	//   capability its own entry (Singleton: at most one per blueprint)
	//   credential, raw  their own entry, with a typed payload
	//   top        top-level descriptor fields (Fields)
	Emit      string `json:"-"`
	Singleton bool   `json:"-"`
	// ConfigLess types carry no config, only optional/description.
	ConfigLess bool     `json:"-"`
	Lifecycle  string   `json:"-"`
	Fields     []string `json:"-"`
	starter    func(slug string) map[string]any
	validate   func(map[string]any) error
}

// Validate checks a payload's structure. The compiler then validates the
// whole blueprint with the spec package, which owns the real rules.
func (t Type) Validate(p map[string]any) error {
	if t.validate != nil {
		return t.validate(p)
	}
	if t.ConfigLess {
		for k := range p {
			if k != "optional" && k != "description" && k != "_k" {
				return fmt.Errorf("%s takes no settings, only optional and description (found %q)", t.Label, k)
			}
		}
	}
	return nil
}

// Sections in outline order.
var Sections = []struct {
	Key   string `json:"key"`
	Label string `json:"label"`
}{
	{"agent-base", "Agent & base"},
	{"network", "Network"},
	{"credentials", "Credentials"},
	{"setup", "Setup"},
	{"agent", "Agent"},
	{"runtime", "Runtime"},
}

// Block type keys the code refers to by name.
const (
	NetworkRules  = "network-rules"
	Credential    = "credential"
	RawCapability = "raw-capability"
)

const sbx = "com.docker.sandbox/"

// A capability block's payload is the capability's config, plus two
// entry-level keys the compiler lifts out: optional and description (the
// entry's name is the block's name). Starters come from the spec's
// kitchen-sink example and are valid on their own.
var types = []Type{
	// --- Agent & base: top-level descriptor fields ---
	{Key: "about", Label: "About", Section: "agent-base", Emit: "top", Fields: []string{"author", "sourceUrl", "iconUrl", "licenses"},
		Description: "Publisher, source link, icon and licenses shown in catalogs.",
		starter: func(string) map[string]any {
			return map[string]any{"author": "", "sourceUrl": "", "iconUrl": "", "licenses": []any{}}
		}},
	{Key: "dockerfile", Label: "Dockerfile", Section: "agent-base", Emit: "top", Fields: []string{"build"},
		Description: "The content recipe: Dockerfile text built into the kit's layers (a workload needs one), plus any build files its COPY lines read (files: path → content, put beside the kit when it's published or downloaded).",
		starter: func(string) map[string]any {
			return map[string]any{"build": "# syntax=docker/dockerfile:1\nFROM docker/sandbox-templates:shell\n"}
		}},
	{Key: "kits", Label: "Included kits", Section: "agent-base", Emit: "top", Fields: []string{"kits"},
		Description: "Published kits merged in as this one's content (a kit set), instead of a Dockerfile.",
		starter: func(string) map[string]any {
			return map[string]any{"kits": []any{map[string]any{"ref": "docker.io/dockerdev/sbx-kit-gh:2.72.0"}}}
		}},
	{Key: "dependencies", Label: "Dependencies", Section: "agent-base", Emit: "top", Fields: []string{"provides", "requires", "integrates", "conflicts"},
		Description: "What this provides to other kits, and what it requires, works with or conflicts with.",
		starter: func(slug string) map[string]any {
			return map[string]any{"provides": []any{slug}, "requires": []any{}, "integrates": []any{}, "conflicts": []any{}}
		}},
	{Key: "args", Label: "Arguments", Section: "agent-base", Emit: "top", Fields: []string{"args"},
		Description: "Inputs the installer supplies, used as ${{ kit.args.<name> }}; env exports one into the sandbox.",
		starter: func(string) map[string]any {
			return map[string]any{"args": map[string]any{"team": map[string]any{
				"default": "", "description": "Team name", "env": "SANDBOX_TEAM"}}}
		}},

	// --- Network ---
	{Key: NetworkRules, Label: "Network rules", Section: "network", Emit: "network", Capability: sbx + "network-policy@2",
		Description: "Hosts the sandbox may reach (or never reach), per phase.", validate: validateNetwork,
		starter: func(string) map[string]any {
			return map[string]any{"runtime": map[string]any{"allow": []any{}, "deny": []any{}}}
		}},

	// --- Credentials ---
	{Key: Credential, Label: "Credential", Section: "credentials", Emit: "credential", Capability: sbx + "credential@1",
		Description: "An API key the sbx proxy injects into requests; the sandbox only sees a placeholder.", validate: validateCredential,
		starter: credentialStarter},
	{Key: "oauth-credential", Label: "OAuth credential", Section: "credentials", Emit: "capability", Capability: sbx + "credential@1",
		Description: "An OAuth login the proxy refreshes, written to a credentials file the tool reads.",
		starter: func(slug string) map[string]any {
			return map[string]any{
				"service": slug, "phase": "runtime",
				"oauth": map[string]any{
					"tokenEndpoint": map[string]any{"host": "auth.example.com", "path": "/oauth/token"},
					"sentinels":     map[string]any{"accessToken": "sentinel-access", "refreshToken": "sentinel-refresh"},
				},
			}
		}},
	{Key: "ssh-agent", Label: "SSH agent", Section: "credentials", Emit: "capability", Capability: sbx + "ssh-agent@1",
		Description: "Lets the sandbox use the host's SSH agent to sign or authenticate, without the key.",
		starter: func(string) map[string]any {
			return map[string]any{"phase": "runtime", "unrestricted": false, "sign": []any{"git"}, "authenticate": []any{"git@github.com"}}
		}},

	// --- Setup: merged into one lifecycle@1 ---
	{Key: "install-steps", Label: "Install steps", Section: "setup", Emit: "lifecycle", Lifecycle: "install", Capability: sbx + "lifecycle@1",
		Description: "Commands run once at create, before the agent starts (install-phase network applies).",
		starter: func(string) map[string]any {
			return map[string]any{"install": []any{map[string]any{
				"command": []any{"sh", "-c", "echo installing"}, "user": "root", "description": "Install dependencies"}}}
		}},
	{Key: "startup-steps", Label: "Startup steps", Section: "setup", Emit: "lifecycle", Lifecycle: "startup", Capability: sbx + "lifecycle@1",
		Description: "Commands run each time the sandbox starts.",
		starter: func(string) map[string]any {
			return map[string]any{"startup": []any{map[string]any{
				"command": "echo booted", "user": "agent", "description": "Start-up message"}}}
		}},
	{Key: "files", Label: "Files", Section: "setup", Emit: "lifecycle", Lifecycle: "files", Capability: sbx + "lifecycle@1",
		Description: "Files written into the sandbox at create.",
		starter: func(string) map[string]any {
			return map[string]any{"files": []any{map[string]any{
				"path": "/home/agent/.config/example/settings.json", "content": "{}", "mode": "0644", "description": "Settings"}}}
		}},

	// --- Agent ---
	{Key: "agent-launch", Label: "Agent launch", Section: "agent", Emit: "capability", Capability: sbx + "sbx@1", Singleton: true, ConfigLess: true,
		Description: "Marks this as an agent sandbox: the host launches the agent and honours the image's user. Workload blueprints only."},
	{Key: "interactive", Label: "Interactive launch", Section: "agent", Emit: "lifecycle", Lifecycle: "interactive", Capability: sbx + "lifecycle@1",
		Description: "Arguments the agent starts with when a terminal is attached.",
		starter:     func(string) map[string]any { return map[string]any{"interactive": []any{"--interactive"}} }},
	{Key: "agent-context", Label: "Agent context", Section: "agent", Emit: "capability", Capability: sbx + "agent-context@2",
		Description: "Instructions written where the agent reads them (AGENTS.md, CLAUDE.md…).",
		starter: func(string) map[string]any {
			return map[string]any{"directory": "/home/agent", "filename": "AGENTS.md", "content": "Describe how the agent should work in this sandbox."}
		}},
	{Key: "agent-sessions", Label: "Agent sessions", Section: "agent", Emit: "capability", Capability: sbx + "agent-sessions@1", Singleton: true,
		Description: "How a harness prompts and resumes the agent without a terminal.",
		starter: func(string) map[string]any {
			return map[string]any{"prompt": []any{"-p", "{{.Prompt}}"}, "resume": []any{"--resume", "{{.SessionID}}"}, "continue": []any{"--continue"}}
		}},
	{Key: "interactive-sessions", Label: "Interactive sessions", Section: "agent", Emit: "capability", Capability: sbx + "agent-interactive-sessions@1", Singleton: true,
		Description: "How a terminal session prompts, resumes or picks an agent session.",
		starter: func(string) map[string]any {
			return map[string]any{"prompt": []any{"{{.Prompt}}"}, "resume": []any{"--resume", "{{.SessionID}}"}, "continue": []any{"--continue"}}
		}},
	{Key: "acp", Label: "ACP agent", Section: "agent", Emit: "capability", Capability: sbx + "acp@1",
		Description: "Runs an agent over the Agent Client Protocol.",
		starter: func(slug string) map[string]any {
			return map[string]any{"agent": slug, "command": []any{slug, "--acp"}, "protocolVersion": 1}
		}},
	{Key: "agent-skills", Label: "Skills folder", Section: "agent", Emit: "capability", Capability: sbx + "agent-skills@1",
		Description: "A folder of host skills the agent can read (or write).",
		starter: func(string) map[string]any {
			return map[string]any{"path": "/home/agent/.local/skills", "mode": "readonly"}
		}},
	{Key: "agent-skill", Label: "Skill", Section: "agent", Emit: "capability", Capability: sbx + "agent-skill@1",
		Description: "One skill shipped in the image, offered to the agent.",
		starter:     func(slug string) map[string]any { return map[string]any{"path": "/opt/skills/" + slug, "name": slug} }},

	// --- Runtime ---
	{Key: "resources", Label: "Resources", Section: "runtime", Emit: "capability", Capability: sbx + "resources@1", Singleton: true,
		Description: "CPU, memory and GPU limits for the sandbox.",
		starter:     func(string) map[string]any { return map[string]any{"cpu": 2, "memory": "4GiB"} }},
	{Key: "port", Label: "Port", Section: "runtime", Emit: "capability", Capability: sbx + "port@1",
		Description: "A container port published to the host.",
		starter: func(string) map[string]any {
			return map[string]any{"name": "web", "container": 8080, "transport": "tcp"}
		}},
	{Key: "volume", Label: "Volume", Section: "runtime", Emit: "capability", Capability: sbx + "volume@1",
		Description: "Storage that survives restarts and recreates (or tmpfs scratch space).",
		starter:     func(string) map[string]any { return map[string]any{"path": "/home/agent/.cache", "size": "10GiB"} }},
	{Key: "host-mount", Label: "Host mount", Section: "runtime", Emit: "capability", Capability: sbx + "host-mount@1",
		Description: "A host folder mounted into the sandbox.",
		starter:     func(string) map[string]any { return map[string]any{"path": "/home/agent/.shared", "mode": "0700"} }},
	{Key: "usb-device", Label: "USB device", Section: "runtime", Emit: "capability", Capability: sbx + "usb-device@1",
		Description: "A USB device passed through by vendor/product or class.",
		starter:     func(string) map[string]any { return map[string]any{"vendorId": "0x1234", "productId": "0x5678"} }},
	{Key: "git-identity", Label: "Git identity", Section: "runtime", Emit: "capability", Capability: sbx + "git-identity@1", Singleton: true, ConfigLess: true,
		Description: "Shares the host's git name and email with the sandbox."},
	{Key: "long-running", Label: "Long-running", Section: "runtime", Emit: "capability", Capability: sbx + "long-running@1", Singleton: true, ConfigLess: true,
		Description: "Keeps the sandbox running when no session is attached."},
	{Key: "kit-registry", Label: "Kit registry", Section: "runtime", Emit: "capability", Capability: sbx + "kit-registry@1", Singleton: true, ConfigLess: true,
		Description: "Lets the sandbox publish kits into the host's image store."},
	{Key: "privileged", Label: "Privileged", Section: "runtime", Emit: "capability", Capability: sbx + "privileged@1", Singleton: true, ConfigLess: true,
		Description: "Runs the sandbox privileged. Broad: only when nothing narrower works.",
		starter:     func(string) map[string]any { return map[string]any{"optional": true} }},
	{Key: RawCapability, Label: "Raw capability", Section: "runtime", Emit: "raw",
		Description: "Any other capability type, including your own (namespace/name@1), with its config as is.",
		validate:    validateRaw,
		starter: func(string) map[string]any {
			return map[string]any{"type": "com.example.vendor/widget@1", "optional": true, "config": map[string]any{}}
		}},
}

// Get returns a block type.
func Get(key string) (Type, bool) {
	for _, t := range types {
		if t.Key == key {
			return t, true
		}
	}
	return Type{}, false
}

// All returns every block type.
func All() []Type { return append([]Type(nil), types...) }

// ForSection returns the block types that fill a section, in order.
func ForSection(section string) []Type {
	var out []Type
	for _, t := range types {
		if t.Section == section {
			out = append(out, t)
		}
	}
	return out
}

// Starter is a valid first payload for a new block of type key, named slug.
func Starter(key, slug string) map[string]any {
	t, ok := Get(key)
	if !ok {
		t, _ = Get(RawCapability)
	}
	if t.starter == nil {
		return map[string]any{}
	}
	return t.starter(slug)
}

func credentialStarter(slug string) map[string]any {
	env := strings.ToUpper(strings.ReplaceAll(slug, "-", "_"))
	if !strings.HasSuffix(env, "_TOKEN") && !strings.HasSuffix(env, "_KEY") {
		env += "_TOKEN"
	}
	return map[string]any{
		"service": slug,
		"phase":   []any{"runtime"},
		"apiKey":  map[string]any{"name": env, "proxyManaged": true, "inject": []any{}},
	}
}

var (
	methodRE  = regexp.MustCompile(`^[A-Z]+$`)
	serviceRE = regexp.MustCompile(`^[a-z0-9]([a-z0-9-]*[a-z0-9])?$`)
	capTypeRE = regexp.MustCompile(`^[a-z0-9]([a-z0-9.-]*[a-z0-9])?/[a-z0-9]([a-z0-9-]*[a-z0-9])?@[1-9][0-9]*$`)
	envNameRE = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_]*$`)
)

func validateNetwork(p map[string]any) error {
	for phase, v := range p {
		if phase != "install" && phase != "runtime" {
			return fmt.Errorf("network rules: unknown phase %q (install or runtime)", phase)
		}
		rules, ok := v.(map[string]any)
		if !ok {
			return fmt.Errorf("network rules: %s must be an object with allow/deny", phase)
		}
		for list, entries := range rules {
			if list != "allow" && list != "deny" {
				return fmt.Errorf("network rules: %s.%s: only allow and deny", phase, list)
			}
			items, ok := entries.([]any)
			if !ok {
				return fmt.Errorf("network rules: %s.%s must be a list", phase, list)
			}
			for i, it := range items {
				if err := validateEntry(it); err != nil {
					return fmt.Errorf("network rules: %s.%s[%d]: %w", phase, list, i, err)
				}
			}
		}
	}
	return nil
}

func validateEntry(it any) error {
	e, ok := it.(map[string]any)
	if !ok {
		return fmt.Errorf("entry must be an object")
	}
	hosts := strings_(e["hosts"])
	if len(hosts) == 0 {
		return fmt.Errorf("needs at least one host")
	}
	methods := strings_(e["methods"])
	for _, m := range methods {
		if !methodRE.MatchString(m) {
			return fmt.Errorf("method %q must be an uppercase HTTP method or ANY", m)
		}
	}
	paths := strings_(e["paths"])
	for _, path := range paths {
		if !strings.HasPrefix(path, "/") {
			return fmt.Errorf("path %q must start with /", path)
		}
	}
	if len(paths) > 0 && len(methods) == 0 {
		return fmt.Errorf("paths need methods")
	}
	if len(methods) > 0 {
		for _, h := range hosts {
			if strings.Contains(h, "*") {
				return fmt.Errorf("bounded entries need literal hosts, not %q: drop the methods or list the hosts", h)
			}
		}
	}
	return nil
}

func validateCredential(p map[string]any) error {
	svc, _ := p["service"].(string)
	if !serviceRE.MatchString(svc) {
		return fmt.Errorf("credential: service %q must be lowercase-kebab", svc)
	}
	phases := strings_(p["phase"])
	if len(phases) == 0 {
		return fmt.Errorf("credential: pick at least one phase")
	}
	for _, ph := range phases {
		if ph != "install" && ph != "runtime" {
			return fmt.Errorf("credential: unknown phase %q", ph)
		}
	}
	api, _ := p["apiKey"].(map[string]any)
	if api == nil {
		return fmt.Errorf("credential: apiKey is required (OAuth credentials come later)")
	}
	name, _ := api["name"].(string)
	inject, _ := api["inject"].([]any)
	if name == "" && len(inject) == 0 {
		return fmt.Errorf("credential: name the env var or add an inject rule")
	}
	if name != "" && !envNameRE.MatchString(name) {
		return fmt.Errorf("credential: %q is not a valid env var name", name)
	}
	for i, it := range inject {
		m, _ := it.(map[string]any)
		if d, _ := m["domain"].(string); d == "" {
			return fmt.Errorf("credential: inject[%d] needs a domain", i)
		}
	}
	return nil
}

func validateRaw(p map[string]any) error {
	t, _ := p["type"].(string)
	if !capTypeRE.MatchString(t) {
		return fmt.Errorf("raw capability: type %q must look like namespace/name@1", t)
	}
	if slices.Contains([]string{"com.docker.sandbox/network-policy@2", "com.docker.sandbox/network-policy@1"}, t) {
		return fmt.Errorf("raw capability: use a network-rules block for %s", t)
	}
	return nil
}

func strings_(v any) []string {
	list, _ := v.([]any)
	out := make([]string, 0, len(list))
	for _, x := range list {
		if s, ok := x.(string); ok && s != "" {
			out = append(out, s)
		}
	}
	return out
}
