package http

import (
	"archive/tar"
	"compress/gzip"
	"context"
	"errors"
	"fmt"
	nethttp "net/http"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/docker/kit-maker/internal/authz"
	"github.com/docker/kit-maker/internal/blueprints"
	"github.com/docker/kit-maker/internal/drivers"
	"github.com/docker/kit-maker/internal/ent"
	"github.com/docker/kit-maker/internal/ent/blueprint"
	"github.com/docker/kit-maker/internal/ent/blueprintversion"
	"github.com/docker/kit-maker/internal/publish"
)

// Running a blueprint (kit-maker-lrq.10). The main path is a published
// version: Publish builds the kit image with BuildKit and pushes it to the
// deployment's registry as vN and latest; people run it by reference
// (sbx run <repo>:latest) or list it in an sbxenv.yaml with pullPolicy:
// always, so every new sandbox gets the newest version. sbx only pulls from
// its kit.allowedSources, so the registry prefix has to be allowed there.
// The fallback is a local copy: a folder holding <slug>/<slug>.yaml, which
// sbx builds in its sbx-kit-builder sandbox (sbx ZIPs are the older v2
// format).

// RunInfo is what the Run dialog shows.
type RunInfo struct {
	Slug  string `json:"slug"`
	Kind  string `json:"kind"`
	Valid bool   `json:"valid"`
	Error string `json:"error,omitempty"`
	// Published is the latest published version, if any.
	Published *PublishedRun `json:"published,omitempty"`
	// CanPublish says whether this person may publish; PublishSetup says
	// what's missing (registry or build runner) when publishing can't run.
	CanPublish   bool   `json:"canPublish"`
	PublishSetup string `json:"publishSetup,omitempty"`
	// AllowedSource is the prefix sbx's kit.allowedSources must include.
	AllowedSource string `json:"allowedSource,omitempty"`
	// Workload is what a mixin runs on: a v3 mixin needs a v3 workload kit
	// (sbx's built-in agents aren't kits), so this is the most recently
	// published workload blueprint, if there is one.
	Workload *WorkloadRef `json:"workload,omitempty"`
	// The local copy.
	Extract      string `json:"extract"`
	LocalCommand string `json:"localCommand"`
}

// WorkloadRef names a published workload blueprint.
type WorkloadRef struct {
	Name  string `json:"name"`
	Slug  string `json:"slug"`
	Image string `json:"image"`
}

