package openaicompletions

import (
	"testing"

	"github.com/go-go-golems/geppetto/pkg/events"
)

func TestCompletionEventSinkTranslatesTextDelta(t *testing.T) {
	ch := make(chan CompletionStreamFrame, 1)
	sink := &CompletionEventSink{ID: "cmpl_1", Model: "sonnet", Created: 123, Out: ch}
	if err := sink.PublishEvent(events.NewTextDeltaEvent(events.EventMetadata{}, events.Correlation{}, "hel", "hel", 1)); err != nil {
		t.Fatalf("PublishEvent error: %v", err)
	}
	frame := <-ch
	if frame.Chunk == nil {
		t.Fatalf("expected chunk")
	}
	if frame.Chunk.Choices[0].Text != "hel" {
		t.Fatalf("text = %q", frame.Chunk.Choices[0].Text)
	}
}
