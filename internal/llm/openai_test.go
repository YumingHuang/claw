package llm

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/YumingHuang/claw/internal/config"
	"github.com/YumingHuang/claw/internal/models"
)

// newTestProvider creates an OpenAIProvider pointing at the given test server.
func newTestProvider(t *testing.T, serverURL string) *OpenAIProvider {
	t.Helper()
	cfg := config.ProviderConfig{
		Name:    "test",
		BaseURL: serverURL,
		APIKey:  "sk-test",
		Model:   "gpt-4o",
		Timeout: 5 * time.Second,
	}
	p, err := NewOpenAIProvider(cfg)
	if err != nil {
		t.Fatalf("NewOpenAIProvider: %v", err)
	}
	return p
}

func TestNewOpenAIProvider(t *testing.T) {
	cfg := config.ProviderConfig{
		Name:    "openai",
		BaseURL: "https://api.openai.com/v1",
		APIKey:  "sk-test",
		Model:   "gpt-4o",
		Timeout: 30 * time.Second,
	}
	p, err := NewOpenAIProvider(cfg)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if p.Name() != "openai" {
		t.Errorf("Name() = %q, want %q", p.Name(), "openai")
	}
}

func TestChat_TextResponse(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			t.Errorf("method = %s, want POST", r.Method)
		}
		if r.Header.Get("Authorization") != "Bearer sk-test" {
			t.Errorf("Authorization header missing or incorrect")
		}

		resp := `{
			"id": "chatcmpl-1",
			"choices": [{
				"index": 0,
				"message": {"role": "assistant", "content": "Hello!"},
				"finish_reason": "stop"
			}],
			"usage": {"prompt_tokens": 10, "completion_tokens": 5, "total_tokens": 15}
		}`
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprint(w, resp)
	}))
	defer server.Close()

	p := newTestProvider(t, server.URL)
	req := &ChatRequest{
		Messages: []models.Message{models.NewUserMessage("Hi")},
	}

	resp, err := p.Chat(context.Background(), req)
	if err != nil {
		t.Fatalf("Chat: %v", err)
	}
	if resp.Content != "Hello!" {
		t.Errorf("Content = %q, want %q", resp.Content, "Hello!")
	}
	if resp.FinishReason != "stop" {
		t.Errorf("FinishReason = %q, want %q", resp.FinishReason, "stop")
	}
	if resp.Usage.TotalTokens != 15 {
		t.Errorf("Usage.TotalTokens = %d, want 15", resp.Usage.TotalTokens)
	}
}

func TestChat_ToolCallResponse(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		resp := `{
			"id": "chatcmpl-2",
			"choices": [{
				"index": 0,
				"message": {
					"role": "assistant",
					"content": null,
					"tool_calls": [{
						"id": "call_abc",
						"type": "function",
						"function": {"name": "get_current_time", "arguments": "{\"timezone\":\"UTC\"}"}
					}]
				},
				"finish_reason": "tool_calls"
			}],
			"usage": {"prompt_tokens": 20, "completion_tokens": 10, "total_tokens": 30}
		}`
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprint(w, resp)
	}))
	defer server.Close()

	p := newTestProvider(t, server.URL)
	resp, err := p.Chat(context.Background(), &ChatRequest{
		Messages: []models.Message{models.NewUserMessage("What time is it?")},
	})
	if err != nil {
		t.Fatalf("Chat: %v", err)
	}
	if len(resp.ToolCalls) != 1 {
		t.Fatalf("ToolCalls len = %d, want 1", len(resp.ToolCalls))
	}
	if resp.ToolCalls[0].Name != "get_current_time" {
		t.Errorf("ToolCalls[0].Name = %q, want %q", resp.ToolCalls[0].Name, "get_current_time")
	}
	if resp.ToolCalls[0].ID != "call_abc" {
		t.Errorf("ToolCalls[0].ID = %q, want %q", resp.ToolCalls[0].ID, "call_abc")
	}

	var args map[string]string
	if err := json.Unmarshal(resp.ToolCalls[0].Arguments, &args); err != nil {
		t.Fatalf("Unmarshal arguments: %v", err)
	}
	if args["timezone"] != "UTC" {
		t.Errorf("timezone = %q, want %q", args["timezone"], "UTC")
	}
}

