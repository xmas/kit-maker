// Package drivers is the registry of per-deployment drivers (DESIGN.md
// §1.2): registries for blueprint images, build runners, git hosts and AI providers. Each
// driver declares the config fields it takes, validates a config, and can
// test one against the real service before it is saved. AI drivers register
// from internal/ai, which owns the model clients.
package drivers

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"sort"
)

// Kind is an integration kind. It matches the integrations.kind enum.
type Kind string

const (
	KindRegistry    Kind = "registry"
	KindBuildRunner Kind = "build_runner"
	KindGitHost     Kind = "git_host"
	KindAI          Kind = "ai"
)

// Kinds lists every kind in display order.
var Kinds = []Kind{KindRegistry, KindBuildRunner, KindGitHost, KindAI}

// Field describes one config field, for the admin UI and the CLI.
type Field struct {
	Key         string `json:"key"`
	Label       string `json:"label"`
	Secret      bool   `json:"secret,omitempty"`
	Required    bool   `json:"required,omitempty"`
	Placeholder string `json:"placeholder,omitempty"`
	Help        string `json:"help,omitempty"`
}

// Config is a driver's settings, as stored (sealed) in integrations.
type Config map[string]string

// ErrTestUnavailable means the driver can't be tested from here yet; the
// config is still saved after validation.
var ErrTestUnavailable = errors.New("this driver can't be tested from Kit Maker yet")

// Driver is one implementation of a kind.
type Driver interface {
	Kind() Kind
	Name() string
	Label() string
	Fields() []Field
	// Validate checks the config without network access.
	Validate(Config) error
	// Test checks the config against the real service.
	Test(context.Context, Config) error
}

var registry = map[Kind]map[string]Driver{}

// Register adds a driver. It panics on duplicates: registration happens in
// init and a clash is a programming error.
func Register(d Driver) {
	byName, ok := registry[d.Kind()]
	if !ok {
		byName = map[string]Driver{}
		registry[d.Kind()] = byName
	}
	if _, dup := byName[d.Name()]; dup {
		panic(fmt.Sprintf("driver %s/%s registered twice", d.Kind(), d.Name()))
	}
	byName[d.Name()] = d
}

// Get returns the driver for kind and name.
func Get(kind Kind, name string) (Driver, error) {
	d, ok := registry[kind][name]
	if !ok {
		return nil, fmt.Errorf("unknown %s driver %q (have %v)", kind, name, Names(kind))
	}
	return d, nil
}

// Names lists the drivers registered for kind, sorted.
func Names(kind Kind) []string {
	var out []string
	for n := range registry[kind] {
		out = append(out, n)
	}
	sort.Strings(out)
	return out
}

// ValidKind reports whether k is a known kind.
func ValidKind(k Kind) bool { return slices.Contains(Kinds, k) }

// requireFields checks that every required field is non-empty.
func requireFields(d Driver, c Config) error {
	var missing []string
	for _, f := range d.Fields() {
		if f.Required && c[f.Key] == "" {
			missing = append(missing, f.Key)
		}
	}
	if len(missing) > 0 {
		return fmt.Errorf("%s/%s: missing %v", d.Kind(), d.Name(), missing)
	}
	return nil
}

// Redact returns c with secret fields masked, for display and logs.
func Redact(d Driver, c Config) Config {
	out := Config{}
	secret := map[string]bool{}
	for _, f := range d.Fields() {
		secret[f.Key] = f.Secret
	}
	for k, v := range c {
		if secret[k] && v != "" {
			out[k] = "••••••"
			continue
		}
		out[k] = v
	}
	return out
}