// PublishedRun is how to run the published version.
type PublishedRun struct {
	blueprints.Version
	Image   string `json:"image"` // repository:latest
	Pinned  string `json:"pinned"`
	Command string `json:"command"`
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

func (s *server) runInfo(r *nethttp.Request, l *blueprints.Loaded) RunInfo {
	slug := l.Blueprint.Slug
	ri := RunInfo{Slug: slug, Kind: l.Blueprint.Kind, Valid: l.Compiled.Valid, Error: l.Compiled.Error,
		Extract: fmt.Sprintf("tar -xzf %s.tar.gz", slug)}
	if l.Blueprint.Kind == "mixin" {
		ri.Workload = s.latestWorkload(r)
	}
	ri.LocalCommand = runCommand(l.Blueprint.Kind, "./"+slug+"/", ri.Workload)
	if v, err := s.blueprints().LatestVersion(r.Context(), l.Blueprint.ID); err == nil && v != nil {
		image := v.Repository + ":latest"
		ri.Published = &PublishedRun{Version: *v, Image: image, Pinned: v.Ref(), Command: runCommand(l.Blueprint.Kind, image, ri.Workload)}
	}
	ri.CanPublish = authz.SubjectFrom(r.Context()).Can(authz.BlueprintPublish, l.Blueprint.TeamID)
	if reg, _, err := s.publishTargets(r.Context()); err != nil {
		ri.PublishSetup = err.Error()
	} else {
		ri.AllowedSource = strings.TrimSuffix(reg.Repository(""), "sbx-kit-")
	}
	return ri
}

// latestWorkload is the most recently published workload blueprint this
// person can see.
func (s *server) latestWorkload(r *nethttp.Request) *WorkloadRef {
	teams, all := authz.SubjectFrom(r.Context()).TeamsWith(authz.BlueprintView)
	q := s.Ent.BlueprintVersion.Query().
		Where(blueprintversion.HasBlueprintWith(blueprint.KindEQ(blueprint.KindWorkload))).
		WithBlueprint().Order(ent.Desc(blueprintversion.FieldCreatedAt))
	if !all {
		q = q.Where(blueprintversion.HasBlueprintWith(blueprint.TeamIDIn(teams...)))
	}
	v, err := q.First(r.Context())
	if err != nil || v.Edges.Blueprint == nil {
		return nil
	}
	return &WorkloadRef{Name: v.Edges.Blueprint.DisplayName, Slug: v.Edges.Blueprint.Slug, Image: v.Repository + ":latest"}
}

// publishTargets resolves the default registry and BuildKit integrations.
func (s *server) publishTargets(ctx context.Context) (publish.Registry, string, error) {
	if s.Settings == nil {
		return publish.Registry{}, "", errors.New("publishing isn't set up on this deployment")
	}
	d, cfg, err := s.Settings.Default(ctx, drivers.KindRegistry)
	if err != nil {
		return publish.Registry{}, "", errors.New("add a registry integration to publish (kitmaker integration set registry …)")
	}
	push, pull := drivers.RegistryHosts(d, cfg)
	reg := publish.Registry{Host: push, PullHost: pull, Namespace: cfg["namespace"], Username: cfg["username"],
		Password: cfg["password"], Insecure: cfg["insecure"] == "true"}
	bd, bcfg, err := s.Settings.Default(ctx, drivers.KindBuildRunner)
	if err != nil || bd.Name() != "buildkit" || bcfg["address"] == "" {
		return reg, "", errors.New("add a BuildKit build runner to publish (kitmaker integration set build_runner buildkit …)")
	}
	return reg, bcfg["address"], nil
}

// sbxenvYAML is an environment file for a project. With a published
// version it follows the registry's latest tag; otherwise it points at a
// local copy of the folder next to it.
func sbxenvYAML(l *blueprints.Loaded, ri RunInfo) string {
	var b strings.Builder
	fmt.Fprintf(&b, "# sbxenv.yaml for %s (Kit Maker blueprint %s).\n", l.Blueprint.DisplayName, l.Blueprint.ID)
	b.WriteString("#   sbx env plan   # preview, changes nothing\n#   sbx env run    # create the sandbox, or attach to it\n")
	// agent: is the handle sbx derives from the workload kit's reference.
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

// blueprintRunInfo returns the Run dialog's data as JSON.
func (s *server) blueprintRunInfo(w nethttp.ResponseWriter, r *nethttp.Request) {
	l, ok := s.loadBlueprint(w, r, chi.URLParam(r, "blueprintID"), authz.BlueprintDownload)
	if !ok {
		return
	}
	writeJSON(w, nethttp.StatusOK, s.runInfo(r, l))
}

// blueprintPublish publishes the blueprint's next version: builds the kit
// image with BuildKit, pushes vN and latest, records the version.
func (s *server) blueprintPublish(w nethttp.ResponseWriter, r *nethttp.Request) {
	l, ok := s.loadBlueprint(w, r, chi.URLParam(r, "blueprintID"), authz.BlueprintPublish)
	if !ok {
		return
	}
	reg, addr, err := s.publishTargets(r.Context())
	if err != nil {
		writeJSON(w, nethttp.StatusServiceUnavailable, map[string]string{"error": err.Error()})
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 15*time.Minute)
	defer cancel()
	v, err := s.blueprints().Publish(ctx, l.Blueprint.ID, userID(r), func(ctx context.Context, slug string, version int, yaml string) (string, string, error) {
		res, err := publish.Publish(ctx, addr, reg, publish.Input{Slug: slug, Version: version, YAML: yaml})
		return res.Repository, res.Digest, err
	})
	switch {
	case errors.Is(err, blueprints.ErrNotValid):
		writeJSON(w, nethttp.StatusConflict, map[string]string{"error": err.Error()})
		return
	case err != nil:
		s.Log.WarnContext(r.Context(), "publish", "blueprint", l.Blueprint.ID, "error", err)
		writeJSON(w, nethttp.StatusBadGateway, map[string]string{"error": "Publishing failed: " + err.Error()})
		return
	}
	s.Log.InfoContext(r.Context(), "publish", "blueprint", l.Blueprint.ID, "version", v.Version, "digest", v.Digest, "unchanged", v.Unchanged)
	writeJSON(w, nethttp.StatusOK, s.runInfo(r, l))
}

// blueprintDownload sends <slug>.tar.gz holding just <slug>/<slug>.yaml,
// the compiled kit. The folder is the kit's build context, so nothing else
// goes in it; the sbxenv.yaml is its own download, kept beside the folder
// (never inside the archive, where extracting could overwrite a project's).
func (s *server) blueprintDownload(w nethttp.ResponseWriter, r *nethttp.Request) {
	l, ok := s.loadBlueprint(w, r, chi.URLParam(r, "blueprintID"), authz.BlueprintDownload)
	if !ok {
		return
	}
	if !l.Compiled.Valid {
		nethttp.Error(w, "This blueprint isn't a valid kit yet: "+l.Compiled.Error, nethttp.StatusConflict)
		return
	}
	slug := l.Blueprint.Slug
	w.Header().Set("Content-Type", "application/gzip")
	w.Header().Set("Content-Disposition", fmt.Sprintf(`attachment; filename="%s.tar.gz"`, slug))
	gz := gzip.NewWriter(w)
	tw := tar.NewWriter(gz)
	now := time.Now()
	if err := tw.WriteHeader(&tar.Header{Name: slug + "/", Typeflag: tar.TypeDir, Mode: 0o755, ModTime: now}); err != nil {
		return
	}
	body := l.Compiled.YAML
	if err := tw.WriteHeader(&tar.Header{Name: slug + "/" + slug + ".yaml", Mode: 0o644, Size: int64(len(body)), ModTime: now}); err != nil {
		return
	}
	_, _ = tw.Write([]byte(body))
	_ = tw.Close()
	_ = gz.Close()
	s.Log.InfoContext(r.Context(), "blueprint download", "blueprint", l.Blueprint.ID, "user", userID(r), "kind", "folder")
}

// blueprintSbxenv sends the sbxenv.yaml on its own.
func (s *server) blueprintSbxenv(w nethttp.ResponseWriter, r *nethttp.Request) {
	l, ok := s.loadBlueprint(w, r, chi.URLParam(r, "blueprintID"), authz.BlueprintDownload)
	if !ok {
		return
	}
	w.Header().Set("Content-Type", "application/yaml; charset=utf-8")
	w.Header().Set("Content-Disposition", `attachment; filename="sbxenv.yaml"`)
	_, _ = w.Write([]byte(sbxenvYAML(l, s.runInfo(r, l))))
	s.Log.InfoContext(r.Context(), "blueprint download", "blueprint", l.Blueprint.ID, "user", userID(r), "kind", "sbxenv")
}
