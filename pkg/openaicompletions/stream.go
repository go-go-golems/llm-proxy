package openaicompletions

import (
	"fmt"
	"time"

	"github.com/go-go-golems/geppetto/pkg/events"
)

type CompletionStreamFrame struct {
	Chunk *CompletionStreamChunk
	Err   error
	Done  bool
}

func NewCompletionID() string {
	return fmt.Sprintf("cmpl_proxy_%d", time.Now().UnixNano())
}

func DeltaFrame(id, model string, created int64, text string) CompletionStreamFrame {
	return CompletionStreamFrame{Chunk: &CompletionStreamChunk{
		ID:      id,
		Object:  "text_completion",
		Created: created,
		Model:   model,
		Choices: []CompletionStreamChoice{{Text: text, Index: 0, Logprobs: nil, FinishReason: nil}},
	}}
}

func FinalFrame(id, model string, created int64, finishReason string) CompletionStreamFrame {
	return CompletionStreamFrame{Chunk: &CompletionStreamChunk{
		ID:      id,
		Object:  "text_completion",
		Created: created,
		Model:   model,
		Choices: []CompletionStreamChoice{{Text: "", Index: 0, Logprobs: nil, FinishReason: &finishReason}},
	}}
}

type CompletionEventSink struct {
	ID      string
	Model   string
	Created int64
	Out     chan<- CompletionStreamFrame
}

func (s *CompletionEventSink) PublishEvent(ev events.Event) error {
	switch e := ev.(type) {
	case *events.EventTextDelta:
		if e.Delta != "" {
			s.Out <- DeltaFrame(s.ID, s.Model, s.Created, e.Delta)
		}
	}
	return nil
}
