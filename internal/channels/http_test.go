package channels

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/YumingHuang/claw/internal/agent"
	"github.com/YumingHuang/claw/internal/audit"
	"github.com/YumingHuang/claw/internal/config"
	"github.com/YumingHuang/claw/internal/gateway"
	"github.com/YumingHuang/claw/internal/llm"
	"github.com/YumingHuang/claw/internal/metrics"
	"github.com/YumingHuang/claw/internal/tools"
)

type fakeProvider struct {
	response *llm.ChatResponse
}

func (f *fakeProvider) Name() string { return "fake" }
func (f *fakeProvider) Chat(_ context.Context, _ *llm.ChatRequest) (*llm.ChatResponse, error) {
	return f.response, nil
}
func (f *fakeProvider) ChatStream(_ context.Context, _ *llm.ChatRequest) (<-chan llm.StreamChunk, error) {
	ch := make(chan llm.StreamChunk, 1)
	ch <- llm.StreamChunk{Delta: f.response.Content, Done: true}
	close(ch)
	return ch, nil
}

type testHTTPChannelOptions struct {
	auth    config.AuthConfig
	rate    config.RateLimitConfig
	auditor *audit.Logger
}

func newTestHTTPChannel(opts ...func(*testHTTPChannelOptions)) *HTTPChannel {
	provider := &fakeProvider{response: &llm.ChatResponse{Content: "Hello!", FinishReason: "stop"}}
	registry := tools.NewRegistry()
	a := agent.NewAgent(provider, registry, agent.AgentOptions{
		SystemPrompt:  "test",
		MaxIterations: 10,
		ContextWindow: 100000,
	})

	ctx := context.Background()
	sessions := gateway.NewMemorySessionStore(ctx, 1*time.Hour, 100, 5*time.Minute)
	queue := agent.NewSessionQueue()
	gw := gateway.NewGateway(a, sessions, queue)
	collector := metrics.New()
	gw.SetMetrics(collector)

	options := testHTTPChannelOptions{}
	for _, opt := range opts {
		opt(&options)
	}

	return NewHTTPChannel(gw, ":0", options.auth, options.rate, options.auditor, collector)
}

func TestHandleChat_Sync(t *testing.T) {
	ch := newTestHTTPChannel()
	body := `{"message":"Hi","session_id":"s1"}`
	req := httptest.NewRequest(http.MethodPost, "/v1/chat", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()

	ch.Router().ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", w.Code, http.StatusOK)
	}

	var resp map[string]interface{}
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if resp["session_id"] != "s1" {
		t.Errorf("session_id = %v, want s1", resp["session_id"])
	}
}

func TestHandleChat_MissingMessage(t *testing.T) {
	ch := newTestHTTPChannel()
	body := `{"session_id":"s1"}`
	req := httptest.NewRequest(http.MethodPost, "/v1/chat", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()

	ch.Router().ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want %d", w.Code, http.StatusBadRequest)
	}
}

func TestHandleChat_AutoSessionID(t *testing.T) {
	ch := newTestHTTPChannel()
	body := `{"message":"Hi"}`
	req := httptest.NewRequest(http.MethodPost, "/v1/chat", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()

	ch.Router().ServeHTTP(w, req)

	var resp map[string]interface{}
	_ = json.Unmarshal(w.Body.Bytes(), &resp)
	if resp["session_id"] == "" {
		t.Error("session_id should be auto-generated")
	}
}

func TestHandleHealth(t *testing.T) {
	ch := newTestHTTPChannel()
	req := httptest.NewRequest(http.MethodGet, "/health", nil)
	w := httptest.NewRecorder()

	ch.Router().ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", w.Code, http.StatusOK)
	}

	var resp map[string]interface{}
	_ = json.Unmarshal(w.Body.Bytes(), &resp)
	if resp["status"] != "ok" {
		t.Errorf("status = %v, want ok", resp["status"])
	}
}

func TestHandleGetSession_NotFound(t *testing.T) {
	ch := newTestHTTPChannel()
	req := httptest.NewRequest(http.MethodGet, "/v1/sessions/nonexistent", nil)
	w := httptest.NewRecorder()

	ch.Router().ServeHTTP(w, req)

	if w.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want %d", w.Code, http.StatusNotFound)
	}
}

