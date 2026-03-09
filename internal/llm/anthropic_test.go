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

	"github.com/mingminliu/claw/internal/config"
	"github.com/mingminliu/claw/internal/models"
)

func newTestAnthropicProvider(t *testing.T, serverURL string) *AnthropicProvider {
	t.Helper()
	cfg := config.ProviderConfig{
		Name:    "claude",
		BaseURL: serverURL,
		APIKey:  "sk-ant-test",
		Model:   "claude-3-5-sonnet-20241022",
		Timeout: 5 * time.Second,
	}
	p, err := NewAnthropicProvider(cfg)
	if err != nil {
		t.Fatalf("NewAnthropicProvider: %v", err)
	}
	return p
}

func TestAnthropicChat_TextResponse(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("x-api-key") != "sk-ant-test" {
			t.Errorf("x-api-key header = %q", r.Header.Get("x-api-key"))
		}
		if r.Header.Get("anthropic-version") != anthropicAPIVersion {
			t.Errorf("anthropic-version = %q", r.Header.Get("anthropic-version"))
		}

		resp := `{
			"id": "msg_01",
			"type": "message",
			"role": "assistant",
			"content": [{"type": "text", "text": "Hello from Claude!"}],
			"stop_reason": "end_turn",
			"usage": {"input_tokens": 12, "output_tokens": 8}
		}`
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprint(w, resp)
	}))
	defer server.Close()

	p := newTestAnthropicProvider(t, server.URL)
	resp, err := p.Chat(context.Background(), &ChatRequest{
		Messages: []models.Message{models.NewUserMessage("Hi")},
	})
	if err != nil {
		t.Fatalf("Chat: %v", err)
	}
	if resp.Content != "Hello from Claude!" {
		t.Errorf("Content = %q, want %q", resp.Content, "Hello from Claude!")
	}
	if resp.Usage.PromptTokens != 12 {
		t.Errorf("PromptTokens = %d, want 12", resp.Usage.PromptTokens)
	}
	if resp.Usage.CompletionTokens != 8 {
		t.Errorf("CompletionTokens = %d, want 8", resp.Usage.CompletionTokens)
	}
	if resp.Usage.TotalTokens != 20 {
		t.Errorf("TotalTokens = %d, want 20", resp.Usage.TotalTokens)
	}
	if resp.FinishReason != "end_turn" {
		t.Errorf("FinishReason = %q, want %q", resp.FinishReason, "end_turn")
	}
}

func TestAnthropicChat_ToolCallResponse(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		resp := `{
			"id": "msg_02",
			"type": "message",
			"role": "assistant",
			"content": [
				{"type": "text", "text": "Let me check the time."},
				{
					"type": "tool_use",
					"id": "toolu_01",
					"name": "get_current_time",
					"input": {"timezone": "UTC"}
				}
			],
			"stop_reason": "tool_use",
			"usage": {"input_tokens": 20, "output_tokens": 15}
		}`
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprint(w, resp)
	}))
	defer server.Close()

	p := newTestAnthropicProvider(t, server.URL)
	resp, err := p.Chat(context.Background(), &ChatRequest{
		Messages: []models.Message{models.NewUserMessage("What time is it?")},
	})
	if err != nil {
		t.Fatalf("Chat: %v", err)
	}
	if resp.Content != "Let me check the time." {
		t.Errorf("Content = %q", resp.Content)
	}
	if len(resp.ToolCalls) != 1 {
		t.Fatalf("ToolCalls len = %d, want 1", len(resp.ToolCalls))
	}
	if resp.ToolCalls[0].Name != "get_current_time" {
		t.Errorf("ToolCalls[0].Name = %q", resp.ToolCalls[0].Name)
	}
	if resp.ToolCalls[0].ID != "toolu_01" {
		t.Errorf("ToolCalls[0].ID = %q", resp.ToolCalls[0].ID)
	}

	var args map[string]string
	if err := json.Unmarshal(resp.ToolCalls[0].Arguments, &args); err != nil {
		t.Fatalf("Unmarshal arguments: %v", err)
	}
	if args["timezone"] != "UTC" {
		t.Errorf("timezone = %q, want %q", args["timezone"], "UTC")
	}
}

