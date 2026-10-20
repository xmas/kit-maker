package drivers

import (
	"context"
	"fmt"
	"net"
	"net/http"
	"net/url"
	"strings"
	"time"
)

// base carries the static parts every driver shares.
type base struct {
	kind   Kind
	name   string
	label  string
	fields []Field
}

func (b base) Kind() Kind      { return b.kind }
func (b base) Name() string    { return b.name }
func (b base) Label() string   { return b.label }
func (b base) Fields() []Field { return b.fields }

// --- Registries (blueprint images) -------------------------------------------

// ociRegistry is any registry speaking the OCI distribution API. Test pings
// /v2/ with the configured credentials. A 401 without credentials still proves
// the endpoint is a registry.
type ociRegistry struct {
	base
	defaultHost string
}

var registryFields = []Field{
	{Key: "host", Label: "Registry host", Placeholder: "registry.example.com", Help: "Where blueprint images are pushed."},
	{Key: "pull_host", Label: "Pull host", Placeholder: "localhost:5001", Help: "Where people pull from, when it differs from the push host (local stacks)."},
	{Key: "namespace", Label: "Namespace", Required: true, Placeholder: "acme", Help: "Blueprint versions are pushed as <namespace>/sbx-kit-<slug>."},
	{Key: "username", Label: "Username"},
	{Key: "password", Label: "Token or password", Secret: true},
	{Key: "insecure", Label: "Plain HTTP", Help: "Only for local registries such as zot in the compose stack."},
}

func (r ociRegistry) Validate(c Config) error {
	if err := requireFields(r, c); err != nil {
		return err
	}
	if r.host(c) == "" {
		return fmt.Errorf("%s: host is required", r.name)
	}
	return nil
}

func (r ociRegistry) host(c Config) string {
	if h := c["host"]; h != "" {
		return h
	}
	return r.defaultHost
}

func (r ociRegistry) Test(ctx context.Context, c Config) error {
	if err := r.Validate(c); err != nil {
		return err
	}
	scheme := "https"
	if c["insecure"] == "true" {
		scheme = "http"
	}
	u := url.URL{Scheme: scheme, Host: r.host(c), Path: "/v2/"}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u.String(), nil)
	if err != nil {
		return err
	}
	if c["username"] != "" {
		req.SetBasicAuth(c["username"], c["password"])
	}
	resp, err := (&http.Client{Timeout: 10 * time.Second}).Do(req)
	if err != nil {
		return fmt.Errorf("reach registry: %w", err)
	}
	defer resp.Body.Close()
	switch {
	case resp.StatusCode == http.StatusOK:
		return nil
	case resp.StatusCode == http.StatusUnauthorized && c["username"] == "" &&
		strings.Contains(resp.Header.Get("Docker-Distribution-Api-Version"), "registry/2.0"):
		return nil
	case resp.StatusCode == http.StatusUnauthorized:
		return fmt.Errorf("registry rejected the credentials (401)")
	default:
		return fmt.Errorf("registry answered %s at %s", resp.Status, u.String())
	}
}

// --- Build runners -------------------------------------------------------------

type noBuildRunner struct{ base }

func (noBuildRunner) Validate(Config) error              { return nil }
func (noBuildRunner) Test(context.Context, Config) error { return nil }

type buildkitRunner struct{ base }

func (b buildkitRunner) Validate(c Config) error {
	if err := requireFields(b, c); err != nil {
		return err
	}
	if _, _, err := tcpAddr(c["address"]); err != nil {
		return err
	}
	return nil
}

func (b buildkitRunner) Test(ctx context.Context, c Config) error {
	host, port, err := tcpAddr(c["address"])
	if err != nil {
		return err
	}
	var d net.Dialer
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	conn, err := d.DialContext(ctx, "tcp", net.JoinHostPort(host, port))
	if err != nil {
		return fmt.Errorf("reach buildkitd: %w", err)
	}
	return conn.Close()
}

func tcpAddr(addr string) (string, string, error) {
	u, err := url.Parse(addr)
	if err != nil || u.Scheme != "tcp" || u.Hostname() == "" || u.Port() == "" {
		return "", "", fmt.Errorf("buildkit address must look like tcp://host:1234, got %q", addr)
	}
	return u.Hostname(), u.Port(), nil
}

// validateOnly drivers check required fields; a live test comes with the
// feature that uses them.
type validateOnly struct{ base }

func (v validateOnly) Validate(c Config) error            { return requireFields(v, c) }
func (v validateOnly) Test(context.Context, Config) error { return ErrTestUnavailable }

func init() {
	for _, r := range []ociRegistry{
		{base{KindRegistry, "dockerhub", "Docker Hub", registryFields}, "registry-1.docker.io"},
		{base{KindRegistry, "ghcr", "GitHub Container Registry", registryFields}, "ghcr.io"},
		{base{KindRegistry, "ecr", "Amazon ECR", registryFields}, ""},
		{base{KindRegistry, "acr", "Azure Container Registry", registryFields}, ""},
		{base{KindRegistry, "quay", "Quay", registryFields}, "quay.io"},
		{base{KindRegistry, "zot", "zot / any OCI registry", registryFields}, ""},
	} {
		Register(r)
	}

	Register(noBuildRunner{base{KindBuildRunner, "none", "None (blueprints without Dockerfile content only)", nil}})
	Register(buildkitRunner{base{KindBuildRunner, "buildkit", "BuildKit daemon", []Field{
		{Key: "address", Label: "buildkitd address", Required: true, Placeholder: "tcp://buildkit:1234"},
	}}})
	Register(validateOnly{base{KindBuildRunner, "build_cloud", "Docker Build Cloud", []Field{
		{Key: "builder", Label: "Cloud builder", Required: true, Placeholder: "acme/default"},
		{Key: "token", Label: "Docker access token", Secret: true, Required: true},
	}}})
	Register(validateOnly{base{KindBuildRunner, "github_actions", "GitHub Actions", []Field{
		{Key: "repository", Label: "Repository that runs builds", Required: true, Placeholder: "acme/blueprint-builds"},
		{Key: "workflow", Label: "Workflow file", Required: true, Placeholder: "build-blueprint.yml"},
	}}})

	Register(validateOnly{base{KindGitHost, "github_app", "GitHub App", []Field{
		{Key: "app_id", Label: "App ID", Required: true},
		{Key: "installation_id", Label: "Installation ID", Required: true},
		{Key: "private_key", Label: "Private key (PEM)", Secret: true, Required: true},
		{Key: "api_url", Label: "API URL", Placeholder: "https://api.github.com", Help: "Change for GitHub Enterprise Server."},
	}}})
}

// RegistryHosts returns where a registry integration pushes and where
// people pull from (pull_host, Docker Hub's docker.io, or the push host).
func RegistryHosts(d Driver, c Config) (push, pull string) {
	r, ok := d.(ociRegistry)
	if !ok {
		return c["host"], c["pull_host"]
	}
	push = r.host(c)
	switch {
	case c["pull_host"] != "":
		pull = c["pull_host"]
	case r.name == "dockerhub":
		pull = "docker.io"
	default:
		pull = push
	}
	return push, pull
}