func TestHandleDeleteSession(t *testing.T) {
	ch := newTestHTTPChannel()

	// Create a session first
	body := `{"message":"Hi","session_id":"del-me"}`
	req := httptest.NewRequest(http.MethodPost, "/v1/chat", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	ch.Router().ServeHTTP(w, req)

	// Delete it
	req = httptest.NewRequest(http.MethodDelete, "/v1/sessions/del-me", nil)
	w = httptest.NewRecorder()
	ch.Router().ServeHTTP(w, req)

	if w.Code != http.StatusNoContent {
		t.Fatalf("status = %d, want %d", w.Code, http.StatusNoContent)
	}
}

func TestRequestIDMiddleware(t *testing.T) {
	ch := newTestHTTPChannel()
	req := httptest.NewRequest(http.MethodGet, "/health", nil)
	w := httptest.NewRecorder()

	ch.Router().ServeHTTP(w, req)

	if w.Header().Get("X-Request-ID") == "" {
		t.Error("X-Request-ID header should be set")
	}
}

func TestHandleStatus(t *testing.T) {
	ch := newTestHTTPChannel()
	req := httptest.NewRequest(http.MethodGet, "/status", nil)
	w := httptest.NewRecorder()

	ch.Router().ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", w.Code, http.StatusOK)
	}

	var resp map[string]interface{}
	_ = json.Unmarshal(w.Body.Bytes(), &resp)
	if resp["active_sessions"] == nil {
		t.Error("active_sessions should be present")
	}
}

func TestHandleReady(t *testing.T) {
	ch := newTestHTTPChannel()
	req := httptest.NewRequest(http.MethodGet, "/ready", nil)
	w := httptest.NewRecorder()

	ch.Router().ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", w.Code, http.StatusOK)
	}
}

func TestHandleMetrics(t *testing.T) {
	ch := newTestHTTPChannel()
	req := httptest.NewRequest(http.MethodGet, "/metrics", nil)
	w := httptest.NewRecorder()

	ch.Router().ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", w.Code, http.StatusOK)
	}
	if !strings.Contains(w.Body.String(), "claw_app_ready") {
		t.Fatal("expected Prometheus metrics output")
	}
}

func TestAuthMiddleware_Authorized(t *testing.T) {
	ch := newTestHTTPChannel(func(o *testHTTPChannelOptions) {
		o.auth = config.AuthConfig{Enabled: true, APIKeys: []string{"secret"}}
	})
	req := httptest.NewRequest(http.MethodGet, "/health", nil)
	req.Header.Set("X-API-Key", "secret")
	w := httptest.NewRecorder()

	ch.Router().ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", w.Code, http.StatusOK)
	}
}

func TestAuthMiddleware_Unauthorized(t *testing.T) {
	ch := newTestHTTPChannel(func(o *testHTTPChannelOptions) {
		o.auth = config.AuthConfig{Enabled: true, APIKeys: []string{"secret"}}
	})
	req := httptest.NewRequest(http.MethodGet, "/health", nil)
	w := httptest.NewRecorder()

	ch.Router().ServeHTTP(w, req)

	if w.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want %d", w.Code, http.StatusUnauthorized)
	}
}

func TestRateLimitMiddleware(t *testing.T) {
	ch := newTestHTTPChannel(func(o *testHTTPChannelOptions) {
		o.rate = config.RateLimitConfig{Enabled: true, RequestsPerMinute: 1, Burst: 1}
	})

	req := httptest.NewRequest(http.MethodGet, "/health", nil)
	req.RemoteAddr = "127.0.0.1:12345"
	w := httptest.NewRecorder()
	ch.Router().ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("first status = %d, want %d", w.Code, http.StatusOK)
	}

	req = httptest.NewRequest(http.MethodGet, "/health", nil)
	req.RemoteAddr = "127.0.0.1:12345"
	w = httptest.NewRecorder()
	ch.Router().ServeHTTP(w, req)

	if w.Code != http.StatusTooManyRequests {
		t.Fatalf("second status = %d, want %d", w.Code, http.StatusTooManyRequests)
	}
	if w.Header().Get("Retry-After") == "" {
		t.Fatal("Retry-After should be set")
	}
}
