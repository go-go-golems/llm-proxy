package openaichat

import (
	"encoding/json"
	"testing"

	"github.com/go-go-golems/geppetto/pkg/inference/engine"
	"github.com/go-go-golems/geppetto/pkg/turns"
)

func rawString(s string) json.RawMessage {
	b, _ := json.Marshal(s)
	return b
}

func TestMapperRequestToTurn(t *testing.T) {
	req := &ChatCompletionRequest{Model: "sonnet", Messages: []ChatMessage{
		{Role: "system", Content: rawString("system")},
		{Role: "developer", Content: rawString("dev")},
		{Role: "user", Content: rawString("hello")},
		{Role: "assistant", Content: rawString("hi")},
	}}
	turn, err := Mapper{}.RequestToTurn(req)
	if err != nil {
		t.Fatalf("RequestToTurn error: %v", err)
	}
	if len(turn.Blocks) != 4 {
		t.Fatalf("blocks = %d", len(turn.Blocks))
	}
	if turn.Blocks[0].Kind != turns.BlockKindSystem || turn.Blocks[1].Kind != turns.BlockKindSystem || turn.Blocks[2].Kind != turns.BlockKindUser || turn.Blocks[3].Kind != turns.BlockKindLLMText {
		t.Fatalf("block kinds = %#v", turn.Blocks)
	}
}

func TestMapperRequestToTurnWithUserImageContent(t *testing.T) {
	content := json.RawMessage(`[
		{"type":"text","text":"describe this"},
		{"type":"image_url","image_url":{"url":"data:image/png;base64,UE5H","detail":"high"}}
	]`)
	req := &ChatCompletionRequest{Model: "sonnet", Messages: []ChatMessage{{Role: "user", Content: content}}}
	turn, err := Mapper{}.RequestToTurn(req)
	if err != nil {
		t.Fatalf("RequestToTurn error: %v", err)
	}
	if len(turn.Blocks) != 1 || turn.Blocks[0].Kind != turns.BlockKindUser {
		t.Fatalf("blocks = %#v", turn.Blocks)
	}
	if got, _ := turn.Blocks[0].Payload[turns.PayloadKeyText].(string); got != "describe this" {
		t.Fatalf("text = %q", got)
	}
	images, ok := turn.Blocks[0].Payload[turns.PayloadKeyImages].([]map[string]any)
	if !ok || len(images) != 1 {
		t.Fatalf("images = %#v", turn.Blocks[0].Payload[turns.PayloadKeyImages])
	}
	if images[0]["url"] != "data:image/png;base64,UE5H" || images[0]["media_type"] != "image/png" || images[0]["detail"] != "high" {
		t.Fatalf("image payload = %#v", images[0])
	}
}

func TestMapperTurnToChatCompletion(t *testing.T) {
	req := &ChatCompletionRequest{Model: "sonnet", Messages: []ChatMessage{{Role: "user", Content: rawString("hello")}}}
	turn := &turns.Turn{}
	turns.AppendBlock(turn, turns.NewUserTextBlock("hello"))
	pre := len(turn.Blocks)
	turns.AppendBlock(turn, turns.NewAssistantTextBlock("hi "))
	turns.AppendBlock(turn, turns.NewAssistantTextBlock("there"))
	result := &engine.InferenceResult{Usage: &turns.InferenceUsage{InputTokens: 2, OutputTokens: 3}}
	resp, err := Mapper{}.TurnToChatCompletion(req, turn, result, pre)
	if err != nil {
		t.Fatalf("TurnToChatCompletion error: %v", err)
	}
	if resp.Object != "chat.completion" || resp.Choices[0].Message.Content != "hi there" {
		t.Fatalf("response = %#v", resp)
	}
	if resp.Usage.TotalTokens != 5 {
		t.Fatalf("usage = %#v", resp.Usage)
	}
}

