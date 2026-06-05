package openaichat

import (
	"encoding/json"
	"fmt"
	"strings"
	"time"

	geppettoengine "github.com/go-go-golems/geppetto/pkg/inference/engine"
	"github.com/go-go-golems/geppetto/pkg/turns"
)

type Mapper struct{}

func (Mapper) RequestToTurn(req *ChatCompletionRequest) (*turns.Turn, error) {
	if req == nil || len(req.Messages) == 0 {
		return nil, FieldError{Field: "messages", Message: "messages is required", Code: "missing_messages"}
	}
	t := &turns.Turn{ID: fmt.Sprintf("turn_proxy_%d", time.Now().UnixNano())}
	if err := attachTools(t, req); err != nil {
		return nil, err
	}
	for i, msg := range req.Messages {
		if err := msg.Validate(i); err != nil {
			return nil, err
		}
		switch msg.Role {
		case "system", "developer":
			text, _ := msg.ContentString()
			turns.AppendBlock(t, turns.NewSystemTextBlock(text))
		case "user":
			text, _ := msg.ContentString()
			turns.AppendBlock(t, turns.NewUserTextBlock(text))
		case "assistant":
			if len(msg.Content) != 0 && string(msg.Content) != "null" {
				text, _ := msg.ContentString()
				if text != "" {
					turns.AppendBlock(t, turns.NewAssistantTextBlock(text))
				}
			}
			for _, tc := range msg.ToolCalls {
				turns.AppendBlock(t, turns.NewToolCallBlock(tc.ID, tc.Function.Name, tc.Function.Arguments))
			}
		case "tool":
			text, _ := msg.ContentString()
			turns.AppendBlock(t, turns.NewToolUseBlock(msg.ToolCallID, text))
		}
	}
	return t, nil
}

func attachTools(t *turns.Turn, req *ChatCompletionRequest) error {
	if len(req.Tools) == 0 {
		return nil
	}
	defs := make(geppettoengine.ToolDefinitions, 0, len(req.Tools))
	for i, tool := range req.Tools {
		if err := tool.Validate(i); err != nil {
			return err
		}
		defs = append(defs, geppettoengine.ToolDefinitionSnapshot{
			Name:        tool.Function.Name,
			Description: tool.Function.Description,
			Parameters:  tool.Function.Parameters,
		})
	}
	if err := geppettoengine.KeyToolDefinitions.Set(&t.Data, defs); err != nil {
		return fmt.Errorf("attach tool definitions: %w", err)
	}
	cfg := geppettoengine.ToolConfig{Enabled: true, ToolChoice: geppettoengine.ToolChoiceAuto}
	if choice, ok, err := parseToolChoice(req.ToolChoice); err != nil {
		return err
	} else if ok {
		cfg.ToolChoice = choice
		if choice == geppettoengine.ToolChoiceNone {
			cfg.Enabled = false
		}
	}
	if err := geppettoengine.KeyToolConfig.Set(&t.Data, cfg); err != nil {
		return fmt.Errorf("attach tool config: %w", err)
	}
	return nil
}

func parseToolChoice(raw json.RawMessage) (geppettoengine.ToolChoice, bool, error) {
	if len(raw) == 0 || string(raw) == "null" {
		return "", false, nil
	}
	var s string
	if err := json.Unmarshal(raw, &s); err == nil {
		switch s {
		case "auto":
			return geppettoengine.ToolChoiceAuto, true, nil
		case "none":
			return geppettoengine.ToolChoiceNone, true, nil
		case "required":
			return geppettoengine.ToolChoiceRequired, true, nil
		default:
			return "", false, FieldError{Field: "tool_choice", Message: fmt.Sprintf("unsupported tool_choice %q", s), Code: "unsupported_tool_choice"}
		}
	}
	var obj struct {
		Type string `json:"type"`
	}
	if err := json.Unmarshal(raw, &obj); err != nil {
		return "", false, FieldError{Field: "tool_choice", Message: "tool_choice must be a string or object", Code: "unsupported_tool_choice"}
	}
	if obj.Type == "function" {
		return "", false, FieldError{Field: "tool_choice", Message: "named function tool_choice is not supported in this prototype", Code: "unsupported_tool_choice"}
	}
	return "", false, FieldError{Field: "tool_choice", Message: fmt.Sprintf("unsupported tool_choice type %q", obj.Type), Code: "unsupported_tool_choice"}
}

func (Mapper) TurnToChatCompletion(req *ChatCompletionRequest, out *turns.Turn, result *geppettoengine.InferenceResult, preBlockCount int) (*ChatCompletionResponse, error) {
	text := generatedAssistantText(out, preBlockCount)
	toolCalls := generatedToolCalls(out, preBlockCount)
	return &ChatCompletionResponse{
		ID:      NewChatCompletionID(),
		Object:  "chat.completion",
		Created: time.Now().Unix(),
		Model:   req.Model,
		Choices: []ChatChoice{{
			Index:        0,
			Message:      ChatMessageOut{Role: "assistant", Content: text, ToolCalls: toolCalls},
			FinishReason: finishReason(result, len(toolCalls) > 0),
		}},
		Usage: usageFromResult(result),
	}, nil
}

func generatedAssistantText(t *turns.Turn, preBlockCount int) string {
	if t == nil {
		return ""
	}
	preBlockCount = clampPreBlockCount(preBlockCount, len(t.Blocks))
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

func generatedToolCalls(t *turns.Turn, preBlockCount int) []ChatToolCall {
	if t == nil {
		return nil
	}
	preBlockCount = clampPreBlockCount(preBlockCount, len(t.Blocks))
	var ret []ChatToolCall
	for _, block := range t.Blocks[preBlockCount:] {
		if block.Kind != turns.BlockKindToolCall {
			continue
		}
		id, _ := block.Payload[turns.PayloadKeyID].(string)
		name, _ := block.Payload[turns.PayloadKeyName].(string)
		args := argumentsString(block.Payload[turns.PayloadKeyArgs])
		ret = append(ret, ChatToolCall{ID: id, Type: "function", Function: ChatToolCallFunction{Name: name, Arguments: args}})
	}
	return ret
}

func argumentsString(v any) string {
	switch x := v.(type) {
	case nil:
		return "{}"
	case string:
		if strings.TrimSpace(x) == "" {
			return "{}"
		}
		return x
	case json.RawMessage:
		if len(x) == 0 {
			return "{}"
		}
		return string(x)
	default:
		b, err := json.Marshal(x)
		if err != nil || len(b) == 0 {
			return "{}"
		}
		return string(b)
	}
}

func clampPreBlockCount(preBlockCount, blockCount int) int {
	if preBlockCount < 0 {
		return 0
	}
	if preBlockCount > blockCount {
		return blockCount
	}
	return preBlockCount
}

func finishReason(result *geppettoengine.InferenceResult, hasToolCalls bool) string {
	if result != nil && (result.Truncated || result.FinishClass == geppettoengine.InferenceFinishClassMaxTokens) {
		return "length"
	}
	if hasToolCalls || (result != nil && result.FinishClass == geppettoengine.InferenceFinishClassToolCallsPending) {
		return "tool_calls"
	}
	return "stop"
}

func usageFromResult(result *geppettoengine.InferenceResult) *Usage {
	if result == nil || result.Usage == nil {
		return nil
	}
	return &Usage{PromptTokens: result.Usage.InputTokens, CompletionTokens: result.Usage.OutputTokens, TotalTokens: result.Usage.InputTokens + result.Usage.OutputTokens}
}

func NewChatCompletionID() string {
	return fmt.Sprintf("chatcmpl_proxy_%d", time.Now().UnixNano())
}
