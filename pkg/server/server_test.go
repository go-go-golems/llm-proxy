package server

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/go-go-golems/llm-proxy/pkg/openaichat"
	"github.com/go-go-golems/llm-proxy/pkg/openaicompletions"
)

func TestHealthz(t *testing.T) {
	srv := New(Options{})
	req := httptest.NewRequest(http.MethodGet, "/healthz", nil)
	w := httptest.NewRecorder()
	srv.Handler().ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d body=%s", w.Code, w.Body.String())
	}
}

type fixedReadiness struct{ err error }

func (r fixedReadiness) Ready(context.Context) error { return r.err }

func TestReadyzReflectsDependencyReadinessWithoutChangingLiveness(t *testing.T) {
	srv := New(Options{Readiness: fixedReadiness{err: errors.New("dependency unavailable")}})
	for path, want := range map[string]int{"/readyz": http.StatusServiceUnavailable, "/healthz": http.StatusOK} {
		req := httptest.NewRequest(http.MethodGet, path, nil)
		w := httptest.NewRecorder()
		srv.Handler().ServeHTTP(w, req)
		if w.Code != want {
			t.Fatalf("%s status = %d body=%s, want %d", path, w.Code, w.Body.String(), want)
		}
	}
	ready := New(Options{Readiness: fixedReadiness{}})
	req := httptest.NewRequest(http.MethodGet, "/readyz", nil)
	w := httptest.NewRecorder()
	ready.Handler().ServeHTTP(w, req)
	if w.Code != http.StatusOK || !strings.Contains(w.Body.String(), `"status":"ready"`) {
		t.Fatalf("ready response: %d %s", w.Code, w.Body.String())
	}
}

func TestCompletionsPlaceholder(t *testing.T) {
	srv := New(Options{})
	req := httptest.NewRequest(http.MethodPost, "/v1/completions", strings.NewReader(`{"model":"test-profile","prompt":"hello"}`))
	w := httptest.NewRecorder()
	srv.Handler().ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d body=%s", w.Code, w.Body.String())
	}
	if !strings.Contains(w.Body.String(), `"object":"text_completion"`) {
		t.Fatalf("missing text_completion response: %s", w.Body.String())
	}
}

func TestCompletionsRejectsBadRequest(t *testing.T) {
	srv := New(Options{})
	req := httptest.NewRequest(http.MethodPost, "/v1/completions", strings.NewReader(`{"prompt":"hello"}`))
	w := httptest.NewRecorder()
	srv.Handler().ServeHTTP(w, req)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("status = %d body=%s", w.Code, w.Body.String())
	}
}

type fakeModelLister struct{}

func (fakeModelLister) ListModels(_ context.Context) ([]ModelDescriptor, error) {
	return []ModelDescriptor{{ID: "sonnet", Object: "model", OwnedBy: "geppetto-profile"}}, nil
}

func TestModelsListsConfiguredProfiles(t *testing.T) {
	srv := New(Options{ModelLister: fakeModelLister{}})
	req := httptest.NewRequest(http.MethodGet, "/v1/models", nil)
	w := httptest.NewRecorder()
	srv.Handler().ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d body=%s", w.Code, w.Body.String())
	}
	if !strings.Contains(w.Body.String(), `"id":"sonnet"`) {
		t.Fatalf("missing model listing: %s", w.Body.String())
	}
}

type fakeStreamingService struct{}

func (fakeStreamingService) Complete(_ context.Context, req *openaicompletions.CompletionRequest) (*openaicompletions.CompletionResponse, error) {
	return StaticCompletionService{}.Complete(context.Background(), req)
}

func (fakeStreamingService) Stream(_ context.Context, req *openaicompletions.CompletionRequest) (<-chan openaicompletions.CompletionStreamFrame, error) {
	ch := make(chan openaicompletions.CompletionStreamFrame, 3)
	ch <- openaicompletions.DeltaFrame("cmpl_test", req.Model, 123, "hel")
	ch <- openaicompletions.FinalFrame("cmpl_test", req.Model, 123, "stop")
	close(ch)
	return ch, nil
}

func TestCompletionsStreamingSSE(t *testing.T) {
	srv := New(Options{CompletionService: fakeStreamingService{}})
	req := httptest.NewRequest(http.MethodPost, "/v1/completions", strings.NewReader(`{"model":"test-profile","prompt":"hello","stream":true}`))
	w := httptest.NewRecorder()
	srv.Handler().ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d body=%s", w.Code, w.Body.String())
	}
	if !strings.Contains(w.Body.String(), "data: [DONE]") {
		t.Fatalf("missing DONE: %s", w.Body.String())
	}
	if !strings.Contains(w.Body.String(), `"text":"hel"`) {
		t.Fatalf("missing delta: %s", w.Body.String())
	}
}

