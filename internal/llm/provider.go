package llm

import (
	"context"
	"encoding/json"

	"github.com/mingminliu/claw/internal/models"
)

// Provider abstracts an LLM service that can generate chat completions.
type Provider interface {
	Name() string
	Chat(ctx context.Context, req *ChatRequest) (*ChatResponse, error)
	ChatStream(ctx context.Context, req *ChatRequest) (<-chan StreamChunk, error)
}

// ChatRequest holds the parameters for an LLM chat completion call.
type ChatRequest struct {
	Model       string
	Messages    []models.Message
	Tools       []ToolSchema
	Temperature float64
	MaxTokens   int
}

// ChatResponse holds the result of a non-streaming LLM call.
type ChatResponse struct {
	Content      string
	ToolCalls    []models.ToolCall
	Usage        models.Usage
	FinishReason string
}

// StreamChunk represents a single piece of a streaming LLM response.
type StreamChunk struct {
	Delta     string
	ToolCalls []models.ToolCall
	Done      bool
	Usage     *models.Usage
	Err       error
}

// ToolSchema describes a tool in OpenAI function calling format.
type ToolSchema struct {
	Type     string      `json:"type"`
	Function FunctionDef `json:"function"`
}

// FunctionDef describes the name, description, and parameter schema of a tool.
type FunctionDef struct {
	Name        string          `json:"name"`
	Description string          `json:"description"`
	Parameters  json.RawMessage `json:"parameters"`
}
