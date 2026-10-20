package seed

import (
	"encoding/json"
	"testing"

	"github.com/docker/kit-maker/internal/blueprints/compiler"
	"github.com/docker/kit-maker/internal/blueprints/patch"
)

// Every spec example converts into blocks that compile back into a valid kit.
func TestExamplesCompile(t *testing.T) {
	exs, err := Examples()
	if err != nil {
		t.Fatal(err)
	}
	if len(exs) < 30 {
		t.Fatalf("only %d examples", len(exs))
	}
	for _, ex := range exs {
		t.Run(ex.Name, func(t *testing.T) {
			if len(ex.Slots) == 0 {
				t.Fatal("no blocks")
			}
			in := compiler.Input{Blueprint: compiler.Blueprint{Slug: ex.Name, DisplayName: ex.DisplayName, Description: ex.Description, Kind: ex.Kind}}
			for i, s := range ex.Slots {
				p := s.Payload
				patch.AssignKeys(p)
				in.Slots = append(in.Slots, compiler.Slot{Key: s.Type + string(rune('a'+i)), Label: s.Label, Required: true,
					Block: &compiler.Block{Name: s.Label, Slug: s.Type, Type: s.Type, Version: 1, Payload: p}})
			}
			out := compiler.Compile(in)
			if !out.Valid {
				t.Fatalf("%s %v\n%s", out.Error, out.Problems, out.YAML)
			}
		})
	}
}

// Every sample profile compiles to a valid kit from its shared blocks.
func TestProfilesCompile(t *testing.T) {
	payloads := map[string]profileBlock{}
	for _, b := range profileBlocks {
		payloads[b.slug] = b
	}
	for _, d := range demoBlocks {
		var p map[string]any
		if err := json.Unmarshal([]byte(d.payload), &p); err != nil {
			t.Fatal(err)
		}
		payloads[d.slug] = profileBlock{slug: d.slug, name: d.name, typ: d.typ, payload: p}
	}
	for _, pr := range profiles {
		t.Run(pr.slug, func(t *testing.T) {
			in := compiler.Input{Blueprint: compiler.Blueprint{Slug: pr.slug, DisplayName: pr.name, Kind: pr.kind}}
			for _, slug := range pr.blocks {
				b, ok := payloads[slug]
				if !ok {
					t.Fatalf("unknown block %s", slug)
				}
				stableKeys(b.payload)
				in.Slots = append(in.Slots, compiler.Slot{Key: slug, Label: b.name, Required: true,
					Block: &compiler.Block{Name: b.name, Slug: slug, Type: b.typ, Version: 1, Payload: b.payload}})
			}
			out := compiler.Compile(in)
			if !out.Valid {
				t.Fatalf("%s %v\n%s", out.Error, out.Problems, out.YAML)
			}
		})
	}
}
