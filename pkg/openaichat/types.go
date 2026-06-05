package openaichat

import (
	"encoding/json"
	"fmt"
	"io"
	"strings"
)

type ChatCompletionRequest struct {
	Model       string          `json:"model"`
	Messages    []ChatMessage   `json:"messages"`
	MaxTokens   *int            `json:"max_tokens,omitempty"`
	Temperature *float64        `json:"temperature,omitempty"`
	TopP        *float64        `json:"top_p,omitempty"`
	Stop        json.RawMessage `json:"stop,omitempty"`
	Stream      bool            `json:"stream,omitempty"`
}

type ChatMessage struct {
	Role    string          `json:"role"`
	Content json.RawMessage `json:"content"`
}

func DecodeChatCompletionRequest(r io.Reader) (*ChatCompletionRequest, error) {
	dec := json.NewDecoder(r)
	dec.DisallowUnknownFields()
	var req ChatCompletionRequest
	if err := dec.Decode(&req); err != nil {
		return nil, fmt.Errorf("decode chat completion request: %w", err)
	}
	if strings.TrimSpace(req.Model) == "" {
		return nil, FieldError{Field: "model", Message: "model is required", Code: "missing_model"}
	}
	if len(req.Messages) == 0 {
		return nil, FieldError{Field: "messages", Message: "messages is required", Code: "missing_messages"}
	}
	for i := range req.Messages {
		if err := req.Messages[i].Validate(i); err != nil {
			return nil, err
		}
	}
	return &req, nil
}

func (m ChatMessage) Validate(index int) error {
	role := strings.TrimSpace(m.Role)
	switch role {
	case "system", "developer", "user", "assistant":
	default:
		return FieldError{Field: fmt.Sprintf("messages[%d].role", index), Message: fmt.Sprintf("unsupported message role %q", m.Role), Code: "unsupported_role"}
	}
	if len(m.Content) == 0 || string(m.Content) == "null" {
		return FieldError{Field: fmt.Sprintf("messages[%d].content", index), Message: "message content is required", Code: "missing_content"}
	}
	if _, err := m.ContentString(); err != nil {
		if fe, ok := err.(FieldError); ok {
			fe.Field = fmt.Sprintf("messages[%d].content", index)
			return fe
		}
		return err
	}
	return nil
}

func (m ChatMessage) ContentString() (string, error) {
	var s string
	if err := json.Unmarshal(m.Content, &s); err == nil {
		return s, nil
	}
	var arr []json.RawMessage
	if err := json.Unmarshal(m.Content, &arr); err == nil {
		return "", FieldError{Field: "content", Message: "message content arrays are not supported in this prototype", Code: "unsupported_content_shape"}
	}
	return "", FieldError{Field: "content", Message: "message content must be a string", Code: "unsupported_content_shape"}
}

type FieldError struct {
	Field   string
	Message string
	Code    string
}

func (e FieldError) Error() string { return e.Message }

type ChatCompletionResponse struct {
	ID      string       `json:"id"`
	Object  string       `json:"object"`
	Created int64        `json:"created"`
	Model   string       `json:"model"`
	Choices []ChatChoice `json:"choices"`
	Usage   *Usage       `json:"usage,omitempty"`
}

type ChatChoice struct {
	Index        int            `json:"index"`
	Message      ChatMessageOut `json:"message"`
	FinishReason string         `json:"finish_reason,omitempty"`
}

type ChatMessageOut struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

type Usage struct {
	PromptTokens     int `json:"prompt_tokens"`
	CompletionTokens int `json:"completion_tokens"`
	TotalTokens      int `json:"total_tokens"`
}

type ChatCompletionChunk struct {
	ID      string             `json:"id"`
	Object  string             `json:"object"`
	Created int64              `json:"created"`
	Model   string             `json:"model"`
	Choices []ChatStreamChoice `json:"choices"`
}

type ChatStreamChoice struct {
	Index        int       `json:"index"`
	Delta        ChatDelta `json:"delta"`
	FinishReason *string   `json:"finish_reason"`
}

type ChatDelta struct {
	Role    string `json:"role,omitempty"`
	Content string `json:"content,omitempty"`
}
