package compiler

import (
	"strings"
	"testing"
)

// A Dockerfile block's build files come out in Output.Files (not the YAML),
// named in a comment above build:, and bad paths are problems.
func TestBuildFiles(t *testing.T) {
	in := Input{Blueprint: Blueprint{Slug: "demo", DisplayName: "Demo", Kind: "workload"}, Slots: []Slot{{
		Key: "dockerfile", Label: "Dockerfile", Section: "agent-base",
		Block: &Block{Name: "Dockerfile", Slug: "demo-dockerfile", Type: "dockerfile", Version: 1, Payload: map[string]any{
			"build": "FROM docker/sandbox-templates:shell\nCOPY scripts/run.sh /usr/local/bin/run\n",
			"files": map[string]any{"scripts/run.sh": "#!/bin/sh\necho hi\n"},
		}},
	}}}
	out := Compile(in)
	if out.Files["scripts/run.sh"] != "#!/bin/sh\necho hi\n" {
		t.Fatalf("files: %v", out.Files)
	}
	if strings.Contains(out.YAML, "echo hi") || !strings.Contains(out.YAML, "# Build files beside this file: scripts/run.sh") {
		t.Fatalf("yaml:\n%s", out.YAML)
	}
	if !out.Valid {
		t.Fatalf("not valid: %s", out.Error)
	}

	for _, bad := range []string{"/etc/passwd", "../x", "a/../../x", "./x", ""} {
		in.Slots[0].Block.Payload["files"] = map[string]any{bad: "x"}
		if out := Compile(in); len(out.Problems) == 0 {
			t.Errorf("%q: no problem", bad)
		}
	}
}
