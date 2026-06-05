package openaicompletions

import (
	"strings"
	"testing"
)

func TestDecodeCompletionRequestStringPrompt(t *testing.T) {
	req, err := DecodeCompletionRequest(strings.NewReader(`{"model":"test-profile","prompt":"hello","stream":false}`))
	if err != nil {
		t.Fatalf("DecodeCompletionRequest error: %v", err)
	}
	if req.Model != "test-profile" {
		t.Fatalf("model = %q", req.Model)
	}
	prompt, err := req.PromptString()
	if err != nil {
		t.Fatalf("PromptString error: %v", err)
	}
	if prompt != "hello" {
		t.Fatalf("prompt = %q", prompt)
	}
}

func TestDecodeCompletionRequestRejectsPromptArray(t *testing.T) {
	_, err := DecodeCompletionRequest(strings.NewReader(`{"model":"test-profile","prompt":["a","b"]}`))
	if err == nil {
		t.Fatalf("expected prompt array error")
	}
	fieldErr, ok := err.(FieldError)
	if !ok {
		t.Fatalf("expected FieldError, got %T: %v", err, err)
	}
	if fieldErr.Code != "unsupported_prompt_shape" {
		t.Fatalf("code = %q", fieldErr.Code)
	}
}

func TestDecodeCompletionRequestRequiresModelAndPrompt(t *testing.T) {
	if _, err := DecodeCompletionRequest(strings.NewReader(`{"prompt":"hello"}`)); err == nil {
		t.Fatalf("expected missing model error")
	}
	if _, err := DecodeCompletionRequest(strings.NewReader(`{"model":"test"}`)); err == nil {
		t.Fatalf("expected missing prompt error")
	}
}
