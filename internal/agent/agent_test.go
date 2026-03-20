package agent

import (
	"context"
	"encoding/json"
	"fmt"
	"sync"
	"testing"

	"github.com/YumingHuang/claw/internal/llm"
	"github.com/YumingHuang/claw/internal/models"
	"github.com/YumingHuang/claw/internal/requestctx"
	"github.com/YumingHuang/claw/internal/tools"
)

// --- fake provider ---

type fakeProvider struct {
	name      string
	responses []*llm.ChatResponse
	callIndex int
	lastReq   *llm.ChatRequest
}

func (f *fakeProvider) Name() string { return f.name }

func (f *fakeProvider) Chat(_ context.Context, req *llm.ChatRequest) (*llm.ChatResponse, error) {
	f.lastReq = req
	if f.callIndex >= len(f.responses) {
		return &llm.ChatResponse{Content: "fallback response"}, nil
	}
	resp := f.responses[f.callIndex]
	f.callIndex++
	return resp, nil
}

func (f *fakeProvider) ChatStream(_ context.Context, req *llm.ChatRequest) (<-chan llm.StreamChunk, error) {
	f.lastReq = req
	ch := make(chan llm.StreamChunk, 1)
	ch <- llm.StreamChunk{Delta: "streamed", Done: true}
	close(ch)
	return ch, nil
}

// --- fake tool for agents tests ---

type echoTool struct{}

func (e *echoTool) Name() string                { return "echo" }
func (e *echoTool) Description() string         { return "echoes input" }
func (e *echoTool) Parameters() json.RawMessage { return json.RawMessage(`{"type":"object"}`) }
func (e *echoTool) Execute(_ context.Context, params json.RawMessage) (models.ToolResult, error) {
	return models.ToolResult{Content: "echo: " + string(params)}, nil
}

type failTool struct{}

func (f *failTool) Name() string                { return "fail_tool" }
func (f *failTool) Description() string         { return "always fails" }
func (f *failTool) Parameters() json.RawMessage { return json.RawMessage(`{"type":"object"}`) }
func (f *failTool) Execute(_ context.Context, _ json.RawMessage) (models.ToolResult, error) {
	return models.ToolResult{Content: "something went wrong", IsError: true}, nil
}

func newTestRegistry(tt ...tools.Tool) *tools.Registry {
	r := tools.NewRegistry()
	for _, t := range tt {
		_ = r.Register(t)
	}
	return r
}

func TestRun_SimpleTextResponse(t *testing.T) {
	provider := &fakeProvider{
		name:      "fake",
		responses: []*llm.ChatResponse{{Content: "Hello!", FinishReason: "stop"}},
	}
	agent := NewAgent(provider, newTestRegistry(), AgentOptions{
		SystemPrompt:  "you are helpful",
		MaxIterations: 10,
		ContextWindow: 100000,
	})
	session := NewSession("s1", "test")

	result, err := agent.Run(context.Background(), session, "Hi")
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if result != "Hello!" {
		t.Errorf("result = %q, want %q", result, "Hello!")
	}
	if session.MessagesCount() != 2 {
		t.Errorf("session messages = %d, want 2 (user + assistant)", session.MessagesCount())
	}
}

func TestRun_SingleToolCall(t *testing.T) {
	provider := &fakeProvider{
		name: "fake",
		responses: []*llm.ChatResponse{
			{
				ToolCalls: []models.ToolCall{
					{ID: "call_1", Name: "echo", Arguments: json.RawMessage(`{"input":"test"}`)},
				},
				FinishReason: "tool_calls",
			},
			{Content: "The echo returned: echo: test", FinishReason: "stop"},
		},
	}
	agent := NewAgent(provider, newTestRegistry(&echoTool{}), AgentOptions{
		SystemPrompt:  "you are helpful",
		MaxIterations: 10,
		ContextWindow: 100000,
	})
	session := NewSession("s1", "test")

	result, err := agent.Run(context.Background(), session, "echo something")
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if result != "The echo returned: echo: test" {
		t.Errorf("result = %q", result)
	}
	// user + assistant(tool_call) + tool_result + assistant(final)
	if session.MessagesCount() != 4 {
		t.Errorf("session messages = %d, want 4", session.MessagesCount())
	}
}

