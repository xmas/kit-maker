package ai_test

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"entgo.io/ent/dialect"
	entsql "entgo.io/ent/dialect/sql"

	"github.com/docker/kit-maker/internal/ai"
	"github.com/docker/kit-maker/internal/ent"
	"github.com/docker/kit-maker/internal/ent/aicall"
	"github.com/docker/kit-maker/internal/testutil"
)

func TestEffortPerRoute(t *testing.T) {
	for route, want := range map[ai.Route]ai.Effort{
		ai.RouteExplain: ai.EffortLow, ai.RouteFill: ai.EffortMedium,
		ai.RoutePlan: ai.EffortHigh, ai.RouteReview: ai.EffortHigh, "unknown": ai.EffortMedium,
	} {
		if got := ai.EffortFor(route); got != want {
			t.Errorf("%s: effort %s, want %s", route, got, want)
		}
	}
}

func TestFake(t *testing.T) {
	ctx := context.Background()
	f := ai.NewFake("")

	t.Run("ping is structured", func(t *testing.T) {
		res, err := ai.Ping(ctx, f)
		if err != nil {
			t.Fatal(err)
		}
		if !json.Valid(res.JSON) || res.Usage.InputTokens == 0 {
			t.Fatalf("ping: %+v", res)
		}
	})

	t.Run("route answers match the prompt", func(t *testing.T) {
		req := ai.ExplainRequest(ai.ExplainInput{Kit: "k", BlockType: "credential", Payload: map[string]any{}})
		var streamed strings.Builder
		res, err := f.Stream(ctx, req, func(d string) error { streamed.WriteString(d); return nil })
		if err != nil {
			t.Fatal(err)
		}
		if !strings.Contains(res.Text, "placeholder") || streamed.String() != res.Text {
			t.Fatalf("stream %q vs text %q", streamed.String(), res.Text)
		}
	})

	t.Run("unknown route has no fixture", func(t *testing.T) {
		_, err := f.Generate(ctx, ai.Request{Route: ai.RouteImport, Prompt: "x"})
		if !errors.Is(err, ai.ErrNoFixture) {
			t.Fatalf("err = %v", err)
		}
	})

	t.Run("recorded fixtures replay exactly", func(t *testing.T) {
		dir := t.TempDir()
		req := ai.Request{Route: ai.RouteImport, Prompt: "split this kit"}
		rec := &ai.Recorder{Client: stubClient{text: "recorded answer"}, Dir: dir}
		if _, err := rec.Generate(ctx, req); err != nil {
			t.Fatal(err)
		}
		if _, err := os.Stat(filepath.Join(dir, req.Key()+".json")); err != nil {
			t.Fatal(err)
		}
		res, err := ai.NewFake(dir).Generate(ctx, req)
		if err != nil || res.Text != "recorded answer" {
			t.Fatalf("replay: %v %+v", err, res)
		}
		// Built-in fixtures still work through the layered FS.
		if _, err := ai.Ping(ctx, ai.NewFake(dir)); err != nil {
			t.Fatal(err)
		}
	})
}

type stubClient struct{ text string }

func (stubClient) Provider() string { return "stub" }
func (s stubClient) Generate(context.Context, ai.Request) (*ai.Response, error) {
	return &ai.Response{Text: s.text, Model: "stub", Usage: ai.Usage{InputTokens: 100, OutputTokens: 50}}, nil
}
func (s stubClient) Stream(ctx context.Context, r ai.Request, on func(string) error) (*ai.Response, error) {
	res, _ := s.Generate(ctx, r)
	return res, on(res.Text)
}

