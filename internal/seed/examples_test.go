package seed

import (
	"encoding/json"
	"strings"
	"testing"
	"testing/fstest"

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

// A workload on the public Ubuntu base block compiles to a valid kit.
func TestUbuntuBaseWorkload(t *testing.T) {
	var ub profileBlock
	for _, b := range profileBlocks {
		if b.slug == "ubuntu-base" {
			ub = b
		}
	}
	out := compiler.Compile(compiler.Input{Blueprint: compiler.Blueprint{Slug: "ubuntu", Kind: "workload"}, Slots: []compiler.Slot{
		{Key: "image", Label: ub.name, Required: true, Block: &compiler.Block{Name: ub.name, Slug: ub.slug, Type: ub.typ, Version: 1, Payload: ub.payload}},
	}})
	if !out.Valid || !strings.Contains(out.YAML, "FROM ubuntu:24.04") {
		t.Fatalf("%s\n%s", out.Error, out.YAML)
	}
}

// A blueprint has no build folder, so a Dockerfile block carries the files
// its COPY lines read: every one of them must be among its build files.
func TestExampleDockerfilesCarryTheirFiles(t *testing.T) {
	exs, err := Examples()
	if err != nil {
		t.Fatal(err)
	}
	carrying := 0
	for _, ex := range exs {
		for _, sl := range ex.Slots {
			build, _ := sl.Payload["build"].(string)
			files, _ := sl.Payload["files"].(map[string]any)
			if len(files) > 0 {
				carrying++
			}
			for _, line := range strings.Split(build, "\n") {
				m := copyLine.FindStringSubmatch(line)
				if m == nil || strings.Contains(m[1], "<<") || strings.Contains(m[1], "--from") {
					continue
				}
				fields := strings.Fields(m[1])
				src := fields[len(fields)-2]
				found := false
				for name := range files {
					found = found || name == src || strings.HasPrefix(name, strings.TrimSuffix(src, "/")+"/")
				}
				if !found {
					t.Errorf("%s copies %s but its Dockerfile block doesn't carry it", ex.Name, src)
				}
			}
		}
	}
	if carrying != 9 { // claude, claude-acp, claude-mixin, codex, codex-acp, codex-mixin, cursor, gemini, review-skill
		t.Fatalf("%d Dockerfile blocks carry build files, want 9", carrying)
	}
}

func TestBuildFiles(t *testing.T) {
	fsys := fstest.MapFS{
		"ex/scripts/run.sh":    {Data: []byte("#!/bin/sh\necho hi\n")},
		"ex/content/a.md":      {Data: []byte("# A\n")},
		"ex/content/refs/b.md": {Data: []byte("b\n")},
	}
	got, err := buildFiles(fsys, "ex", "FROM x\nCOPY scripts/run.sh /usr/local/bin/run\nCOPY --chown=1000 content /srv/c\nCOPY --from=build /a /b\nCOPY <<'EOF' /x\nhi\nEOF\n")
	if err != nil {
		t.Fatal(err)
	}
	want := map[string]string{"scripts/run.sh": "#!/bin/sh\necho hi\n", "content/a.md": "# A\n", "content/refs/b.md": "b\n"}
	if len(got) != len(want) {
		t.Fatalf("got %v", got)
	}
	for k, v := range want {
		if got[k] != v {
			t.Errorf("%s = %q, want %q", k, got[k], v)
		}
	}
}
