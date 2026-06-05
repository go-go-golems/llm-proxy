package openaichat

import (
	"fmt"
	"strings"
	"time"

	"github.com/go-go-golems/geppetto/pkg/inference/engine"
	"github.com/go-go-golems/geppetto/pkg/turns"
)

type Mapper struct{}

func (Mapper) RequestToTurn(req *ChatCompletionRequest) (*turns.Turn, error) {
	if req == nil || len(req.Messages) == 0 {
		return nil, FieldError{Field: "messages", Message: "messages is required", Code: "missing_messages"}
	}
	t := &turns.Turn{ID: fmt.Sprintf("turn_proxy_%d", time.Now().UnixNano())}
	for i, msg := range req.Messages {
		if err := msg.Validate(i); err != nil {
			return nil, err
		}
		text, err := msg.ContentString()
		if err != nil {
			return nil, err
		}
		switch msg.Role {
		case "system", "developer":
			turns.AppendBlock(t, turns.NewSystemTextBlock(text))
		case "user":
			turns.AppendBlock(t, turns.NewUserTextBlock(text))
		case "assistant":
			turns.AppendBlock(t, turns.NewAssistantTextBlock(text))
		}
	}
	return t, nil
}

func (Mapper) TurnToChatCompletion(req *ChatCompletionRequest, out *turns.Turn, result *engine.InferenceResult, preBlockCount int) (*ChatCompletionResponse, error) {
	return &ChatCompletionResponse{
		ID:      fmt.Sprintf("chatcmpl_proxy_%d", time.Now().UnixNano()),
		Object:  "chat.completion",
		Created: time.Now().Unix(),
		Model:   req.Model,
		Choices: []ChatChoice{{
			Index:        0,
			Message:      ChatMessageOut{Role: "assistant", Content: generatedAssistantText(out, preBlockCount)},
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
	return &Usage{PromptTokens: result.Usage.InputTokens, CompletionTokens: result.Usage.OutputTokens, TotalTokens: result.Usage.InputTokens + result.Usage.OutputTokens}
}
