package models

import (
	"encoding/json"
	"errors"
	"testing"
)

func TestAPIError_Error(t *testing.T) {
	tests := []struct {
		name string
		err  *APIError
		want string
	}{
		{
			name: "with message",
			err:  &APIError{Code: "invalid_request", Message: "session_id is required", HTTPStatus: 400},
			want: "invalid_request: session_id is required",
		},
		{
			name: "empty message",
			err:  &APIError{Code: "internal_error", Message: "", HTTPStatus: 500},
			want: "internal_error",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.err.Error(); got != tt.want {
				t.Errorf("Error() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestAPIError_ImplementsError(t *testing.T) {
	var err error = &APIError{Code: "test", Message: "msg", HTTPStatus: 400}
	if err.Error() == "" {
		t.Error("expected non-empty error string")
	}
}

func TestNewAPIError(t *testing.T) {
	tests := []struct {
		name       string
		base       *APIError
		message    string
		wantCode   string
		wantStatus int
		wantMsg    string
	}{
		{
			name:       "from invalid_request",
			base:       ErrInvalidRequest,
			message:    "missing field: message",
			wantCode:   "invalid_request",
			wantStatus: 400,
			wantMsg:    "missing field: message",
		},
		{
			name:       "from provider_error",
			base:       ErrProviderError,
			message:    "openai returned 500",
			wantCode:   "provider_error",
			wantStatus: 502,
			wantMsg:    "openai returned 500",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := NewAPIError(tt.base, tt.message)

			if err.Code != tt.wantCode {
				t.Errorf("Code = %q, want %q", err.Code, tt.wantCode)
			}
			if err.HTTPStatus != tt.wantStatus {
				t.Errorf("HTTPStatus = %d, want %d", err.HTTPStatus, tt.wantStatus)
			}
			if err.Message != tt.wantMsg {
				t.Errorf("Message = %q, want %q", err.Message, tt.wantMsg)
			}
		})
	}
}

func TestNewAPIError_DoesNotMutateBase(t *testing.T) {
	original := ErrInvalidRequest.Message
	_ = NewAPIError(ErrInvalidRequest, "custom message")

	if ErrInvalidRequest.Message != original {
		t.Errorf("base error mutated: Message = %q, want %q", ErrInvalidRequest.Message, original)
	}
}

func TestPredefinedErrors(t *testing.T) {
	tests := []struct {
		name       string
		err        *APIError
		wantCode   string
		wantStatus int
	}{
		{"ErrInvalidRequest", ErrInvalidRequest, "invalid_request", 400},
		{"ErrUnauthorized", ErrUnauthorized, "unauthorized", 401},
		{"ErrSessionNotFound", ErrSessionNotFound, "session_not_found", 404},
		{"ErrRateLimited", ErrRateLimited, "rate_limited", 429},
		{"ErrInternal", ErrInternal, "internal_error", 500},
		{"ErrProviderError", ErrProviderError, "provider_error", 502},
		{"ErrProviderTimeout", ErrProviderTimeout, "provider_timeout", 504},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if tt.err.Code != tt.wantCode {
				t.Errorf("Code = %q, want %q", tt.err.Code, tt.wantCode)
			}
			if tt.err.HTTPStatus != tt.wantStatus {
				t.Errorf("HTTPStatus = %d, want %d", tt.err.HTTPStatus, tt.wantStatus)
			}
		})
	}
}

func TestAPIError_JSONMarshal(t *testing.T) {
	err := &APIError{
		Code:      "invalid_request",
		Message:   "bad input",
		RequestID: "req-abc",
	}

	data, jsonErr := json.Marshal(err)
	if jsonErr != nil {
		t.Fatalf("Marshal: %v", jsonErr)
	}

	var m map[string]interface{}
	if jsonErr := json.Unmarshal(data, &m); jsonErr != nil {
		t.Fatalf("Unmarshal: %v", jsonErr)
	}

	if m["code"] != "invalid_request" {
		t.Errorf("code = %v, want invalid_request", m["code"])
	}
	if _, exists := m["http_status"]; exists {
		t.Error("http_status should not appear in JSON (json:\"-\")")
	}
}

func TestAPIError_Unwrap(t *testing.T) {
	var target *APIError
	err := NewAPIError(ErrInvalidRequest, "test")

	if !errors.As(err, &target) {
		t.Error("errors.As should match *APIError")
	}
}
