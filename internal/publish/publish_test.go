package publish

import (
	"context"
	"os"
	"testing"
	"time"
)

// TestPublishLocal pushes a real kit through the compose stack's BuildKit to
// its zot registry. Opt in: KITMAKER_PUBLISH_IT=<path to a compiled kit
// YAML> (with make up or make dev running).
func TestPublishLocal(t *testing.T) {
	file := os.Getenv("KITMAKER_PUBLISH_IT")
	if file == "" {
		t.Skip("set KITMAKER_PUBLISH_IT to a compiled kit YAML to run")
	}
	yaml, err := os.ReadFile(file)
	if err != nil {
		t.Fatal(err)
	}
	addr := os.Getenv("KITMAKER_BUILDKIT_ADDR")
	if addr == "" {
		addr = "tcp://127.0.0.1:1234"
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Minute)
	defer cancel()
	host := os.Getenv("KITMAKER_REGISTRY_URL")
	if host == "" {
		host = "zot:5000"
	}
	slug := os.Getenv("KITMAKER_PUBLISH_SLUG")
	if slug == "" {
		slug = "publish-it"
	}
	res, err := Publish(ctx, addr, Registry{Host: host, PullHost: os.Getenv("KITMAKER_REGISTRY_PULL_HOST"), Namespace: "blueprints", Insecure: os.Getenv("KITMAKER_REGISTRY_INSECURE") != "false"},
		Input{Slug: slug, Version: 1, YAML: string(yaml)})
	if err != nil {
		t.Fatal(err)
	}
	t.Logf("pushed %s (tags %v)", res.Ref(), res.Tags)
}
