package server

import (
	"encoding/json"
	"fmt"
	"net/http"

	"github.com/go-go-golems/llm-proxy/pkg/openaicompletions"
)

func (s *Server) writeCompletionSSE(w http.ResponseWriter, r *http.Request, frames <-chan openaicompletions.CompletionStreamFrame) {
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
		case frame, ok := <-frames:
			if !ok {
				fmt.Fprint(w, "data: [DONE]\n\n")
				flusher.Flush()
				return
			}
			if frame.Done {
				fmt.Fprint(w, "data: [DONE]\n\n")
				flusher.Flush()
				return
			}
			if frame.Err != nil {
				b, _ := json.Marshal(openAIErrorResponse{Error: openAIError{Message: frame.Err.Error(), Type: "api_error", Code: "stream_error"}})
				fmt.Fprintf(w, "data: %s\n\n", b)
				fmt.Fprint(w, "data: [DONE]\n\n")
				flusher.Flush()
				return
			}
			if frame.Chunk == nil {
				continue
			}
			b, err := json.Marshal(frame.Chunk)
			if err != nil {
				continue
			}
			fmt.Fprintf(w, "data: %s\n\n", b)
			flusher.Flush()
		}
	}
}
