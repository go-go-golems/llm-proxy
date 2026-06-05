package openaichat

import (
	"testing"

	"github.com/go-go-golems/geppetto/pkg/events"
)

func TestChatEventSinkSuppressesRequestedArgumentsAfterDeltas(t *testing.T) {
	ch := make(chan ChatStreamFrame, 4)
	sink := &ChatEventSink{ID: "chatcmpl_1", Model: "sonnet", Created: 123, Out: ch}
	metadata := events.EventMetadata{}
	corr := events.Correlation{}

	if err := sink.PublishEvent(events.NewToolCallStartedEvent(metadata, corr, "call_1", "lookup")); err != nil {
		t.Fatalf("PublishEvent start error: %v", err)
	}
	if err := sink.PublishEvent(events.NewToolCallArgumentsDeltaEvent(metadata, corr, "call_1", `{"q":`, `{"q":`, 1)); err != nil {
		t.Fatalf("PublishEvent delta error: %v", err)
	}
	if err := sink.PublishEvent(events.NewToolCallRequestedEvent(metadata, corr, "call_1", "lookup", `{"q":"x"}`)); err != nil {
		t.Fatalf("PublishEvent requested error: %v", err)
	}

	close(ch)
	argumentFrames := 0
	for frame := range ch {
		if frame.Chunk == nil {
			continue
		}
		toolCalls := frame.Chunk.Choices[0].Delta.ToolCalls
		if len(toolCalls) == 0 || toolCalls[0].Function == nil || toolCalls[0].Function.Arguments == "" {
			continue
		}
		argumentFrames++
	}
	if argumentFrames != 1 {
		t.Fatalf("argument frames = %d", argumentFrames)
	}
}

func TestChatEventSinkEmitsRequestedArgumentsWithoutDeltas(t *testing.T) {
	ch := make(chan ChatStreamFrame, 2)
	sink := &ChatEventSink{ID: "chatcmpl_1", Model: "sonnet", Created: 123, Out: ch}
	if err := sink.PublishEvent(events.NewToolCallRequestedEvent(events.EventMetadata{}, events.Correlation{}, "call_1", "lookup", `{"q":"x"}`)); err != nil {
		t.Fatalf("PublishEvent requested error: %v", err)
	}
	frame := <-ch
	toolCalls := frame.Chunk.Choices[0].Delta.ToolCalls
	if len(toolCalls) != 1 || toolCalls[0].Function == nil || toolCalls[0].Function.Arguments != `{"q":"x"}` {
		t.Fatalf("tool calls = %#v", toolCalls)
	}
}
