package models

import (
	"encoding/json"
	"testing"
)

func TestChatRequest_JSONParsing(t *testing.T) {
	tests := []struct {
		name      string
		input     string
		wantID    string
		wantMsg   string
		wantStream bool
	}{
		{
			name:       "full request",
			input:      `{"session_id":"abc-123","message":"hello","stream":true}`,
			wantID:     "abc-123",
			wantMsg:    "hello",
			wantStream: true,
		},
		{
			name:       "minimal request",
			input:      `{"message":"hi"}`,
			wantID:     "",
			wantMsg:    "hi",
			wantStream: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var req ChatRequest
			if err := json.Unmarshal([]byte(tt.input), &req); err != nil {
				t.Fatalf("Unmarshal: %v", err)
			}
			if req.SessionID != tt.wantID {
				t.Errorf("SessionID = %q, want %q", req.SessionID, tt.wantID)
			}
			if req.Message != tt.wantMsg {
				t.Errorf("Message = %q, want %q", req.Message, tt.wantMsg)
			}
			if req.Stream != tt.wantStream {
				t.Errorf("Stream = %v, want %v", req.Stream, tt.wantStream)
			}
		})
	}
}

func TestChatResponse_JSONMarshal(t *testing.T) {
	resp := ChatResponse{
		SessionID: "abc-123",
		RequestID: "req-001",
		Message: Message{
			Role:    "assistant",
			Content: "hello back",
		},
		Usage:          &Usage{PromptTokens: 10, CompletionTokens: 5, TotalTokens: 15},
		ToolCallsCount: 2,
	}

	data, err := json.Marshal(resp)
	if err != nil {
		t.Fatalf("Marshal: %v", err)
	}

	var m map[string]interface{}
	if err := json.Unmarshal(data, &m); err != nil {
		t.Fatalf("Unmarshal: %v", err)
	}

	if m["session_id"] != "abc-123" {
		t.Errorf("session_id = %v, want abc-123", m["session_id"])
	}
	if m["request_id"] != "req-001" {
		t.Errorf("request_id = %v, want req-001", m["request_id"])
	}
	if int(m["tool_calls_count"].(float64)) != 2 {
		t.Errorf("tool_calls_count = %v, want 2", m["tool_calls_count"])
	}
}

func TestChatResponse_UsageOmitEmpty(t *testing.T) {
	resp := ChatResponse{
		SessionID: "s1",
		RequestID: "r1",
		Message:   Message{Role: "assistant", Content: "ok"},
	}

	data, err := json.Marshal(resp)
	if err != nil {
		t.Fatalf("Marshal: %v", err)
	}

	var m map[string]interface{}
	if err := json.Unmarshal(data, &m); err != nil {
		t.Fatalf("Unmarshal: %v", err)
	}

	if _, exists := m["usage"]; exists {
		t.Error("usage should be omitted when nil")
	}
}

func TestStreamChunk_Fields(t *testing.T) {
	chunk := StreamChunk{
		Delta: "hello",
		Done:  false,
	}

	if chunk.Delta != "hello" {
		t.Errorf("Delta = %q, want %q", chunk.Delta, "hello")
	}
	if chunk.Done {
		t.Error("Done should be false")
	}

	doneChunk := StreamChunk{
		Done:  true,
		Usage: &Usage{TotalTokens: 100},
	}

	if !doneChunk.Done {
		t.Error("Done should be true")
	}
	if doneChunk.Usage.TotalTokens != 100 {
		t.Errorf("Usage.TotalTokens = %d, want 100", doneChunk.Usage.TotalTokens)
	}
}
