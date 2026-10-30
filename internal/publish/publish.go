// Package publish turns a blueprint version into a kit image in a registry
// (DESIGN.md §1, kit-maker-lrq.2). It hands the compiled descriptor to
// BuildKit: the descriptor's first line, "# syntax=docker/sandbox-kit:3",
// makes BuildKit run the kit spec's own frontend, which validates it, builds
// any Dockerfile content and pushes one OCI image carrying the kit
// annotations, the same way sbx builds a local kit folder. People then run
// the image by reference (sbx run <ref>, or kits: in an sbxenv.yaml).
package publish

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/docker/cli/cli/config/types"
	"github.com/moby/buildkit/client"
	"github.com/moby/buildkit/session"
	"github.com/moby/buildkit/session/auth/authprovider"
	"github.com/tonistiigi/fsutil"

	"github.com/docker/kit-maker/internal/blueprints/compiler"
)

// Registry is where kit images go.
type Registry struct {
	// Host is where BuildKit pushes (e.g. zot:5000 inside the compose
	// stack); PullHost is where people pull from when that differs (e.g.
	// localhost:5001 on the host).
	Host, PullHost string
	Namespace      string
	Username       string
	Password       string
	Insecure       bool // plain HTTP, local registries only
}

// Repository is the image name without a tag, as people pull it.
func (r Registry) Repository(slug string) string {
	host := r.PullHost
	if host == "" {
		host = r.Host
	}
	return strings.TrimSuffix(host, "/") + "/" + r.Namespace + "/sbx-kit-" + slug
}

func (r Registry) pushRepository(slug string) string {
	return strings.TrimSuffix(r.Host, "/") + "/" + r.Namespace + "/sbx-kit-" + slug
}

// Input is one blueprint version to publish.
type Input struct {
	Slug    string
	Version int
	YAML    string // the compiled descriptor, starting with "# syntax=docker/sandbox-kit:3"
	// Files are the Dockerfile's build files (compiler.Output.Files),
	// written beside the descriptor: the build context its COPY lines read.
	Files map[string]string
}

// Result is what was pushed.
type Result struct {
	Repository string   // as people pull it
	Tags       []string // vN and latest
	Digest     string   // manifest digest
}

// Ref is the pinned reference for this version.
func (r Result) Ref() string { return r.Repository + "@" + r.Digest }

// Publish builds and pushes the kit image with BuildKit at addr
// (tcp://host:port), tagged vN and latest.
func Publish(ctx context.Context, addr string, reg Registry, in Input) (Result, error) {
	if !strings.HasPrefix(in.YAML, "# syntax=docker/sandbox-kit:3") {
		return Result{}, fmt.Errorf("the descriptor must start with # syntax=docker/sandbox-kit:3")
	}
	dir, err := os.MkdirTemp("", "kitmaker-publish-")
	if err != nil {
		return Result{}, err
	}
	defer os.RemoveAll(dir)
	file := in.Slug + ".yaml"
	if err := os.WriteFile(filepath.Join(dir, file), []byte(in.YAML), 0o644); err != nil {
		return Result{}, err
	}
	for name, content := range in.Files {
		if !compiler.BuildFilePath(name) || name == file {
			return Result{}, fmt.Errorf("build file %q must be a relative path inside the build folder", name)
		}
		p := filepath.Join(dir, filepath.FromSlash(name))
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			return Result{}, err
		}
		mode := os.FileMode(0o644)
		if strings.HasPrefix(content, "#!") {
			mode = 0o755
		}
		if err := os.WriteFile(p, []byte(content), mode); err != nil {
			return Result{}, err
		}
	}
	fs, err := fsutil.NewFS(dir)
	if err != nil {
		return Result{}, err
	}

	c, err := client.New(ctx, addr)
	if err != nil {
		return Result{}, fmt.Errorf("connect to BuildKit at %s: %w", addr, err)
	}
	defer c.Close()

	tags := []string{fmt.Sprintf("v%d", in.Version), "latest"}
	push := reg.pushRepository(in.Slug)
	names := make([]string, len(tags))
	for i, t := range tags {
		names[i] = push + ":" + t
	}
	attrs := map[string]string{"name": strings.Join(names, ","), "push": "true"}
	if reg.Insecure {
		attrs["registry.insecure"] = "true"
	}
	opt := client.SolveOpt{
		Frontend:      "dockerfile.v0",
		FrontendAttrs: map[string]string{"filename": file},
		LocalMounts:   map[string]fsutil.FS{"context": fs, "dockerfile": fs},
		Exports:       []client.ExportEntry{{Type: client.ExporterImage, Attrs: attrs}},
		Session:       []session.Attachable{authprovider.NewDockerAuthProvider(authprovider.DockerAuthProviderConfig{AuthConfigProvider: reg.auth})},
	}
	status := make(chan *client.SolveStatus)
	var log []string
	done := make(chan struct{})
	go func() {
		defer close(done)
		for s := range status {
			for _, l := range s.Logs {
				log = append(log, strings.TrimSpace(string(l.Data)))
			}
			for _, v := range s.Vertexes {
				if v.Error != "" {
					log = append(log, v.Name+": "+v.Error)
				}
			}
		}
	}()
	resp, err := c.Solve(ctx, nil, opt, status)
	<-done
	if err != nil {
		if tail := lastLines(log, 6); tail != "" {
			return Result{}, fmt.Errorf("build: %w\n%s", err, tail)
		}
		return Result{}, fmt.Errorf("build: %w", err)
	}
	digest := resp.ExporterResponse["containerimage.digest"]
	if digest == "" {
		return Result{}, fmt.Errorf("BuildKit pushed no image digest")
	}
	return Result{Repository: reg.Repository(in.Slug), Tags: tags, Digest: digest}, nil
}

// auth answers BuildKit's credential requests for the registry host.
func (r Registry) auth(_ context.Context, host string, _ []string, _ authprovider.ExpireCachedAuthCheck) (types.AuthConfig, error) {
	if r.Username == "" || !strings.EqualFold(host, r.Host) {
		return types.AuthConfig{}, nil
	}
	return types.AuthConfig{Username: r.Username, Password: r.Password, ServerAddress: host}, nil
}

func lastLines(lines []string, n int) string {
	var out []string
	for _, l := range lines {
		if l != "" {
			out = append(out, l)
		}
	}
	if len(out) > n {
		out = out[len(out)-n:]
	}
	return strings.Join(out, "\n")
}
