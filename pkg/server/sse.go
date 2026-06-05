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
	Next() (any, error, bool, bool)
}

type completionFrameSource struct {
	frames <-chan openaicompletions.CompletionStreamFrame
}

func (s completionFrameSource) Next() (any, error, bool, bool) {
	frame, ok := <-s.frames
	if !ok {
		return nil, nil, true, false
	}
	return frame.Chunk, frame.Err, frame.Done, true
}

type chatFrameSource struct {
	frames <-chan openaichat.ChatStreamFrame
}

func (s chatFrameSource) Next() (any, error, bool, bool) {
	frame, ok := <-s.frames
	if !ok {
		return nil, nil, true, false
	}
	return frame.Chunk, frame.Err, frame.Done, true
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
		chunk, frameErr, done, ok := frames.Next()
		if !ok || done {
			fmt.Fprint(w, "data: [DONE]\n\n")
			flusher.Flush()
			return
		}
		if frameErr != nil {
			b, _ := json.Marshal(openAIErrorResponse{Error: openAIError{Message: frameErr.Error(), Type: "api_error", Code: "stream_error"}})
			fmt.Fprintf(w, "data: %s\n\n", b)
			fmt.Fprint(w, "data: [DONE]\n\n")
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
		fmt.Fprintf(w, "data: %s\n\n", b)
		flusher.Flush()
	}
}
