// Package mcp provides MCP (Model Context Protocol) client support.
// It connects to external MCP servers and exposes their tools as
// native Claw tools that can be registered in the tool registry.
package mcp

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"os/exec"
	"strings"
	"syscall"

	mcpclient "github.com/mark3labs/mcp-go/client"
	"github.com/mark3labs/mcp-go/client/transport"
	"github.com/mark3labs/mcp-go/mcp"

	"github.com/YumingHuang/claw/internal/config"
	"github.com/YumingHuang/claw/internal/models"
)

// mcpTool wraps an MCP tool as a Claw Tool.
type mcpTool struct {
	name        string
	description string
	parameters  json.RawMessage
	client      *mcpclient.Client
}

func (t *mcpTool) Name() string                { return t.name }
func (t *mcpTool) Description() string         { return t.description }
func (t *mcpTool) Parameters() json.RawMessage { return t.parameters }

func (t *mcpTool) Execute(ctx context.Context, params json.RawMessage) (models.ToolResult, error) {
	var args map[string]any
	if len(params) > 0 {
		if err := json.Unmarshal(params, &args); err != nil {
			return models.ToolResult{IsError: true, Content: err.Error()}, nil
		}
	}

	req := mcp.CallToolRequest{}
	req.Params.Name = t.name
	req.Params.Arguments = args

	result, err := t.client.CallTool(ctx, req)
	if err != nil {
		return models.ToolResult{IsError: true, Content: err.Error()}, nil
	}

	content := extractText(result)
	return models.ToolResult{Content: content, IsError: result.IsError}, nil
}

func extractText(result *mcp.CallToolResult) string {
	var parts []string
	for _, c := range result.Content {
		switch v := c.(type) {
		case mcp.TextContent:
			parts = append(parts, v.Text)
		case mcp.ImageContent:
			parts = append(parts, fmt.Sprintf("[image: %s]", v.MIMEType))
		default:
			if b, err := json.Marshal(v); err == nil {
				parts = append(parts, string(b))
			}
		}
	}
	return strings.Join(parts, "\n")
}

// Tool is the interface that Claw tools must implement.
type Tool interface {
	Name() string
	Description() string
	Parameters() json.RawMessage
	Execute(ctx context.Context, params json.RawMessage) (models.ToolResult, error)
}

// Manager holds MCP clients and their tools, providing lifecycle management.
type Manager struct {
	clients []*mcpclient.Client
	tools   []Tool
}

// LoadTools connects to all configured MCP servers and returns a Manager.
// Errors connecting to individual servers are logged but not fatal.
func LoadTools(ctx context.Context, cfg config.MCPConfig) *Manager {
	m := &Manager{}
	for _, srv := range cfg.Servers {
		tools, client, err := loadServerTools(ctx, srv)
		if err != nil {
			slog.Error("mcp: failed to connect to server", "name", srv.Name, "error", err)
			continue
		}
		slog.Info("mcp: loaded tools from server", "name", srv.Name, "count", len(tools))
		m.clients = append(m.clients, client)
		m.tools = append(m.tools, tools...)
	}
	return m
}

// Tools returns all loaded MCP tools.
func (m *Manager) Tools() []Tool {
	if m == nil {
		return nil
	}
	return m.tools
}

// Close shuts down all MCP client connections.
func (m *Manager) Close() {
	if m == nil {
		return
	}
	for _, c := range m.clients {
		if err := c.Close(); err != nil {
			slog.Debug("mcp: close client", "error", err)
		}
	}
}

func loadServerTools(ctx context.Context, cfg config.MCPServerConfig) ([]Tool, *mcpclient.Client, error) {
	c, err := newClient(cfg)
	if err != nil {
		return nil, nil, err
	}

	if _, err := c.Initialize(ctx, mcp.InitializeRequest{}); err != nil {
		_ = c.Close()
		return nil, nil, fmt.Errorf("initialize: %w", err)
	}

	result, err := c.ListTools(ctx, mcp.ListToolsRequest{})
	if err != nil {
		_ = c.Close()
		return nil, nil, fmt.Errorf("list tools: %w", err)
	}

	tools := make([]Tool, 0, len(result.Tools))
	for _, t := range result.Tools {
		schema, err := toolSchema(t)
		if err != nil {
			slog.Warn("mcp: failed to get schema for tool", "tool", t.Name, "error", err)
			continue
		}
		tools = append(tools, &mcpTool{
			name:        t.Name,
			description: t.Description,
			parameters:  schema,
			client:      c,
		})
	}
	return tools, c, nil
}

func newClient(cfg config.MCPServerConfig) (*mcpclient.Client, error) {
	switch cfg.Type {
	case "stdio":
		env := make([]string, 0, len(cfg.Env))
		for k, v := range cfg.Env {
			env = append(env, k+"="+v)
		}
		// Put child processes in their own process group so that
		// exec.CommandContext can kill the entire tree on context cancel.
		cmdFunc := transport.WithCommandFunc(
			func(ctx context.Context, command string, cmdEnv []string, args []string) (*exec.Cmd, error) {
				cmd := exec.CommandContext(ctx, command, args...)
				cmd.Env = cmdEnv
				cmd.SysProcAttr = &syscall.SysProcAttr{
					Setpgid: true,
				}
				return cmd, nil
			},
		)
		return mcpclient.NewStdioMCPClientWithOptions(cfg.Command, env, cfg.Args, cmdFunc)
	case "sse":
		return mcpclient.NewSSEMCPClient(cfg.URL)
	default:
		return nil, fmt.Errorf("unknown MCP server type %q (use stdio or sse)", cfg.Type)
	}
}

func toolSchema(t mcp.Tool) (json.RawMessage, error) {
	schema := map[string]any{
		"type": "object",
	}
	if t.InputSchema.Properties != nil {
		schema["properties"] = t.InputSchema.Properties
	} else {
		schema["properties"] = map[string]any{}
	}
	if len(t.InputSchema.Required) > 0 {
		schema["required"] = t.InputSchema.Required
	}
	return json.Marshal(schema)
}
