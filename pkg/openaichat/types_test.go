package openaichat

import (
	"strings"
	"testing"
)

func TestDecodeChatCompletionRequest(t *testing.T) {
	req, err := DecodeChatCompletionRequest(strings.NewReader(`{"model":"sonnet","messages":[{"role":"user","content":"hello"}]}`))
	if err != nil {
		t.Fatalf("DecodeChatCompletionRequest error: %v", err)
	}
	if req.Model != "sonnet" || len(req.Messages) != 1 {
		t.Fatalf("request = %#v", req)
	}
	text, err := req.Messages[0].ContentString()
	if err != nil || text != "hello" {
		t.Fatalf("content text=%q err=%v", text, err)
	}
}

func TestDecodeChatCompletionRejectsMissingMessages(t *testing.T) {
	_, err := DecodeChatCompletionRequest(strings.NewReader(`{"model":"sonnet"}`))
	if err == nil {
		t.Fatalf("expected error")
	}
}

func TestDecodeChatCompletionRejectsUnsupportedContentArray(t *testing.T) {
	_, err := DecodeChatCompletionRequest(strings.NewReader(`{"model":"sonnet","messages":[{"role":"user","content":[{"type":"text","text":"hello"}]}]}`))
	if err == nil {
		t.Fatalf("expected content array error")
	}
	fieldErr, ok := err.(FieldError)
	if !ok {
		t.Fatalf("expected FieldError, got %T: %v", err, err)
	}
	if fieldErr.Code != "unsupported_content_shape" {
		t.Fatalf("code = %q", fieldErr.Code)
	}
}

func TestDecodeChatCompletionRejectsUnsupportedRole(t *testing.T) {
	_, err := DecodeChatCompletionRequest(strings.NewReader(`{"model":"sonnet","messages":[{"role":"tool","content":"hello"}]}`))
	if err == nil {
		t.Fatalf("expected role error")
	}
}
