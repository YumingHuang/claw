package models

import "fmt"

// APIError represents a structured error returned to API clients.
type APIError struct {
	Code       string `json:"code"`
	Message    string `json:"message"`
	RequestID  string `json:"request_id,omitempty"`
	HTTPStatus int    `json:"-"`
}

func (e *APIError) Error() string {
	if e.Message == "" {
		return e.Code
	}
	return fmt.Sprintf("%s: %s", e.Code, e.Message)
}

// NewAPIError creates a new APIError based on a predefined template,
// preserving the code and HTTP status while setting a specific message.
func NewAPIError(base *APIError, message string) *APIError {
	return &APIError{
		Code:       base.Code,
		Message:    message,
		HTTPStatus: base.HTTPStatus,
	}
}

var (
	ErrInvalidRequest  = &APIError{Code: "invalid_request", HTTPStatus: 400}
	ErrUnauthorized    = &APIError{Code: "unauthorized", HTTPStatus: 401}
	ErrSessionNotFound = &APIError{Code: "session_not_found", HTTPStatus: 404}
	ErrRateLimited     = &APIError{Code: "rate_limited", HTTPStatus: 429}
	ErrInternal        = &APIError{Code: "internal_error", HTTPStatus: 500}
	ErrProviderError   = &APIError{Code: "provider_error", HTTPStatus: 502}
	ErrProviderTimeout = &APIError{Code: "provider_timeout", HTTPStatus: 504}
)
