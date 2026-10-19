package ai

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/anthropics/anthropic-sdk-go"
	"github.com/anthropics/anthropic-sdk-go/bedrock"
	"github.com/anthropics/anthropic-sdk-go/option"
	"github.com/anthropics/anthropic-sdk-go/vertex"
	"github.com/aws/aws-sdk-go-v2/config"
)

// AnthropicOptions selects where Claude runs.
type AnthropicOptions struct {
	Provider string // anthropic | bedrock | vertex
	APIKey   string // anthropic
	BaseURL  string // anthropic; tests and proxies
	Region   string // bedrock, vertex
	Project  string // vertex
	Model    string
}

type anthropicClient struct {
	c        anthropic.Client
	provider string
	model    string
	// fallbacks turns on the server-side refusal fallback. It is a
	// first-party API feature, so Bedrock and Vertex run without it.
	fallbacks bool
}

// NewAnthropic builds a client for the Anthropic API, Bedrock or Vertex.
func NewAnthropic(ctx context.Context, o AnthropicOptions) (Client, error) {
	var opts []option.RequestOption
	model := o.Model
	switch o.Provider {
	case "anthropic":
		if o.APIKey == "" {
			return nil, fmt.Errorf("anthropic: api_key is required")
		}
		opts = append(opts, option.WithAPIKey(o.APIKey))
		if o.BaseURL != "" {
			opts = append(opts, option.WithBaseURL(o.BaseURL))
		}
	case "bedrock":
		if model == "" {
			return nil, fmt.Errorf("bedrock: model is required (the Bedrock model or inference profile ID)")
		}
		opts = append(opts, bedrock.WithLoadDefaultConfig(ctx, config.WithRegion(o.Region)))
	case "vertex":
		opts = append(opts, vertex.WithGoogleAuth(ctx, o.Region, o.Project))
	default:
		return nil, fmt.Errorf("unknown AI provider %q", o.Provider)
	}
	if model == "" {
		model = Model
	}
	return &anthropicClient{
		c:         anthropic.NewClient(opts...),
		provider:  o.Provider,
		model:     model,
		fallbacks: o.Provider == "anthropic",
	}, nil
}

func (a *anthropicClient) Provider() string { return a.provider }

func (a *anthropicClient) params(req Request) anthropic.BetaMessageNewParams {
	p := anthropic.BetaMessageNewParams{
		Model:     anthropic.Model(a.model),
		MaxTokens: req.maxTokens(),
		Messages:  []anthropic.BetaMessageParam{anthropic.NewBetaUserMessage(anthropic.NewBetaTextBlock(req.Prompt))},
		OutputConfig: anthropic.BetaOutputConfigParam{
			Effort: anthropic.BetaOutputConfigEffort(EffortFor(req.Route)),
		},
		Metadata: anthropic.BetaMetadataParam{UserID: anthropic.String(req.UserID)},
	}
	if req.UserID == "" {
		p.Metadata = anthropic.BetaMetadataParam{}
	}
	// Two cache breakpoints: the frozen product prefix (1h) and team context.
	p.System = append(p.System, cached(req.System, anthropic.BetaCacheControlEphemeralTTLTTL1h)...)
	p.System = append(p.System, cached(req.Context, anthropic.BetaCacheControlEphemeralTTLTTL5m)...)
	if req.Schema != nil {
		p.OutputConfig.Format = anthropic.BetaJSONOutputFormatParam{Schema: req.Schema}
	}
	if a.fallbacks {
		p.Fallbacks = anthropic.BetaFallbacksParamOfDefault()
		p.Betas = []anthropic.AnthropicBeta{anthropic.AnthropicBetaServerSideFallback2026_07_01}
	}
	return p
}

// cached turns text blocks into system blocks with a breakpoint on the last.
func cached(blocks []string, ttl anthropic.BetaCacheControlEphemeralTTL) []anthropic.BetaTextBlockParam {
	var out []anthropic.BetaTextBlockParam
	for _, b := range blocks {
		if strings.TrimSpace(b) != "" {
			out = append(out, anthropic.BetaTextBlockParam{Text: b})
		}
	}
	if len(out) > 0 {
		out[len(out)-1].CacheControl = anthropic.BetaCacheControlEphemeralParam{TTL: ttl}
	}
	return out
}

func (a *anthropicClient) Generate(ctx context.Context, req Request) (*Response, error) {
	msg, err := a.c.Beta.Messages.New(ctx, a.params(req))
	if err != nil {
		return nil, err
	}
	return toResponse(req, msg)
}

func (a *anthropicClient) Stream(ctx context.Context, req Request, onDelta func(string) error) (*Response, error) {
	stream := a.c.Beta.Messages.NewStreaming(ctx, a.params(req))
	defer stream.Close()
	var msg anthropic.BetaMessage
	for stream.Next() {
		ev := stream.Current()
		if err := msg.Accumulate(ev); err != nil {
			return nil, err
		}
		if ev.Type == "content_block_delta" && ev.Delta.Type == "text_delta" && ev.Delta.Text != "" {
			if err := onDelta(ev.Delta.Text); err != nil {
				return nil, err
			}
		}
	}
	if err := stream.Err(); err != nil {
		return nil, err
	}
	return toResponse(req, &msg)
}

func toResponse(req Request, msg *anthropic.BetaMessage) (*Response, error) {
	var text strings.Builder
	for _, b := range msg.Content {
		if b.Type == "text" {
			text.WriteString(b.Text)
		}
	}
	r := &Response{
		Text:       text.String(),
		Model:      string(msg.Model),
		StopReason: string(msg.StopReason),
		Usage: Usage{
			InputTokens:      msg.Usage.InputTokens,
			OutputTokens:     msg.Usage.OutputTokens,
			CacheReadTokens:  msg.Usage.CacheReadInputTokens,
			CacheWriteTokens: msg.Usage.CacheCreationInputTokens,
		},
	}
	if r.StopReason == "refusal" {
		return r, ErrRefused
	}
	if req.Schema != nil {
		if !json.Valid([]byte(r.Text)) {
			return r, fmt.Errorf("ai: structured output is not valid JSON (stop reason %s)", r.StopReason)
		}
		r.JSON = json.RawMessage(r.Text)
	}
	return r, nil
}