func TestChatCompletionsPlaceholder(t *testing.T) {
	srv := New(Options{})
	req := httptest.NewRequest(http.MethodPost, "/v1/chat/completions", strings.NewReader(`{"model":"test-profile","messages":[{"role":"user","content":"hello"}]}`))
	w := httptest.NewRecorder()
	srv.Handler().ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d body=%s", w.Code, w.Body.String())
	}
	if !strings.Contains(w.Body.String(), `"object":"chat.completion"`) {
		t.Fatalf("missing chat.completion response: %s", w.Body.String())
	}
}

func TestChatCompletionsRejectsBadRequest(t *testing.T) {
	srv := New(Options{})
	req := httptest.NewRequest(http.MethodPost, "/v1/chat/completions", strings.NewReader(`{"messages":[{"role":"user","content":"hello"}]}`))
	w := httptest.NewRecorder()
	srv.Handler().ServeHTTP(w, req)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("status = %d body=%s", w.Code, w.Body.String())
	}
}

type fakeStreamingChatService struct{}

func (fakeStreamingChatService) Complete(_ context.Context, req *openaichat.ChatCompletionRequest) (*openaichat.ChatCompletionResponse, error) {
	return StaticChatCompletionService{}.Complete(context.Background(), req)
}

func (fakeStreamingChatService) Stream(_ context.Context, req *openaichat.ChatCompletionRequest) (<-chan openaichat.ChatStreamFrame, error) {
	ch := make(chan openaichat.ChatStreamFrame, 4)
	ch <- openaichat.RoleFrame("chatcmpl_test", req.Model, 123)
	ch <- openaichat.DeltaFrame("chatcmpl_test", req.Model, 123, "hel")
	ch <- openaichat.FinalFrame("chatcmpl_test", req.Model, 123, "stop")
	close(ch)
	return ch, nil
}

func TestChatCompletionsStreamingSSE(t *testing.T) {
	srv := New(Options{ChatCompletionService: fakeStreamingChatService{}})
	req := httptest.NewRequest(http.MethodPost, "/v1/chat/completions", strings.NewReader(`{"model":"test-profile","messages":[{"role":"user","content":"hello"}],"stream":true}`))
	w := httptest.NewRecorder()
	srv.Handler().ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d body=%s", w.Code, w.Body.String())
	}
	if !strings.Contains(w.Body.String(), "data: [DONE]") {
		t.Fatalf("missing DONE: %s", w.Body.String())
	}
	if !strings.Contains(w.Body.String(), `"content":"hel"`) {
		t.Fatalf("missing delta: %s", w.Body.String())
	}
}

func TestChatCompletionsStreamingToolCallSSE(t *testing.T) {
	ch := make(chan openaichat.ChatStreamFrame, 3)
	ch <- openaichat.ToolCallStartFrame("chatcmpl_test", "test-profile", 123, 0, "call_1", "lookup")
	ch <- openaichat.ToolCallArgumentsFrame("chatcmpl_test", "test-profile", 123, 0, `{"q":"x"}`)
	ch <- openaichat.FinalFrame("chatcmpl_test", "test-profile", 123, "tool_calls")
	close(ch)
	srv := New(Options{ChatCompletionService: fixedStreamingChatService{frames: ch}})
	req := httptest.NewRequest(http.MethodPost, "/v1/chat/completions", strings.NewReader(`{"model":"test-profile","messages":[{"role":"user","content":"hello"}],"tools":[{"type":"function","function":{"name":"lookup"}}],"stream":true}`))
	w := httptest.NewRecorder()
	srv.Handler().ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d body=%s", w.Code, w.Body.String())
	}
	if !strings.Contains(w.Body.String(), `"tool_calls"`) || !strings.Contains(w.Body.String(), `"finish_reason":"tool_calls"`) {
		t.Fatalf("missing tool call stream: %s", w.Body.String())
	}
}

type fixedStreamingChatService struct {
	frames <-chan openaichat.ChatStreamFrame
}

func (s fixedStreamingChatService) Complete(_ context.Context, req *openaichat.ChatCompletionRequest) (*openaichat.ChatCompletionResponse, error) {
	return StaticChatCompletionService{}.Complete(context.Background(), req)
}

func (s fixedStreamingChatService) Stream(_ context.Context, _ *openaichat.ChatCompletionRequest) (<-chan openaichat.ChatStreamFrame, error) {
	return s.frames, nil
}
