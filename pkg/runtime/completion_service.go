package runtime

import (
	"context"
	"fmt"
	"time"

	"github.com/go-go-golems/geppetto/pkg/events"
	geppettoengine "github.com/go-go-golems/geppetto/pkg/inference/engine"
	"github.com/go-go-golems/llm-proxy/pkg/openaicompletions"
	"github.com/go-go-golems/llm-proxy/pkg/profiles"
)

type GeppettoCompletionService struct {
	Profiles profiles.ProfileResolver
	Engines  EngineProvider
	Mapper   openaicompletions.Mapper
	Usage    UsageRecorder
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
	if s.Usage != nil {
		s.Usage.RecordInference(ctx, req.Model, usageFrom(result), false, err)
	}
	if err != nil {
		return nil, fmt.Errorf("run inference for profile %q: %w", req.Model, err)
	}
	return s.Mapper.TurnToCompletion(req, out, result, preBlockCount)
}

func (s *GeppettoCompletionService) Stream(ctx context.Context, req *openaicompletions.CompletionRequest) (<-chan openaicompletions.CompletionStreamFrame, error) {
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
	id := openaicompletions.NewCompletionID()
	created := time.Now().Unix()
	frames := make(chan openaicompletions.CompletionStreamFrame, 64)
	sink := &openaicompletions.CompletionEventSink{ID: id, Model: req.Model, Created: created, Out: frames}
	runCtx := events.WithEventSinks(ctx, sink)
	go func() {
		defer close(frames)
		_, result, err := geppettoengine.RunInferenceWithResult(runCtx, eng, turn)
		if s.Usage != nil {
			s.Usage.RecordInference(ctx, req.Model, usageFrom(result), true, err)
		}
		if err != nil {
			frames <- openaicompletions.CompletionStreamFrame{Err: fmt.Errorf("run inference for profile %q: %w", req.Model, err)}
			return
		}
		frames <- openaicompletions.FinalFrame(id, req.Model, created, completionFinishReason(result))
	}()
	return frames, nil
}

func completionFinishReason(result *geppettoengine.InferenceResult) string {
	if result == nil {
		return "stop"
	}
	if result.Truncated || result.FinishClass == geppettoengine.InferenceFinishClassMaxTokens {
		return "length"
	}
	return "stop"
}