func TestAnthropicChat_SystemMessage(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body map[string]interface{}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Fatalf("decode request body: %v", err)
		}

		systemVal, ok := body["system"]
		if !ok {
			t.Fatal("expected 'system' field in request body")
		}
		if systemVal != "You are a helpful assistant." {
			t.Errorf("system = %q", systemVal)
		}

		msgs, ok := body["messages"].([]interface{})
		if !ok {
			t.Fatal("expected messages array")
		}
		for _, m := range msgs {
			msg := m.(map[string]interface{})
			if msg["role"] == "system" {
				t.Error("system message should not appear in messages array")
			}
		}

		w.Header().Set("Content-Type", "application/json")
		fmt.Fprint(w, `{
			"id": "msg_03",
			"type": "message",
			"role": "assistant",
			"content": [{"type": "text", "text": "ok"}],
			"stop_reason": "end_turn",
			"usage": {"input_tokens": 5, "output_tokens": 2}
		}`)
	}))
	defer server.Close()

	p := newTestAnthropicProvider(t, server.URL)
	_, err := p.Chat(context.Background(), &ChatRequest{
		Messages: []models.Message{
			models.NewSystemMessage("You are a helpful assistant."),
			models.NewUserMessage("Hi"),
		},
	})
	if err != nil {
		t.Fatalf("Chat: %v", err)
	}
}

func TestAnthropicChat_Error429(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusTooManyRequests)
		fmt.Fprint(w, `{"error":{"type":"rate_limit_error","message":"rate limited"}}`)
	}))
	defer server.Close()

	p := newTestAnthropicProvider(t, server.URL)
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

func TestAnthropicChat_Error500(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
		fmt.Fprint(w, `{"error":{"type":"api_error","message":"internal error"}}`)
	}))
	defer server.Close()

	p := newTestAnthropicProvider(t, server.URL)
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

func TestAnthropicChat_Timeout(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		time.Sleep(3 * time.Second)
	}))
	defer server.Close()

	cfg := config.ProviderConfig{
		Name:    "claude",
		BaseURL: server.URL,
		APIKey:  "sk-ant-test",
		Model:   "claude-3-5-sonnet-20241022",
		Timeout: 200 * time.Millisecond,
	}
	p, _ := NewAnthropicProvider(cfg)

	_, err := p.Chat(context.Background(), &ChatRequest{
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

func TestAnthropicChatStream_Normal(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		flusher := w.(http.Flusher)

		events := []string{
			"event: message_start\ndata: {\"type\":\"message_start\",\"message\":{\"id\":\"msg_01\",\"usage\":{\"input_tokens\":10}}}",
			"event: content_block_start\ndata: {\"type\":\"content_block_start\",\"content_block\":{\"type\":\"text\",\"text\":\"\"}}",
			"event: content_block_delta\ndata: {\"type\":\"content_block_delta\",\"delta\":{\"type\":\"text_delta\",\"text\":\"Hel\"}}",
			"event: content_block_delta\ndata: {\"type\":\"content_block_delta\",\"delta\":{\"type\":\"text_delta\",\"text\":\"lo!\"}}",
			"event: content_block_stop\ndata: {\"type\":\"content_block_stop\"}",
			"event: message_delta\ndata: {\"type\":\"message_delta\",\"usage\":{\"output_tokens\":5}}",
			"event: message_stop\ndata: {\"type\":\"message_stop\"}",
		}
		for _, e := range events {
			fmt.Fprintf(w, "%s\n\n", e)
			flusher.Flush()
		}
	}))
	defer server.Close()

	p := newTestAnthropicProvider(t, server.URL)
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
	if lastChunk.Usage.PromptTokens != 10 {
		t.Errorf("PromptTokens = %d, want 10", lastChunk.Usage.PromptTokens)
	}
	if lastChunk.Usage.CompletionTokens != 5 {
		t.Errorf("CompletionTokens = %d, want 5", lastChunk.Usage.CompletionTokens)
	}
}

