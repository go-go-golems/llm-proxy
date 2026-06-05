package server

import (
	"context"
	"fmt"
	"net/http"
	"time"

	"github.com/go-go-golems/llm-proxy/pkg/openaicompletions"
)

type CompletionService interface {
	Complete(ctx context.Context, req *openaicompletions.CompletionRequest) (*openaicompletions.CompletionResponse, error)
}

type Options struct {
	CompletionService CompletionService
	MaxBodyBytes      int64
}

type Server struct {
	completionService CompletionService
	maxBodyBytes      int64
}

func New(opts Options) *Server {
	maxBodyBytes := opts.MaxBodyBytes
	if maxBodyBytes <= 0 {
		maxBodyBytes = 1 << 20
	}
	service := opts.CompletionService
	if service == nil {
		service = StaticCompletionService{}
	}
	return &Server{completionService: service, maxBodyBytes: maxBodyBytes}
}

func (s *Server) Handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /healthz", s.handleHealthz)
	mux.HandleFunc("POST /v1/completions", s.handleCompletions)
	return mux
}

func (s *Server) handleHealthz(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

func (s *Server) handleCompletions(w http.ResponseWriter, r *http.Request) {
	r.Body = http.MaxBytesReader(w, r.Body, s.maxBodyBytes)
	req, err := openaicompletions.DecodeCompletionRequest(r.Body)
	if err != nil {
		writeOpenAIError(w, http.StatusBadRequest, err)
		return
	}
	if req.Stream {
		writeOpenAIError(w, http.StatusBadRequest, openaicompletions.FieldError{Field: "stream", Message: "streaming is not implemented yet", Code: "stream_not_implemented"})
		return
	}
	resp, err := s.completionService.Complete(r.Context(), req)
	if err != nil {
		writeOpenAIError(w, http.StatusInternalServerError, err)
		return
	}
	writeJSON(w, http.StatusOK, resp)
}

type StaticCompletionService struct{}

func (StaticCompletionService) Complete(_ context.Context, req *openaicompletions.CompletionRequest) (*openaicompletions.CompletionResponse, error) {
	prompt, err := req.PromptString()
	if err != nil {
		return nil, err
	}
	return &openaicompletions.CompletionResponse{
		ID:      fmt.Sprintf("cmpl_proxy_%d", time.Now().UnixNano()),
		Object:  "text_completion",
		Created: time.Now().Unix(),
		Model:   req.Model,
		Choices: []openaicompletions.CompletionChoice{{
			Text:         "prototype completion for: " + prompt,
			Index:        0,
			Logprobs:     nil,
			FinishReason: "stop",
		}},
	}, nil
}
