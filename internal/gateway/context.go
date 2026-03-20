package gateway

import (
	"context"

	"github.com/YumingHuang/claw/internal/requestctx"
)

type contextKey = requestctx.Key

const (
	ContextKeyRequestID = requestctx.RequestIDKey
	ContextKeySessionID = requestctx.SessionIDKey
)

// RequestIDFromContext extracts the request ID from the context.
func RequestIDFromContext(ctx context.Context) string {
	return requestctx.RequestIDFromContext(ctx)
}

// SessionIDFromContext extracts the session ID from the context.
func SessionIDFromContext(ctx context.Context) string {
	return requestctx.SessionIDFromContext(ctx)
}
