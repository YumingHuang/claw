package tools

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/mingminliu/claw/internal/llm"
	"github.com/mingminliu/claw/internal/models"
)

// Tool represents an atomic capability the agent can invoke.
type Tool interface {
	Name() string
	Description() string
	Parameters() json.RawMessage
	Execute(ctx context.Context, params json.RawMessage) (models.ToolResult, error)
}

// Registry holds registered tools and provides lookup and schema generation.
type Registry struct {
	tools map[string]Tool
	order []string // preserves insertion order for List()
}

// NewRegistry creates an empty tool registry.
func NewRegistry() *Registry {
	return &Registry{tools: make(map[string]Tool)}
}

// Register adds a tool. Returns an error if a tool with the same name exists.
func (r *Registry) Register(tool Tool) error {
	name := tool.Name()
	if _, exists := r.tools[name]; exists {
		return fmt.Errorf("tool already registered: %s", name)
	}
	r.tools[name] = tool
	r.order = append(r.order, name)
	return nil
}

// Get returns a tool by name.
func (r *Registry) Get(name string) (Tool, bool) {
	t, ok := r.tools[name]
	return t, ok
}

// Execute runs the named tool with the given parameters.
func (r *Registry) Execute(ctx context.Context, name string, params json.RawMessage) (models.ToolResult, error) {
	t, ok := r.tools[name]
	if !ok {
		return models.ToolResult{Content: fmt.Sprintf("tool not found: %s", name), IsError: true}, fmt.Errorf("tool not found: %s", name)
	}
	return t.Execute(ctx, params)
}

// List returns all registered tools in insertion order.
func (r *Registry) List() []Tool {
	out := make([]Tool, 0, len(r.order))
	for _, name := range r.order {
		out = append(out, r.tools[name])
	}
	return out
}

// Schemas returns OpenAI function-calling tool schemas for all registered tools.
func (r *Registry) Schemas() []llm.ToolSchema {
	out := make([]llm.ToolSchema, 0, len(r.order))
	for _, name := range r.order {
		t := r.tools[name]
		out = append(out, llm.ToolSchema{
			Type: "function",
			Function: llm.FunctionDef{
				Name:        t.Name(),
				Description: t.Description(),
				Parameters:  t.Parameters(),
			},
		})
	}
	return out
}
