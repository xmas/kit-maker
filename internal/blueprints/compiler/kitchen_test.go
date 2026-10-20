package compiler

import (
	"strings"
	"testing"
)

func blk(typ string, p map[string]any) *Block {
	return &Block{Name: typ, Slug: typ, Type: typ, Version: 1, Payload: p}
}

// A blueprint using most block types compiles to one valid workload: setup
// blocks merge into one lifecycle@1, top-level fields land at the top, and
// every slot gets its YAML span.
func TestCompileEverySection(t *testing.T) {
	slots := []Slot{
		{Key: "about", Block: blk("about", map[string]any{"author": "Jane Doe <jane@example.com>", "sourceUrl": "https://github.com/example/sink", "licenses": []any{"Apache-2.0"}})},
		{Key: "image", Block: blk("dockerfile", map[string]any{"build": "# syntax=docker/dockerfile:1\nFROM docker/sandbox-templates:shell\nRUN echo hi\n"})},
		{Key: "deps", Block: blk("dependencies", map[string]any{"provides": []any{"sink@1.2.3"}, "requires": []any{"node >= 20.0.0, < 23.0.0"}})},
		{Key: "inputs", Block: blk("args", map[string]any{"args": map[string]any{"port": map[string]any{"default": "8080", "pattern": "^[0-9]+$"}}})},
		{Key: "net", Block: blk("network-rules", map[string]any{"runtime": map[string]any{"allow": []any{map[string]any{"hosts": []any{"api.github.com"}}}}})},
		{Key: "gh", Block: blk("credential", map[string]any{"service": "github", "phase": []any{"runtime"},
			"apiKey": map[string]any{"name": "GH_TOKEN", "proxyManaged": true, "inject": []any{map[string]any{"domain": "api.github.com", "header": "Authorization", "format": "Bearer %s"}}}})},
		{Key: "deps-install", Block: blk("install-steps", map[string]any{"install": []any{map[string]any{"command": []any{"sh", "-c", "npm i -g x"}, "user": "root"}}})},
		{Key: "boot", Block: blk("startup-steps", map[string]any{"startup": []any{map[string]any{"command": "echo booted", "background": true}}})},
		{Key: "settings", Block: blk("files", map[string]any{"files": []any{map[string]any{"path": "/home/agent/.config/s.json", "content": "{}", "mode": "0644"}}})},
		{Key: "more-install", Block: blk("install-steps", map[string]any{"install": []any{map[string]any{"command": "echo second"}}})},
		{Key: "tty", Block: blk("interactive", map[string]any{"interactive": []any{"--interactive"}})},
		{Key: "launch", Block: blk("agent-launch", map[string]any{})},
		{Key: "ctx", Block: blk("agent-context", map[string]any{"directory": "/home/agent", "filename": "AGENTS.md", "content": "Line one.\nLine two."})},
		{Key: "res", Block: blk("resources", map[string]any{"cpu": 2, "memory": "4GiB"})},
		{Key: "cache", Block: blk("volume", map[string]any{"path": "/home/agent/.cache", "size": "10GiB", "mode": "0755"})},
		{Key: "web", Block: blk("port", map[string]any{"name": "web", "container": 8080, "transport": "tcp"})},
		{Key: "who", Block: blk("git-identity", map[string]any{"optional": true})},
	}
	out := Compile(Input{Blueprint: Blueprint{Slug: "sink", DisplayName: "Kitchen Sink", Kind: "workload"}, Slots: slots})
	if !out.Valid {
		t.Fatalf("invalid: %s %v\n%s", out.Error, out.Problems, out.YAML)
	}
	if n := strings.Count(out.YAML, "com.docker.sandbox/lifecycle@1"); n != 1 {
		t.Errorf("want one lifecycle@1, got %d:\n%s", n, out.YAML)
	}
	for _, want := range []string{"author: Jane Doe <jane@example.com>", "build: |", `mode: "0644"`, "content: |-", "name: git-identity", "optional: true"} {
		if !strings.Contains(out.YAML, want) {
			t.Errorf("missing %q:\n%s", want, out.YAML)
		}
	}
	for _, s := range slots {
		if len(out.Spans[s.Key]) == 0 {
			t.Errorf("slot %s has no span:\n%s", s.Key, out.YAML)
		}
	}
	// The second install block sits under the first in the same list.
	lines := strings.Split(out.YAML, "\n")
	if r := out.Spans["more-install"]; len(r) == 0 || !strings.Contains(strings.Join(lines[r[0][0]-1:r[0][1]], "\n"), "echo second") {
		t.Errorf("more-install span %v doesn't cover its step:\n%s", r, out.YAML)
	}
	if r := out.Spans["about"]; len(r) != 3 {
		t.Errorf("about should own its three fields, got %v", r)
	}
}

func TestCompileOneOwnerConflicts(t *testing.T) {
	slots := []Slot{
		{Key: "image", Block: blk("dockerfile", map[string]any{"build": "FROM scratch\n"})},
		{Key: "set", Block: blk("kits", map[string]any{"kits": []any{map[string]any{"ref": "docker.io/dockerdev/sbx-kit-gh:2.72.0"}}})},
		{Key: "res1", Block: blk("resources", map[string]any{"cpu": 2})},
		{Key: "res2", Block: blk("resources", map[string]any{"cpu": 4})},
		{Key: "a1", Block: blk("args", map[string]any{"args": map[string]any{"team": map[string]any{"default": "a"}}})},
		{Key: "a2", Block: blk("args", map[string]any{"args": map[string]any{"team": map[string]any{"default": "b"}}})},
		{Key: "tty1", Block: blk("interactive", map[string]any{"interactive": []any{"-i"}})},
		{Key: "tty2", Block: blk("interactive", map[string]any{"interactive": []any{"-t"}})},
	}
	out := Compile(Input{Blueprint: Blueprint{Slug: "x", Kind: "mixin"}, Slots: slots})
	got := map[string]string{}
	for _, p := range out.Problems {
		got[p.Slot] = p.Message
	}
	for _, slot := range []string{"set", "res2", "a2", "tty2"} {
		if got[slot] == "" {
			t.Errorf("want a problem on %s, got %v", slot, out.Problems)
		}
	}
}
