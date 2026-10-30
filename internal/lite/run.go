package lite

import (
	"archive/tar"
	"compress/gzip"
	"errors"
	"fmt"
	"net/http"
	"sort"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
)

// Running a blueprint in this edition. Publishing is done by the backend
// (cloudflare/functions/api/publish.js starts a GitHub Actions workflow that
// builds the kit with BuildKit and pushes it to GHCR), so it is
// asynchronous: a version starts pending and the workflow reports back.
// Without a backend, or before publishing is set up, the Run dialog offers
// the local copy (a folder sbx builds itself) and an sbxenv.yaml for it.

// PublishSetup is shown when there's no backend to publish through.
const PublishSetup = "Publishing needs this site's backend (Pages Functions with publishing set up). Here, download a local copy and run it with sbx."

// Version is one published (or publishing, or failed) blueprint version.
type Version struct {
	Version     int    `json:"version"`
	Repository  string `json:"repository"`
	Digest      string `json:"digest"`
	Status      string `json:"status"` // pending | published | failed
	Error       string `json:"error,omitempty"`
	RunURL      string `json:"runUrl,omitempty"`
	PublishedAt string `json:"publishedAt"`
	StartedAt   string `json:"startedAt,omitempty"`
	PublishedBy string `json:"publishedBy,omitempty"`
	YAML        string `json:"yaml,omitempty"` // the newest published version only
}

// PublishState is what the backend knows about publishing a blueprint.
type PublishState struct {
	Configured    bool         `json:"configured"`
	Setup         string       `json:"setup"`
	AllowedSource string       `json:"allowedSource"`
	Versions      []Version    `json:"versions"` // newest first
	Workload      *WorkloadRef `json:"workload"`
}

// PublishRequest asks the backend to publish one version.
type PublishRequest struct {
	BlueprintID string `json:"blueprintId"`
	Slug        string `json:"slug"`
	Kind        string `json:"kind"`
	Name        string `json:"name"`
	Version     int    `json:"version"`
	YAML        string `json:"yaml"`
	// Files are the Dockerfile's build files, written beside the YAML.
	Files map[string]string `json:"files,omitempty"`
}

func (st *PublishState) latest(status string) *Version {
	for i := range st.Versions {
		if st.Versions[i].Status == status {
			return &st.Versions[i]
		}
	}
	return nil
}

// RunInfo is what the Run dialog shows (internal/http.RunInfo, plus the
// pending and failed publishes of this edition).
type RunInfo struct {
	Slug          string        `json:"slug"`
	Kind          string        `json:"kind"`
	Valid         bool          `json:"valid"`
	Error         string        `json:"error,omitempty"`
	Published     *PublishedRun `json:"published,omitempty"`
	Pending       *Version      `json:"pending,omitempty"`
	Failed        *Version      `json:"failed,omitempty"`
	CanPublish    bool          `json:"canPublish"`
	PublishSetup  string        `json:"publishSetup,omitempty"`
	AllowedSource string        `json:"allowedSource,omitempty"`
	Workload      *WorkloadRef  `json:"workload,omitempty"`
	Extract       string        `json:"extract"`
	LocalCommand  string        `json:"localCommand"`
	// Unchanged is set when Publish found nothing new since the last version.
	Unchanged bool `json:"unchanged,omitempty"`
}

// PublishedRun is how to run the newest published version.
type PublishedRun struct {
	Version
	Image   string `json:"image"` // repository:latest
	Pinned  string `json:"pinned"`
	Command string `json:"command"`
}

// WorkloadRef names a published workload blueprint a mixin can run on.
type WorkloadRef struct {
	Name  string `json:"name"`
	Slug  string `json:"slug"`
	Image string `json:"image"`
}

// runCommand runs ref; a mixin runs on top of a workload kit.
func runCommand(kind, ref string, w *WorkloadRef) string {
	if kind != "mixin" {
		return "sbx run " + ref
	}
	if w != nil {
		return "sbx run " + w.Image + " --kit " + ref
	}
	return "sbx run <workload-kit> --kit " + ref
}

// publishState asks the backend; without one, publishing isn't available.
func (e *Engine) publishState(id string) *PublishState {
	st, err := e.Host.Versions(id)
	if err != nil || st == nil {
		return &PublishState{Setup: PublishSetup}
	}
	return st
}

