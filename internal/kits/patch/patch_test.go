package patch_test

import (
	"encoding/json"
	"reflect"
	"testing"

	"github.com/docker/kit-maker/internal/kits/patch"
)

func obj(t *testing.T, s string) map[string]any {
	t.Helper()
	var m map[string]any
	if err := json.Unmarshal([]byte(s), &m); err != nil {
		t.Fatal(err)
	}
	return m
}

var base = `{"runtime":{"allow":[
  {"_k":"gh","hosts":["github.com"]},
  {"_k":"api","hosts":["api.github.com"],"methods":["GET","HEAD","POST","PATCH","PUT","DELETE"]}
],"deny":[{"_k":"del","hosts":["api.github.com"],"methods":["DELETE"],"paths":["/repos/**"]}]}}`

func TestDiffApplyRoundTrip(t *testing.T) {
	b := obj(t, base)
	edited := obj(t, `{"runtime":{"allow":[
  {"_k":"gh","hosts":["github.com"]},
  {"_k":"api","hosts":["api.github.com"],"methods":["GET","HEAD"]},
  {"_k":"w","hosts":["api.github.com"],"methods":["POST","PATCH","PUT"],"paths":["/repos/acme/**"]}
],"deny":[{"_k":"del","hosts":["api.github.com"],"methods":["DELETE"],"paths":["/repos/**"]}]}}`)

	ops := patch.Diff(b, edited)
	if len(ops) != 2 || ops[0].Op != "replace" || ops[0].Key != "api" || ops[1].Op != "add" || ops[1].After != "api" {
		t.Fatalf("unexpected ops: %+v", ops)
	}
	got, origins, conflicts := patch.Apply(b, ops)
	if len(conflicts) != 0 {
		t.Fatalf("conflicts: %+v", conflicts)
	}
	if !reflect.DeepEqual(got, edited) {
		t.Fatalf("apply(diff) != edited\n got %v\nwant %v", got, edited)
	}
	if origins[patch.ItemPath("/runtime/allow", "api")] != patch.Changed ||
		origins[patch.ItemPath("/runtime/allow", "w")] != patch.Added {
		t.Fatalf("origins: %v", origins)
	}
	if len(patch.Diff(b, b)) != 0 {
		t.Fatal("no edits should mean no ops")
	}
}

func TestOverridesSurviveUpstreamChanges(t *testing.T) {
	ops := []patch.Op{
		{Op: "replace", Path: "/runtime/allow", Key: "api", Value: map[string]any{"hosts": []any{"api.github.com"}, "methods": []any{"GET"}}},
		{Op: "remove", Path: "/runtime/deny", Key: "del"},
	}
	// The library reorders rules and adds one: edits still land by key.
	v2 := obj(t, `{"runtime":{"allow":[
  {"_k":"new","hosts":["codeload.github.com"],"methods":["GET"]},
  {"_k":"api","hosts":["api.github.com"],"methods":["GET","POST"]},
  {"_k":"gh","hosts":["github.com"]}
],"deny":[{"_k":"del","hosts":["api.github.com"],"methods":["DELETE"]}]}}`)
	got, _, conflicts := patch.Apply(v2, ops)
	if len(conflicts) != 0 {
		t.Fatalf("conflicts: %+v", conflicts)
	}
	allow := got["runtime"].(map[string]any)["allow"].([]any)
	if len(allow) != 3 || allow[1].(map[string]any)["methods"].([]any)[0] != "GET" || len(allow[1].(map[string]any)["methods"].([]any)) != 1 {
		t.Fatalf("edit lost: %v", allow)
	}
	if deny, _ := got["runtime"].(map[string]any)["deny"].([]any); len(deny) != 0 {
		t.Fatalf("remove lost: %v", deny)
	}

	// The library deleted the rule we edited: the op becomes a conflict.
	v3 := obj(t, `{"runtime":{"allow":[{"_k":"gh","hosts":["github.com"]}]}}`)
	_, _, conflicts = patch.Apply(v3, ops)
	if len(conflicts) != 2 {
		t.Fatalf("want 2 conflicts, got %+v", conflicts)
	}
}

func TestScalarsAndKeys(t *testing.T) {
	b := obj(t, `{"service":"github","phase":["runtime"],"apiKey":{"name":"GH_TOKEN","proxyManaged":true}}`)
	e := obj(t, `{"service":"github","phase":["install","runtime"],"apiKey":{"name":"GITHUB_TOKEN","proxyManaged":true},"optional":true}`)
	ops := patch.Diff(b, e)
	got, origins, _ := patch.Apply(b, ops)
	if !reflect.DeepEqual(got, e) {
		t.Fatalf("got %v", got)
	}
	if origins["/apiKey/name"] != patch.Changed || origins["/phase"] != patch.Changed || origins["/optional"] != patch.Changed {
		t.Fatalf("origins %v", origins)
	}

	items := []any{map[string]any{"hosts": []any{"a"}}, map[string]any{"_k": "keep", "hosts": []any{"b"}}}
	patch.AssignKeys(map[string]any{"allow": items})
	if items[0].(map[string]any)["_k"] == "" || items[1].(map[string]any)["_k"] != "keep" {
		t.Fatalf("AssignKeys: %v", items)
	}
	stripped := patch.Strip(map[string]any{"allow": items}).(map[string]any)
	if _, ok := stripped["allow"].([]any)[0].(map[string]any)["_k"]; ok {
		t.Fatal("Strip left a key")
	}
}
