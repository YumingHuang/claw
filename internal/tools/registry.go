package tools

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/YumingHuang/claw/internal/audit"
	"github.com/YumingHuang/claw/internal/llm"
	"github.com/YumingHuang/claw/internal/metrics"
	"github.com/YumingHuang/claw/internal/models"
)

// Tool represents an atomic capability the agents can invoke.
type Tool interface {
	Name() string
	Description() string
	Parameters() json.RawMessage
	Execute(ctx context.Context, params json.RawMessage) (models.ToolResult, error)
}

// Registry holds registered tools and provides lookup and schema generation.
type Registry struct {
	tools          map[string]Tool
	order          []string // preserves insertion order for List()
	profiles       map[string][]string
	defaultProfile string
	auditor        *audit.Logger
	metrics        *metrics.Collector
}

// NewRegistry creates an empty tool registry.
func NewRegistry() *Registry {
	return &Registry{
		tools:    make(map[string]Tool),
		profiles: make(map[string][]string),
	}
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
	result, err := t.Execute(ctx, params)
	if r.auditor != nil {
		r.auditor.LogToolExecuted(ctx, name, result)
	}
	if r.metrics != nil {
		status := "success"
		if err != nil || result.IsError {
			status = "error"
		}
		r.metrics.ObserveToolExecution(name, status)
	}
	return result, err
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
	return r.schemasForToolNames(r.order)
}

// SetProfiles configures named tool profiles and the default profile.
func (r *Registry) SetProfiles(profiles map[string][]string, defaultProfile string) error {
	if len(profiles) == 0 {
		r.profiles = make(map[string][]string)
		r.defaultProfile = ""
		return nil
	}

	normalized := make(map[string][]string, len(profiles))
	for profile, tools := range profiles {
		names := make([]string, 0, len(tools))
		seen := make(map[string]struct{}, len(tools))
		for _, name := range tools {
			if _, ok := r.tools[name]; !ok {
				return fmt.Errorf("unknown tool %q in profile %q", name, profile)
			}
			if _, ok := seen[name]; ok {
				continue
			}
			seen[name] = struct{}{}
			names = append(names, name)
		}
		normalized[profile] = names
	}
	if defaultProfile != "" {
		if _, ok := normalized[defaultProfile]; !ok {
			return fmt.Errorf("default profile %q not found", defaultProfile)
		}
	}

	r.profiles = normalized
	r.defaultProfile = defaultProfile
	return nil
}

func (r *Registry) SetAuditor(auditor *audit.Logger) {
	r.auditor = auditor
}

func (r *Registry) SetMetrics(collector *metrics.Collector) {
	r.metrics = collector
}

// FilterByProfile returns the tools enabled for the requested profile.
func (r *Registry) FilterByProfile(profile string) []llm.ToolSchema {
	if len(r.profiles) == 0 {
		return r.Schemas()
	}
	if profile == "" {
		profile = r.defaultProfile
	}
	if profile == "" {
		return r.Schemas()
	}
	names, ok := r.profiles[profile]
	if !ok {
		return nil
	}
	return r.schemasForToolNames(names)
}

func (r *Registry) schemasForToolNames(names []string) []llm.ToolSchema {
	out := make([]llm.ToolSchema, 0, len(r.order))
	for _, name := range names {
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
