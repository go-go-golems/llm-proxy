package openaichat

import (
	"encoding/json"
	"sync"

	"github.com/go-go-golems/geppetto/pkg/events"
)

type ChatStreamFrame struct {
	Chunk *ChatCompletionChunk
	Err   error
	Done  bool
}

func RoleFrame(id, model string, created int64) ChatStreamFrame {
	return ChatStreamFrame{Chunk: &ChatCompletionChunk{ID: id, Object: "chat.completion.chunk", Created: created, Model: model, Choices: []ChatStreamChoice{{Index: 0, Delta: ChatDelta{Role: "assistant"}, FinishReason: nil}}}}
}

func DeltaFrame(id, model string, created int64, text string) ChatStreamFrame {
	return ChatStreamFrame{Chunk: &ChatCompletionChunk{ID: id, Object: "chat.completion.chunk", Created: created, Model: model, Choices: []ChatStreamChoice{{Index: 0, Delta: ChatDelta{Content: text}, FinishReason: nil}}}}
}

func ToolCallStartFrame(id, model string, created int64, index int, toolCallID, name string) ChatStreamFrame {
	return ChatStreamFrame{Chunk: &ChatCompletionChunk{ID: id, Object: "chat.completion.chunk", Created: created, Model: model, Choices: []ChatStreamChoice{{Index: 0, Delta: ChatDelta{ToolCalls: []ChatToolCallDelta{{Index: index, ID: toolCallID, Type: "function", Function: &ChatToolCallFunctionDelta{Name: name}}}}, FinishReason: nil}}}}
}

func ToolCallArgumentsFrame(id, model string, created int64, index int, argumentsDelta string) ChatStreamFrame {
	return ChatStreamFrame{Chunk: &ChatCompletionChunk{ID: id, Object: "chat.completion.chunk", Created: created, Model: model, Choices: []ChatStreamChoice{{Index: 0, Delta: ChatDelta{ToolCalls: []ChatToolCallDelta{{Index: index, Function: &ChatToolCallFunctionDelta{Arguments: argumentsDelta}}}}, FinishReason: nil}}}}
}

func FinalFrame(id, model string, created int64, finish string) ChatStreamFrame {
	return ChatStreamFrame{Chunk: &ChatCompletionChunk{ID: id, Object: "chat.completion.chunk", Created: created, Model: model, Choices: []ChatStreamChoice{{Index: 0, Delta: ChatDelta{}, FinishReason: &finish}}}}
}

type ChatEventSink struct {
	ID      string
	Model   string
	Created int64
	Out     chan<- ChatStreamFrame

	mu          sync.Mutex
	toolIndexes map[string]int
}

func (s *ChatEventSink) PublishEvent(ev events.Event) error {
	switch e := ev.(type) {
	case *events.EventTextDelta:
		if e.Delta != "" {
			s.Out <- DeltaFrame(s.ID, s.Model, s.Created, e.Delta)
		}
	case *events.EventToolCallStarted:
		idx := s.toolIndex(e.ToolCallID)
		s.Out <- ToolCallStartFrame(s.ID, s.Model, s.Created, idx, e.ToolCallID, e.ToolName)
	case *events.EventToolCallArgumentsDelta:
		idx := s.toolIndex(e.ToolCallID)
		if e.Delta != "" {
			s.Out <- ToolCallArgumentsFrame(s.ID, s.Model, s.Created, idx, e.Delta)
		}
	case *events.EventToolCallRequested:
		idx := s.toolIndex(e.ToolCallID)
		if e.Input != "" {
			s.Out <- ToolCallArgumentsFrame(s.ID, s.Model, s.Created, idx, e.Input)
		}
	}
	return nil
}

func (s *ChatEventSink) toolIndex(toolCallID string) int {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.toolIndexes == nil {
		s.toolIndexes = map[string]int{}
	}
	if idx, ok := s.toolIndexes[toolCallID]; ok {
		return idx
	}
	idx := len(s.toolIndexes)
	s.toolIndexes[toolCallID] = idx
	return idx
}

func (s *ChatEventSink) Close() error { return nil }

func MarshalChunkForTest(chunk *ChatCompletionChunk) string {
	b, _ := json.Marshal(chunk)
	return string(b)
}
