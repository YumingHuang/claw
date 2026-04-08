package requestctx

import (
	"context"
	"testing"
)

func TestRequestIDFromContext(t *testing.T) {
	ctx := context.WithValue(context.Background(), RequestIDKey, "req-123")
	if got := RequestIDFromContext(ctx); got != "req-123" {
		t.Errorf("expected req-123, got %q", got)
	}
	if got := RequestIDFromContext(context.Background()); got != "" {
		t.Errorf("expected empty, got %q", got)
	}
}

func TestSessionIDFromContext(t *testing.T) {
	ctx := context.WithValue(context.Background(), SessionIDKey, "sess-456")
	if got := SessionIDFromContext(ctx); got != "sess-456" {
		t.Errorf("expected sess-456, got %q", got)
	}
	if got := SessionIDFromContext(context.Background()); got != "" {
		t.Errorf("expected empty, got %q", got)
	}
}

func TestToolProfileFromContext(t *testing.T) {
	ctx := context.WithValue(context.Background(), ToolProfileKey, "default")
	if got := ToolProfileFromContext(ctx); got != "default" {
		t.Errorf("expected default, got %q", got)
	}
	if got := ToolProfileFromContext(context.Background()); got != "" {
		t.Errorf("expected empty, got %q", got)
	}
}
