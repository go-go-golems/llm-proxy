package openaicompletions

import (
	"encoding/json"
	"fmt"
	"io"
	"strings"
)

// CompletionRequest is the small OpenAI legacy Completions subset supported by
// the first proxy prototype.
type CompletionRequest struct {
	Model       string          `json:"model"`
	Prompt      json.RawMessage `json:"prompt"`
	MaxTokens   *int            `json:"max_tokens,omitempty"`
	Temperature *float64        `json:"temperature,omitempty"`
	TopP        *float64        `json:"top_p,omitempty"`
	Stop        json.RawMessage `json:"stop,omitempty"`
	Stream      bool            `json:"stream,omitempty"`
}

func DecodeCompletionRequest(r io.Reader) (*CompletionRequest, error) {
	dec := json.NewDecoder(r)
	dec.DisallowUnknownFields()
	var req CompletionRequest
	if err := dec.Decode(&req); err != nil {
		return nil, fmt.Errorf("decode completion request: %w", err)
	}
	if strings.TrimSpace(req.Model) == "" {
		return nil, FieldError{Field: "model", Message: "model is required", Code: "missing_model"}
	}
	if len(req.Prompt) == 0 || string(req.Prompt) == "null" {
		return nil, FieldError{Field: "prompt", Message: "prompt is required", Code: "missing_prompt"}
	}
	if _, err := req.PromptString(); err != nil {
		return nil, err
	}
	return &req, nil
}

func (r *CompletionRequest) PromptString() (string, error) {
	if r == nil || len(r.Prompt) == 0 || string(r.Prompt) == "null" {
		return "", FieldError{Field: "prompt", Message: "prompt is required", Code: "missing_prompt"}
	}
	var s string
	if err := json.Unmarshal(r.Prompt, &s); err == nil {
		return s, nil
	}
	var arr []json.RawMessage
	if err := json.Unmarshal(r.Prompt, &arr); err == nil {
		return "", FieldError{Field: "prompt", Message: "prompt arrays are not supported in this prototype", Code: "unsupported_prompt_shape"}
	}
	return "", FieldError{Field: "prompt", Message: "prompt must be a string", Code: "unsupported_prompt_shape"}
}

type FieldError struct {
	Field   string
	Message string
	Code    string
}

func (e FieldError) Error() string { return e.Message }

type CompletionResponse struct {
	ID      string             `json:"id"`
	Object  string             `json:"object"`
	Created int64              `json:"created"`
	Model   string             `json:"model"`
	Choices []CompletionChoice `json:"choices"`
	Usage   *Usage             `json:"usage,omitempty"`
}

type CompletionChoice struct {
	Text         string `json:"text"`
	Index        int    `json:"index"`
	Logprobs     any    `json:"logprobs"`
	FinishReason string `json:"finish_reason,omitempty"`
}

type Usage struct {
	PromptTokens     int `json:"prompt_tokens"`
	CompletionTokens int `json:"completion_tokens"`
	TotalTokens      int `json:"total_tokens"`
}

type CompletionStreamChunk struct {
	ID      string                   `json:"id"`
	Object  string                   `json:"object"`
	Created int64                    `json:"created"`
	Model   string                   `json:"model"`
	Choices []CompletionStreamChoice `json:"choices"`
}

type CompletionStreamChoice struct {
	Text         string  `json:"text"`
	Index        int     `json:"index"`
	Logprobs     any     `json:"logprobs"`
	FinishReason *string `json:"finish_reason"`
}
