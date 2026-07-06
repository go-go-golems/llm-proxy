// Package apierr defines OpenAI-shaped API errors that carry an HTTP status.
// pkg/server recognizes them structurally (via errors.As on an interface) so
// BYOK policy failures surface as proper 401/403/429 responses instead of 500s.
package apierr

import "fmt"

// APIError is an error with OpenAI wire-format fields and an HTTP status.
type APIError struct {
	Status  int
	Type    string
	Code    string
	Param   string
	Message string
}

func (e *APIError) Error() string { return e.Message }

func (e *APIError) HTTPStatus() int          { return e.Status }
func (e *APIError) OpenAIErrorType() string  { return e.Type }
func (e *APIError) OpenAIErrorCode() string  { return e.Code }
func (e *APIError) OpenAIErrorParam() string { return e.Param }

func NewMissingAPIKey() *APIError {
	return &APIError{Status: 401, Type: "invalid_request_error", Code: "missing_api_key",
		Message: "Missing bearer token. Pass a minted BYOK token as the API key."}
}

func NewInvalidAPIKey() *APIError {
	return &APIError{Status: 401, Type: "invalid_request_error", Code: "invalid_api_key",
		Message: "Invalid API key."}
}

func NewTokenRevoked() *APIError {
	return &APIError{Status: 401, Type: "invalid_request_error", Code: "token_revoked",
		Message: "This token has been revoked."}
}

func NewTokenExpired() *APIError {
	return &APIError{Status: 401, Type: "invalid_request_error", Code: "token_expired",
		Message: "This token has expired."}
}

func NewModelNotAllowed(model string) *APIError {
	return &APIError{Status: 403, Type: "invalid_request_error", Code: "model_not_allowed", Param: "model",
		Message: fmt.Sprintf("Model %q is not allowed for this token.", model)}
}

func NewNoCredentialForModel(model string) *APIError {
	return &APIError{Status: 403, Type: "invalid_request_error", Code: "no_credential_for_model", Param: "model",
		Message: fmt.Sprintf("No stored credential on this token matches the provider of model %q.", model)}
}

func NewBudgetExhausted(limit int64) *APIError {
	return &APIError{Status: 429, Type: "tokens", Code: "budget_exhausted",
		Message: fmt.Sprintf("Token budget exhausted (%d tokens).", limit)}
}

func NewRequestBudgetExhausted(limit int64) *APIError {
	return &APIError{Status: 429, Type: "tokens", Code: "budget_exhausted",
		Message: fmt.Sprintf("Request budget exhausted (%d requests).", limit)}
}

func NewRateLimited(rpm int64) *APIError {
	return &APIError{Status: 429, Type: "tokens", Code: "rate_limit_exceeded",
		Message: fmt.Sprintf("Rate limit exceeded (%d requests per minute).", rpm)}
}
