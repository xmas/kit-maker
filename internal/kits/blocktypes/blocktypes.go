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
	Key     string `json:"key"`
	Label   string `json:"label"`
	Section string `json:"section"`
	// Capability is the kit capability it compiles into.
	Capability string `json:"capability"`
	validate   func(map[string]any) error
}

// Validate checks a payload's structure. The compiler then validates the
// whole kit with the spec package.
func (t Type) Validate(p map[string]any) error {
	if t.validate == nil {
		return nil
	}
	return t.validate(p)
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

const (
	NetworkRules  = "network-rules"
	Credential    = "credential"
	RawCapability = "raw-capability"
)

var types = []Type{
	{Key: NetworkRules, Label: "Network rules", Section: "network", Capability: "com.docker.sandbox/network-policy@2", validate: validateNetwork},
	{Key: Credential, Label: "Credential", Section: "credentials", Capability: "com.docker.sandbox/credential@1", validate: validateCredential},
	{Key: RawCapability, Label: "Raw capability", Section: "runtime", validate: validateRaw},
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
	switch key {
	case NetworkRules:
		return map[string]any{"runtime": map[string]any{"allow": []any{}, "deny": []any{}}}
	case Credential:
		env := strings.ToUpper(strings.ReplaceAll(slug, "-", "_"))
		if !strings.HasSuffix(env, "_TOKEN") && !strings.HasSuffix(env, "_KEY") {
			env += "_TOKEN"
		}
		return map[string]any{
			"service": slug,
			"phase":   []any{"runtime"},
			"apiKey":  map[string]any{"name": env, "proxyManaged": true, "inject": []any{}},
		}
	default:
		return map[string]any{"type": "com.docker.sandbox/long-running@1"}
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