// TestAnthropicRequest checks what goes over the wire: effort, cache
// breakpoints, structured output, refusal fallback, and the streamed answer.
func TestAnthropicRequest(t *testing.T) {
	var got map[string]any
	var beta string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		b, _ := io.ReadAll(r.Body)
		got = nil
		_ = json.Unmarshal(b, &got)
		beta = r.Header.Get("anthropic-beta")
		if got["stream"] == true {
			w.Header().Set("Content-Type", "text/event-stream")
			for _, ev := range []string{
				`{"type":"message_start","message":{"id":"m1","type":"message","role":"assistant","model":"claude-opus-5-5","content":[],"stop_reason":null,"usage":{"input_tokens":120,"output_tokens":1,"cache_read_input_tokens":900,"cache_creation_input_tokens":0}}}`,
				`{"type":"content_block_start","index":0,"content_block":{"type":"text","text":""}}`,
				`{"type":"content_block_delta","index":0,"delta":{"type":"text_delta","text":"Allows "}}`,
				`{"type":"content_block_delta","index":0,"delta":{"type":"text_delta","text":"GitHub."}}`,
				`{"type":"content_block_stop","index":0}`,
				`{"type":"message_delta","delta":{"stop_reason":"end_turn","stop_sequence":null},"usage":{"output_tokens":7}}`,
				`{"type":"message_stop"}`,
			} {
				var typ struct{ Type string }
				_ = json.Unmarshal([]byte(ev), &typ)
				fmt.Fprintf(w, "event: %s\ndata: %s\n\n", typ.Type, ev)
			}
			return
		}
		w.Header().Set("Content-Type", "application/json")
		if strings.Contains(string(b), "please refuse") {
			fmt.Fprint(w, `{"id":"m2","type":"message","role":"assistant","model":"claude-opus-5-5","content":[],"stop_reason":"refusal","stop_details":{"type":"refusal"},"usage":{"input_tokens":10,"output_tokens":0}}`)
			return
		}
		fmt.Fprint(w, `{"id":"m3","type":"message","role":"assistant","model":"claude-opus-5-5","content":[{"type":"text","text":"{\"ok\":true,\"message\":\"hi\"}"}],"stop_reason":"end_turn","usage":{"input_tokens":30,"output_tokens":9}}`)
	}))
	defer srv.Close()

	ctx := context.Background()
	c, err := ai.NewAnthropic(ctx, ai.AnthropicOptions{Provider: "anthropic", APIKey: "test-key", BaseURL: srv.URL})
	if err != nil {
		t.Fatal(err)
	}

	res, err := ai.Ping(ctx, c)
	if err != nil {
		t.Fatalf("ping: %v", err)
	}
	if string(res.JSON) != `{"ok":true,"message":"hi"}` || res.Usage.InputTokens != 30 {
		t.Fatalf("ping response: %+v", res)
	}
	oc, _ := got["output_config"].(map[string]any)
	format, _ := oc["format"].(map[string]any)
	if got["model"] != ai.Model || oc["effort"] != "low" || format["type"] != "json_schema" || format["schema"] == nil {
		t.Fatalf("request: %v", got)
	}
	if got["fallbacks"] != "default" || !strings.Contains(beta, "server-side-fallback") {
		t.Fatalf("fallback not requested: fallbacks=%v beta=%q", got["fallbacks"], beta)
	}

	req := ai.ExplainRequest(ai.ExplainInput{Kit: "acme", BlockType: "network-rules", Payload: map[string]any{"runtime": map[string]any{}}})
	req.Context = []string{"team library summary"}
	var deltas []string
	res, err = c.Stream(ctx, req, func(d string) error { deltas = append(deltas, d); return nil })
	if err != nil {
		t.Fatalf("stream: %v", err)
	}
	if res.Text != "Allows GitHub." || len(deltas) != 2 || res.Usage.CacheReadTokens != 900 || res.Usage.OutputTokens != 7 {
		t.Fatalf("stream response: %+v deltas=%q", res, deltas)
	}
	system, _ := got["system"].([]any)
	if len(system) != 2 {
		t.Fatalf("system blocks: %v", got["system"])
	}
	first, _ := system[0].(map[string]any)["cache_control"].(map[string]any)
	second, _ := system[1].(map[string]any)["cache_control"].(map[string]any)
	if first["ttl"] != "1h" || second["ttl"] != "5m" {
		t.Fatalf("cache breakpoints: %v", system)
	}

	if _, err := c.Generate(ctx, ai.Request{Route: ai.RouteEdit, Prompt: "please refuse"}); !errors.Is(err, ai.ErrRefused) {
		t.Fatalf("refusal: err = %v", err)
	}
}

func TestMeteredBudget(t *testing.T) {
	ctx := context.Background()
	db := testutil.StartPostgres(t)
	client := ent.NewClient(ent.Driver(entsql.OpenDB(dialect.Postgres, db.SQL)))
	now := time.Date(2026, 10, 17, 12, 0, 0, 0, time.UTC)
	budget := ai.Budget{TeamMonthlyTokens: 200, Teams: map[string]int64{"rich": 0}}
	m := &ai.Metered{
		Client: stubClient{text: "ok"}, Ent: client, Now: func() time.Time { return now },
		Budget: func(context.Context) (ai.Budget, error) { return budget, nil },
	}

	// Last month's spend doesn't count.
	client.AICall.Create().SetTeamID("t1").SetRoute("fill").SetProvider("stub").SetStatus(aicall.StatusOk).
		SetInputTokens(10_000).SetCreatedAt(now.AddDate(0, -1, 0)).ExecX(ctx)

	req := ai.Request{Route: ai.RouteFill, Prompt: "x", TeamID: "t1", UserID: "u1", KitID: "k1"}
	if _, err := m.Generate(ctx, req); err != nil { // 150 used
		t.Fatal(err)
	}
	if _, err := m.Generate(ctx, req); err != nil { // 300 used: the check is before the call
		t.Fatal(err)
	}
	if _, err := m.Generate(ctx, req); !errors.Is(err, ai.ErrOverBudget) {
		t.Fatalf("third call: err = %v", err)
	}
	used, err := ai.Used(ctx, client, "t1", now)
	if err != nil || used != 300 {
		t.Fatalf("used = %d, %v", used, err)
	}
	if n := client.AICall.Query().Where(aicall.StatusEQ(aicall.StatusOverBudget), aicall.KitID("k1")).CountX(ctx); n != 1 {
		t.Fatalf("over_budget rows = %d", n)
	}
	// A team override of 0 means unlimited; calls without a team aren't limited.
	for _, team := range []string{"rich", ""} {
		for range 3 {
			if _, err := m.Generate(ctx, ai.Request{Route: ai.RouteFill, TeamID: team}); err != nil {
				t.Fatalf("team %q: %v", team, err)
			}
		}
	}
}