func TestRun_MultipleToolCalls(t *testing.T) {
	provider := &fakeProvider{
		name: "fake",
		responses: []*llm.ChatResponse{
			{
				ToolCalls: []models.ToolCall{
					{ID: "call_1", Name: "echo", Arguments: json.RawMessage(`{"a":"1"}`)},
					{ID: "call_2", Name: "echo", Arguments: json.RawMessage(`{"b":"2"}`)},
				},
				FinishReason: "tool_calls",
			},
			{Content: "done", FinishReason: "stop"},
		},
	}
	agent := NewAgent(provider, newTestRegistry(&echoTool{}), AgentOptions{
		SystemPrompt:  "sys",
		MaxIterations: 10,
		ContextWindow: 100000,
	})
	session := NewSession("s1", "test")

	result, err := agent.Run(context.Background(), session, "multi")
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if result != "done" {
		t.Errorf("result = %q, want %q", result, "done")
	}
	// user + assistant(tool_calls) + tool1 + tool2 + assistant(final)
	if session.MessagesCount() != 5 {
		t.Errorf("session messages = %d, want 5", session.MessagesCount())
	}
}

func TestRun_MaxIterationsExceeded(t *testing.T) {
	// Provider always returns a tool call
	toolCallResp := &llm.ChatResponse{
		ToolCalls:    []models.ToolCall{{ID: "call_x", Name: "echo", Arguments: json.RawMessage(`{}`)}},
		FinishReason: "tool_calls",
	}
	responses := make([]*llm.ChatResponse, 20)
	for i := range responses {
		responses[i] = toolCallResp
	}
	provider := &fakeProvider{name: "fake", responses: responses}

	agent := NewAgent(provider, newTestRegistry(&echoTool{}), AgentOptions{
		SystemPrompt:  "sys",
		MaxIterations: 3,
		ContextWindow: 100000,
	})
	session := NewSession("s1", "test")

	_, err := agent.Run(context.Background(), session, "loop")
	if err == nil {
		t.Fatal("expected error for max iterations exceeded")
	}
}

func TestRun_ToolExecutionError(t *testing.T) {
	provider := &fakeProvider{
		name: "fake",
		responses: []*llm.ChatResponse{
			{
				ToolCalls:    []models.ToolCall{{ID: "call_1", Name: "fail_tool", Arguments: json.RawMessage(`{}`)}},
				FinishReason: "tool_calls",
			},
			{Content: "I see the tool failed", FinishReason: "stop"},
		},
	}
	agent := NewAgent(provider, newTestRegistry(&failTool{}), AgentOptions{
		SystemPrompt:  "sys",
		MaxIterations: 10,
		ContextWindow: 100000,
	})
	session := NewSession("s1", "test")

	result, err := agent.Run(context.Background(), session, "do something")
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if result != "I see the tool failed" {
		t.Errorf("result = %q", result)
	}
}

func TestRun_ContextCancelled(t *testing.T) {
	provider := &fakeProvider{
		name:      "fake",
		responses: []*llm.ChatResponse{{Content: "should not reach"}},
	}

	// Cancel context before calling Run
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	agent := NewAgent(provider, newTestRegistry(), AgentOptions{
		SystemPrompt:  "sys",
		MaxIterations: 10,
		ContextWindow: 100000,
	})
	session := NewSession("s1", "test")

	_, err := agent.Run(ctx, session, "hello")
	if err == nil {
		t.Fatal("expected error for cancelled context")
	}
}

