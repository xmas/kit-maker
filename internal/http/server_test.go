package http_test

import (
	"archive/tar"
	"compress/gzip"
	"context"
	"encoding/json"
	"fmt"
	"html"
	"io"
	nethttp "net/http"
	"net/http/cookiejar"
	"net/http/httptest"
	"regexp"
	"strings"
	"testing"
	"testing/fstest"

	"entgo.io/ent/dialect"
	entsql "entgo.io/ent/dialect/sql"
	"gocloud.dev/blob/memblob"

	"github.com/docker/kit-maker/internal/ai"
	"github.com/docker/kit-maker/internal/auth"
	"github.com/docker/kit-maker/internal/blueprints"
	"github.com/docker/kit-maker/internal/dbmigrate"
	"github.com/docker/kit-maker/internal/drivers"
	"github.com/docker/kit-maker/internal/ent"
	"github.com/docker/kit-maker/internal/ent/aicall"
	"github.com/docker/kit-maker/internal/ent/block"
	"github.com/docker/kit-maker/internal/ent/blockversion"
	"github.com/docker/kit-maker/internal/ent/blueprint"
	kmhttp "github.com/docker/kit-maker/internal/http"
	"github.com/docker/kit-maker/internal/seed"
	"github.com/docker/kit-maker/internal/testutil"
)

// fakeAssets stands in for the Vite build so tests don't need Node.
var fakeAssets = fstest.MapFS{
	".vite/manifest.json": {Data: []byte(`{
		"app.ts": {"file": "assets/app-abc123.js", "src": "app.ts", "isEntry": true},
		"styles.css": {"file": "assets/styles-def456.css", "src": "styles.css", "isEntry": true}
	}`)},
	"assets/app-abc123.js":     {Data: []byte(`console.log("kitmaker")`)},
	"assets/styles-def456.css": {Data: []byte(`body{}`)},
}

