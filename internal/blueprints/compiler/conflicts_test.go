package compiler

import "testing"

func netBlock(name string, p map[string]any) *Block {
	b := blk("network-rules", p)
	b.Name = name
	return b
}

func entry(k string, hosts []any, methods ...any) map[string]any {
	e := map[string]any{"_k": k, "hosts": hosts}
	if len(methods) > 0 {
		e["methods"] = methods
	}
	return e
}

func TestConflicts(t *testing.T) {
	slots := []Slot{
		{Key: "internal", Block: netBlock("Internal services", map[string]any{"runtime": map[string]any{
			"allow": []any{entry("jira", []any{"jira.acme.internal"}), entry("meta", []any{"metadata.google.internal"})}}})},
		{Key: "tickets", Block: netBlock("Tickets (read-only)", map[string]any{"runtime": map[string]any{
			"allow": []any{entry("read", []any{"jira.acme.internal"}, "GET")}}})},
		{Key: "deny-meta", Block: netBlock("Deny cloud metadata", map[string]any{"runtime": map[string]any{
			"deny": []any{entry("gcp", []any{"metadata.google.internal"})}}})},
		{Key: "tok", Block: blk("credential", map[string]any{"service": "stripe", "phase": []any{"runtime"},
			"apiKey": map[string]any{"name": "STRIPE_KEY", "inject": []any{map[string]any{"domain": "api.stripe.com"}}}})},
		{Key: "c1", Block: blk("volume", map[string]any{"path": "/home/agent/.cache"})},
		{Key: "c2", Block: blk("volume", map[string]any{"path": "/home/agent/.cache/"})},
	}
	out := Compile(Input{Blueprint: Blueprint{Slug: "x", Kind: "mixin"}, Slots: slots})
	kinds := map[string]Conflict{}
	for _, c := range out.Conflicts {
		kinds[c.Kind] = c
	}
	if c := kinds["blocked"]; len(c.Fixes) != 2 || c.Fixes[0].Slot != "deny-meta" || c.Fixes[0].Key != "gcp" || c.Fixes[1].Key != "meta" {
		t.Errorf("blocked: %+v", c)
	}
	if c := kinds["widened"]; len(c.Fixes) != 2 || c.Fixes[0].Slot != "internal" || c.Fixes[0].Path != "/runtime/allow" {
		t.Errorf("widened: %+v", c)
	}
	if c := kinds["duplicate"]; len(c.Slots) != 2 {
		t.Errorf("duplicate: %+v", c)
	}
	if c := kinds["gap"]; len(c.Slots) != 1 || c.Slots[0] != "tok" {
		t.Errorf("gap: %+v", c)
	}
	if len(out.Conflicts) != 4 {
		t.Errorf("want 4 conflicts, got %+v", out.Conflicts)
	}
}
