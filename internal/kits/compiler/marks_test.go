package compiler

import (
	"strings"
	"testing"
)

// Edits made in a kit show in the YAML: added and changed entries carry a
// comment and are marked line by line; removed library entries are noted.
func TestCompileMarksKitEdits(t *testing.T) {
	base := payload(t, `{"runtime":{
		"allow":[{"_k":"v1","label":"v1 API","hosts":["api.acme.internal"],"methods":["GET"]},
		         {"_k":"docs","label":"Docs","hosts":["docs.acme.internal"],"methods":["GET"]}],
		"deny":[{"_k":"del","label":"No deletes","hosts":["api.acme.internal"],"methods":["DELETE"]}]}}`)
	eff := payload(t, `{"runtime":{
		"allow":[{"_k":"v1","label":"v1 API","hosts":["api.acme.internal"],"methods":["GET","POST"]}],
		"deny":[{"_k":"del","label":"No deletes","hosts":["api.acme.internal"],"methods":["DELETE"]},
		        {"_k":"prod","label":"No prod writes","hosts":["api.acme.internal"],"methods":["POST"],"paths":["/v1/prod/**"]}]}}`)
	out := Compile(Input{Kit: Kit{Slug: "k", Kind: "mixin"}, Slots: []Slot{
		{Key: "acme", Block: &Block{Slug: "acme-internal", Type: "network-rules", Version: 1, Edits: 3, Payload: eff, Base: base}},
		{Key: "meta", Block: &Block{Slug: "deny-cloud-metadata", Type: "network-rules", Version: 1,
			Payload: payload(t, `{"runtime":{"deny":[{"_k":"m","hosts":["169.254.169.254"]}]}}`)}},
	}})
	if !out.Valid {
		t.Fatalf("invalid: %s\n%s", out.Error, out.YAML)
	}
	lines := strings.Split(out.YAML, "\n")
	kinds := map[string][]string{}
	for n, k := range out.Marks {
		kinds[k] = append(kinds[k], strings.TrimSpace(lines[n-1]))
	}
	joined := func(k string) string { return strings.Join(kinds[k], " | ") }
	if !strings.Contains(joined("changed"), "methods: [GET, POST]") {
		t.Errorf("changed entry not marked: %q\n%s", joined("changed"), out.YAML)
	}
	if !strings.Contains(joined("added"), "/v1/prod/**") || strings.Contains(joined("added"), "DELETE") {
		t.Errorf("added entry marks: %q\n%s", joined("added"), out.YAML)
	}
	if !strings.Contains(joined("removed"), "Docs (docs.acme.internal GET)") {
		t.Errorf("removed entry not noted: %q\n%s", joined("removed"), out.YAML)
	}
	if strings.Contains(joined("added")+joined("changed"), "169.254") {
		t.Errorf("an unedited block was marked:\n%s", out.YAML)
	}
	// The slot's span still covers its entries and the annotations.
	sp := out.Spans["acme"]
	if len(sp) != 2 {
		t.Fatalf("acme spans %v\n%s", sp, out.YAML)
	}
	deny := strings.Join(lines[sp[1][0]-1:sp[1][1]], "\n")
	if !strings.Contains(deny, "No prod") && !strings.Contains(deny, "/v1/prod/**") || strings.Contains(deny, "169.254") {
		t.Errorf("acme deny span: %q", deny)
	}
}
