package runtime

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/go-go-golems/geppetto/pkg/events"
	geppettoengine "github.com/go-go-golems/geppetto/pkg/inference/engine"
	geppettotools "github.com/go-go-golems/geppetto/pkg/inference/tools"
	"github.com/go-go-golems/geppetto/pkg/turns"
	"github.com/go-go-golems/llm-proxy/pkg/openaichat"
	"github.com/go-go-golems/llm-proxy/pkg/profiles"
	"github.com/invopop/jsonschema"
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
	runCtx, err := contextWithRequestTools(ctx, req)
	if err != nil {
		return nil, err
	}
	out, result, err := geppettoengine.RunInferenceWithResult(runCtx, eng, turn)
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
	toolCtx, err := contextWithRequestTools(ctx, req)
	if err != nil {
		return nil, err
	}
	id := openaichat.NewChatCompletionID()
	created := time.Now().Unix()
	frames := make(chan openaichat.ChatStreamFrame, 64)
	sink := &openaichat.ChatEventSink{ID: id, Model: req.Model, Created: created, Out: frames}
	runCtx := events.WithEventSinks(toolCtx, sink)
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

func contextWithRequestTools(ctx context.Context, req *openaichat.ChatCompletionRequest) (context.Context, error) {
	if req == nil || len(req.Tools) == 0 {
		return ctx, nil
	}
	registry := geppettotools.NewInMemoryToolRegistry()
	for i, tool := range req.Tools {
		if err := tool.Validate(i); err != nil {
			return nil, err
		}
		schema, err := jsonSchemaFromMap(tool.Function.Parameters)
		if err != nil {
			return nil, openaichat.FieldError{Field: fmt.Sprintf("tools[%d].function.parameters", i), Message: err.Error(), Code: "invalid_tool_parameters"}
		}
		if err := registry.RegisterTool(tool.Function.Name, geppettotools.ToolDefinition{
			Name:        tool.Function.Name,
			Description: tool.Function.Description,
			Parameters:  schema,
		}); err != nil {
			return nil, fmt.Errorf("register request tool %q: %w", tool.Function.Name, err)
		}
	}
	return geppettotools.WithRegistry(ctx, registry), nil
}

func jsonSchemaFromMap(parameters map[string]any) (*jsonschema.Schema, error) {
	if parameters == nil {
		return nil, nil
	}
	b, err := json.Marshal(parameters)
	if err != nil {
		return nil, fmt.Errorf("marshal JSON schema: %w", err)
	}
	var schema jsonschema.Schema
	if err := json.Unmarshal(b, &schema); err != nil {
		return nil, fmt.Errorf("decode JSON schema: %w", err)
	}
	return &schema, nil
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
