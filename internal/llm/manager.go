package llm

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"strconv"
	"time"

	"github.com/YumingHuang/claw/internal/config"
	"github.com/YumingHuang/claw/internal/metrics"
	"github.com/YumingHuang/claw/internal/models"
)

// ProviderManager wraps multiple Providers and provides automatic
// fallback and retry logic. It implements the Provider interface so
// that Agent can use it transparently.
type ProviderManager struct {
	providers     map[string]Provider
	fallbackOrder []string
	retry         config.RetryConfig
	metrics       *metrics.Collector
}

// NewProviderManager builds a manager from the given provider map,
// fallback ordering, and retry configuration.
func NewProviderManager(providers map[string]Provider, fallbackOrder []string, retry config.RetryConfig) *ProviderManager {
	return &ProviderManager{
		providers:     providers,
		fallbackOrder: fallbackOrder,
		retry:         retry,
	}
}

// SetMetrics sets the metrics collector for LLM observability.
func (m *ProviderManager) SetMetrics(collector *metrics.Collector) {
	m.metrics = collector
}

func (m *ProviderManager) Name() string { return "manager" }

// Chat tries each provider in fallback order with retry logic.
func (m *ProviderManager) Chat(ctx context.Context, req *ChatRequest) (*ChatResponse, error) {
	var lastErr error
	start := time.Now()

	for _, name := range m.fallbackOrder {
		p, ok := m.providers[name]
		if !ok {
			continue
		}

		for attempt := 0; attempt < m.retry.MaxAttempts; attempt++ {
			if err := ctx.Err(); err != nil {
				return nil, fmt.Errorf("context cancelled: %w", err)
			}

			resp, err := p.Chat(ctx, req)
			if err == nil {
				dur := time.Since(start)
				if m.metrics != nil {
					m.metrics.ObserveLLMRequest(name, req.Model, "success", dur)
					m.metrics.ObserveLLMTokens(name, req.Model, resp.Usage.PromptTokens, resp.Usage.CompletionTokens)
				}
				return resp, nil
			}

			if !isRetryable(err) {
				lastErr = err
				// 4xx errors (except 429) should not trigger fallback
				if isClientError(err) {
					return nil, err
				}
				break
			}

			lastErr = err
			slog.Warn("provider call failed, retrying",
				"provider", name,
				"attempt", attempt+1,
				"error", err,
			)

			if wait := retryAfterDuration(err); wait > 0 {
				select {
				case <-time.After(wait):
				case <-ctx.Done():
					return nil, fmt.Errorf("context cancelled during retry wait: %w", ctx.Err())
				}
				continue
			}

			backoff := m.retry.Backoff * time.Duration(1<<uint(attempt))
			select {
			case <-time.After(backoff):
			case <-ctx.Done():
				return nil, fmt.Errorf("context cancelled during backoff: %w", ctx.Err())
			}
		}

		slog.Warn("provider exhausted retries, trying next",
			"provider", name,
			"error", lastErr,
		)
	}

	if lastErr != nil {
		if m.metrics != nil {
			m.metrics.ObserveLLMRequest("", req.Model, "error", time.Since(start))
		}
		return nil, fmt.Errorf("all providers failed: %w", lastErr)
	}
	return nil, models.NewAPIError(models.ErrProviderError, "no providers available")
}

// ChatStream tries each provider in fallback order with retry logic for streaming.
func (m *ProviderManager) ChatStream(ctx context.Context, req *ChatRequest) (<-chan StreamChunk, error) {
	var lastErr error
	start := time.Now()

	for _, name := range m.fallbackOrder {
		p, ok := m.providers[name]
		if !ok {
			continue
		}

		for attempt := 0; attempt < m.retry.MaxAttempts; attempt++ {
			if err := ctx.Err(); err != nil {
				return nil, fmt.Errorf("context cancelled: %w", err)
			}

			ch, err := p.ChatStream(ctx, req)
			if err == nil {
				if m.metrics != nil {
					m.metrics.ObserveLLMRequest(name, req.Model, "success", time.Since(start))
				}
				return ch, nil
			}

			if !isRetryable(err) {
				lastErr = err
				if isClientError(err) {
					return nil, err
				}
				break
			}

			lastErr = err
			slog.Warn("provider stream failed, retrying",
				"provider", name,
				"attempt", attempt+1,
				"error", err,
			)

			if wait := retryAfterDuration(err); wait > 0 {
				select {
				case <-time.After(wait):
				case <-ctx.Done():
					return nil, fmt.Errorf("context cancelled during retry wait: %w", ctx.Err())
				}
				continue
			}

			backoff := m.retry.Backoff * time.Duration(1<<uint(attempt))
			select {
			case <-time.After(backoff):
			case <-ctx.Done():
				return nil, fmt.Errorf("context cancelled during backoff: %w", ctx.Err())
			}
		}

		slog.Warn("provider exhausted retries for stream, trying next",
			"provider", name,
			"error", lastErr,
		)
	}

	if lastErr != nil {
		if m.metrics != nil {
			m.metrics.ObserveLLMRequest("", req.Model, "error", time.Since(start))
		}
		return nil, fmt.Errorf("all providers failed: %w", lastErr)
	}
	return nil, models.NewAPIError(models.ErrProviderError, "no providers available")
}

// isRetryable returns true for errors that should trigger a retry or fallback:
// network errors, 5xx, timeouts, and 429 (rate limited).
func isRetryable(err error) bool {
	var apiErr *models.APIError
	if !errors.As(err, &apiErr) {
		return true // unknown errors (network, etc.) are retryable
	}
	switch apiErr.HTTPStatus {
	case http.StatusTooManyRequests:
		return true
	case http.StatusBadGateway, http.StatusServiceUnavailable, http.StatusGatewayTimeout:
		return true
	}
	if apiErr.Code == "provider_error" || apiErr.Code == "provider_timeout" {
		return true
	}
	return false
}

// isClientError returns true for 4xx errors (except 429) that should not
// trigger fallback to another provider.
func isClientError(err error) bool {
	var apiErr *models.APIError
	if !errors.As(err, &apiErr) {
		return false
	}
	return apiErr.HTTPStatus >= 400 && apiErr.HTTPStatus < 500 &&
		apiErr.HTTPStatus != http.StatusTooManyRequests
}

// retryAfterDuration extracts a Retry-After style hint from rate-limited errors.
// Returns 0 if no hint is available.
func retryAfterDuration(err error) time.Duration {
	var apiErr *models.APIError
	if !errors.As(err, &apiErr) {
		return 0
	}
	if apiErr.Code != "rate_limited" {
		return 0
	}
	// Try to parse a numeric Retry-After value from the message
	// (e.g. "rate limited" won't parse, but providers can embed the value).
	if secs, parseErr := strconv.Atoi(apiErr.Message); parseErr == nil && secs > 0 {
		return time.Duration(secs) * time.Second
	}
	return 0
}
