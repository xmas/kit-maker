//go:build !js

package ai

import (
	"context"
	"encoding/json"
	"fmt"
	"os"

	"github.com/docker/kit-maker/internal/drivers"
)

// Driver names for the AI integration kind.
const (
	DriverFake      = "fake"
	DriverAnthropic = "anthropic"
	DriverBedrock   = "bedrock"
	DriverVertex    = "vertex"
)

// New builds the client for an AI integration. fixturesDir adds recorded
// fixtures on top of the built-in ones for the fake driver.
func New(ctx context.Context, driver string, cfg drivers.Config, fixturesDir string) (Client, error) {
	switch driver {
	case DriverFake:
		return NewFake(fixturesDir), nil
	case DriverAnthropic, DriverBedrock, DriverVertex:
		return NewAnthropic(ctx, AnthropicOptions{
			Provider: driver,
			APIKey:   cfg["api_key"],
			BaseURL:  cfg["base_url"],
			Region:   cfg["region"],
			Project:  cfg["project"],
			Model:    cfg["model"],
		})
	}
	return nil, fmt.Errorf("unknown AI driver %q", driver)
}

// PingSchema is the structured output the integration test asks for.
var PingSchema = map[string]any{
	"type": "object",
	"properties": map[string]any{
		"ok":      map[string]any{"type": "boolean"},
		"message": map[string]any{"type": "string"},
	},
	"required":             []string{"ok", "message"},
	"additionalProperties": false,
}

// Ping makes the smallest structured call, to prove the integration works.
func Ping(ctx context.Context, c Client) (*Response, error) {
	res, err := c.Generate(ctx, Request{
		Route:     RoutePing,
		Prompt:    `This is a connectivity check from Kit Maker. Reply with ok=true and a one-sentence message naming the model you are.`,
		Schema:    PingSchema,
		MaxTokens: 256,
	})
	if err != nil {
		return res, err
	}
	var v struct{ OK bool }
	if err := json.Unmarshal(res.JSON, &v); err != nil || !v.OK {
		return res, fmt.Errorf("ai: unexpected ping answer: %s", res.Text)
	}
	return res, nil
}

type aiDriver struct {
	name, label string
	fields      []drivers.Field
}

func (d aiDriver) Kind() drivers.Kind      { return drivers.KindAI }
func (d aiDriver) Name() string            { return d.name }
func (d aiDriver) Label() string           { return d.label }
func (d aiDriver) Fields() []drivers.Field { return d.fields }

func (d aiDriver) Validate(c drivers.Config) error {
	for _, f := range d.fields {
		if f.Required && c[f.Key] == "" {
			return fmt.Errorf("%s: %s is required", d.label, f.Label)
		}
	}
	return nil
}

func (d aiDriver) Test(ctx context.Context, c drivers.Config) error {
	client, err := New(ctx, d.name, c, os.Getenv("KITMAKER_AI_FIXTURES"))
	if err != nil {
		return err
	}
	_, err = Ping(ctx, client)
	return err
}

func init() {
	model := drivers.Field{Key: "model", Label: "Model", Placeholder: Model, Help: "Defaults to " + Model + "."}
	drivers.Register(aiDriver{DriverFake, "Fake (recorded fixtures, no key)", nil})
	drivers.Register(aiDriver{DriverAnthropic, "Anthropic API", []drivers.Field{
		{Key: "api_key", Label: "API key", Secret: true, Required: true},
		model,
		{Key: "base_url", Label: "Base URL", Placeholder: "https://api.anthropic.com", Help: "Only for a proxy or gateway."},
	}})
	drivers.Register(aiDriver{DriverBedrock, "Amazon Bedrock", []drivers.Field{
		{Key: "region", Label: "AWS region", Required: true, Placeholder: "us-east-1"},
		{Key: "model", Label: "Model or inference profile ID", Required: true, Help: "Credentials come from the standard AWS chain (IRSA, instance role, env)."},
	}})
	drivers.Register(aiDriver{DriverVertex, "Google Vertex AI", []drivers.Field{
		{Key: "project", Label: "GCP project", Required: true},
		{Key: "region", Label: "Region", Required: true, Placeholder: "global"},
		model,
	}})
}
