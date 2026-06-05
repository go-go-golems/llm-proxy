package openaicompletions

import (
	"fmt"
	"strings"
	"time"

	"github.com/go-go-golems/geppetto/pkg/inference/engine"
	"github.com/go-go-golems/geppetto/pkg/turns"
)

type Mapper struct{}

func (Mapper) RequestToTurn(req *CompletionRequest) (*turns.Turn, error) {
	prompt, err := req.PromptString()
	if err != nil {
		return nil, err
	}
	if strings.TrimSpace(prompt) == "" {
		return nil, FieldError{Field: "prompt", Message: "prompt is required", Code: "missing_prompt"}
	}
	t := &turns.Turn{ID: fmt.Sprintf("turn_proxy_%d", time.Now().UnixNano())}
	turns.AppendBlock(t, turns.NewUserTextBlock(prompt))
	return t, nil
}

func (Mapper) TurnToCompletion(req *CompletionRequest, out *turns.Turn, result *engine.InferenceResult, preBlockCount int) (*CompletionResponse, error) {
	return &CompletionResponse{
		ID:      fmt.Sprintf("cmpl_proxy_%d", time.Now().UnixNano()),
		Object:  "text_completion",
		Created: time.Now().Unix(),
		Model:   req.Model,
		Choices: []CompletionChoice{{
			Text:         generatedAssistantText(out, preBlockCount),
			Index:        0,
			Logprobs:     nil,
			FinishReason: finishReason(result),
		}},
		Usage: usageFromResult(result),
	}, nil
}

func generatedAssistantText(t *turns.Turn, preBlockCount int) string {
	if t == nil {
		return ""
	}
	if preBlockCount < 0 {
		preBlockCount = 0
	}
	if preBlockCount > len(t.Blocks) {
		preBlockCount = len(t.Blocks)
	}
	var b strings.Builder
	for _, block := range t.Blocks[preBlockCount:] {
		if block.Kind != turns.BlockKindLLMText && block.Role != turns.RoleAssistant {
			continue
		}
		if text, ok := block.Payload[turns.PayloadKeyText].(string); ok {
			b.WriteString(text)
		}
	}
	return b.String()
}

func finishReason(result *engine.InferenceResult) string {
	if result == nil {
		return "stop"
	}
	if result.Truncated || result.FinishClass == engine.InferenceFinishClassMaxTokens {
		return "length"
	}
	return "stop"
}

func usageFromResult(result *engine.InferenceResult) *Usage {
	if result == nil || result.Usage == nil {
		return nil
	}
	return &Usage{
		PromptTokens:     result.Usage.InputTokens,
		CompletionTokens: result.Usage.OutputTokens,
		TotalTokens:      result.Usage.InputTokens + result.Usage.OutputTokens,
	}
}
