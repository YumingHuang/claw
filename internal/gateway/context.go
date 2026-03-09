package gateway

import "context"

type contextKey string

const (
	ContextKeyRequestID contextKey = "request_id"
	ContextKeySessionID contextKey = "session_id"
)

// RequestIDFromContext extracts the request ID from the context.
func RequestIDFromContext(ctx context.Context) string {
	if v, ok := ctx.Value(ContextKeyRequestID).(string); ok {
		return v
	}
	return ""
}
