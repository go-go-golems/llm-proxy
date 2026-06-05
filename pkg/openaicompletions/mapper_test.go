package openaicompletions

import (
	"encoding/json"
	"testing"

	"github.com/go-go-golems/geppetto/pkg/inference/engine"
	"github.com/go-go-golems/geppetto/pkg/turns"
)

func TestMapperRequestToTurn(t *testing.T) {
	raw, _ := json.Marshal("hello")
	req := &CompletionRequest{Model: "sonnet", Prompt: raw}
	turn, err := Mapper{}.RequestToTurn(req)
	if err != nil {
		t.Fatalf("RequestToTurn error: %v", err)
	}
	if len(turn.Blocks) != 1 {
		t.Fatalf("blocks = %d", len(turn.Blocks))
	}
	if turn.Blocks[0].Kind != turns.BlockKindUser {
		t.Fatalf("kind = %v", turn.Blocks[0].Kind)
	}
	if turn.Blocks[0].Payload[turns.PayloadKeyText] != "hello" {
		t.Fatalf("payload = %#v", turn.Blocks[0].Payload)
	}
}

func TestMapperTurnToCompletion(t *testing.T) {
	raw, _ := json.Marshal("hello")
	req := &CompletionRequest{Model: "sonnet", Prompt: raw}
	turn := &turns.Turn{}
	turns.AppendBlock(turn, turns.NewUserTextBlock("hello"))
	pre := len(turn.Blocks)
	turns.AppendBlock(turn, turns.NewAssistantTextBlock("hi "))
	turns.AppendBlock(turn, turns.NewAssistantTextBlock("there"))
	result := &engine.InferenceResult{Usage: &turns.InferenceUsage{InputTokens: 2, OutputTokens: 3}}
	resp, err := Mapper{}.TurnToCompletion(req, turn, result, pre)
	if err != nil {
		t.Fatalf("TurnToCompletion error: %v", err)
	}
	if resp.Choices[0].Text != "hi there" {
		t.Fatalf("text = %q", resp.Choices[0].Text)
	}
	if resp.Usage.TotalTokens != 5 {
		t.Fatalf("usage = %#v", resp.Usage)
	}
}
