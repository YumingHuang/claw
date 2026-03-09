package llm

import (
	"context"
	"errors"
	"sync/atomic"
	"testing"
	"time"

	"github.com/YumingHuang/claw/internal/config"
	"github.com/YumingHuang/claw/internal/models"
)

// --- mock provider for manager tests ---

type mockProvider struct {
	name     string
	chatFunc func(ctx context.Context, req *ChatRequest) (*ChatResponse, error)
}

func (m *mockProvider) Name() string { return m.name }

func (m *mockProvider) Chat(ctx context.Context, req *ChatRequest) (*ChatResponse, error) {
	return m.chatFunc(ctx, req)
}

func (m *mockProvider) ChatStream(ctx context.Context, req *ChatRequest) (<-chan StreamChunk, error) {
	resp, err := m.chatFunc(ctx, req)
	if err != nil {
		return nil, err
	}
	ch := make(chan StreamChunk, 1)
	ch <- StreamChunk{Delta: resp.Content, Done: true, Usage: &resp.Usage}
	close(ch)
	return ch, nil
}

func newMockProvider(name string, fn func(ctx context.Context, req *ChatRequest) (*ChatResponse, error)) *mockProvider {
	return &mockProvider{name: name, chatFunc: fn}
}

func TestProviderManager_PrimarySuccess(t *testing.T) {
	primary := newMockProvider("primary", func(_ context.Context, _ *ChatRequest) (*ChatResponse, error) {
		return &ChatResponse{Content: "from primary"}, nil
	})
	backup := newMockProvider("backup", func(_ context.Context, _ *ChatRequest) (*ChatResponse, error) {
		return &ChatResponse{Content: "from backup"}, nil
	})

	mgr := NewProviderManager(
		map[string]Provider{"primary": primary, "backup": backup},
		[]string{"primary", "backup"},
		config.RetryConfig{MaxAttempts: 2, Backoff: 10 * time.Millisecond},
	)

	resp, err := mgr.Chat(context.Background(), &ChatRequest{
		Messages: []models.Message{models.NewUserMessage("hi")},
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if resp.Content != "from primary" {
		t.Errorf("Content = %q, want %q", resp.Content, "from primary")
	}
}

func TestProviderManager_FallbackOnServerError(t *testing.T) {
	primary := newMockProvider("primary", func(_ context.Context, _ *ChatRequest) (*ChatResponse, error) {
		return nil, models.NewAPIError(models.ErrProviderError, "internal server error")
	})
	backup := newMockProvider("backup", func(_ context.Context, _ *ChatRequest) (*ChatResponse, error) {
		return &ChatResponse{Content: "from backup"}, nil
	})

	mgr := NewProviderManager(
		map[string]Provider{"primary": primary, "backup": backup},
		[]string{"primary", "backup"},
		config.RetryConfig{MaxAttempts: 1, Backoff: 10 * time.Millisecond},
	)

	resp, err := mgr.Chat(context.Background(), &ChatRequest{
		Messages: []models.Message{models.NewUserMessage("hi")},
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if resp.Content != "from backup" {
		t.Errorf("Content = %q, want %q", resp.Content, "from backup")
	}
}

func TestProviderManager_NoFallbackOn4xx(t *testing.T) {
	primary := newMockProvider("primary", func(_ context.Context, _ *ChatRequest) (*ChatResponse, error) {
		return nil, models.NewAPIError(models.ErrInvalidRequest, "bad request")
	})
	backupCalled := false
	backup := newMockProvider("backup", func(_ context.Context, _ *ChatRequest) (*ChatResponse, error) {
		backupCalled = true
		return &ChatResponse{Content: "from backup"}, nil
	})

	mgr := NewProviderManager(
		map[string]Provider{"primary": primary, "backup": backup},
		[]string{"primary", "backup"},
		config.RetryConfig{MaxAttempts: 2, Backoff: 10 * time.Millisecond},
	)

	_, err := mgr.Chat(context.Background(), &ChatRequest{
		Messages: []models.Message{models.NewUserMessage("hi")},
	})
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if backupCalled {
		t.Error("backup should not be called on 4xx error")
	}

	var apiErr *models.APIError
	if !errors.As(err, &apiErr) {
		t.Fatalf("expected *models.APIError, got %T", err)
	}
	if apiErr.Code != "invalid_request" {
		t.Errorf("Code = %q, want %q", apiErr.Code, "invalid_request")
	}
}

func TestProviderManager_RetryOnTimeout(t *testing.T) {
	var calls int32
	primary := newMockProvider("primary", func(_ context.Context, _ *ChatRequest) (*ChatResponse, error) {
		n := atomic.AddInt32(&calls, 1)
		if n < 2 {
			return nil, models.NewAPIError(models.ErrProviderTimeout, "timeout")
		}
		return &ChatResponse{Content: "recovered"}, nil
	})

	mgr := NewProviderManager(
		map[string]Provider{"primary": primary},
		[]string{"primary"},
		config.RetryConfig{MaxAttempts: 3, Backoff: 10 * time.Millisecond},
	)

	resp, err := mgr.Chat(context.Background(), &ChatRequest{
		Messages: []models.Message{models.NewUserMessage("hi")},
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if resp.Content != "recovered" {
		t.Errorf("Content = %q, want %q", resp.Content, "recovered")
	}
	if atomic.LoadInt32(&calls) != 2 {
		t.Errorf("calls = %d, want 2", atomic.LoadInt32(&calls))
	}
}

func TestProviderManager_AllFail(t *testing.T) {
	primary := newMockProvider("primary", func(_ context.Context, _ *ChatRequest) (*ChatResponse, error) {
		return nil, models.NewAPIError(models.ErrProviderError, "primary down")
	})
	backup := newMockProvider("backup", func(_ context.Context, _ *ChatRequest) (*ChatResponse, error) {
		return nil, models.NewAPIError(models.ErrProviderError, "backup down")
	})

	mgr := NewProviderManager(
		map[string]Provider{"primary": primary, "backup": backup},
		[]string{"primary", "backup"},
		config.RetryConfig{MaxAttempts: 1, Backoff: 10 * time.Millisecond},
	)

	_, err := mgr.Chat(context.Background(), &ChatRequest{
		Messages: []models.Message{models.NewUserMessage("hi")},
	})
	if err == nil {
		t.Fatal("expected error, got nil")
	}
}

func TestProviderManager_ContextCancelled(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel() // cancel immediately

	primary := newMockProvider("primary", func(_ context.Context, _ *ChatRequest) (*ChatResponse, error) {
		return &ChatResponse{Content: "ok"}, nil
	})

	mgr := NewProviderManager(
		map[string]Provider{"primary": primary},
		[]string{"primary"},
		config.RetryConfig{MaxAttempts: 2, Backoff: 10 * time.Millisecond},
	)

	_, err := mgr.Chat(ctx, &ChatRequest{
		Messages: []models.Message{models.NewUserMessage("hi")},
	})
	if err == nil {
		t.Fatal("expected error, got nil")
	}
}

func TestProviderManager_StreamFallback(t *testing.T) {
	primary := newMockProvider("primary", func(_ context.Context, _ *ChatRequest) (*ChatResponse, error) {
		return nil, models.NewAPIError(models.ErrProviderError, "down")
	})
	backup := newMockProvider("backup", func(_ context.Context, _ *ChatRequest) (*ChatResponse, error) {
		return &ChatResponse{Content: "stream from backup", Usage: models.Usage{TotalTokens: 10}}, nil
	})

	mgr := NewProviderManager(
		map[string]Provider{"primary": primary, "backup": backup},
		[]string{"primary", "backup"},
		config.RetryConfig{MaxAttempts: 1, Backoff: 10 * time.Millisecond},
	)

	ch, err := mgr.ChatStream(context.Background(), &ChatRequest{
		Messages: []models.Message{models.NewUserMessage("hi")},
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	var content string
	for chunk := range ch {
		content += chunk.Delta
	}
	if content != "stream from backup" {
		t.Errorf("content = %q, want %q", content, "stream from backup")
	}
}

func TestProviderManager_RateLimitRetry(t *testing.T) {
	var calls int32
	primary := newMockProvider("primary", func(_ context.Context, _ *ChatRequest) (*ChatResponse, error) {
		n := atomic.AddInt32(&calls, 1)
		if n == 1 {
			return nil, models.NewAPIError(models.ErrRateLimited, "rate limited")
		}
		return &ChatResponse{Content: "ok after wait"}, nil
	})

	mgr := NewProviderManager(
		map[string]Provider{"primary": primary},
		[]string{"primary"},
		config.RetryConfig{MaxAttempts: 3, Backoff: 10 * time.Millisecond},
	)

	resp, err := mgr.Chat(context.Background(), &ChatRequest{
		Messages: []models.Message{models.NewUserMessage("hi")},
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if resp.Content != "ok after wait" {
		t.Errorf("Content = %q, want %q", resp.Content, "ok after wait")
	}
}