func TestChat_Error429(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Retry-After", "5")
		w.WriteHeader(http.StatusTooManyRequests)
		fmt.Fprint(w, `{"error":{"message":"rate limited","type":"rate_limit_error"}}`)
	}))
	defer server.Close()

	p := newTestProvider(t, server.URL)
	_, err := p.Chat(context.Background(), &ChatRequest{
		Messages: []models.Message{models.NewUserMessage("test")},
	})
	if err == nil {
		t.Fatal("expected error, got nil")
	}

	var apiErr *models.APIError
	if !errors.As(err, &apiErr) {
		t.Fatalf("expected *models.APIError, got %T: %v", err, err)
	}
	if apiErr.Code != "rate_limited" {
		t.Errorf("Code = %q, want %q", apiErr.Code, "rate_limited")
	}
}

func TestChat_Error500(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
		fmt.Fprint(w, `{"error":{"message":"internal server error"}}`)
	}))
	defer server.Close()

	p := newTestProvider(t, server.URL)
	_, err := p.Chat(context.Background(), &ChatRequest{
		Messages: []models.Message{models.NewUserMessage("test")},
	})
	if err == nil {
		t.Fatal("expected error, got nil")
	}

	var apiErr *models.APIError
	if !errors.As(err, &apiErr) {
		t.Fatalf("expected *models.APIError, got %T: %v", err, err)
	}
	if apiErr.Code != "provider_error" {
		t.Errorf("Code = %q, want %q", apiErr.Code, "provider_error")
	}
}

func TestChat_Timeout(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		time.Sleep(3 * time.Second)
		fmt.Fprint(w, `{"choices":[{"message":{"content":"late"}}]}`)
	}))
	defer server.Close()

	cfg := config.ProviderConfig{
		Name:    "test",
		BaseURL: server.URL,
		APIKey:  "sk-test",
		Model:   "gpt-4o",
		Timeout: 500 * time.Millisecond,
	}
	p, err := NewOpenAIProvider(cfg)
	if err != nil {
		t.Fatalf("NewOpenAIProvider: %v", err)
	}

	_, err = p.Chat(context.Background(), &ChatRequest{
		Messages: []models.Message{models.NewUserMessage("test")},
	})
	if err == nil {
		t.Fatal("expected timeout error, got nil")
	}

	var apiErr *models.APIError
	if !errors.As(err, &apiErr) {
		t.Fatalf("expected *models.APIError, got %T: %v", err, err)
	}
	if apiErr.Code != "provider_timeout" {
		t.Errorf("Code = %q, want %q", apiErr.Code, "provider_timeout")
	}
}

func TestChatStream_Normal(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		flusher, ok := w.(http.Flusher)
		if !ok {
			t.Fatal("expected http.Flusher")
		}

		chunks := []string{
			`data: {"choices":[{"delta":{"role":"assistant","content":"Hel"}}]}`,
			`data: {"choices":[{"delta":{"content":"lo!"}}]}`,
			`data: {"choices":[{"delta":{}}],"usage":{"prompt_tokens":10,"completion_tokens":5,"total_tokens":15}}`,
			`data: [DONE]`,
		}
		for _, c := range chunks {
			fmt.Fprintf(w, "%s\n\n", c)
			flusher.Flush()
		}
	}))
	defer server.Close()

	p := newTestProvider(t, server.URL)
	ch, err := p.ChatStream(context.Background(), &ChatRequest{
		Messages: []models.Message{models.NewUserMessage("Hi")},
	})
	if err != nil {
		t.Fatalf("ChatStream: %v", err)
	}

	var content string
	var lastChunk StreamChunk
	for chunk := range ch {
		if chunk.Err != nil {
			t.Fatalf("chunk error: %v", chunk.Err)
		}
		content += chunk.Delta
		lastChunk = chunk
	}

	if content != "Hello!" {
		t.Errorf("content = %q, want %q", content, "Hello!")
	}
	if lastChunk.Usage == nil {
		t.Fatal("expected usage in last chunk")
	}
	if lastChunk.Usage.TotalTokens != 15 {
		t.Errorf("Usage.TotalTokens = %d, want 15", lastChunk.Usage.TotalTokens)
	}
}

