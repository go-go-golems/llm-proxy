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
	Tools       []ChatTool      `json:"tools,omitempty"`
	ToolChoice  json.RawMessage `json:"tool_choice,omitempty"`
	MaxTokens   *int            `json:"max_tokens,omitempty"`
	Temperature *float64        `json:"temperature,omitempty"`
	TopP        *float64        `json:"top_p,omitempty"`
	Stop        json.RawMessage `json:"stop,omitempty"`
	Stream      bool            `json:"stream,omitempty"`
}

type ChatMessage struct {
	Role       string          `json:"role"`
	Content    json.RawMessage `json:"content,omitempty"`
	ToolCallID string          `json:"tool_call_id,omitempty"`
	ToolCalls  []ChatToolCall  `json:"tool_calls,omitempty"`
}

type ChatTool struct {
	Type     string           `json:"type"`
	Function ChatToolFunction `json:"function"`
}

type ChatToolFunction struct {
	Name        string         `json:"name"`
	Description string         `json:"description,omitempty"`
	Parameters  map[string]any `json:"parameters,omitempty"`
}

type ChatToolCall struct {
	ID       string               `json:"id"`
	Type     string               `json:"type"`
	Function ChatToolCallFunction `json:"function"`
}

type ChatToolCallFunction struct {
	Name      string `json:"name"`
	Arguments string `json:"arguments"`
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
	for i := range req.Tools {
		if err := req.Tools[i].Validate(i); err != nil {
			return nil, err
		}
	}
	return &req, nil
}

func (m ChatMessage) Validate(index int) error {
	role := strings.TrimSpace(m.Role)
	switch role {
	case "system", "developer", "user":
		if _, err := m.RequiredContentString(index); err != nil {
			return err
		}
	case "assistant":
		if len(m.Content) != 0 && string(m.Content) != "null" {
			if _, err := m.ContentString(); err != nil {
				return withField(err, fmt.Sprintf("messages[%d].content", index))
			}
		}
		for j := range m.ToolCalls {
			if err := m.ToolCalls[j].Validate(fmt.Sprintf("messages[%d].tool_calls[%d]", index, j)); err != nil {
				return err
			}
		}
		if (len(m.Content) == 0 || string(m.Content) == "null") && len(m.ToolCalls) == 0 {
			return FieldError{Field: fmt.Sprintf("messages[%d].content", index), Message: "assistant message must include content or tool_calls", Code: "missing_content"}
		}
	case "tool":
		if strings.TrimSpace(m.ToolCallID) == "" {
			return FieldError{Field: fmt.Sprintf("messages[%d].tool_call_id", index), Message: "tool_call_id is required for tool messages", Code: "missing_tool_call_id"}
		}
		if _, err := m.RequiredContentString(index); err != nil {
			return err
		}
	default:
		return FieldError{Field: fmt.Sprintf("messages[%d].role", index), Message: fmt.Sprintf("unsupported message role %q", m.Role), Code: "unsupported_role"}
	}
	return nil
}

func (m ChatMessage) RequiredContentString(index int) (string, error) {
	if len(m.Content) == 0 || string(m.Content) == "null" {
		return "", FieldError{Field: fmt.Sprintf("messages[%d].content", index), Message: "message content is required", Code: "missing_content"}
	}
	text, err := m.ContentString()
	if err != nil {
		return "", withField(err, fmt.Sprintf("messages[%d].content", index))
	}
	return text, nil
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

func (t ChatTool) Validate(index int) error {
	if t.Type != "function" {
		return FieldError{Field: fmt.Sprintf("tools[%d].type", index), Message: "only function tools are supported", Code: "unsupported_tool_type"}
	}
	if strings.TrimSpace(t.Function.Name) == "" {
		return FieldError{Field: fmt.Sprintf("tools[%d].function.name", index), Message: "tool function name is required", Code: "missing_tool_name"}
	}
	return nil
}

func (tc ChatToolCall) Validate(field string) error {
	if strings.TrimSpace(tc.ID) == "" {
		return FieldError{Field: field + ".id", Message: "tool call id is required", Code: "missing_tool_call_id"}
	}
	if tc.Type != "function" {
		return FieldError{Field: field + ".type", Message: "only function tool calls are supported", Code: "unsupported_tool_type"}
	}
	if strings.TrimSpace(tc.Function.Name) == "" {
		return FieldError{Field: field + ".function.name", Message: "tool call function name is required", Code: "missing_tool_name"}
	}
	if strings.TrimSpace(tc.Function.Arguments) == "" {
		return FieldError{Field: field + ".function.arguments", Message: "tool call function arguments are required", Code: "missing_tool_arguments"}
	}
	return nil
}

func withField(err error, field string) error {
	if fe, ok := err.(FieldError); ok {
		fe.Field = field
		return fe
	}
	return err
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
	Role      string         `json:"role"`
	Content   string         `json:"content"`
	ToolCalls []ChatToolCall `json:"tool_calls,omitempty"`
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
	Role      string              `json:"role,omitempty"`
	Content   string              `json:"content,omitempty"`
	ToolCalls []ChatToolCallDelta `json:"tool_calls,omitempty"`
}

type ChatToolCallDelta struct {
	Index    int                        `json:"index"`
	ID       string                     `json:"id,omitempty"`
	Type     string                     `json:"type,omitempty"`
	Function *ChatToolCallFunctionDelta `json:"function,omitempty"`
}

type ChatToolCallFunctionDelta struct {
	Name      string `json:"name,omitempty"`
	Arguments string `json:"arguments,omitempty"`
}
