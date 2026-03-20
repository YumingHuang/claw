package requestctx

import "context"

// Key is the shared context key type used across packages.
type Key string

const (
	RequestIDKey   Key = "request_id"
	SessionIDKey   Key = "session_id"
	ToolProfileKey Key = "tool_profile"
)

func RequestIDFromContext(ctx context.Context) string {
	if v, ok := ctx.Value(RequestIDKey).(string); ok {
		return v
	}
	return ""
}

func SessionIDFromContext(ctx context.Context) string {
	if v, ok := ctx.Value(SessionIDKey).(string); ok {
		return v
	}
	return ""
}

func ToolProfileFromContext(ctx context.Context) string {
	if v, ok := ctx.Value(ToolProfileKey).(string); ok {
		return v
	}
	return ""
}
