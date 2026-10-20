package settings_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"entgo.io/ent/dialect"
	entsql "entgo.io/ent/dialect/sql"

	"github.com/docker/kit-maker/internal/drivers"
	"github.com/docker/kit-maker/internal/ent"
	"github.com/docker/kit-maker/internal/secrets"
	"github.com/docker/kit-maker/internal/settings"
	"github.com/docker/kit-maker/internal/testutil"
)

func newService(t *testing.T) *settings.Service {
	t.Helper()
	ctx := context.Background()
	db := testutil.StartPostgres(t)
	url, err := secrets.NewLocalKeyURL()
	if err != nil {
		t.Fatal(err)
	}
	k, err := secrets.Open(ctx, url)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = k.Close() })
	client := ent.NewClient(ent.Driver(entsql.OpenDB(dialect.Postgres, db.SQL)))
	return &settings.Service{Ent: client, Keeper: k}
}

func TestIntegrations(t *testing.T) {
	ctx := context.Background()
	s := newService(t)

	// A fake OCI registry: /v2/ answers 200 only with the right credentials.
	reg := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if u, p, ok := r.BasicAuth(); r.URL.Path == "/v2/" && ok && u == "bot" && p == "s3cret" {
			return
		}
		w.Header().Set("Docker-Distribution-Api-Version", "registry/2.0")
		w.WriteHeader(http.StatusUnauthorized)
	}))
	defer reg.Close()
	host := strings.TrimPrefix(reg.URL, "http://")

	t.Run("validate rejects missing required fields", func(t *testing.T) {
		_, err := s.Save(ctx, settings.SaveInput{Kind: drivers.KindRegistry, Driver: "zot", Config: drivers.Config{"host": host}})
		if err == nil || !strings.Contains(err.Error(), "namespace") {
			t.Fatalf("want missing namespace error, got %v", err)
		}
	})

	t.Run("test failure blocks the save", func(t *testing.T) {
		_, err := s.Save(ctx, settings.SaveInput{
			Kind: drivers.KindRegistry, Driver: "zot", Name: "bad",
			Config: drivers.Config{"host": host, "namespace": "blueprints", "insecure": "true", "username": "bot", "password": "wrong"},
			Test:   true,
		})
		if err == nil || !strings.Contains(err.Error(), "401") {
			t.Fatalf("want 401 test failure, got %v", err)
		}
	})

	var regID string
	t.Run("save, seal, redact and default", func(t *testing.T) {
		i, err := s.Save(ctx, settings.SaveInput{
			Kind: drivers.KindRegistry, Driver: "zot", Name: "local",
			Config:    drivers.Config{"host": host, "namespace": "blueprints", "insecure": "true", "username": "bot", "password": "s3cret"},
			IsDefault: true, Test: true,
		})
		if err != nil {
			t.Fatal(err)
		}
		regID = i.ID
		if i.VerifiedAt == nil {
			t.Error("want verified_at after a passing test")
		}
		if i.Config["password"] == "s3cret" {
			t.Error("password must be redacted in views")
		}
		raw, err := s.Ent.Integration.Get(ctx, i.ID)
		if err != nil {
			t.Fatal(err)
		}
		if strings.Contains(string(raw.ConfigSealed), "s3cret") {
			t.Error("config must be sealed at rest")
		}
		d, cfg, err := s.Default(ctx, drivers.KindRegistry)
		if err != nil || d.Name() != "zot" || cfg["password"] != "s3cret" {
			t.Fatalf("Default = %v %v %v", d, cfg, err)
		}
	})

	t.Run("a new default clears the old one", func(t *testing.T) {
		if _, err := s.Save(ctx, settings.SaveInput{
			Kind: drivers.KindRegistry, Driver: "ghcr", Name: "ghcr",
			Config: drivers.Config{"namespace": "acme"}, IsDefault: true,
		}); err != nil {
			t.Fatal(err)
		}
		list, err := s.List(ctx)
		if err != nil {
			t.Fatal(err)
		}
		defaults := 0
		for _, i := range list {
			if i.Kind == drivers.KindRegistry && i.IsDefault {
				defaults++
				if i.Name != "ghcr" {
					t.Errorf("default is %s, want ghcr", i.Name)
				}
			}
		}
		if defaults != 1 {
			t.Errorf("want exactly one default registry, got %d", defaults)
		}
	})

	t.Run("test records failures", func(t *testing.T) {
		reg.Close()
		i, err := s.Test(ctx, regID)
		if err != nil {
			t.Fatal(err)
		}
		if i.LastError == "" || i.VerifiedAt != nil {
			t.Errorf("want a recorded failure, got %+v", i)
		}
	})

	t.Run("settings round-trip sealed", func(t *testing.T) {
		type branding struct{ Name string }
		if err := s.SetSetting(ctx, "branding", branding{Name: "Acme"}); err != nil {
			t.Fatal(err)
		}
		if err := s.SetSetting(ctx, "branding", branding{Name: "Acme Corp"}); err != nil {
			t.Fatal(err)
		}
		var got branding
		ok, err := s.GetSetting(ctx, "branding", &got)
		if err != nil || !ok || got.Name != "Acme Corp" {
			t.Fatalf("GetSetting = %v %v %+v", ok, err, got)
		}
		ok, err = s.GetSetting(ctx, "missing", &got)
		if err != nil || ok {
			t.Fatalf("missing setting: ok=%v err=%v", ok, err)
		}
	})
}
