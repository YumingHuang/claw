package gateway

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/YumingHuang/claw/internal/agent"
	"github.com/YumingHuang/claw/internal/llm"
	"github.com/YumingHuang/claw/internal/models"
	"github.com/YumingHuang/claw/internal/tools"
)

type fakeProvider struct {
	response *llm.ChatResponse
	lastReq  *llm.ChatRequest
}

func (f *fakeProvider) Name() string { return "fake" }
func (f *fakeProvider) Chat(_ context.Context, req *llm.ChatRequest) (*llm.ChatResponse, error) {
	f.lastReq = req
	return f.response, nil
}
func (f *fakeProvider) ChatStream(_ context.Context, req *llm.ChatRequest) (<-chan llm.StreamChunk, error) {
	f.lastReq = req
	ch := make(chan llm.StreamChunk, 1)
	ch <- llm.StreamChunk{Delta: f.response.Content, Done: true}
	close(ch)
	return ch, nil
}

func newTestGateway(providerResp *llm.ChatResponse) *Gateway {
	provider := &fakeProvider{response: providerResp}
	registry := tools.NewRegistry()
	a := agent.NewAgent(provider, registry, agent.AgentOptions{
		SystemPrompt:  "test",
		MaxIterations: 10,
		ContextWindow: 100000,
	})

	ctx := context.Background()
	sessions := NewMemorySessionStore(ctx, 1*time.Hour, 100, 5*time.Minute)
	queue := agent.NewSessionQueue()

	return NewGateway(a, sessions, queue)
}

func TestHandleMessage_Normal(t *testing.T) {
	gw := newTestGateway(&llm.ChatResponse{Content: "Hello!", FinishReason: "stop"})

	resp, err := gw.HandleMessage(context.Background(), "s1", "http", "Hi")
	if err != nil {
		t.Fatalf("HandleMessage: %v", err)
	}
	if resp.SessionID != "s1" {
		t.Errorf("SessionID = %q, want %q", resp.SessionID, "s1")
	}
	if resp.RequestID == "" {
		t.Error("RequestID should not be empty")
	}
	if resp.Message.Content != "Hello!" {
		t.Errorf("Content = %q, want %q", resp.Message.Content, "Hello!")
	}
}

func TestHandleMessage_SessionPersists(t *testing.T) {
	gw := newTestGateway(&llm.ChatResponse{Content: "response", FinishReason: "stop"})

	_, err := gw.HandleMessage(context.Background(), "s1", "http", "first")
	if err != nil {
		t.Fatal(err)
	}

	session, ok := gw.GetSession("s1")
	if !ok {
		t.Fatal("session should exist")
	}
	if session.MessagesCount() != 2 {
		t.Errorf("messages = %d, want 2", session.MessagesCount())
	}
}

func TestHandleMessageStream_Normal(t *testing.T) {
	gw := newTestGateway(&llm.ChatResponse{Content: "streamed!", FinishReason: "stop"})

	ch, err := gw.HandleMessageStream(context.Background(), "s1", "http", "Hi")
	if err != nil {
		t.Fatalf("HandleMessageStream: %v", err)
	}

	var content string
	for chunk := range ch {
		content += chunk.Delta
	}
	if content != "streamed!" {
		t.Errorf("content = %q, want %q", content, "streamed!")
	}
}

func TestDeleteSession(t *testing.T) {
	gw := newTestGateway(&llm.ChatResponse{Content: "ok"})

	_, _ = gw.HandleMessage(context.Background(), "s1", "http", "test")
	gw.DeleteSession("s1")

	_, ok := gw.GetSession("s1")
	if ok {
		t.Error("session should be deleted")
	}
}

func TestSessionCount(t *testing.T) {
	gw := newTestGateway(&llm.ChatResponse{Content: "ok"})

	_, _ = gw.HandleMessage(context.Background(), "s1", "http", "a")
	_, _ = gw.HandleMessage(context.Background(), "s2", "http", "b")

	if gw.SessionCount() != 2 {
		t.Errorf("SessionCount = %d, want 2", gw.SessionCount())
	}
}

// Verify ToolNames helper works (used by /status endpoint)
func TestToolNames(t *testing.T) {
	provider := &fakeProvider{response: &llm.ChatResponse{Content: "ok"}}
	registry := tools.NewRegistry()

	fakeTool := &simpleFakeTool{name: "test_tool"}
	_ = registry.Register(fakeTool)

	a := agent.NewAgent(provider, registry, agent.AgentOptions{MaxIterations: 10, ContextWindow: 100000})
	ctx := context.Background()
	sessions := NewMemorySessionStore(ctx, 1*time.Hour, 100, 5*time.Minute)
	gw := NewGateway(a, sessions, agent.NewSessionQueue())

	names := gw.ToolNames()
	if len(names) != 1 || names[0] != "test_tool" {
		t.Errorf("ToolNames = %v, want [test_tool]", names)
	}
}

func TestGateway_DefaultToolProfileApplied(t *testing.T) {
	provider := &fakeProvider{response: &llm.ChatResponse{Content: "ok"}}
	registry := tools.NewRegistry()
	_ = registry.Register(&simpleFakeTool{name: "tool_a"})
	_ = registry.Register(&simpleFakeTool{name: "tool_b"})
	if err := registry.SetProfiles(map[string][]string{
		"safe": {"tool_a"},
	}, "safe"); err != nil {
		t.Fatalf("SetProfiles: %v", err)
	}

	a := agent.NewAgent(provider, registry, agent.AgentOptions{MaxIterations: 10, ContextWindow: 100000})
	ctx := context.Background()
	sessions := NewMemorySessionStore(ctx, 1*time.Hour, 100, 5*time.Minute)
	gw := NewGateway(a, sessions, agent.NewSessionQueue())
	gw.SetToolProfile("safe")

	_, err := gw.HandleMessage(context.Background(), "s1", "http", "hi")
	if err != nil {
		t.Fatalf("HandleMessage: %v", err)
	}
	if provider.lastReq == nil {
		t.Fatal("provider lastReq should exist")
	}
	if len(provider.lastReq.Tools) != 1 {
		t.Fatalf("tools len = %d, want 1", len(provider.lastReq.Tools))
	}
	if provider.lastReq.Tools[0].Function.Name != "tool_a" {
		t.Fatalf("tool = %q, want tool_a", provider.lastReq.Tools[0].Function.Name)
	}
}

type simpleFakeTool struct{ name string }

func (f *simpleFakeTool) Name() string                { return f.name }
func (f *simpleFakeTool) Description() string         { return "fake" }
func (f *simpleFakeTool) Parameters() json.RawMessage { return json.RawMessage(`{}`) }
func (f *simpleFakeTool) Execute(_ context.Context, _ json.RawMessage) (models.ToolResult, error) {
	return models.ToolResult{Content: "ok"}, nil
}
