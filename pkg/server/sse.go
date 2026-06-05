package server

import (
	"encoding/json"
	"fmt"
	"net/http"

	"github.com/go-go-golems/llm-proxy/pkg/openaichat"
	"github.com/go-go-golems/llm-proxy/pkg/openaicompletions"
)

func (s *Server) writeCompletionSSE(w http.ResponseWriter, r *http.Request, frames <-chan openaicompletions.CompletionStreamFrame) {
	writeSSE(w, r, completionFrameSource{frames: frames})
}

func (s *Server) writeChatSSE(w http.ResponseWriter, r *http.Request, frames <-chan openaichat.ChatStreamFrame) {
	writeSSE(w, r, chatFrameSource{frames: frames})
}

type sseFrameSource interface {
	Next() (any, bool, bool, error)
}

type completionFrameSource struct {
	frames <-chan openaicompletions.CompletionStreamFrame
}

func (s completionFrameSource) Next() (any, bool, bool, error) {
	frame, ok := <-s.frames
	if !ok {
		return nil, true, false, nil
	}
	return frame.Chunk, frame.Done, true, frame.Err
}

type chatFrameSource struct {
	frames <-chan openaichat.ChatStreamFrame
}

func (s chatFrameSource) Next() (any, bool, bool, error) {
	frame, ok := <-s.frames
	if !ok {
		return nil, true, false, nil
	}
	return frame.Chunk, frame.Done, true, frame.Err
}

func writeSSE(w http.ResponseWriter, r *http.Request, frames sseFrameSource) {
	flusher, ok := w.(http.Flusher)
	if !ok {
		writeOpenAIError(w, http.StatusInternalServerError, fmt.Errorf("streaming unsupported"))
		return
	}
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	w.WriteHeader(http.StatusOK)
	for {
		select {
		case <-r.Context().Done():
			return
		default:
		}
		chunk, done, ok, frameErr := frames.Next()
		if !ok || done {
			if _, err := fmt.Fprint(w, "data: [DONE]\n\n"); err != nil {
				return
			}
			flusher.Flush()
			return
		}
		if frameErr != nil {
			b, _ := json.Marshal(openAIErrorResponse{Error: openAIError{Message: frameErr.Error(), Type: "api_error", Code: "stream_error"}})
			if _, err := fmt.Fprintf(w, "data: %s\n\n", b); err != nil {
				return
			}
			if _, err := fmt.Fprint(w, "data: [DONE]\n\n"); err != nil {
				return
			}
			flusher.Flush()
			return
		}
		if chunk == nil {
			continue
		}
		b, err := json.Marshal(chunk)
		if err != nil {
			continue
		}
		if _, err := fmt.Fprintf(w, "data: %s\n\n", b); err != nil {
			return
		}
		flusher.Flush()
	}
}