func TestMapperAttachesToolsAndMapsToolMessages(t *testing.T) {
	req := &ChatCompletionRequest{
		Model: "sonnet",
		Tools: []ChatTool{{Type: "function", Function: ChatToolFunction{Name: "lookup", Description: "Lookup data", Parameters: map[string]any{"type": "object"}}}},
		Messages: []ChatMessage{
			{Role: "user", Content: rawString("use a tool")},
			{Role: "assistant", Content: json.RawMessage("null"), ToolCalls: []ChatToolCall{{ID: "call_1", Type: "function", Function: ChatToolCallFunction{Name: "lookup", Arguments: `{"q":"x"}`}}}},
			{Role: "tool", ToolCallID: "call_1", Content: rawString(`{"ok":true}`)},
		},
	}
	turn, err := Mapper{}.RequestToTurn(req)
	if err != nil {
		t.Fatalf("RequestToTurn error: %v", err)
	}
	if len(turn.Blocks) != 3 {
		t.Fatalf("blocks = %d", len(turn.Blocks))
	}
	if turn.Blocks[1].Kind != turns.BlockKindToolCall || turn.Blocks[2].Kind != turns.BlockKindToolUse {
		t.Fatalf("tool blocks = %#v", turn.Blocks)
	}
	defs, ok, err := engine.KeyToolDefinitions.Get(turn.Data)
	if err != nil || !ok || len(defs) != 1 || defs[0].Name != "lookup" {
		t.Fatalf("tool definitions ok=%v err=%v defs=%#v", ok, err, defs)
	}
	cfg, ok, err := engine.KeyToolConfig.Get(turn.Data)
	if err != nil || !ok || !cfg.Enabled || cfg.ToolChoice != engine.ToolChoiceAuto {
		t.Fatalf("tool config ok=%v err=%v cfg=%#v", ok, err, cfg)
	}
}

func TestMapperTurnToChatCompletionWithToolCalls(t *testing.T) {
	req := &ChatCompletionRequest{Model: "sonnet", Messages: []ChatMessage{{Role: "user", Content: rawString("hello")}}}
	turn := &turns.Turn{}
	turns.AppendBlock(turn, turns.NewUserTextBlock("hello"))
	pre := len(turn.Blocks)
	turns.AppendBlock(turn, turns.NewToolCallBlock("call_1", "lookup", map[string]any{"q": "x"}))
	result := &engine.InferenceResult{FinishClass: engine.InferenceFinishClassToolCallsPending}
	resp, err := Mapper{}.TurnToChatCompletion(req, turn, result, pre)
	if err != nil {
		t.Fatalf("TurnToChatCompletion error: %v", err)
	}
	if resp.Choices[0].FinishReason != "tool_calls" {
		t.Fatalf("finish = %q", resp.Choices[0].FinishReason)
	}
	if len(resp.Choices[0].Message.ToolCalls) != 1 {
		t.Fatalf("tool calls = %#v", resp.Choices[0].Message.ToolCalls)
	}
	if resp.Choices[0].Message.ToolCalls[0].Function.Arguments != `{"q":"x"}` {
		t.Fatalf("arguments = %q", resp.Choices[0].Message.ToolCalls[0].Function.Arguments)
	}
}

func TestMapperRejectsNamedFunctionToolChoice(t *testing.T) {
	req := &ChatCompletionRequest{
		Model:      "sonnet",
		ToolChoice: json.RawMessage(`{"type":"function","function":{"name":"lookup"}}`),
		Tools: []ChatTool{
			{Type: "function", Function: ChatToolFunction{Name: "lookup"}},
			{Type: "function", Function: ChatToolFunction{Name: "other"}},
		},
		Messages: []ChatMessage{{Role: "user", Content: rawString("hello")}},
	}
	_, err := Mapper{}.RequestToTurn(req)
	if err == nil {
		t.Fatalf("expected named function tool_choice error")
	}
	fieldErr, ok := err.(FieldError)
	if !ok {
		t.Fatalf("expected FieldError, got %T: %v", err, err)
	}
	if fieldErr.Field != "tool_choice" || fieldErr.Code != "unsupported_tool_choice" {
		t.Fatalf("field error = %#v", fieldErr)
	}
}
