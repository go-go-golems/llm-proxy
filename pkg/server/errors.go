package server

import (
	"encoding/json"
	"errors"
	"net/http"

	"github.com/go-go-golems/llm-proxy/pkg/openaicompletions"
)

type openAIErrorResponse struct {
	Error openAIError `json:"error"`
}

type openAIError struct {
	Message string `json:"message"`
	Type    string `json:"type"`
	Param   string `json:"param,omitempty"`
	Code    string `json:"code,omitempty"`
}

func writeOpenAIError(w http.ResponseWriter, status int, err error) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)

	resp := openAIErrorResponse{Error: openAIError{
		Message: "request failed",
		Type:    "api_error",
		Code:    "internal_error",
	}}
	if err != nil {
		resp.Error.Message = err.Error()
	}
	var fieldErr openaicompletions.FieldError
	if errors.As(err, &fieldErr) {
		resp.Error.Type = "invalid_request_error"
		resp.Error.Param = fieldErr.Field
		resp.Error.Code = fieldErr.Code
	}
	_ = json.NewEncoder(w).Encode(resp)
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}