func (e *Engine) runInfo(l *Loaded, st *PublishState) RunInfo {
	slug := l.Blueprint.Slug
	ri := RunInfo{Slug: slug, Kind: l.Blueprint.Kind, Valid: l.Compiled.Valid, Error: l.Compiled.Error,
		CanPublish: true, PublishSetup: st.Setup, AllowedSource: st.AllowedSource,
		Extract: fmt.Sprintf("tar -xzf %s.tar.gz", slug)}
	if l.Blueprint.Kind == "mixin" {
		ri.Workload = st.Workload
	}
	ri.LocalCommand = runCommand(l.Blueprint.Kind, "./"+slug+"/", ri.Workload)
	if v := st.latest("published"); v != nil {
		image := v.Repository + ":latest"
		pub := *v
		pub.YAML = ""
		ri.Published = &PublishedRun{Version: pub, Image: image, Pinned: v.Repository + "@" + v.Digest, Command: runCommand(l.Blueprint.Kind, image, ri.Workload)}
	}
	if len(st.Versions) > 0 {
		switch newest := st.Versions[0]; newest.Status {
		case "pending":
			ri.Pending = &newest
		case "failed":
			ri.Failed = &newest
		}
	}
	return ri
}

func (e *Engine) runInfoJSON(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	l, err := e.Store.Load(id)
	if err != nil {
		http.Error(w, "not found", http.StatusNotFound)
		return
	}
	writeJSON(w, http.StatusOK, e.runInfo(l, e.publishState(id)))
}

// errPublish is a publish refusal with the status to answer.
type errPublish struct {
	status int
	msg    string
}

func (e errPublish) Error() string { return e.msg }

// Publish compiles the blueprint as its next version and hands it to the
// backend. When nothing changed since the newest published version, it
// publishes nothing and says so.
func (e *Engine) Publish(id string) (RunInfo, error) {
	l, err := e.Store.Load(id)
	if err != nil {
		return RunInfo{}, err
	}
	st := e.publishState(id)
	if !st.Configured {
		return RunInfo{}, errPublish{http.StatusServiceUnavailable, st.Setup}
	}
	if p := st.latest("pending"); p != nil && st.Versions[0].Status == "pending" {
		return RunInfo{}, errPublish{http.StatusConflict, fmt.Sprintf("v%d is still publishing.", p.Version)}
	}
	next := 1
	if pub := st.latest("published"); pub != nil {
		next = pub.Version + 1
		// Compiled under the last version's number, an unchanged blueprint
		// gives the same YAML byte for byte.
		same, err := e.Store.load(id, nil, pub.Version)
		if err != nil {
			return RunInfo{}, err
		}
		if same.Compiled.Valid && pub.YAML != "" && same.Compiled.YAML == pub.YAML {
			ri := e.runInfo(l, st)
			ri.Unchanged = true
			return ri, nil
		}
	}
	nl, err := e.Store.load(id, nil, next)
	if err != nil {
		return RunInfo{}, err
	}
	if !nl.Compiled.Valid {
		return RunInfo{}, errPublish{http.StatusConflict, "This blueprint isn't a valid kit yet: " + nl.Compiled.Error}
	}
	st, err = e.Host.Publish(PublishRequest{BlueprintID: id, Slug: nl.Blueprint.Slug, Kind: nl.Blueprint.Kind,
		Name: nl.Blueprint.DisplayName, Version: next, YAML: nl.Compiled.YAML, Files: nl.Compiled.Files})
	if err != nil {
		return RunInfo{}, errPublish{http.StatusBadGateway, err.Error()}
	}
	return e.runInfo(l, st), nil
}

func (e *Engine) publish(w http.ResponseWriter, r *http.Request) {
	ri, err := e.Publish(chi.URLParam(r, "id"))
	var pe errPublish
	switch {
	case errors.As(err, &pe):
		writeJSON(w, pe.status, map[string]string{"error": pe.msg})
	case errors.Is(err, ErrNotFound):
		http.Error(w, "not found", http.StatusNotFound)
	case err != nil:
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
	default:
		writeJSON(w, http.StatusOK, ri)
	}
}