func TestRun_UsesToolProfileFromContext(t *testing.T) {
	provider := &fakeProvider{
		name:      "fake",
		responses: []*llm.ChatResponse{{Content: "ok", FinishReason: "stop"}},
	}
	registry := newTestRegistry(&echoTool{}, &failTool{})
	if err := registry.SetProfiles(map[string][]string{
		"safe": {"echo"},
	}, "safe"); err != nil {
		t.Fatalf("SetProfiles: %v", err)
	}
	agent := NewAgent(provider, registry, AgentOptions{
		SystemPrompt:  "sys",
		MaxIterations: 10,
		ContextWindow: 100000,
	})
	session := NewSession("s1", "test")
	ctx := context.WithValue(context.Background(), requestctx.ToolProfileKey, "safe")

	_, err := agent.Run(ctx, session, "hello")
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if provider.lastReq == nil {
		t.Fatal("expected lastReq to be captured")
	}
	if len(provider.lastReq.Tools) != 1 {
		t.Fatalf("tools len = %d, want 1", len(provider.lastReq.Tools))
	}
	if provider.lastReq.Tools[0].Function.Name != "echo" {
		t.Fatalf("tool = %q, want echo", provider.lastReq.Tools[0].Function.Name)
	}
}

// --- Concurrent safety tests ---

func TestSession_ConcurrentAppends(t *testing.T) {
	session := NewSession("test", "http")
	const goroutines = 100
	const appendsPerGoroutine = 100

	var wg sync.WaitGroup
	wg.Add(goroutines)

	for i := 0; i < goroutines; i++ {
		go func(id int) {
			defer wg.Done()
			for j := 0; j < appendsPerGoroutine; j++ {
				msg := models.NewUserMessage(fmt.Sprintf("msg-%d-%d", id, j))
				session.Append(msg)
			}
		}(i)
	}

	wg.Wait()

	// Verify final message count
	expectedCount := goroutines * appendsPerGoroutine
	if session.MessagesCount() != expectedCount {
		t.Errorf("expected %d messages, got %d", expectedCount, session.MessagesCount())
	}

	// Verify message content integrity
	msgs := session.Messages()
	if len(msgs) != expectedCount {
		t.Errorf("Messages() returned %d, want %d", len(msgs), expectedCount)
	}
}

func TestSession_ConcurrentReadsAndWrites(t *testing.T) {
	session := NewSession("test", "http")

	// Initialize some messages
	for i := 0; i < 10; i++ {
		session.Append(models.NewUserMessage(fmt.Sprintf("initial-%d", i)))
	}

	const goroutines = 50
	var wg sync.WaitGroup
	wg.Add(goroutines * 2) // readers + writers

	// Start multiple writers
	for i := 0; i < goroutines; i++ {
		go func(id int) {
			defer wg.Done()
			session.Append(models.NewUserMessage(fmt.Sprintf("write-%d", id)))
		}(i)
	}

	// Start multiple readers - verify they can read without panicking
	for i := 0; i < goroutines; i++ {
		go func(id int) {
			defer wg.Done()
			msgs := session.Messages()
			count := session.MessagesCount()

			// Verify data integrity: each read should be self-consistent
			// (count should match the length of the slice we got)
			// Note: Due to concurrent writes, we just verify no crashes occur
			_ = msgs
			_ = count
		}(i)
	}

	wg.Wait()

	// Verify final state
	finalCount := session.MessagesCount()
	expectedCount := 10 + goroutines
	if finalCount != expectedCount {
		t.Errorf("final count: got %d, want %d", finalCount, expectedCount)
	}

	// Verify all messages are present
	msgs := session.Messages()
	if len(msgs) != expectedCount {
		t.Errorf("final Messages() length: got %d, want %d", len(msgs), expectedCount)
	}
}

func TestSession_MessagesDefensiveCopy(t *testing.T) {
	session := NewSession("test", "http")
	session.Append(models.NewUserMessage("original"))

	msgs1 := session.Messages()
	msgs2 := session.Messages()

	// Verify that different slice instances are returned
	if &msgs1[0] == &msgs2[0] {
		t.Error("Messages() should return a copy, not the original slice")
	}

	// Verify that modifying the copy doesn't affect the original data
	msgs1[0].Content = "modified"
	msgs3 := session.Messages()
	if msgs3[0].Content == "modified" {
		t.Error("modifying returned Messages() should not affect session")
	}
}
