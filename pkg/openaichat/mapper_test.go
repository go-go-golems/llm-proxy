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
