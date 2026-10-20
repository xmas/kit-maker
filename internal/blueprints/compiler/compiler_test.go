package compiler

import (
	"os"
	"testing"

	"github.com/docker/sandbox-kit-spec/v3/spec"
)

// testdata/alpine.yaml is examples/alpine/alpine.yaml from
// docker/sandbox-kit-spec at the version pinned in go.mod.
func TestValidateSpecExample(t *testing.T) {
	raw, err := os.ReadFile("testdata/alpine.yaml")
	if err != nil {
		t.Fatal(err)
	}
	d, warnings, err := Validate(raw)
	if err != nil {
		t.Fatalf("example kit should validate: %v", err)
	}
	for _, w := range warnings {
		t.Log("warning:", w)
	}
	if d.Kind != spec.KindWorkload {
		t.Errorf("kind = %q, want %q", d.Kind, spec.KindWorkload)
	}
	if d.DisplayName != "Alpine" {
		t.Errorf("displayName = %q, want Alpine", d.DisplayName)
	}
	if len(d.Capabilities) == 0 {
		t.Error("expected capabilities")
	}
}

func TestValidateRejectsUnknownField(t *testing.T) {
	raw := []byte("schemaVersion: \"3\"\nkind: workload\nblueprintId: bp_123\n")
	if _, _, err := Validate(raw); err == nil {
		t.Fatal("unknown top-level key should fail: the kit top level is additionalProperties: false")
	}
}
