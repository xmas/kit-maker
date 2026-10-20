package compiler

import (
	"strings"
	"testing"

	"github.com/docker/kit-maker/internal/blueprints/blocktypes"
)

// Every block type's starter compiles on its own into a descriptor the spec
// package accepts, so adding any block never starts from an invalid kit.
func TestEveryStarterCompiles(t *testing.T) {
	for _, bt := range blocktypes.All() {
		t.Run(bt.Key, func(t *testing.T) {
			slots := []Slot{{Key: "s", Label: bt.Label, Section: bt.Section, Block: &Block{
				Name: bt.Label, Slug: "example", Type: bt.Key, Version: 1, Payload: blocktypes.Starter(bt.Key, "example"),
			}}}
			kind := "mixin"
			if bt.Key == "agent-launch" { // sbx@1 is workload-only
				kind = "workload"
			}
			out := Compile(Input{Blueprint: Blueprint{Slug: "t", Kind: kind}, Slots: slots})
			if !out.Valid {
				t.Fatalf("starter is invalid: %s %v\n%s", out.Error, out.Problems, out.YAML)
			}
			// About and Network rules start as empty templates: nothing to emit yet.
			if bt.Key == "about" || bt.Key == blocktypes.NetworkRules {
				return
			}
			if bt.Capability != "" && !strings.Contains(out.YAML, bt.Capability) {
				t.Fatalf("missing %s:\n%s", bt.Capability, out.YAML)
			}
			if len(out.Spans["s"]) == 0 {
				t.Fatalf("no span for the slot:\n%s", out.YAML)
			}
		})
	}
}
