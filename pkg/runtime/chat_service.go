package runtime

import (
	"context"
	"fmt"
	"time"

	"github.com/go-go-golems/geppetto/pkg/events"
	geppettoengine "github.com/go-go-golems/geppetto/pkg/inference/engine"
	"github.com/go-go-golems/geppetto/pkg/turns"
	"github.com/go-go-golems/llm-proxy/pkg/openaichat"
	"github.com/go-go-golems/llm-proxy/pkg/profiles"
)

type GeppettoChatCompletionService struct {
	Profiles profiles.ProfileResolver
	Engines  EngineProvider
	Mapper   openaichat.Mapper
}

func (s *GeppettoChatCompletionService) Complete(ctx context.Context, req *openaichat.ChatCompletionRequest) (*openaichat.ChatCompletionResponse, error) {
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
	return s.Mapper.TurnToChatCompletion(req, out, result, preBlockCount)
}

func (s *GeppettoChatCompletionService) Stream(ctx context.Context, req *openaichat.ChatCompletionRequest) (<-chan openaichat.ChatStreamFrame, error) {
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
	id := openaichat.NewChatCompletionID()
	created := time.Now().Unix()
	frames := make(chan openaichat.ChatStreamFrame, 64)
	sink := &openaichat.ChatEventSink{ID: id, Model: req.Model, Created: created, Out: frames}
	runCtx := events.WithEventSinks(ctx, sink)
	go func() {
		defer close(frames)
		frames <- openaichat.RoleFrame(id, req.Model, created)
		out, result, err := geppettoengine.RunInferenceWithResult(runCtx, eng, turn)
		if err != nil {
			frames <- openaichat.ChatStreamFrame{Err: fmt.Errorf("run inference for profile %q: %w", req.Model, err)}
			return
		}
		finish := chatFinishReason(result, hasGeneratedToolCalls(out, preBlockCount))
		frames <- openaichat.FinalFrame(id, req.Model, created, finish)
	}()
	return frames, nil
}

func chatFinishReason(result *geppettoengine.InferenceResult, hasToolCalls bool) string {
	if result != nil && (result.Truncated || result.FinishClass == geppettoengine.InferenceFinishClassMaxTokens) {
		return "length"
	}
	if hasToolCalls || (result != nil && result.FinishClass == geppettoengine.InferenceFinishClassToolCallsPending) {
		return "tool_calls"
	}
	return "stop"
}

func hasGeneratedToolCalls(out *turns.Turn, preBlockCount int) bool {
	if out == nil {
		return false
	}
	if preBlockCount < 0 {
		preBlockCount = 0
	}
	if preBlockCount > len(out.Blocks) {
		preBlockCount = len(out.Blocks)
	}
	for _, block := range out.Blocks[preBlockCount:] {
		if block.Kind == turns.BlockKindToolCall {
			return true
		}
	}
	return false
}