func TestServer(t *testing.T) {
	db := testutil.StartPostgres(t)
	ctx := context.Background()

	// Migrations are idempotent.
	if err := dbmigrate.Up(ctx, db.SQL, db.Pool, testutil.Logger()); err != nil {
		t.Fatalf("second migrate: %v", err)
	}

	client := ent.NewClient(ent.Driver(entsql.OpenDB(dialect.Postgres, db.SQL)))
	if err := seed.Run(ctx, client, "admin@example.com"); err != nil {
		t.Fatalf("seed: %v", err)
	}
	if err := seed.Run(ctx, client, "admin@example.com"); err != nil {
		t.Fatalf("seed is idempotent: %v", err)
	}

	authSvc := &auth.Service{Ent: client}
	if _, err := seed.BootstrapPassword(ctx, authSvc, "admin@example.com", adminPassword); err != nil {
		t.Fatalf("bootstrap password: %v", err)
	}

	bucket := memblob.OpenBucket(nil)
	t.Cleanup(func() { _ = bucket.Close() })
	h, err := kmhttp.NewHandler(kmhttp.Deps{
		Log:         testutil.Logger(),
		DB:          db.SQL,
		Ent:         client,
		Bucket:      bucket,
		Assets:      fakeAssets,
		ViteHotFile: t.TempDir() + "/hot", // absent: use the build
		Sessions:    auth.NewSessions(db.Pool, "http://localhost"),
		Auth:        authSvc,
		AI: &ai.Provider{Ent: client, Default: func(context.Context) (drivers.Driver, drivers.Config, error) {
			d, err := drivers.Get(drivers.KindAI, ai.DriverFake)
			return d, nil, err
		}},
	})
	if err != nil {
		t.Fatal(err)
	}
	srv := httptest.NewServer(h)
	t.Cleanup(srv.Close)
	anon := newClient(t, srv.URL)
	signedIn := newClient(t, srv.URL)

	t.Run("kitmaker dev signs requests in as the dev user", func(t *testing.T) {
		devHandler := func(email string) *httptest.Server {
			h, err := kmhttp.NewHandler(kmhttp.Deps{
				Log: testutil.Logger(), DB: db.SQL, Ent: client, Bucket: bucket, Assets: fakeAssets,
				ViteHotFile: t.TempDir() + "/hot", Sessions: auth.NewSessions(db.Pool, "http://localhost"),
				Auth: authSvc, DevUser: email,
			})
			if err != nil {
				t.Fatal(err)
			}
			srv := httptest.NewServer(h)
			t.Cleanup(srv.Close)
			return srv
		}
		dev := newClient(t, devHandler("Admin@Example.com").URL)
		if code, body := dev.get(t, "/blueprints", nil); code != 200 || !strings.Contains(body, "Blueprints/Index") {
			t.Fatalf("dev user: %d", code)
		}
		if code, loc := dev.noFollow(t, "GET", "/login", ""); code != nethttp.StatusSeeOther || loc != "/blueprints" {
			t.Fatalf("login page with dev user: %d → %q", code, loc)
		}
		// An unknown account signs no one in.
		nobody := newClient(t, devHandler("nobody@example.com").URL)
		if code, loc := nobody.noFollow(t, "GET", "/blueprints", ""); code != nethttp.StatusSeeOther || loc != "/login" {
			t.Fatalf("unknown dev user: %d → %q", code, loc)
		}
	})

	t.Run("signed out visitors are sent to login", func(t *testing.T) {
		code, loc := anon.noFollow(t, "GET", "/blueprints", "")
		if code != nethttp.StatusSeeOther || loc != "/login" {
			t.Fatalf("got %d → %q", code, loc)
		}
	})

	t.Run("security headers", func(t *testing.T) {
		res := anon.raw(t, "GET", "/login", "", nil)
		if csp := res.Header.Get("Content-Security-Policy"); !strings.Contains(csp, "frame-ancestors 'none'") {
			t.Errorf("CSP = %q", csp)
		}
	})

	t.Run("wrong password is refused with the same message", func(t *testing.T) {
		code, body := anon.post(t, "/login", `{"email":"admin@example.com","password":"nope-nope-nope"}`)
		if code != 200 || !strings.Contains(body, "email or password is incorrect") {
			t.Fatalf("status %d: %s", code, body)
		}
		code, body = anon.post(t, "/login", `{"email":"nobody@example.com","password":"nope-nope-nope"}`)
		if code != 200 || !strings.Contains(body, "email or password is incorrect") {
			t.Fatalf("unknown user: status %d: %s", code, body)
		}
	})

	t.Run("sign in", func(t *testing.T) {
		code, body := signedIn.post(t, "/login", `{"email":"Admin@Example.com","password":"`+adminPassword+`","next":"/blueprints"}`)
		if code != 200 || !strings.Contains(body, "Blueprints/Index") {
			t.Fatalf("status %d: %s", code, body)
		}
	})

	t.Run("healthz", func(t *testing.T) {
		code, _ := get(t, srv.URL+"/healthz", nil)
		if code != 200 {
			t.Fatalf("status %d", code)
		}
	})

	t.Run("readyz", func(t *testing.T) {
		code, body := get(t, srv.URL+"/readyz", nil)
		if code != 200 {
			t.Fatalf("status %d: %s", code, body)
		}
		var got struct {
			Status string            `json:"status"`
			Checks map[string]string `json:"checks"`
		}
		if err := json.Unmarshal([]byte(body), &got); err != nil {
			t.Fatal(err)
		}
		for _, name := range []string{"database", "migrations", "storage"} {
			if got.Checks[name] != "ok" {
				t.Errorf("check %s = %q", name, got.Checks[name])
			}
		}
	})

	t.Run("blueprints page html", func(t *testing.T) {
		code, body := signedIn.get(t, "/blueprints", nil)
		if code != 200 {
			t.Fatalf("status %d: %s", code, body)
		}
		for _, want := range []string{`data-page=`, `Blueprints/Index`, `/build/assets/app-abc123.js`, `/build/assets/styles-def456.css`, `acme-shop`} {
			if !strings.Contains(body, want) {
				t.Errorf("page missing %q", want)
			}
		}
	})

	t.Run("blueprints page inertia visit", func(t *testing.T) {
		// A stale asset version makes Inertia do a full reload.
		if code, _ := signedIn.get(t, "/blueprints", map[string]string{"X-Inertia": "true", "X-Inertia-Version": "stale"}); code != 409 {
			t.Fatalf("stale version: status %d, want 409", code)
		}
		_, html := signedIn.get(t, "/blueprints", nil)
		code, body := signedIn.get(t, "/blueprints", map[string]string{"X-Inertia": "true", "X-Inertia-Version": pageVersion(t, html)})
		if code != 200 {
			t.Fatalf("status %d: %s", code, body)
		}
		var page struct {
			Component string `json:"component"`
			Props     struct {
				Blueprints []struct {
					Slug   string `json:"slug"`
					Blanks int    `json:"blanks"`
				} `json:"blueprints"`
			} `json:"props"`
		}
		if err := json.Unmarshal([]byte(body), &page); err != nil {
			t.Fatalf("decode: %v: %s", err, body)
		}
		// The demo blueprints plus every spec example (internal/seed/examples).
		if page.Component != "Blueprints/Index" || len(page.Props.Blueprints) < 30 {
			t.Fatalf("unexpected page: %s", body)
		}
		for _, bp := range page.Props.Blueprints {
			if bp.Slug == "acme-shop" && bp.Blanks != 2 {
				t.Fatalf("acme-shop should have 2 blanks: %+v", bp)
			}
		}
	})

	t.Run("block search falls back to keywords and says so", func(t *testing.T) {
		res := signedIn.raw(t, "POST", "/blocks/search", `{"q":"jira read"}`, map[string]string{"Accept": "application/json"})
		defer res.Body.Close()
		var out struct {
			Source, Message string
			Matches         []struct{ ID, Reason string }
		}
		if err := json.NewDecoder(res.Body).Decode(&out); err != nil || res.StatusCode != 200 {
			t.Fatalf("search: %d %v", res.StatusCode, err)
		}
		// The fake AI answers with no matches, so keyword ranking takes over.
		if out.Source != "keyword" || out.Message == "" || len(out.Matches) == 0 {
			t.Fatalf("search: %+v", out)
		}
		found := false
		for _, m := range out.Matches {
			if b, _ := client.Block.Get(context.Background(), m.ID); b != nil && b.Slug == "tickets-readonly" {
				found = true
			}
		}
		if !found {
			t.Errorf("tickets-readonly not among %+v", out.Matches)
		}
	})

	t.Run("run a blueprint: folder download, sbxenv and commands", func(t *testing.T) {
		for slug, want := range map[string]string{"shell": "sbx run ./shell/", "motd": "sbx run <workload-kit> --kit ./motd/"} {
			bp := client.Blueprint.Query().Where(blueprint.Slug(slug)).OnlyX(context.Background())
			res := signedIn.raw(t, "GET", "/blueprints/"+bp.ID+"/run", "", map[string]string{"Accept": "application/json"})
			var ri struct {
				LocalCommand string
				Valid        bool
			}
			_ = json.NewDecoder(res.Body).Decode(&ri)
			res.Body.Close()
			if ri.LocalCommand != want || !ri.Valid {
				t.Fatalf("%s run info: %+v", slug, ri)
			}
			res = signedIn.raw(t, "GET", "/blueprints/"+bp.ID+"/download", "", nil)
			if res.StatusCode != 200 || !strings.Contains(res.Header.Get("Content-Disposition"), slug+".tar.gz") {
				t.Fatalf("%s download: %d %v", slug, res.StatusCode, res.Header)
			}
			gz, err := gzip.NewReader(res.Body)
			if err != nil {
				t.Fatal(err)
			}
			tr := tar.NewReader(gz)
			files := map[string]string{}
			for {
				h, err := tr.Next()
				if err != nil {
					break
				}
				b, _ := io.ReadAll(tr)
				files[h.Name] = string(b)
			}
			res.Body.Close()
			kit := files[slug+"/"+slug+".yaml"]
			if !strings.HasPrefix(kit, "# syntax=docker/sandbox-kit:3") || len(files) != 2 {
				t.Fatalf("%s archive: %v", slug, files)
			}
			res = signedIn.raw(t, "GET", "/blueprints/"+bp.ID+"/sbxenv.yaml", "", nil)
			env, _ := io.ReadAll(res.Body)
			res.Body.Close()
			agent := map[string]string{"shell": "shell", "motd": "<workload>"}[slug]
			if !strings.Contains(string(env), "kits:\n  - ./"+slug+"\n") || !strings.Contains(string(env), "agent: "+agent+"\n") {
				t.Fatalf("%s sbxenv: %s", slug, env)
			}
		}
	})

	t.Run("published versions: unchanged skips, sbxenv follows latest", func(t *testing.T) {
		ctx := context.Background()
		bp := client.Blueprint.Query().Where(blueprint.Slug("hello")).OnlyX(ctx)
		svc := &blueprints.Service{Ent: client}
		pushes := 0
		push := func(_ context.Context, slug string, version int, yaml string) (string, string, error) {
			pushes++
			if !strings.Contains(yaml, fmt.Sprintf("version: \"%d\"", version)) {
				t.Errorf("published YAML lacks version %d", version)
			}
			return "localhost:5001/blueprints/sbx-kit-" + slug, fmt.Sprintf("sha256:%064d", version), nil
		}
		v, err := svc.Publish(ctx, bp.ID, "", push)
		if err != nil || v.Version != 1 || v.Unchanged {
			t.Fatalf("first publish: %+v %v", v, err)
		}
		v, err = svc.Publish(ctx, bp.ID, "", push)
		if err != nil || !v.Unchanged || pushes != 1 {
			t.Fatalf("second publish should be unchanged: %+v %v (pushes %d)", v, err, pushes)
		}
		res := signedIn.raw(t, "GET", "/blueprints/"+bp.ID+"/sbxenv.yaml", "", nil)
		env, _ := io.ReadAll(res.Body)
		res.Body.Close()
		for _, want := range []string{"agent: sbx-kit-hello", "  - source: localhost:5001/blueprints/sbx-kit-hello:latest", "pullPolicy: always"} {
			if !strings.Contains(string(env), want) {
				t.Errorf("sbxenv missing %q:\n%s", want, env)
			}
		}
		// A mixin runs on the most recently published workload.
		motd := client.Blueprint.Query().Where(blueprint.Slug("motd")).OnlyX(ctx)
		res = signedIn.raw(t, "GET", "/blueprints/"+motd.ID+"/run", "", map[string]string{"Accept": "application/json"})
		var ri struct{ LocalCommand string }
		_ = json.NewDecoder(res.Body).Decode(&ri)
		res.Body.Close()
		if ri.LocalCommand != "sbx run localhost:5001/blueprints/sbx-kit-hello:latest --kit ./motd/" {
			t.Errorf("mixin command: %q", ri.LocalCommand)
		}
	})

	t.Run("old /kits links redirect to /blueprints", func(t *testing.T) {
		for path, want := range map[string]string{
			"/kits":                         "/blueprints",
			"/kits/new":                     "/blueprints/new",
			"/kits/kit_01ABC/slots/net?x=1": "/blueprints/bp_01ABC/slots/net?x=1",
			"/kits/kit_01ABC/history":       "/blueprints/bp_01ABC/history",
		} {
			if code, loc := signedIn.noFollow(t, "GET", path, ""); code != 301 || loc != want {
				t.Errorf("GET %s = %d %q, want 301 %q", path, code, loc, want)
			}
		}
	})

	t.Run("blueprint builder", func(t *testing.T) {
		_, html := signedIn.get(t, "/blueprints", nil)
		version := pageVersion(t, html)
		type slot struct {
			Key       string         `json:"key"`
			Origin    string         `json:"origin"`
			Edits     int            `json:"edits"`
			BlockSlug string         `json:"blockSlug"`
			Effective map[string]any `json:"effective"`
		}
		type blueprintPage struct {
			Component string `json:"component"`
			Props     struct {
				Blueprint struct {
					ID string `json:"id"`
				} `json:"blueprint"`
				Blueprints []struct {
					ID     string `json:"id"`
					Blanks int    `json:"blanks"`
				} `json:"blueprints"`
				Selected *slot `json:"selected"`
				Compiled struct {
					YAML   string   `json:"yaml"`
					Blanks []string `json:"blanks"`
				} `json:"compiled"`
				Checks     []struct{ Status string } `json:"checks"`
				Candidates []struct {
					ID   string `json:"id"`
					Slug string `json:"slug"`
				} `json:"candidates"`
				Can struct{ Edit bool } `json:"can"`
			} `json:"props"`
		}
		visit := func(path string) blueprintPage {
			t.Helper()
			code, body := signedIn.get(t, path, map[string]string{"X-Inertia": "true", "X-Inertia-Version": version})
			if code != 200 {
				t.Fatalf("GET %s: status %d: %s", path, code, body)
			}
			var p blueprintPage
			if err := json.Unmarshal([]byte(body), &p); err != nil {
				t.Fatalf("decode %s: %v", path, err)
			}
			return p
		}
		send := func(method, path, body string) {
			t.Helper()
			res := signedIn.raw(t, method, path, body, map[string]string{"X-Inertia": "true", "X-Inertia-Version": version})
			res.Body.Close()
			if res.StatusCode != 200 {
				t.Fatalf("%s %s: status %d", method, path, res.StatusCode)
			}
		}

		blueprintURL := "/blueprints/" + visit("/blueprints").Props.Blueprints[0].ID
		overview := visit(blueprintURL)
		if overview.Component != "Blueprints/Show" || overview.Props.Selected != nil || len(overview.Props.Checks) == 0 || !overview.Props.Can.Edit {
			t.Fatalf("overview: %+v", overview)
		}
		if !strings.Contains(overview.Props.Compiled.YAML, "network-policy@2") {
			t.Fatalf("yaml lacks the network policy:\n%s", overview.Props.Compiled.YAML)
		}

		egress := visit(blueprintURL + "/slots/github-egress").Props.Selected
		if egress == nil || egress.Origin != "modified" || egress.Edits != 2 {
			t.Fatalf("github-egress: %+v", egress)
		}

		// Saving the unchanged effective payload keeps the same overrides.
		payload, _ := json.Marshal(map[string]any{"payload": egress.Effective})
		send("PUT", blueprintURL+"/slots/github-egress", string(payload))
		if got := visit(blueprintURL + "/slots/github-egress").Props.Selected; got.Edits != 2 {
			t.Fatalf("after save: %+v", got)
		}

		// Reverting drops the overrides.
		send("POST", blueprintURL+"/slots/github-egress/revert", "{}")
		if got := visit(blueprintURL + "/slots/github-egress").Props.Selected; got.Origin != "inherited" || got.Edits != 0 {
			t.Fatalf("after revert: %+v", got)
		}

		// A required blank lists library blocks; linking one fills it.
		blank := visit(blueprintURL + "/slots/packagist")
		if blank.Props.Selected.BlockSlug != "" {
			t.Fatalf("packagist should be blank: %+v", blank.Props.Selected)
		}
		var blockID string
		for _, c := range blank.Props.Candidates {
			if c.Slug == "packagist-acme" {
				blockID = c.ID
			}
		}
		if blockID == "" {
			t.Fatalf("packagist-acme not offered: %+v", blank.Props.Candidates)
		}
		send("POST", blueprintURL+"/slots/packagist/link", `{"blockId":"`+blockID+`"}`)
		if got := visit(blueprintURL + "/slots/packagist").Props.Selected; got.BlockSlug != "packagist-acme" || got.Origin != "inherited" {
			t.Fatalf("after link: %+v", got)
		}
		if b := visit("/blueprints").Props.Blueprints[0].Blanks; b != 1 {
			t.Fatalf("blanks after link = %d, want 1", b)
		}

		// Explain streams server-sent events and records the call.
		res := signedIn.raw(t, "POST", blueprintURL+"/slots/github-egress/explain", `{}`, nil)
		body, _ := io.ReadAll(res.Body)
		res.Body.Close()
		if res.StatusCode != 200 || res.Header.Get("Content-Type") != "text/event-stream" ||
			!strings.Contains(string(body), "event: delta") || !strings.Contains(string(body), "event: done") ||
			!strings.Contains(string(body), `{"text":"network "}`) {
			t.Fatalf("explain: %d %q\n%s", res.StatusCode, res.Header.Get("Content-Type"), body)
		}
		if n := client.AICall.Query().Where(aicall.Route("explain")).CountX(context.Background()); n != 1 {
			t.Fatalf("explain ai_calls rows = %d, want 1", n)
		}
		if res := signedIn.raw(t, "POST", blueprintURL+"/slots/packagist-token/explain", `{}`, nil); res.StatusCode != 404 {
			t.Fatalf("explain on a blank: %d, want 404", res.StatusCode)
		}

		// Invalid payloads come back as a validation error, not a save.
		send("PUT", blueprintURL+"/slots/github-egress", `{"payload":{"runtime":{"allow":[{"hosts":[],"_k":"x"}]}}}`)
		if got := visit(blueprintURL + "/slots/github-egress").Props.Selected; got.Edits != 0 {
			t.Fatalf("invalid payload was saved: %+v", got)
		}
	})

	t.Run("polyglot demo blueprint splits a block across the policy", func(t *testing.T) {
		_, html := signedIn.get(t, "/blueprints", nil)
		k := client.Blueprint.Query().Where(blueprint.Slug("polyglot-ci")).OnlyX(context.Background())
		code, body := signedIn.get(t, "/blueprints/"+k.ID, map[string]string{"X-Inertia": "true", "X-Inertia-Version": pageVersion(t, html)})
		var page struct {
			Props struct {
				Compiled struct {
					Valid bool
					Error string
					YAML  string
					Spans map[string][][2]int
				} `json:"compiled"`
			} `json:"props"`
		}
		if err := json.Unmarshal([]byte(body), &page); code != 200 || err != nil {
			t.Fatalf("%d %v", code, err)
		}
		c := page.Props.Compiled
		if !c.Valid {
			t.Fatalf("polyglot blueprint is not valid: %s\n%s", c.Error, c.YAML)
		}
		// Node has install allow, install deny, runtime allow and runtime deny:
		// four separate places in the YAML, interleaved with other blocks.
		if n := len(c.Spans["node"]); n != 4 {
			t.Errorf("node spans = %d (%v), want 4\n%s", n, c.Spans["node"], c.YAML)
		}
		if n := len(c.Spans["acme-internal"]); n != 2 {
			t.Errorf("acme-internal spans = %d, want 2", n)
		}
	})

	t.Run("create a blueprint and fill it", func(t *testing.T) {
		_, html := signedIn.get(t, "/blueprints", nil)
		hdr := map[string]string{"X-Inertia": "true", "X-Inertia-Version": pageVersion(t, html)}
		type page struct {
			Component string `json:"component"`
			URL       string `json:"url"`
			Props     struct {
				Errors   map[string]string `json:"errors"`
				Selected *struct {
					Key, Origin, BlockSlug string
					BlockType              string `json:"blockType"`
					Version, Latest, Edits int
					Effective              map[string]any
				} `json:"selected"`
				Sections []struct{ Items []struct{ Key string } } `json:"sections"`
				Checks   []struct{ ID, Status string }            `json:"checks"`
				Compiled struct {
					YAML      string                  `json:"yaml"`
					Valid     bool                    `json:"valid"`
					Conflicts []struct{ Kind string } `json:"conflicts"`
				} `json:"compiled"`
				Block struct {
					ID      string
					Version int
					Payload map[string]any
					UsedIn  []struct{ Version int } `json:"usedIn"`
				} `json:"block"`
			} `json:"props"`
		}
		call := func(method, path, body string) page {
			t.Helper()
			res := signedIn.raw(t, method, path, body, hdr)
			defer res.Body.Close()
			b, _ := io.ReadAll(res.Body)
			if res.StatusCode != 200 {
				t.Fatalf("%s %s: %d %s", method, path, res.StatusCode, b)
			}
			var p page
			if err := json.Unmarshal(b, &p); err != nil {
				t.Fatalf("%s %s: %v: %s", method, path, err, b)
			}
			return p
		}
		teamID := client.Team.Query().OnlyX(context.Background()).ID

		if p := call("GET", "/blueprints/new", ""); p.Component != "Blueprints/New" {
			t.Fatalf("new blueprint page: %+v", p)
		}
		p := call("POST", "/blueprints", `{"teamId":"`+teamID+`","displayName":"Payments API","kind":"workload","goal":"Run the payments test suite."}`)
		if p.Component != "Blueprints/Show" || !strings.HasPrefix(p.URL, "/blueprints/bp_") || len(p.Props.Sections) != 6 || len(p.Props.Sections[1].Items) != 1 {
			t.Fatalf("after create: %+v", p)
		}
		blueprintURL := p.URL
		if p := call("POST", "/blueprints", `{"teamId":"`+teamID+`","displayName":"Payments API","kind":"workload"}`); p.Props.Errors["form"] == "" {
			t.Fatalf("duplicate slug accepted: %+v", p.Props.Errors)
		}

		// Adding without a label numbers the type's name.
		call("POST", blueprintURL+"/slots", `{"type":"network-rules","required":true}`)
		p = call("POST", blueprintURL+"/slots", `{"type":"network-rules","required":true}`)
		if p.Props.Selected == nil || p.Props.Selected.Key != "network-rules-2" {
			t.Fatalf("second unnamed network slot: %+v", p.Props.Selected)
		}
		call("DELETE", blueprintURL+"/slots/network-rules", "")
		call("DELETE", blueprintURL+"/slots/network-rules-2", "")

		// Every block type has its own slot: a Resources slot remembers its
		// type, starts a Resources block, and compiles to resources@1.
		p = call("POST", blueprintURL+"/slots", `{"type":"resources","required":true}`)
		if p.Props.Selected == nil || p.Props.Selected.Key != "resources" || p.Props.Selected.BlockType != "resources" {
			t.Fatalf("resources slot: %+v", p.Props.Selected)
		}
		p = call("POST", blueprintURL+"/slots/resources/custom", `{}`)
		if p.Props.Selected == nil || p.Props.Selected.BlockSlug == "" || !strings.Contains(p.Props.Compiled.YAML, "com.docker.sandbox/resources@1") {
			t.Fatalf("resources block: %+v\n%s", p.Props.Selected, p.Props.Compiled.YAML)
		}
		call("DELETE", blueprintURL+"/slots/resources", "")

		// Conflicts: an outright allow cancels another block's read-only
		// rule; hiding the outright allow in this blueprint resolves it.
		call("POST", blueprintURL+"/slots", `{"label":"Open internal","type":"network-rules","required":true}`)
		call("POST", blueprintURL+"/slots/open-internal/custom", `{}`)
		call("PUT", blueprintURL+"/slots/open-internal", `{"payload":{"runtime":{"allow":[{"_k":"j","hosts":["jira.acme.internal"]},{"_k":"w","hosts":["wiki.acme.internal"]}]}}}`)
		call("POST", blueprintURL+"/slots", `{"label":"Read tickets","type":"network-rules","required":true}`)
		call("POST", blueprintURL+"/slots/read-tickets/custom", `{}`)
		p = call("PUT", blueprintURL+"/slots/read-tickets", `{"payload":{"runtime":{"allow":[{"_k":"r","hosts":["jira.acme.internal"],"methods":["GET"]}]}}}`)
		if len(p.Props.Compiled.Conflicts) != 1 || p.Props.Compiled.Conflicts[0].Kind != "widened" {
			t.Fatalf("want one widened conflict, got %+v", p.Props.Compiled.Conflicts)
		}
		p = call("POST", blueprintURL+"/slots/open-internal/hide", `{"path":"/runtime/allow","key":"j","back":"`+blueprintURL+`"}`)
		if len(p.Props.Compiled.Conflicts) != 0 || p.URL != blueprintURL || !strings.Contains(p.Props.Compiled.YAML, "wiki.acme.internal") {
			t.Fatalf("after hide: %s %+v\n%s", p.URL, p.Props.Compiled.Conflicts, p.Props.Compiled.YAML)
		}
		call("DELETE", blueprintURL+"/slots/open-internal", "")
		call("DELETE", blueprintURL+"/slots/read-tickets", "")

		// Start a block with AI: the (fake) model's draft becomes its content.
		call("POST", blueprintURL+"/slots", `{"label":"AI net","type":"network-rules","required":true}`)
		p = call("POST", blueprintURL+"/slots/ai-net/custom", `{"prompt":"read the example API"}`)
		if p.Props.Selected == nil || !strings.Contains(p.Props.Compiled.YAML, "api.example.com") {
			t.Fatalf("AI-started block: %+v\n%s", p.Props.Selected, p.Props.Compiled.YAML)
		}
		// A draft for a filled slot comes back unsaved.
		dres := signedIn.raw(t, "POST", blueprintURL+"/slots/ai-net/draft", `{"prompt":"read the example API"}`, map[string]string{"Accept": "application/json"})
		var d struct {
			Payload map[string]any
			Note    string
		}
		_ = json.NewDecoder(dres.Body).Decode(&d)
		dres.Body.Close()
		if d.Payload == nil || !strings.Contains(d.Note, "Fake AI") {
			t.Fatalf("draft: %+v", d)
		}
		call("DELETE", blueprintURL+"/slots/ai-net", "")

		// Add a credential slot and fill it with a new block just for this blueprint.
		p = call("POST", blueprintURL+"/slots", `{"label":"Stripe key","type":"credential","required":true}`)
		if p.Props.Selected == nil || p.Props.Selected.Key != "stripe-key" || p.Props.Selected.BlockSlug != "" {
			t.Fatalf("after add slot: %+v", p.Props.Selected)
		}
		p = call("POST", blueprintURL+"/slots/stripe-key/custom", `{}`)
		if sel := p.Props.Selected; sel == nil || sel.Origin != "custom" || sel.BlockSlug != "payments-api-stripe-key" {
			t.Fatalf("after custom block: %+v", sel)
		}

		// Fill the starter network slot from the library, then publish v2 of
		// that block and update the blueprint to it, keeping its edit.
		egress := client.Block.Query().Where(block.Slug("github-egress")).OnlyX(context.Background())
		call("POST", blueprintURL+"/slots/network-access/link", `{"blockId":"`+egress.ID+`"}`)
		p = call("GET", blueprintURL+"/slots/network-access", "")
		eff := p.Props.Selected.Effective
		eff["runtime"].(map[string]any)["allow"] = append(eff["runtime"].(map[string]any)["allow"].([]any),
			map[string]any{"label": "Stripe", "hosts": []any{"api.stripe.com"}})
		b, _ := json.Marshal(map[string]any{"payload": eff})

		// Preview compiles the draft without saving it.
		res := signedIn.raw(t, "POST", blueprintURL+"/slots/network-access/preview", string(b), nil)
		var pv struct {
			Compiled struct {
				YAML  string
				Spans map[string][][2]int
			}
		}
		_ = json.NewDecoder(res.Body).Decode(&pv)
		res.Body.Close()
		if res.StatusCode != 200 || !strings.Contains(pv.Compiled.YAML, "api.stripe.com") || !strings.Contains(pv.Compiled.YAML, "1 edit here") {
			t.Fatalf("preview: %d\n%s", res.StatusCode, pv.Compiled.YAML)
		}
		if p := call("GET", blueprintURL+"/slots/network-access", ""); p.Props.Selected.Edits != 0 {
			t.Fatalf("preview saved the draft: %+v", p.Props.Selected)
		}

		p = call("PUT", blueprintURL+"/slots/network-access", string(b))
		if p.Props.Selected.Edits != 1 {
			t.Fatalf("blueprint edit: %+v", p.Props.Selected)
		}

		bp := call("GET", "/blocks/"+egress.ID, "")
		v := bp.Props.Block.Version
		payload := bp.Props.Block.Payload
		payload["runtime"].(map[string]any)["allow"] = append(payload["runtime"].(map[string]any)["allow"].([]any),
			map[string]any{"label": "Codeload", "hosts": []any{"codeload.github.com"}, "methods": []any{"GET"}})
		b, _ = json.Marshal(map[string]any{"payload": payload, "changelog": "Allow codeload downloads."})
		bp = call("POST", "/blocks/"+egress.ID+"/versions", string(b))
		if bp.Props.Block.Version != v+1 {
			t.Fatalf("publish: version %d, want %d (%v)", bp.Props.Block.Version, v+1, bp.Props.Errors)
		}
		if bp = call("POST", "/blocks/"+egress.ID+"/versions", string(b)); bp.Props.Block.Version != v+1 {
			t.Fatalf("an unchanged save made a version: %d", bp.Props.Block.Version)
		}

		p = call("GET", blueprintURL+"/slots/network-access", "")
		if sel := p.Props.Selected; sel.Version != v || sel.Latest != v+1 {
			t.Fatalf("before update: %+v", sel)
		}
		p = call("POST", blueprintURL+"/slots/network-access/update", `{}`)
		sel := p.Props.Selected
		if sel.Version != v+1 || sel.Edits != 1 {
			t.Fatalf("after update: %+v", sel)
		}
		hosts := fmt.Sprint(sel.Effective["runtime"])
		if !strings.Contains(hosts, "api.stripe.com") || !strings.Contains(hosts, "codeload.github.com") {
			t.Fatalf("update lost the blueprint's edit or the new version: %s", hosts)
		}

		// The YAML says which lines each slot produced.
		code, raw := signedIn.get(t, blueprintURL+"/slots/network-access", hdr)
		if code != 200 || !strings.Contains(raw, `"spans":{`) || !strings.Contains(raw, `"network-access":[[`) {
			t.Fatalf("compiled spans missing: %d", code)
		}

		// Save the slot's block plus its edit as a new library block: the slot
		// then links it with no edits, and the original block is untouched.
		p = call("POST", blueprintURL+"/slots/network-access/save-as-block", `{"name":"Payments egress"}`)
		if sel := p.Props.Selected; sel.BlockSlug != "payments-egress" || sel.Edits != 0 || sel.Version != 1 {
			t.Fatalf("after save as block: %+v (%v)", sel, p.Props.Errors)
		}
		if !strings.Contains(fmt.Sprint(p.Props.Selected.Effective), "api.stripe.com") {
			t.Fatalf("the new block lost the blueprint's edit: %v", p.Props.Selected.Effective)
		}
		if n := client.BlockVersion.Query().Where(blockversion.BlockID(egress.ID)).CountX(context.Background()); n != v+1 {
			t.Fatalf("save as block changed the original block: %d versions", n)
		}

		// Re-link the slot back to github-egress.
		p = call("POST", blueprintURL+"/slots/network-access/relink", `{"blockId":"`+egress.ID+`"}`)
		if sel := p.Props.Selected; sel.BlockSlug != "github-egress" || sel.Edits != 0 || sel.Version != v+1 {
			t.Fatalf("after relink: %+v", sel)
		}

		// Checks.
		p = call("POST", blueprintURL+"/checks", `{"method":"GET","url":"https://api.github.com/repos/acme/docs","expect":"reach"}`)
		if len(p.Props.Checks) != 1 || p.Props.Checks[0].Status != "pass" {
			t.Fatalf("after add check: %+v", p.Props.Checks)
		}
		if p := call("POST", blueprintURL+"/checks", `{"url":"not a url","expect":"reach"}`); p.Props.Errors["check"] == "" {
			t.Fatal("bad check URL accepted")
		}
		p = call("DELETE", blueprintURL+"/checks/"+p.Props.Checks[0].ID, "")
		if len(p.Props.Checks) != 0 {
			t.Fatalf("after remove check: %+v", p.Props.Checks)
		}

		// Removing the custom slot keeps its blueprint-only block (history can bring
		// it back), unlinked and out of the library.
		call("DELETE", blueprintURL+"/slots/stripe-key", "")
		if !client.Block.Query().Where(block.Slug("payments-api-stripe-key")).ExistX(context.Background()) {
			t.Fatal("blueprint-only block was deleted with its slot")
		}

		// Blocks library: create one and find it in the list.
		bp = call("POST", "/blocks", `{"teamId":"`+teamID+`","type":"network-rules","name":"npm mirror"}`)
		if bp.Component != "Blocks/Show" || bp.Props.Block.Version != 1 {
			t.Fatalf("create block: %+v", bp)
		}
		code, body := signedIn.get(t, "/blocks", hdr)
		if code != 200 || !strings.Contains(body, `"npm-mirror"`) || strings.Contains(body, "payments-api-stripe-key") {
			t.Fatalf("blocks index: %d %s", code, body)
		}
	})

	t.Run("history: record, restore a slot or the blueprint, resync", func(t *testing.T) {
		_, html := signedIn.get(t, "/blueprints", nil)
		hdr := map[string]string{"X-Inertia": "true", "X-Inertia-Version": pageVersion(t, html)}
		type rev struct {
			ID, Action, Summary, SlotKey string
			Seq                          int
			Slot                         *struct {
				BlockName string
				Version   int
				Overrides []any
			}
		}
		type page struct {
			URL   string `json:"url"`
			Props struct {
				Errors   map[string]string `json:"errors"`
				Selected *struct {
					BlockSlug      string
					Edits, Version int
					Latest         int
					Effective      map[string]any
				} `json:"selected"`
				Sections []struct{ Items []struct{ Key string } } `json:"sections"`
				History  []rev                                    `json:"history"`
			} `json:"props"`
		}
		call := func(method, path, body string) page {
			t.Helper()
			res := signedIn.raw(t, method, path, body, hdr)
			defer res.Body.Close()
			b, _ := io.ReadAll(res.Body)
			if res.StatusCode != 200 {
				t.Fatalf("%s %s: %d %s", method, path, res.StatusCode, b)
			}
			var p page
			if err := json.Unmarshal(b, &p); err != nil {
				t.Fatalf("%s %s: %v", method, path, err)
			}
			return p
		}
		history := func(blueprintURL, slot string) []rev {
			t.Helper()
			code, body := signedIn.get(t, blueprintURL+"/history.json?slot="+slot, nil)
			var out struct{ Revisions []rev }
			if err := json.Unmarshal([]byte(body), &out); code != 200 || err != nil {
				t.Fatalf("history: %d %v", code, err)
			}
			return out.Revisions
		}
		teamID := client.Team.Query().OnlyX(context.Background()).ID
		egress := client.Block.Query().Where(block.Slug("github-egress")).OnlyX(context.Background())

		blueprintURL := call("POST", "/blueprints", `{"teamId":"`+teamID+`","displayName":"History Blueprint","kind":"mixin"}`).URL
		if h := history(blueprintURL, ""); len(h) != 1 || h[0].Action != "create" {
			t.Fatalf("after create: %+v", h)
		}
		created := history(blueprintURL, "")[0]

		call("POST", blueprintURL+"/slots/network-access/link", `{"blockId":"`+egress.ID+`"}`)
		linked := history(blueprintURL, "")[0]
		eff := call("GET", blueprintURL+"/slots/network-access", "").Props.Selected.Effective
		rt := eff["runtime"].(map[string]any)
		rt["allow"] = append(rt["allow"].([]any), map[string]any{"label": "Extra", "hosts": []any{"extra.example.com"}})
		b, _ := json.Marshal(map[string]any{"payload": eff})
		call("PUT", blueprintURL+"/slots/network-access", string(b))

		slotHist := history(blueprintURL, "network-access")
		if len(slotHist) < 3 || slotHist[0].Action != "edit" || slotHist[0].Slot == nil || len(slotHist[0].Slot.Overrides) != 1 ||
			!strings.Contains(slotHist[0].Summary, "1 edit") {
			t.Fatalf("slot history: %+v", slotHist)
		}

		// The revision's YAML is kept for previews.
		code, body := signedIn.get(t, blueprintURL+"/revisions/"+slotHist[0].ID+"/yaml", nil)
		if code != 200 || !strings.Contains(body, "extra.example.com") {
			t.Fatalf("revision yaml: %d %s", code, body)
		}

		// Restore the slot to just after it was linked: the edit is gone.
		p := call("POST", blueprintURL+"/revisions/"+linked.ID+"/restore", `{"slotKey":"network-access"}`)
		if p.Props.Selected == nil || p.Props.Selected.Edits != 0 || p.Props.Selected.BlockSlug != "github-egress" {
			t.Fatalf("after slot restore: %+v", p.Props.Selected)
		}
		// …and back again to the edited state.
		p = call("POST", blueprintURL+"/revisions/"+slotHist[0].ID+"/restore", `{"slotKey":"network-access"}`)
		if p.Props.Selected.Edits != 1 {
			t.Fatalf("restore forward: %+v", p.Props.Selected)
		}

		// Resync drops the edits and takes the block's latest version.
		p = call("POST", blueprintURL+"/slots/network-access/resync", `{}`)
		if sel := p.Props.Selected; sel.Edits != 0 || sel.Version != sel.Latest {
			t.Fatalf("after resync: %+v", sel)
		}
		if h := history(blueprintURL, ""); h[0].Action != "resync" {
			t.Fatalf("resync not recorded: %+v", h[0])
		}

		// Restore the whole blueprint to when it was created: an empty starter slot.
		p = call("POST", blueprintURL+"/revisions/"+created.ID+"/restore", `{}`)
		if !strings.HasSuffix(p.URL, "/history") || len(p.Props.History) == 0 || p.Props.History[0].Action != "restore" {
			t.Fatalf("after blueprint restore: %s %+v", p.URL, p.Props.History)
		}
		p = call("GET", blueprintURL+"/slots/network-access", "")
		if p.Props.Selected.BlockSlug != "" {
			t.Fatalf("blueprint restore left the slot filled: %+v", p.Props.Selected)
		}

		// Blocks: restore an old version as the newest.
		bp := call("POST", "/blocks", `{"teamId":"`+teamID+`","type":"network-rules","name":"History block"}`)
		blockURL := bp.URL
		var bd struct {
			Props struct {
				Block struct {
					Version int
					Payload map[string]any
				}
			}
		}
		get := func() {
			_, body := signedIn.get(t, blockURL, hdr)
			_ = json.Unmarshal([]byte(body), &bd)
		}
		get()
		v1 := fmt.Sprint(bd.Props.Block.Payload)
		pl := bd.Props.Block.Payload
		pl["runtime"].(map[string]any)["allow"] = []any{map[string]any{"label": "x", "hosts": []any{"x.example.com"}}}
		b, _ = json.Marshal(map[string]any{"payload": pl})
		call("POST", blockURL+"/versions", string(b))
		call("POST", blockURL+"/versions/1/restore", `{}`)
		get()
		if bd.Props.Block.Version != 3 || fmt.Sprint(bd.Props.Block.Payload) != v1 {
			t.Fatalf("block restore: v%d %v (want v3 %s)", bd.Props.Block.Version, bd.Props.Block.Payload, v1)
		}
	})

	t.Run("cross-site writes are refused (CSRF)", func(t *testing.T) {
		// A signed-in browser tricked into posting from another site sends
		// Sec-Fetch-Site: cross-site (or a foreign Origin); both are refused
		// before any handler runs, session cookie or not.
		before := client.Blueprint.Query().CountX(context.Background())
		for _, h := range []map[string]string{
			{"Sec-Fetch-Site": "cross-site"},
			{"Origin": "https://evil.example"},
		} {
			res := signedIn.raw(t, "POST", "/blueprints", `{"teamId":"x","displayName":"Evil","kind":"workload"}`, h)
			res.Body.Close()
			if res.StatusCode != nethttp.StatusForbidden {
				t.Fatalf("%v: status %d, want 403", h, res.StatusCode)
			}
		}
		if after := client.Blueprint.Query().CountX(context.Background()); after != before {
			t.Fatal("a cross-site post created a blueprint")
		}
		// Same-origin requests still work.
		res := signedIn.raw(t, "GET", "/blueprints", "", map[string]string{"Sec-Fetch-Site": "same-origin"})
		res.Body.Close()
		if res.StatusCode != 200 {
			t.Fatalf("same-origin GET: %d", res.StatusCode)
		}
	})

	t.Run("sign out everywhere ends other sessions", func(t *testing.T) {
		other := newClient(t, srv.URL)
		if code, _ := other.post(t, "/login", `{"email":"admin@example.com","password":"`+adminPassword+`"}`); code != 200 {
			t.Fatalf("second sign-in: %d", code)
		}
		if code, _ := signedIn.post(t, "/account/sign-out-everywhere", `{}`); code != 200 {
			t.Fatalf("sign out everywhere: %d", code)
		}
		if code, loc := other.noFollow(t, "GET", "/blueprints", ""); code != nethttp.StatusSeeOther || loc != "/login" {
			t.Fatalf("other session still works: %d → %q", code, loc)
		}
	})

	t.Run("assets", func(t *testing.T) {
		code, _ := get(t, srv.URL+"/build/assets/app-abc123.js", nil)
		if code != 200 {
			t.Fatalf("status %d", code)
		}
		if code, _ := get(t, srv.URL+"/build/.vite/manifest.json", nil); code != 404 {
			t.Fatalf("manifest should not be served, got %d", code)
		}
	})
}

