package runtime

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/go-go-golems/geppetto/pkg/events"
	"github.com/go-go-golems/geppetto/pkg/inference/engine"
	"github.com/go-go-golems/geppetto/pkg/turns"
	"github.com/go-go-golems/llm-proxy/pkg/openaichat"
)

func chatRawString(s string) json.RawMessage {
	b, _ := json.Marshal(s)
	return b
}

type chatAppendEngine struct{}

func (chatAppendEngine) RunInference(_ context.Context, t *turns.Turn) (*turns.Turn, error) {
	turns.AppendBlock(t, turns.NewAssistantTextBlock("hello from chat"))
	return t, nil
}

func TestGeppettoChatCompletionServiceComplete(t *testing.T) {
	svc := &GeppettoChatCompletionService{Profiles: fakeProfileResolver{}, Engines: fakeEngineProvider{eng: chatAppendEngine{}}}
	resp, err := svc.Complete(context.Background(), &openaichat.ChatCompletionRequest{Model: "sonnet", Messages: []openaichat.ChatMessage{{Role: "user", Content: chatRawString("hello")}}})
	if err != nil {
		t.Fatalf("Complete error: %v", err)
	}
	if resp.Model != "sonnet" || resp.Choices[0].Message.Content != "hello from chat" {
		t.Fatalf("response = %#v", resp)
	}
}

type chatToolEngine struct{}

func (chatToolEngine) RunInference(_ context.Context, t *turns.Turn) (*turns.Turn, error) {
	turns.AppendBlock(t, turns.NewToolCallBlock("call_1", "lookup", map[string]any{"q": "x"}))
	return t, nil
}

func TestGeppettoChatCompletionServiceCompleteToolCall(t *testing.T) {
	svc := &GeppettoChatCompletionService{Profiles: fakeProfileResolver{}, Engines: fakeEngineProvider{eng: chatToolEngine{}}}
	resp, err := svc.Complete(context.Background(), &openaichat.ChatCompletionRequest{
		Model:    "sonnet",
		Tools:    []openaichat.ChatTool{{Type: "function", Function: openaichat.ChatToolFunction{Name: "lookup", Parameters: map[string]any{"type": "object"}}}},
		Messages: []openaichat.ChatMessage{{Role: "user", Content: chatRawString("hello")}},
	})
	if err != nil {
		t.Fatalf("Complete error: %v", err)
	}
	if resp.Choices[0].FinishReason != "tool_calls" || len(resp.Choices[0].Message.ToolCalls) != 1 {
		t.Fatalf("response = %#v", resp)
	}
}

type chatStreamingEngine struct{}

func (chatStreamingEngine) RunInference(ctx context.Context, t *turns.Turn) (*turns.Turn, error) {
	events.PublishEventToContext(ctx, events.NewTextDeltaEvent(events.EventMetadata{TurnID: t.ID}, events.Correlation{}, "he", "he", 1))
	events.PublishEventToContext(ctx, events.NewTextDeltaEvent(events.EventMetadata{TurnID: t.ID}, events.Correlation{}, "llo", "hello", 2))
	turns.AppendBlock(t, turns.NewAssistantTextBlock("hello"))
	return t, nil
}

func TestGeppettoChatCompletionServiceStream(t *testing.T) {
	svc := &GeppettoChatCompletionService{Profiles: fakeProfileResolver{}, Engines: fakeEngineProvider{eng: chatStreamingEngine{}}}
	frames, err := svc.Stream(context.Background(), &openaichat.ChatCompletionRequest{Model: "sonnet", Messages: []openaichat.ChatMessage{{Role: "user", Content: chatRawString("hello")}}, Stream: true})
	if err != nil {
		t.Fatalf("Stream error: %v", err)
	}
	var sawRole, sawText bool
	var finish string
	for frame := range frames {
		if frame.Err != nil {
			t.Fatalf("frame error: %v", frame.Err)
		}
		if frame.Chunk == nil {
			continue
		}
		delta := frame.Chunk.Choices[0].Delta
		if delta.Role == "assistant" {
			sawRole = true
		}
		if delta.Content != "" {
			sawText = true
		}
		if frame.Chunk.Choices[0].FinishReason != nil {
			finish = *frame.Chunk.Choices[0].FinishReason
		}
	}
	if !sawRole || !sawText || finish != "stop" {
		t.Fatalf("sawRole=%v sawText=%v finish=%q", sawRole, sawText, finish)
	}
}

type chatStreamingToolEngine struct{}

func (chatStreamingToolEngine) RunInference(ctx context.Context, t *turns.Turn) (*turns.Turn, error) {
	events.PublishEventToContext(ctx, events.NewToolCallStartedEvent(events.EventMetadata{TurnID: t.ID}, events.Correlation{}, "call_1", "lookup"))
	events.PublishEventToContext(ctx, events.NewToolCallArgumentsDeltaEvent(events.EventMetadata{TurnID: t.ID}, events.Correlation{}, "call_1", `{"q":`, `{"q":`, 1))
	events.PublishEventToContext(ctx, events.NewToolCallArgumentsDeltaEvent(events.EventMetadata{TurnID: t.ID}, events.Correlation{}, "call_1", `"x"}`, `{"q":"x"}`, 2))
	turns.AppendBlock(t, turns.NewToolCallBlock("call_1", "lookup", map[string]any{"q": "x"}))
	return t, nil
}

func TestGeppettoChatCompletionServiceStreamToolCall(t *testing.T) {
	svc := &GeppettoChatCompletionService{Profiles: fakeProfileResolver{}, Engines: fakeEngineProvider{eng: chatStreamingToolEngine{}}}
	frames, err := svc.Stream(context.Background(), &openaichat.ChatCompletionRequest{Model: "sonnet", Tools: []openaichat.ChatTool{{Type: "function", Function: openaichat.ChatToolFunction{Name: "lookup"}}}, Messages: []openaichat.ChatMessage{{Role: "user", Content: chatRawString("hello")}}, Stream: true})
	if err != nil {
		t.Fatalf("Stream error: %v", err)
	}
	var sawTool bool
	var finish string
	for frame := range frames {
		if frame.Err != nil {
			t.Fatalf("frame error: %v", frame.Err)
		}
		if frame.Chunk == nil {
			continue
		}
		if len(frame.Chunk.Choices[0].Delta.ToolCalls) > 0 {
			sawTool = true
		}
		if frame.Chunk.Choices[0].FinishReason != nil {
			finish = *frame.Chunk.Choices[0].FinishReason
		}
	}
	if !sawTool || finish != "tool_calls" {
		t.Fatalf("sawTool=%v finish=%q", sawTool, finish)
	}
}

var _ = engine.InferenceFinishClassCompleted
