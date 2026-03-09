package models

import (
	"encoding/json"
	"testing"
	"time"
)

func TestNewUserMessage(t *testing.T) {
	before := time.Now()
	msg := NewUserMessage("hello")
	after := time.Now()

	if msg.Role != "user" {
		t.Errorf("Role = %q, want %q", msg.Role, "user")
	}
	if msg.Content != "hello" {
		t.Errorf("Content = %q, want %q", msg.Content, "hello")
	}
	if msg.Timestamp.Before(before) || msg.Timestamp.After(after) {
		t.Errorf("Timestamp %v not between %v and %v", msg.Timestamp, before, after)
	}
}

func TestNewAssistantMessage(t *testing.T) {
	msg := NewAssistantMessage("response text")

	if msg.Role != "assistant" {
		t.Errorf("Role = %q, want %q", msg.Role, "assistant")
	}
	if msg.Content != "response text" {
		t.Errorf("Content = %q, want %q", msg.Content, "response text")
	}
}

func TestNewSystemMessage(t *testing.T) {
	msg := NewSystemMessage("you are helpful")

	if msg.Role != "system" {
		t.Errorf("Role = %q, want %q", msg.Role, "system")
	}
	if msg.Content != "you are helpful" {
		t.Errorf("Content = %q, want %q", msg.Content, "you are helpful")
	}
}

func TestNewToolResultMessage(t *testing.T) {
	result := ToolResult{Content: "file contents here", IsError: false}
	msg := NewToolResultMessage("call-123", result)

	if msg.Role != "tool" {
		t.Errorf("Role = %q, want %q", msg.Role, "tool")
	}
	if msg.ToolCallID != "call-123" {
		t.Errorf("ToolCallID = %q, want %q", msg.ToolCallID, "call-123")
	}
	if msg.Content != "file contents here" {
		t.Errorf("Content = %q, want %q", msg.Content, "file contents here")
	}
}

func TestNewToolResultMessage_Error(t *testing.T) {
	result := ToolResult{Content: "permission denied", IsError: true}
	msg := NewToolResultMessage("call-456", result)

	if msg.Content != "permission denied" {
		t.Errorf("Content = %q, want %q", msg.Content, "permission denied")
	}
}

func TestMessage_JSONRoundTrip(t *testing.T) {
	original := Message{
		Role:    "assistant",
		Content: "hello",
		ToolCalls: []ToolCall{
			{ID: "tc-1", Name: "read_file", Arguments: json.RawMessage(`{"path":"test.txt"}`)},
		},
		Timestamp: time.Date(2026, 3, 6, 12, 0, 0, 0, time.UTC),
	}

	data, err := json.Marshal(original)
	if err != nil {
		t.Fatalf("Marshal: %v", err)
	}

	var decoded Message
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatalf("Unmarshal: %v", err)
	}

	if decoded.Role != original.Role {
		t.Errorf("Role = %q, want %q", decoded.Role, original.Role)
	}
	if decoded.Content != original.Content {
		t.Errorf("Content = %q, want %q", decoded.Content, original.Content)
	}
	if len(decoded.ToolCalls) != 1 {
		t.Fatalf("ToolCalls len = %d, want 1", len(decoded.ToolCalls))
	}
	if decoded.ToolCalls[0].Name != "read_file" {
		t.Errorf("ToolCalls[0].Name = %q, want %q", decoded.ToolCalls[0].Name, "read_file")
	}
}

func TestToolCall_ArgumentsParsing(t *testing.T) {
	tc := ToolCall{
		ID:        "tc-1",
		Name:      "run_command",
		Arguments: json.RawMessage(`{"command":"ls","args":["-la"]}`),
	}

	var args struct {
		Command string   `json:"command"`
		Args    []string `json:"args"`
	}
	if err := json.Unmarshal(tc.Arguments, &args); err != nil {
		t.Fatalf("Unmarshal arguments: %v", err)
	}
	if args.Command != "ls" {
		t.Errorf("Command = %q, want %q", args.Command, "ls")
	}
	if len(args.Args) != 1 || args.Args[0] != "-la" {
		t.Errorf("Args = %v, want [-la]", args.Args)
	}
}

func TestToolResult_JSONOmitEmpty(t *testing.T) {
	result := ToolResult{Content: "ok", IsError: false}
	data, err := json.Marshal(result)
	if err != nil {
		t.Fatalf("Marshal: %v", err)
	}

	var m map[string]interface{}
	if err := json.Unmarshal(data, &m); err != nil {
		t.Fatalf("Unmarshal: %v", err)
	}

	if _, exists := m["is_error"]; exists {
		t.Error("is_error should be omitted when false")
	}
}

func TestUsage_Total(t *testing.T) {
	u := Usage{
		PromptTokens:     100,
		CompletionTokens: 50,
		TotalTokens:      150,
	}

	data, err := json.Marshal(u)
	if err != nil {
		t.Fatalf("Marshal: %v", err)
	}

	var decoded Usage
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatalf("Unmarshal: %v", err)
	}

	if decoded.TotalTokens != 150 {
		t.Errorf("TotalTokens = %d, want 150", decoded.TotalTokens)
	}
}
