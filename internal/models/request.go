package models

// ChatRequest is the incoming HTTP request body for /v1/chat.
type ChatRequest struct {
	SessionID string `json:"session_id"`
	Message   string `json:"message"`
	Stream    bool   `json:"stream"`
}

// ChatResponse is the HTTP response body for a non-streaming /v1/chat call.
type ChatResponse struct {
	SessionID      string  `json:"session_id"`
	RequestID      string  `json:"request_id"`
	Message        Message `json:"message"`
	Usage          *Usage  `json:"usage,omitempty"`
	ToolCallsCount int     `json:"tool_calls_count"`
}

// StreamChunk represents a single piece of a streaming LLM response.
type StreamChunk struct {
	Delta     string     `json:"delta,omitempty"`
	ToolCalls []ToolCall `json:"tool_calls,omitempty"`
	Done      bool       `json:"done"`
	Usage     *Usage     `json:"usage,omitempty"`
	Err       error      `json:"-"`
}