func TestAnthropicChatStream_WithToolCall(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		flusher := w.(http.Flusher)

		events := []string{
			"event: message_start\ndata: {\"type\":\"message_start\",\"message\":{\"id\":\"msg_02\",\"usage\":{\"input_tokens\":15}}}",
			"event: content_block_start\ndata: {\"type\":\"content_block_start\",\"content_block\":{\"type\":\"tool_use\",\"id\":\"toolu_01\",\"name\":\"get_current_time\"}}",
			"event: content_block_delta\ndata: {\"type\":\"content_block_delta\",\"delta\":{\"type\":\"input_json_delta\",\"partial_json\":\"{\\\"timezone\\\":\"}}",
			"event: content_block_delta\ndata: {\"type\":\"content_block_delta\",\"delta\":{\"type\":\"input_json_delta\",\"partial_json\":\"\\\"UTC\\\"}\"}}",
			"event: content_block_stop\ndata: {\"type\":\"content_block_stop\"}",
			"event: message_delta\ndata: {\"type\":\"message_delta\",\"usage\":{\"output_tokens\":12}}",
			"event: message_stop\ndata: {\"type\":\"message_stop\"}",
		}
		for _, e := range events {
			fmt.Fprintf(w, "%s\n\n", e)
			flusher.Flush()
		}
	}))
	defer server.Close()

	p := newTestAnthropicProvider(t, server.URL)
	ch, err := p.ChatStream(context.Background(), &ChatRequest{
		Messages: []models.Message{models.NewUserMessage("What time is it?")},
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

	if len(toolCalls) != 1 {
		t.Fatalf("toolCalls len = %d, want 1", len(toolCalls))
	}
	if toolCalls[0].Name != "get_current_time" {
		t.Errorf("Name = %q", toolCalls[0].Name)
	}
	if toolCalls[0].ID != "toolu_01" {
		t.Errorf("ID = %q", toolCalls[0].ID)
	}

	var args map[string]string
	if err := json.Unmarshal(toolCalls[0].Arguments, &args); err != nil {
		t.Fatalf("Unmarshal arguments: %v", err)
	}
	if args["timezone"] != "UTC" {
		t.Errorf("timezone = %q, want %q", args["timezone"], "UTC")
	}
}

func TestAnthropicChat_ToolResultMessage(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body map[string]interface{}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Fatalf("decode: %v", err)
		}

		msgs := body["messages"].([]interface{})
		lastMsg := msgs[len(msgs)-1].(map[string]interface{})
		if lastMsg["role"] != "user" {
			t.Errorf("tool result should have role 'user', got %q", lastMsg["role"])
		}
		content := lastMsg["content"].([]interface{})
		block := content[0].(map[string]interface{})
		if block["type"] != "tool_result" {
			t.Errorf("block type = %q, want 'tool_result'", block["type"])
		}
		if block["tool_use_id"] != "toolu_01" {
			t.Errorf("tool_use_id = %q", block["tool_use_id"])
		}

		w.Header().Set("Content-Type", "application/json")
		fmt.Fprint(w, `{
			"id": "msg_04",
			"type": "message",
			"role": "assistant",
			"content": [{"type": "text", "text": "The time is 10:00 UTC"}],
			"stop_reason": "end_turn",
			"usage": {"input_tokens": 30, "output_tokens": 10}
		}`)
	}))
	defer server.Close()

	p := newTestAnthropicProvider(t, server.URL)
	toolResult := models.NewToolResultMessage("toolu_01", models.ToolResult{Content: "2026-03-06T10:00:00Z"})
	_, err := p.Chat(context.Background(), &ChatRequest{
		Messages: []models.Message{
			models.NewUserMessage("What time is it?"),
			models.NewAssistantMessage(""),
			toolResult,
		},
	})
	if err != nil {
		t.Fatalf("Chat: %v", err)
	}
}

func TestToAnthropicMessages(t *testing.T) {
	msgs := []models.Message{
		models.NewSystemMessage("You are helpful."),
		models.NewUserMessage("Hi"),
		models.NewAssistantMessage("Hello!"),
	}

	anthMsgs, system := toAnthropicMessages(msgs)

	if system != "You are helpful." {
		t.Errorf("system = %q, want %q", system, "You are helpful.")
	}
	if len(anthMsgs) != 2 {
		t.Fatalf("len = %d, want 2", len(anthMsgs))
	}
	if anthMsgs[0]["role"] != "user" {
		t.Errorf("[0].role = %q", anthMsgs[0]["role"])
	}
	if anthMsgs[1]["role"] != "assistant" {
		t.Errorf("[1].role = %q", anthMsgs[1]["role"])
	}
}

func TestToAnthropicTools(t *testing.T) {
	schemas := []ToolSchema{
		{
			Type: "function",
			Function: FunctionDef{
				Name:        "get_time",
				Description: "Get current time",
				Parameters:  json.RawMessage(`{"type":"object","properties":{"tz":{"type":"string"}}}`),
			},
		},
	}

	tools := toAnthropicTools(schemas)
	if len(tools) != 1 {
		t.Fatalf("len = %d, want 1", len(tools))
	}
	if tools[0]["name"] != "get_time" {
		t.Errorf("name = %q", tools[0]["name"])
	}
	if tools[0]["description"] != "Get current time" {
		t.Errorf("description = %q", tools[0]["description"])
	}
	if tools[0]["input_schema"] == nil {
		t.Error("input_schema should not be nil")
	}
}
