package tools

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/YumingHuang/claw/internal/requestctx"
)

func memoryContext(sessionID string) context.Context {
	return context.WithValue(context.Background(), requestctx.SessionIDKey, sessionID)
}

func TestMemorySetAndGet(t *testing.T) {
	store := NewMemoryStore()
	setTool := NewMemorySetTool(store)
	getTool := NewMemoryGetTool(store)
	ctx := memoryContext("session-a")

	result, err := setTool.Execute(ctx, json.RawMessage(`{"key":"name","value":"claw"}`))
	if err != nil {
		t.Fatalf("memory_set: %v", err)
	}
	if result.IsError {
		t.Fatalf("memory_set error: %s", result.Content)
	}

	result, err = getTool.Execute(ctx, json.RawMessage(`{"key":"name"}`))
	if err != nil {
		t.Fatalf("memory_get: %v", err)
	}
	if result.IsError {
		t.Fatalf("memory_get error: %s", result.Content)
	}
	if result.Content != "claw" {
		t.Fatalf("content = %q, want claw", result.Content)
	}
}

func TestMemoryList(t *testing.T) {
	store := NewMemoryStore()
	setTool := NewMemorySetTool(store)
	listTool := NewMemoryListTool(store)
	ctx := memoryContext("session-a")

	_, _ = setTool.Execute(ctx, json.RawMessage(`{"key":"lang","value":"go"}`))
	_, _ = setTool.Execute(ctx, json.RawMessage(`{"key":"name","value":"claw"}`))

	result, err := listTool.Execute(ctx, nil)
	if err != nil {
		t.Fatalf("memory_list: %v", err)
	}
	if result.IsError {
		t.Fatalf("memory_list error: %s", result.Content)
	}
	if !strings.Contains(result.Content, "lang=go") {
		t.Fatalf("content = %q", result.Content)
	}
	if !strings.Contains(result.Content, "name=claw") {
		t.Fatalf("content = %q", result.Content)
	}
}

func TestMemoryIsNamespacedBySession(t *testing.T) {
	store := NewMemoryStore()
	setTool := NewMemorySetTool(store)
	getTool := NewMemoryGetTool(store)

	_, _ = setTool.Execute(memoryContext("session-a"), json.RawMessage(`{"key":"name","value":"alpha"}`))
	_, _ = setTool.Execute(memoryContext("session-b"), json.RawMessage(`{"key":"name","value":"beta"}`))

	resultA, err := getTool.Execute(memoryContext("session-a"), json.RawMessage(`{"key":"name"}`))
	if err != nil {
		t.Fatalf("memory_get session-a: %v", err)
	}
	resultB, err := getTool.Execute(memoryContext("session-b"), json.RawMessage(`{"key":"name"}`))
	if err != nil {
		t.Fatalf("memory_get session-b: %v", err)
	}
	if resultA.Content != "alpha" {
		t.Fatalf("session-a = %q", resultA.Content)
	}
	if resultB.Content != "beta" {
		t.Fatalf("session-b = %q", resultB.Content)
	}
}

func TestMemoryRequiresSessionContext(t *testing.T) {
	store := NewMemoryStore()
	setTool := NewMemorySetTool(store)

	result, err := setTool.Execute(context.Background(), json.RawMessage(`{"key":"name","value":"claw"}`))
	if err != nil {
		t.Fatalf("memory_set: %v", err)
	}
	if !result.IsError {
		t.Fatal("expected error")
	}
	if !strings.Contains(result.Content, "session_id is required") {
		t.Fatalf("content = %q", result.Content)
	}
}
