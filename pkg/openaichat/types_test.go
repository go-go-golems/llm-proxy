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

func TestDecodeChatCompletionAllowsUnknownCompatibilityFields(t *testing.T) {
	req, err := DecodeChatCompletionRequest(strings.NewReader(`{"model":"sonnet","messages":[{"role":"user","content":"hello"}],"n":1,"presence_penalty":0,"frequency_penalty":0}`))
	if err != nil {
		t.Fatalf("DecodeChatCompletionRequest error: %v", err)
	}
	if req.Model != "sonnet" {
		t.Fatalf("model = %q", req.Model)
	}
}

func TestDecodeChatCompletionRejectsMissingMessages(t *testing.T) {
	_, err := DecodeChatCompletionRequest(strings.NewReader(`{"model":"sonnet"}`))
	if err == nil {
		t.Fatalf("expected error")
	}
}

func TestDecodeChatCompletionAcceptsUserTextContentArray(t *testing.T) {
	req, err := DecodeChatCompletionRequest(strings.NewReader(`{"model":"sonnet","messages":[{"role":"user","content":[{"type":"text","text":"hello"},{"type":"text","text":"world"}]}]}`))
	if err != nil {
		t.Fatalf("DecodeChatCompletionRequest error: %v", err)
	}
	text, images, err := req.Messages[0].RequiredUserContent(0)
	if err != nil {
		t.Fatalf("RequiredUserContent error: %v", err)
	}
	if text != "hello\nworld" || len(images) != 0 {
		t.Fatalf("text=%q images=%#v", text, images)
	}
}

func TestDecodeChatCompletionAcceptsUserImageURLContentArray(t *testing.T) {
	req, err := DecodeChatCompletionRequest(strings.NewReader(`{"model":"sonnet","messages":[{"role":"user","content":[{"type":"text","text":"describe"},{"type":"image_url","image_url":{"url":"data:image/png;base64,UE5H","detail":"high"}}]}]}`))
	if err != nil {
		t.Fatalf("DecodeChatCompletionRequest error: %v", err)
	}
	text, images, err := req.Messages[0].RequiredUserContent(0)
	if err != nil {
		t.Fatalf("RequiredUserContent error: %v", err)
	}
	if text != "describe" || len(images) != 1 {
		t.Fatalf("text=%q images=%#v", text, images)
	}
	if images[0].URL != "data:image/png;base64,UE5H" || images[0].Detail != "high" {
		t.Fatalf("image = %#v", images[0])
	}
	turnImage := images[0].ToTurnImageMap()
	if turnImage["media_type"] != "image/png" || turnImage["detail"] != "high" {
		t.Fatalf("turn image = %#v", turnImage)
	}
}

func TestDecodeChatCompletionAcceptsCompactImageURLString(t *testing.T) {
	req, err := DecodeChatCompletionRequest(strings.NewReader(`{"model":"sonnet","messages":[{"role":"user","content":[{"type":"image_url","image_url":"https://example.com/image.png"}]}]}`))
	if err != nil {
		t.Fatalf("DecodeChatCompletionRequest error: %v", err)
	}
	_, images, err := req.Messages[0].RequiredUserContent(0)
	if err != nil {
		t.Fatalf("RequiredUserContent error: %v", err)
	}
	if len(images) != 1 || images[0].URL != "https://example.com/image.png" {
		t.Fatalf("images=%#v", images)
	}
}

func TestDecodeChatCompletionRejectsUnsupportedContentPart(t *testing.T) {
	_, err := DecodeChatCompletionRequest(strings.NewReader(`{"model":"sonnet","messages":[{"role":"user","content":[{"type":"audio","audio":"..."}]}]}`))
	if err == nil {
		t.Fatalf("expected content part error")
	}
	fieldErr, ok := err.(FieldError)
	if !ok {
		t.Fatalf("expected FieldError, got %T: %v", err, err)
	}
	if fieldErr.Code != "unsupported_content_part" {
		t.Fatalf("code = %q", fieldErr.Code)
	}
}

func TestDecodeChatCompletionRejectsAssistantImageContentArray(t *testing.T) {
	_, err := DecodeChatCompletionRequest(strings.NewReader(`{"model":"sonnet","messages":[{"role":"assistant","content":[{"type":"image_url","image_url":"https://example.com/image.png"}]}]}`))
	if err == nil {
		t.Fatalf("expected assistant content array error")
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
