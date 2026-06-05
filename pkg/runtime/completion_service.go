package runtime

import (
	"context"
	"fmt"

	geppettoengine "github.com/go-go-golems/geppetto/pkg/inference/engine"
	"github.com/go-go-golems/llm-proxy/pkg/openaicompletions"
	"github.com/go-go-golems/llm-proxy/pkg/profiles"
)

type GeppettoCompletionService struct {
	Profiles profiles.ProfileResolver
	Engines  EngineProvider
	Mapper   openaicompletions.Mapper
}

func (s *GeppettoCompletionService) Complete(ctx context.Context, req *openaicompletions.CompletionRequest) (*openaicompletions.CompletionResponse, error) {
	if s.Profiles == nil {
		return nil, fmt.Errorf("profile resolver is required")
	}
	engines := s.Engines
	if engines == nil {
		engines = &FactoryEngineProvider{}
	}
	profile, err := s.Profiles.ResolveProfile(ctx, req.Model)
	if err != nil {
		return nil, fmt.Errorf("resolve profile %q: %w", req.Model, err)
	}
	eng, err := engines.EngineForProfile(ctx, profile)
	if err != nil {
		return nil, fmt.Errorf("create engine for profile %q: %w", req.Model, err)
	}
	turn, err := s.Mapper.RequestToTurn(req)
	if err != nil {
		return nil, err
	}
	preBlockCount := len(turn.Blocks)
	out, result, err := geppettoengine.RunInferenceWithResult(ctx, eng, turn)
	if err != nil {
		return nil, fmt.Errorf("run inference for profile %q: %w", req.Model, err)
	}
	return s.Mapper.TurnToCompletion(req, out, result, preBlockCount)
}
