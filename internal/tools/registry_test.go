package tools

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/YumingHuang/claw/internal/models"
)

type fakeTool struct {
	name string
}

func (f *fakeTool) Name() string                { return f.name }
func (f *fakeTool) Description() string         { return "fake tool for testing" }
func (f *fakeTool) Parameters() json.RawMessage { return json.RawMessage(`{"type":"object"}`) }
func (f *fakeTool) Execute(_ context.Context, _ json.RawMessage) (models.ToolResult, error) {
	return models.ToolResult{Content: "fake result"}, nil
}

func TestRegistry_RegisterAndGet(t *testing.T) {
	r := NewRegistry()
	tool := &fakeTool{name: "test_tool"}

	if err := r.Register(tool); err != nil {
		t.Fatalf("Register: %v", err)
	}

	got, ok := r.Get("test_tool")
	if !ok {
		t.Fatal("Get returned false")
	}
	if got.Name() != "test_tool" {
		t.Errorf("Name() = %q, want %q", got.Name(), "test_tool")
	}
}

func TestRegistry_GetNotFound(t *testing.T) {
	r := NewRegistry()
	_, ok := r.Get("nonexistent")
	if ok {
		t.Error("Get should return false for nonexistent tool")
	}
}

func TestRegistry_DuplicateRegister(t *testing.T) {
	r := NewRegistry()
	tool := &fakeTool{name: "dup"}

	if err := r.Register(tool); err != nil {
		t.Fatalf("first Register: %v", err)
	}
	if err := r.Register(tool); err == nil {
		t.Error("expected error for duplicate registration")
	}
}

func TestRegistry_Execute(t *testing.T) {
	r := NewRegistry()
	if err := r.Register(&fakeTool{name: "exec_test"}); err != nil {
		t.Fatal(err)
	}

	result, err := r.Execute(context.Background(), "exec_test", nil)
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if result.Content != "fake result" {
		t.Errorf("Content = %q, want %q", result.Content, "fake result")
	}
}

func TestRegistry_ExecuteNotFound(t *testing.T) {
	r := NewRegistry()
	_, err := r.Execute(context.Background(), "missing", nil)
	if err == nil {
		t.Error("expected error for missing tool")
	}
}

func TestRegistry_List(t *testing.T) {
	r := NewRegistry()
	_ = r.Register(&fakeTool{name: "a"})
	_ = r.Register(&fakeTool{name: "b"})

	list := r.List()
	if len(list) != 2 {
		t.Errorf("List() len = %d, want 2", len(list))
	}
}

func TestRegistry_Schemas(t *testing.T) {
	r := NewRegistry()
	_ = r.Register(&fakeTool{name: "schema_test"})

	schemas := r.Schemas()
	if len(schemas) != 1 {
		t.Fatalf("Schemas() len = %d, want 1", len(schemas))
	}
	if schemas[0].Type != "function" {
		t.Errorf("Type = %q, want %q", schemas[0].Type, "function")
	}
	if schemas[0].Function.Name != "schema_test" {
		t.Errorf("Function.Name = %q, want %q", schemas[0].Function.Name, "schema_test")
	}
}
