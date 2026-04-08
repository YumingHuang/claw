package models

import (
	"encoding/json"
	"time"
)

// Message represents a single message in a conversation.
type Message struct {
	Role       string     `json:"role"`
	Content    string     `json:"content"`
	Images     []Image    `json:"images,omitempty"`
	ToolCalls  []ToolCall `json:"tool_calls,omitempty"`
	ToolCallID string     `json:"tool_call_id,omitempty"`
	Timestamp  time.Time  `json:"timestamp"`
}

// Image holds a base64-encoded image attached to a message.
type Image struct {
	Data      string `json:"data"`       // base64-encoded image data
	MediaType string `json:"media_type"` // e.g. "image/png", "image/jpeg"
}

// ToolCall represents an LLM's request to invoke a tool.
type ToolCall struct {
	ID        string          `json:"id"`
	Name      string          `json:"name"`
	Arguments json.RawMessage `json:"arguments"`
}

// ToolResult holds the output of a tool execution.
type ToolResult struct {
	Content string `json:"content"`
	IsError bool   `json:"is_error,omitempty"`
}

// Usage tracks token consumption for an LLM call.
type Usage struct {
	PromptTokens     int `json:"prompt_tokens"`
	CompletionTokens int `json:"completion_tokens"`
	TotalTokens      int `json:"total_tokens"`
}

func NewUserMessage(content string) Message {
	return Message{Role: "user", Content: content, Timestamp: time.Now()}
}

// NewUserMessageWithImages creates a user message with text and images.
func NewUserMessageWithImages(content string, images []Image) Message {
	return Message{Role: "user", Content: content, Images: images, Timestamp: time.Now()}
}

func NewAssistantMessage(content string) Message {
	return Message{Role: "assistant", Content: content, Timestamp: time.Now()}
}

func NewSystemMessage(content string) Message {
	return Message{Role: "system", Content: content, Timestamp: time.Now()}
}

func NewToolResultMessage(toolCallID string, result ToolResult) Message {
	return Message{
		Role:       "tool",
		Content:    result.Content,
		ToolCallID: toolCallID,
		Timestamp:  time.Now(),
	}
}