func TestChatStream_WithToolCall(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		flusher := w.(http.Flusher)

		chunks := []string{
			`data: {"choices":[{"delta":{"role":"assistant","tool_calls":[{"index":0,"id":"call_1","type":"function","function":{"name":"get_current_time","arguments":""}}]}}]}`,
			`data: {"choices":[{"delta":{"tool_calls":[{"index":0,"function":{"arguments":"{\"timezone\":"}}]}}]}`,
			`data: {"choices":[{"delta":{"tool_calls":[{"index":0,"function":{"arguments":"\"UTC\"}"}}]}}]}`,
			`data: {"choices":[{"delta":{}}],"usage":{"prompt_tokens":15,"completion_tokens":8,"total_tokens":23}}`,
			`data: [DONE]`,
		}
		for _, c := range chunks {
			fmt.Fprintf(w, "%s\n\n", c)
			flusher.Flush()
		}
	}))
	defer server.Close()

	p := newTestProvider(t, server.URL)
	ch, err := p.ChatStream(context.Background(), &ChatRequest{
		Messages: []models.Message{models.NewUserMessage("What time?")},
	})
	if err != nil {
		t.Fatalf("ChatStream: %v", err)
	}

	var toolCalls []models.ToolCall
	for chunk := range ch {
		if chunk.Err != nil {
			t.Fatalf("chunk error: %v", chunk.Err)
		}
		toolCalls = append(toolCalls, chunk.ToolCalls...)
	}

	if len(toolCalls) == 0 {
		t.Fatal("expected at least one tool call")
	}
}

func TestChatStream_Error(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
		fmt.Fprint(w, `{"error":{"message":"server error"}}`)
	}))
	defer server.Close()

	p := newTestProvider(t, server.URL)
	_, err := p.ChatStream(context.Background(), &ChatRequest{
		Messages: []models.Message{models.NewUserMessage("test")},
	})
	if err == nil {
		t.Fatal("expected error, got nil")
	}

	var apiErr *models.APIError
	if !errors.As(err, &apiErr) {
		t.Fatalf("expected *models.APIError, got %T: %v", err, err)
	}
}

func TestChat_RequestBody(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body map[string]interface{}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Fatalf("decode request body: %v", err)
		}

		if body["model"] != "gpt-4o" {
			t.Errorf("model = %v, want gpt-4o", body["model"])
		}
		if body["stream"] != false {
			t.Errorf("stream = %v, want false", body["stream"])
		}

		msgs, ok := body["messages"].([]interface{})
		if !ok || len(msgs) != 1 {
			t.Fatalf("messages len = %d, want 1", len(msgs))
		}

		w.Header().Set("Content-Type", "application/json")
		fmt.Fprint(w, `{"choices":[{"message":{"content":"ok"},"finish_reason":"stop"}],"usage":{"total_tokens":5}}`)
	}))
	defer server.Close()

	p := newTestProvider(t, server.URL)
	_, err := p.Chat(context.Background(), &ChatRequest{
		Messages:    []models.Message{models.NewUserMessage("test")},
		Temperature: 0.7,
		MaxTokens:   100,
	})
	if err != nil {
		t.Fatalf("Chat: %v", err)
	}
}