var versionRE = regexp.MustCompile(`"version":"([^"]*)"`)

// pageVersion reads the asset version from the page's data-page JSON.
func pageVersion(t *testing.T, body string) string {
	t.Helper()
	m := versionRE.FindStringSubmatch(html.UnescapeString(body))
	if m == nil {
		t.Fatal("no version in data-page")
	}
	return m[1]
}

const adminPassword = "correct-horse-battery"

func get(t *testing.T, url string, headers map[string]string) (int, string) {
	t.Helper()
	req, _ := nethttp.NewRequest("GET", url, nil)
	for k, v := range headers {
		req.Header.Set(k, v)
	}
	res, err := nethttp.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer res.Body.Close()
	b, _ := io.ReadAll(res.Body)
	return res.StatusCode, string(b)
}

// client is a browser-like client with its own cookie jar.
type client struct {
	base string
	c    *nethttp.Client
}

func newClient(t *testing.T, base string) *client {
	t.Helper()
	jar, err := cookiejar.New(nil)
	if err != nil {
		t.Fatal(err)
	}
	return &client{base: base, c: &nethttp.Client{Jar: jar}}
}

func (c *client) raw(t *testing.T, method, path, body string, headers map[string]string) *nethttp.Response {
	t.Helper()
	req, _ := nethttp.NewRequest(method, c.base+path, strings.NewReader(body))
	if body != "" {
		req.Header.Set("Content-Type", "application/json")
	}
	for k, v := range headers {
		req.Header.Set(k, v)
	}
	res, err := c.c.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	return res
}

func (c *client) get(t *testing.T, path string, headers map[string]string) (int, string) {
	t.Helper()
	res := c.raw(t, "GET", path, "", headers)
	defer res.Body.Close()
	b, _ := io.ReadAll(res.Body)
	return res.StatusCode, string(b)
}

// post sends an Inertia-style JSON form post and follows the redirect.
func (c *client) post(t *testing.T, path, body string) (int, string) {
	t.Helper()
	res := c.raw(t, "POST", path, body, map[string]string{"Accept": "text/html"})
	defer res.Body.Close()
	b, _ := io.ReadAll(res.Body)
	return res.StatusCode, string(b)
}

func (c *client) noFollow(t *testing.T, method, path, body string) (int, string) {
	t.Helper()
	nf := *c.c
	nf.CheckRedirect = func(*nethttp.Request, []*nethttp.Request) error { return nethttp.ErrUseLastResponse }
	req, _ := nethttp.NewRequest(method, c.base+path, strings.NewReader(body))
	res, err := nf.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer res.Body.Close()
	return res.StatusCode, res.Header.Get("Location")
}
