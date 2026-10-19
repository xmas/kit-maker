package compiler

import (
	"encoding/json"
	"strings"
	"testing"
)

func payload(t *testing.T, s string) map[string]any {
	t.Helper()
	var m map[string]any
	if err := json.Unmarshal([]byte(s), &m); err != nil {
		t.Fatal(err)
	}
	return m
}

func TestCompileMergesAndValidates(t *testing.T) {
	in := Input{
		Kit: Kit{Slug: "acme-shop", DisplayName: "Acme Shop", Kind: "mixin"},
		Slots: []Slot{
			{Key: "github-egress", Section: "network", Block: &Block{Name: "GitHub egress", Slug: "github-egress", Type: "network-rules", Version: 3, Edits: 2,
				Payload: payload(t, `{"runtime":{"allow":[{"_k":"a","hosts":["github.com"]},{"_k":"b","hosts":["api.github.com"],"methods":["GET","HEAD"],"label":"API reads"}],
				"deny":[{"_k":"c","hosts":["api.github.com"],"methods":["DELETE"],"paths":["/repos/**"]}]}}`)}},
			{Key: "deny-metadata", Section: "network", Block: &Block{Name: "Deny cloud metadata", Slug: "deny-cloud-metadata", Type: "network-rules", Version: 1,
				Payload: payload(t, `{"runtime":{"deny":[{"_k":"m","hosts":["169.254.169.254"]},{"_k":"g","hosts":["metadata.google.internal"]}]}}`)}},
			{Key: "github-token", Section: "credentials", Block: &Block{Name: "GitHub token", Slug: "github-token", Type: "credential", Version: 1,
				Payload: payload(t, `{"service":"github","phase":["runtime"],"optional":true,"description":"GitHub API access",
				"apiKey":{"name":"GH_TOKEN","proxyManaged":true,"inject":[{"_k":"i","domain":"api.github.com","header":"Authorization","format":"Bearer %s"}]}}`)}},
			{Key: "packagist", Section: "network", Required: true},
		},
	}
	out := Compile(in)
	if !out.Valid {
		t.Fatalf("invalid: %s\n%s", out.Error, out.YAML)
	}
	for _, want := range []string{
		"# syntax=docker/sandbox-kit:3",
		"kind: mixin",
		"# slot github-egress · github-egress v3 · 2 edits here",
		"# slot deny-metadata · deny-cloud-metadata v1",
		`- "169.254.169.254"`,
		"methods: [GET, HEAD]",
		"type: com.docker.sandbox/credential@1",
		"service: github",
	} {
		if !strings.Contains(out.YAML, want) {
			t.Errorf("yaml missing %q\n%s", want, out.YAML)
		}
	}
	if strings.Contains(out.YAML, "_k") || strings.Contains(out.YAML, "label") {
		t.Errorf("internal fields leaked into yaml:\n%s", out.YAML)
	}
	if strings.Count(out.YAML, "network-policy@2") != 1 {
		t.Errorf("network blocks must merge into one network-policy@2:\n%s", out.YAML)
	}
	if len(out.Blanks) != 1 || out.Blanks[0] != "packagist" {
		t.Errorf("blanks = %v", out.Blanks)
	}
	if out.Surface == nil || len(out.Surface.CredentialsRuntime) != 1 || !strings.HasPrefix(out.Digest, "sha256:") {
		t.Errorf("surface/digest: %+v %s", out.Surface, out.Digest)
	}
	if len(out.Policy.Runtime.Allow) != 2 || len(out.Policy.Runtime.Deny) != 3 {
		t.Errorf("policy: %+v", out.Policy)
	}

	// Spans: github-egress has an allow group and a deny group; each span
	// starts at its comment and covers exactly that slot's entries.
	lines := strings.Split(out.YAML, "\n")
	text := func(sp [2]int) string { return strings.Join(lines[sp[0]-1:sp[1]], "\n") }
	eg := out.Spans["github-egress"]
	if len(eg) != 2 || !strings.Contains(text(eg[0]), "API reads") && !strings.Contains(text(eg[0]), "GET, HEAD") {
		t.Fatalf("github-egress spans %v\n%s", eg, out.YAML)
	}
	if strings.Contains(text(eg[1]), "169.254") || !strings.Contains(text(eg[1]), "DELETE") {
		t.Errorf("github-egress deny span leaks into the next slot: %q", text(eg[1]))
	}
	dm := out.Spans["deny-metadata"]
	if len(dm) != 1 || !strings.Contains(text(dm[0]), "169.254.169.254") || !strings.Contains(text(dm[0]), "metadata.google.internal") ||
		strings.Contains(text(dm[0]), "credential@1") {
		t.Errorf("deny-metadata span: %q", text(dm[0]))
	}
	tok := out.Spans["github-token"]
	if len(tok) != 1 || !strings.Contains(text(tok[0]), "credential@1") || !strings.Contains(text(tok[0]), "Bearer %s") {
		t.Errorf("github-token span: %q\n%s", text(tok[0]), out.YAML)
	}
}

func TestCompileReportsBlockProblems(t *testing.T) {
	out := Compile(Input{Kit: Kit{Slug: "k", Kind: "mixin"}, Slots: []Slot{
		{Key: "bad", Block: &Block{Slug: "bad", Type: "network-rules", Version: 1,
			Payload: payload(t, `{"runtime":{"allow":[{"_k":"x","hosts":["*.corp.internal"],"methods":["GET"]}]}}`)}},
	}})
	if out.Valid || len(out.Problems) != 1 || !strings.Contains(out.Problems[0].Message, "literal hosts") {
		t.Fatalf("want a literal-host problem, got %+v", out)
	}
}

func TestCompileSpecRejection(t *testing.T) {
	// A credential injecting into a domain the network policy doesn't allow
	// is caught by the spec package, not by us.
	out := Compile(Input{Kit: Kit{Slug: "k", Kind: "mixin"}, Slots: []Slot{
		{Key: "tok", Block: &Block{Slug: "tok", Type: "credential", Version: 1,
			Payload: payload(t, `{"service":"github","phase":["runtime"],"apiKey":{"name":"GH_TOKEN","proxyManaged":true,"inject":[{"_k":"i","domain":"api.github.com","header":"Authorization"}]}}`)}},
	}})
	if out.Valid || out.Error == "" {
		t.Fatalf("want spec validation error, got valid:\n%s", out.YAML)
	}
}
