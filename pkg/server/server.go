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

type StreamingCompletionService interface {
	Stream(ctx context.Context, req *openaicompletions.CompletionRequest) (<-chan openaicompletions.CompletionStreamFrame, error)
}

type ModelLister interface {
	ListModels(ctx context.Context) ([]ModelDescriptor, error)
}

type ModelDescriptor struct {
	ID      string `json:"id"`
	Object  string `json:"object"`
	OwnedBy string `json:"owned_by"`
}

type Options struct {
	CompletionService CompletionService
	ModelLister       ModelLister
	MaxBodyBytes      int64
}

type Server struct {
	completionService CompletionService
	modelLister       ModelLister
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
	return &Server{completionService: service, modelLister: opts.ModelLister, maxBodyBytes: maxBodyBytes}
}

func (s *Server) Handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /healthz", s.handleHealthz)
	mux.HandleFunc("GET /v1/models", s.handleModels)
	mux.HandleFunc("POST /v1/completions", s.handleCompletions)
	return mux
}

func (s *Server) handleHealthz(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

func (s *Server) handleModels(w http.ResponseWriter, r *http.Request) {
	models := []ModelDescriptor{}
	if s.modelLister != nil {
		ret, err := s.modelLister.ListModels(r.Context())
		if err != nil {
			writeOpenAIError(w, http.StatusInternalServerError, err)
			return
		}
		models = ret
	}
	writeJSON(w, http.StatusOK, map[string]any{"object": "list", "data": models})
}

func (s *Server) handleCompletions(w http.ResponseWriter, r *http.Request) {
	r.Body = http.MaxBytesReader(w, r.Body, s.maxBodyBytes)
	req, err := openaicompletions.DecodeCompletionRequest(r.Body)
	if err != nil {
		writeOpenAIError(w, http.StatusBadRequest, err)
		return
	}
	if req.Stream {
		streamer, ok := s.completionService.(StreamingCompletionService)
		if !ok {
			writeOpenAIError(w, http.StatusBadRequest, openaicompletions.FieldError{Field: "stream", Message: "streaming is not implemented yet", Code: "stream_not_implemented"})
			return
		}
		frames, err := streamer.Stream(r.Context(), req)
		if err != nil {
			writeOpenAIError(w, http.StatusInternalServerError, err)
			return
		}
		s.writeCompletionSSE(w, r, frames)
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
