package seed

import (
	"context"
	"fmt"

	"github.com/docker/kit-maker/internal/ai"
	"github.com/docker/kit-maker/internal/drivers"
	"github.com/docker/kit-maker/internal/ent/integration"
	"github.com/docker/kit-maker/internal/settings"
)

// LocalStack describes the compose stack's own services, from env.
type LocalStack struct {
	RegistryHost string // KITMAKER_REGISTRY_URL, e.g. zot:5000 (where BuildKit pushes)
	// RegistryPullHost is where people pull the same registry from, when
	// that differs (KITMAKER_REGISTRY_PULL_HOST, e.g. localhost:5001).
	RegistryPullHost string
	// RegistryInsecure is false when the local registry serves TLS
	// (KITMAKER_REGISTRY_INSECURE; the stack's zot is plain HTTP by default).
	RegistryInsecure bool
	BuildKitAddr     string // KITMAKER_BUILDKIT_ADDR, e.g. tcp://buildkit:1234
}

// Integrations creates default integrations for kinds that have none yet:
// the local registry and BuildKit when the stack provides them, and the fake
// AI driver. The stack-owned local-zot and local-buildkit are also kept in
// step with the stack's addresses, which differ between make up (inside the
// stack) and make dev (the app on the host); other integrations are never
// touched.
func Integrations(ctx context.Context, s *settings.Service, local LocalStack) error {
	want := []settings.SaveInput{{Kind: drivers.KindAI, Driver: ai.DriverFake, Name: "fake", IsDefault: true}}
	if local.RegistryHost != "" {
		want = append(want, settings.SaveInput{
			Kind: drivers.KindRegistry, Driver: "zot", Name: "local-zot", IsDefault: true,
			Config: drivers.Config{"host": local.RegistryHost, "pull_host": local.RegistryPullHost, "namespace": "blueprints", "insecure": fmt.Sprint(local.RegistryInsecure)},
		})
	}
	if local.BuildKitAddr != "" {
		want = append(want, settings.SaveInput{
			Kind: drivers.KindBuildRunner, Driver: "buildkit", Name: "local-buildkit", IsDefault: true,
			Config: drivers.Config{"address": local.BuildKitAddr},
		})
	} else {
		want = append(want, settings.SaveInput{Kind: drivers.KindBuildRunner, Driver: "none", Name: "none", IsDefault: true})
	}
	for _, in := range want {
		if in.Name == "local-zot" || in.Name == "local-buildkit" {
			own, err := s.Ent.Integration.Query().Where(integration.KindEQ(integration.Kind(in.Kind)), integration.Name(in.Name)).Only(ctx)
			if err == nil {
				in.IsDefault = own.IsDefault
				if _, err := s.Save(ctx, in); err != nil {
					return fmt.Errorf("update %s: %w", in.Name, err)
				}
				continue
			}
		}
		n, err := s.Ent.Integration.Query().Where(integration.KindEQ(integration.Kind(in.Kind))).Count(ctx)
		if err != nil {
			return err
		}
		if n > 0 {
			continue
		}
		if _, err := s.Save(ctx, in); err != nil {
			return fmt.Errorf("seed %s integration: %w", in.Kind, err)
		}
	}
	return nil
}