// download sends <slug>.tar.gz: <slug>/<slug>.yaml and the Dockerfile's
// build files beside it (the folder sbx builds).
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
	writeBuildFiles(tw, slug, l.Compiled.Files, t)
	_ = tw.Close()
	_ = gz.Close()
}

// writeBuildFiles adds build files under dir/, scripts (#!) executable.
func writeBuildFiles(tw *tar.Writer, dir string, files map[string]string, t time.Time) {
	names := make([]string, 0, len(files))
	for name := range files {
		names = append(names, name)
	}
	sort.Strings(names)
	for _, name := range names {
		mode := int64(0o644)
		if strings.HasPrefix(files[name], "#!") {
			mode = 0o755
		}
		_ = tw.WriteHeader(&tar.Header{Name: dir + "/" + name, Mode: mode, Size: int64(len(files[name])), ModTime: t})
		_, _ = tw.Write([]byte(files[name]))
	}
}

// sbxenvYAML is an environment file for a project: it follows the
// registry's latest tag once published, else a local copy next to it.
func sbxenvYAML(l *Loaded, ri RunInfo) string {
	var b strings.Builder
	fmt.Fprintf(&b, "# sbxenv.yaml for %s (Kit Maker blueprint %s).\n", l.Blueprint.DisplayName, l.Blueprint.ID)
	b.WriteString("#   sbx env plan   # preview, changes nothing\n#   sbx env run    # create the sandbox, or attach to it\n")
	agent := "sbx-kit-" + ri.Slug
	if ri.Kind == "mixin" {
		agent = "<workload>"
		if ri.Workload != nil {
			agent = "sbx-kit-" + ri.Workload.Slug
		}
	}
	if ri.Published != nil {
		fmt.Fprintf(&b, "# Follows %s: every new sandbox pulls the newest published version.\n", ri.Published.Image)
		fmt.Fprintf(&b, "# Pin instead with %s.\n", ri.Published.Pinned)
		if ri.AllowedSource != "" {
			fmt.Fprintf(&b, "# sbx pulls only from kit.allowedSources; it must include %q.\n", ri.AllowedSource)
		}
	} else {
		if ri.Kind != "mixin" {
			agent = ri.Slug
		}
		fmt.Fprintf(&b, "# Not published yet, so this uses a local copy: put the %s/ folder from %s.tar.gz next to this file.\n", ri.Slug, ri.Slug)
		b.WriteString("# Publish the blueprint, then download this file again to follow its latest version.\n")
	}
	if ri.Kind == "mixin" {
		if ri.Workload != nil {
			fmt.Fprintf(&b, "# This blueprint is a mixin, so it runs on a workload kit: %s here; change both lines to use another.\n", ri.Workload.Name)
		} else {
			b.WriteString("# This blueprint is a mixin: it needs a v3 workload kit under it (sbx's built-in agents aren't kits).\n# Publish a workload blueprint, or add one under kits: and set agent: to sbx-kit-<its name>.\n")
		}
	}
	fmt.Fprintf(&b, "schemaVersion: \"1\"\nname: %s\nagent: %s\nkits:\n", ri.Slug, agent)
	if ri.Kind == "mixin" && ri.Workload != nil {
		fmt.Fprintf(&b, "  - source: %s\n", ri.Workload.Image)
	}
	if ri.Published != nil {
		fmt.Fprintf(&b, "  - source: %s\nworkspace: .\nsandboxOptions:\n  pullPolicy: always\n", ri.Published.Image)
	} else {
		fmt.Fprintf(&b, "  - ./%s\nworkspace: .\n", ri.Slug)
	}
	return b.String()
}

// sbxenv sends the sbxenv.yaml on its own.
func (e *Engine) sbxenv(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	l, err := e.Store.Load(id)
	if err != nil {
		http.Error(w, "not found", http.StatusNotFound)
		return
	}
	w.Header().Set("Content-Type", "application/yaml; charset=utf-8")
	w.Header().Set("Content-Disposition", `attachment; filename="sbxenv.yaml"`)
	_, _ = w.Write([]byte(sbxenvYAML(l, e.runInfo(l, e.publishState(id)))))
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
