package runtime

import (
	"context"
	"fmt"

	"github.com/go-go-golems/geppetto/pkg/inference/engine"
	"github.com/go-go-golems/geppetto/pkg/inference/engine/factory"
	"github.com/go-go-golems/llm-proxy/pkg/profiles"
)

type EngineProvider interface {
	EngineForProfile(ctx context.Context, profile *profiles.ResolvedProfileRuntime) (engine.Engine, error)
}

type FactoryEngineProvider struct {
	Factory factory.EngineFactory
}

func (p *FactoryEngineProvider) EngineForProfile(_ context.Context, profile *profiles.ResolvedProfileRuntime) (engine.Engine, error) {
	if profile == nil {
		return nil, fmt.Errorf("resolved profile is required")
	}
	if profile.Settings == nil {
		return nil, fmt.Errorf("profile %q has no inference settings", profile.ProfileSlug)
	}
	f := p.Factory
	if f == nil {
		f = factory.NewStandardEngineFactory()
	}
	return f.CreateEngine(profile.Settings)
}
