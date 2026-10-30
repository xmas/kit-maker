package lite

import (
	"archive/tar"
	"compress/gzip"
	"fmt"
	"net/http"
	"sort"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
)

// Running a blueprint in this edition: there is no BuildKit or registry, so
// nothing is published; the Run dialog offers the local copy (a folder sbx
// builds itself) and an sbxenv.yaml that points at it.

// PublishSetup is shown in place of the Publish button.
const PublishSetup = "Publishing to a registry needs the full Kit Maker deployment (BuildKit and a registry). Here, download a local copy and run it with sbx."

// RunInfo is what the Run dialog shows (internal/http.RunInfo without a
// published version).
type RunInfo struct {
	Slug         string       `json:"slug"`
	Kind         string       `json:"kind"`
	Valid        bool         `json:"valid"`
	Error        string       `json:"error,omitempty"`
	CanPublish   bool         `json:"canPublish"`
	PublishSetup string       `json:"publishSetup,omitempty"`
	Workload     *WorkloadRef `json:"workload,omitempty"`
	Extract      string       `json:"extract"`
	LocalCommand string       `json:"localCommand"`
}

// WorkloadRef names a workload blueprint a mixin can run on.
type WorkloadRef struct {
	Name  string `json:"name"`
	Slug  string `json:"slug"`
	Image string `json:"image"`
}

func (e *Engine) runInfo(l *Loaded) RunInfo {
	slug := l.Blueprint.Slug
	ri := RunInfo{Slug: slug, Kind: l.Blueprint.Kind, Valid: l.Compiled.Valid, Error: l.Compiled.Error,
		PublishSetup: PublishSetup, Extract: fmt.Sprintf("tar -xzf %s.tar.gz", slug)}
	ri.LocalCommand = "sbx run ./" + slug + "/"
	if l.Blueprint.Kind == "mixin" {
		ri.LocalCommand = "sbx run <workload-kit> --kit ./" + slug + "/"
	}
	return ri
}

func (e *Engine) runInfoJSON(w http.ResponseWriter, r *http.Request) {
	l, err := e.Store.Load(chi.URLParam(r, "id"))
	if err != nil {
		http.Error(w, "not found", http.StatusNotFound)
		return
	}
	writeJSON(w, http.StatusOK, e.runInfo(l))
}

func (e *Engine) publish(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusServiceUnavailable, map[string]string{"error": PublishSetup})
}

// download sends <slug>.tar.gz holding just <slug>/<slug>.yaml.
func (e *Engine) download(w http.ResponseWriter, r *http.Request) {
	l, err := e.Store.Load(chi.URLParam(r, "id"))
	if err != nil {
		http.Error(w, "not found", http.StatusNotFound)
		return
	}
	if !l.Compiled.Valid {
		http.Error(w, "This blueprint isn't a valid kit yet: "+l.Compiled.Error, http.StatusConflict)
		return
	}
	slug := l.Blueprint.Slug
	w.Header().Set("Content-Type", "application/gzip")
	w.Header().Set("Content-Disposition", fmt.Sprintf(`attachment; filename="%s.tar.gz"`, slug))
	gz := gzip.NewWriter(w)
	tw := tar.NewWriter(gz)
	t := time.Now()
	_ = tw.WriteHeader(&tar.Header{Name: slug + "/", Typeflag: tar.TypeDir, Mode: 0o755, ModTime: t})
	body := l.Compiled.YAML
	_ = tw.WriteHeader(&tar.Header{Name: slug + "/" + slug + ".yaml", Mode: 0o644, Size: int64(len(body)), ModTime: t})
	_, _ = tw.Write([]byte(body))
	_ = tw.Close()
	_ = gz.Close()
}

// sbxenv sends an sbxenv.yaml that uses the local copy next to it.
func (e *Engine) sbxenv(w http.ResponseWriter, r *http.Request) {
	l, err := e.Store.Load(chi.URLParam(r, "id"))
	if err != nil {
		http.Error(w, "not found", http.StatusNotFound)
		return
	}
	slug := l.Blueprint.Slug
	var b strings.Builder
	fmt.Fprintf(&b, "# sbxenv.yaml for %s (Kit Maker blueprint %s).\n", l.Blueprint.DisplayName, l.Blueprint.ID)
	b.WriteString("#   sbx env plan   # preview, changes nothing\n#   sbx env run    # create the sandbox, or attach to it\n")
	fmt.Fprintf(&b, "# Uses a local copy: put the %s/ folder from %s.tar.gz next to this file.\n", slug, slug)
	agent := slug
	if l.Blueprint.Kind == "mixin" {
		agent = "<workload>"
		b.WriteString("# This blueprint is a mixin: it needs a v3 workload kit under it (sbx's built-in agents aren't kits).\n# Add one under kits: and set agent: to its name.\n")
	}
	fmt.Fprintf(&b, "schemaVersion: \"1\"\nname: %s\nagent: %s\nkits:\n  - ./%s\nworkspace: .\n", slug, agent, slug)
	w.Header().Set("Content-Type", "application/yaml; charset=utf-8")
	w.Header().Set("Content-Disposition", `attachment; filename="sbxenv.yaml"`)
	_, _ = w.Write([]byte(b.String()))
}

func (s *Store) sortedBlueprints() []*Blueprint {
	out := make([]*Blueprint, 0, len(s.blueprints))
	for _, bp := range s.blueprints {
		out = append(out, bp)
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].DisplayName != out[j].DisplayName {
			return out[i].DisplayName < out[j].DisplayName
		}
		return out[i].ID < out[j].ID
	})
	return out
}
