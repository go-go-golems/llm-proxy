package server

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
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
