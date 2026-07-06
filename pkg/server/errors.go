package server

import (
	"encoding/json"
	"errors"
	"net/http"

	"github.com/go-go-golems/llm-proxy/pkg/openaichat"
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

// httpAPIError lets error values carry their own HTTP status and OpenAI
// error fields (e.g. BYOK policy errors from pkg/byok/apierr) without this
// package importing them.
type httpAPIError interface {
	error
	HTTPStatus() int
	OpenAIErrorType() string
	OpenAIErrorCode() string
	OpenAIErrorParam() string
}

func writeOpenAIError(w http.ResponseWriter, status int, err error) {
	resp := openAIErrorResponse{Error: openAIError{
		Message: "request failed",
		Type:    "api_error",
		Code:    "internal_error",
	}}
	if err != nil {
		resp.Error.Message = err.Error()
	}
	var completionFieldErr openaicompletions.FieldError
	if errors.As(err, &completionFieldErr) {
		resp.Error.Type = "invalid_request_error"
		resp.Error.Param = completionFieldErr.Field
		resp.Error.Code = completionFieldErr.Code
	}
	var chatFieldErr openaichat.FieldError
	if errors.As(err, &chatFieldErr) {
		resp.Error.Type = "invalid_request_error"
		resp.Error.Param = chatFieldErr.Field
		resp.Error.Code = chatFieldErr.Code
	}
	var apiErr httpAPIError
	if errors.As(err, &apiErr) {
		status = apiErr.HTTPStatus()
		resp.Error.Message = apiErr.Error()
		resp.Error.Type = apiErr.OpenAIErrorType()
		resp.Error.Code = apiErr.OpenAIErrorCode()
		resp.Error.Param = apiErr.OpenAIErrorParam()
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(resp)
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}
