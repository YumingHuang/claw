package agent

import (
	"context"
	"fmt"

	"github.com/YumingHuang/claw/internal/llm"
	"github.com/YumingHuang/claw/internal/models"
	"github.com/YumingHuang/claw/internal/requestctx"
	"github.com/YumingHuang/claw/internal/tools"
)

// AgentOptions configures the Agent.
type AgentOptions struct {
	SystemPrompt  string
	MaxIterations int
	ContextWindow int
}

// Agent orchestrates the LLM ↔ tool-calling loop for a session.
type Agent struct {
	provider      llm.Provider
	toolRegistry  *tools.Registry
	systemPrompt  string
	maxIterations int
	contextWindow int
}

// NewAgent creates an Agent with the given provider, tool registry, and options.
func NewAgent(provider llm.Provider, registry *tools.Registry, opts AgentOptions) *Agent {
	return &Agent{
		provider:      provider,
		toolRegistry:  registry,
		systemPrompt:  opts.SystemPrompt,
		maxIterations: opts.MaxIterations,
		contextWindow: opts.ContextWindow,
	}
}

// Run executes the agents loop: send user message, call LLM, execute tools if
// requested, and repeat until the LLM returns a text response or the iteration
// limit is reached.
func (a *Agent) Run(ctx context.Context, session *Session, userMessage string) (string, error) {
	if err := ctx.Err(); err != nil {
		return "", fmt.Errorf("context cancelled: %w", err)
	}

	session.Append(models.NewUserMessage(userMessage))

	for iteration := 0; ; iteration++ {
		if iteration >= a.maxIterations {
			return "", fmt.Errorf("max tool-call iterations exceeded (%d)", a.maxIterations)
		}

		if err := ctx.Err(); err != nil {
			return "", fmt.Errorf("context cancelled: %w", err)
		}

		msgs := a.buildContext(session)
		req := &llm.ChatRequest{
			Messages: msgs,
			Tools:    a.toolSchemasForContext(ctx),
		}

		resp, err := a.provider.Chat(ctx, req)
		if err != nil {
			return "", fmt.Errorf("provider chat: %w", err)
		}

		if len(resp.ToolCalls) == 0 {
			session.Append(models.NewAssistantMessage(resp.Content))
			return resp.Content, nil
		}

		// Append assistant message with tool calls
		assistantMsg := models.NewAssistantMessage(resp.Content)
		assistantMsg.ToolCalls = resp.ToolCalls
		session.Append(assistantMsg)

		// Execute each tool and append results
		for _, tc := range resp.ToolCalls {
			result, _ := a.toolRegistry.Execute(ctx, tc.Name, tc.Arguments)
			session.Append(models.NewToolResultMessage(tc.ID, result))
		}
	}
}

// RunStream executes the agents loop with streaming LLM responses.
func (a *Agent) RunStream(ctx context.Context, session *Session, userMessage string) (<-chan models.StreamChunk, error) {
	if err := ctx.Err(); err != nil {
		return nil, fmt.Errorf("context cancelled: %w", err)
	}

	session.Append(models.NewUserMessage(userMessage))

	msgs := a.buildContext(session)
	req := &llm.ChatRequest{
		Messages: msgs,
		Tools:    a.toolSchemasForContext(ctx),
	}

	llmCh, err := a.provider.ChatStream(ctx, req)
	if err != nil {
		return nil, fmt.Errorf("provider chat stream: %w", err)
	}

	out := make(chan models.StreamChunk)
	go func() {
		defer close(out)
		var fullContent string
		for chunk := range llmCh {
			fullContent += chunk.Delta
			out <- models.StreamChunk{
				Delta: chunk.Delta,
				Done:  chunk.Done,
				Usage: chunk.Usage,
				Err:   chunk.Err,
			}
		}
		session.Append(models.NewAssistantMessage(fullContent))
	}()

	return out, nil
}

func (a *Agent) buildContext(session *Session) []models.Message {
	var msgs []models.Message
	if a.systemPrompt != "" {
		msgs = append(msgs, models.NewSystemMessage(a.systemPrompt))
	}
	msgs = append(msgs, session.Messages()...)

	if a.contextWindow > 0 {
		msgs = llm.TruncateMessages(msgs, a.contextWindow)
	}
	return msgs
}

// ToolNames returns the names of all registered tools.
func (a *Agent) ToolNames() []string {
	list := a.toolRegistry.List()
	names := make([]string, len(list))
	for i, t := range list {
		names[i] = t.Name()
	}
	return names
}

func (a *Agent) toolSchemasForContext(ctx context.Context) []llm.ToolSchema {
	return a.toolRegistry.FilterByProfile(requestctx.ToolProfileFromContext(ctx))
}
