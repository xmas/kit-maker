package blocktypes

import (
	"slices"
	"testing"
)

// A new block starts from its type's starter, so every starter must validate.
func TestStartersValidate(t *testing.T) {
	for _, ty := range All() {
		for _, slug := range []string{"npm-mirror", "stripe-key", "x"} {
			if err := ty.Validate(Starter(ty.Key, slug)); err != nil {
				t.Errorf("%s starter for %q: %v", ty.Key, slug, err)
			}
		}
	}
}

func TestAbilitiesOf(t *testing.T) {
	ro := map[string]any{"runtime": map[string]any{"allow": []any{map[string]any{"hosts": []any{"a.example"}, "methods": []any{"GET"}}}}}
	if got := AbilitiesOf(NetworkRules, ro); len(got) != 1 || got[0] != "readonly" {
		t.Errorf("GET-only allow: %v", got)
	}
	rw := map[string]any{"install": map[string]any{"allow": []any{map[string]any{"hosts": []any{"a.example"}}}},
		"runtime": map[string]any{"deny": []any{map[string]any{"hosts": []any{"b.example"}}}}}
	got := AbilitiesOf(NetworkRules, rw)
	for _, want := range []string{"install-network", "network", "deny"} {
		if !slices.Contains(got, want) {
			t.Errorf("want %s in %v", want, got)
		}
	}
	for _, bt := range All() {
		if len(AbilitiesOf(bt.Key, Starter(bt.Key, "x"))) == 0 && bt.Key != NetworkRules {
			t.Errorf("%s has no ability", bt.Key)
		}
	}
}
