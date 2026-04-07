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
	Temperature   float64
	MaxTokens     int
}

// Agent orchestrates the LLM ↔ tool-calling loop for a session.
type Agent struct {
	provider      llm.Provider
	toolRegistry  *tools.Registry
	systemPrompt  string
	maxIterations int
	contextWindow int
	temperature   float64
	maxTokens     int
}

// NewAgent creates an Agent with the given provider, tool registry, and options.
func NewAgent(provider llm.Provider, registry *tools.Registry, opts AgentOptions) *Agent {
	return &Agent{
		provider:      provider,
		toolRegistry:  registry,
		systemPrompt:  opts.SystemPrompt,
		maxIterations: opts.MaxIterations,
		contextWindow: opts.ContextWindow,
		temperature:   opts.Temperature,
		maxTokens:     opts.MaxTokens,
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
	startCount := session.MessagesCount()

	for iteration := 0; ; iteration++ {
		if iteration >= a.maxIterations {
			session.Rollback(startCount)
			return "", fmt.Errorf("max tool-call iterations exceeded (%d)", a.maxIterations)
		}

		if err := ctx.Err(); err != nil {
			session.Rollback(startCount)
			return "", fmt.Errorf("context cancelled: %w", err)
		}

		msgs := a.buildContext(session)
		req := a.newChatRequest(ctx, msgs)

		resp, err := a.provider.Chat(ctx, req)
		if err != nil {
			session.Rollback(startCount)
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
// It supports tool-call loops: when the LLM requests tool calls, tools are
// executed and the LLM is called again until a text response is produced.
func (a *Agent) RunStream(ctx context.Context, session *Session, userMessage string) (<-chan models.StreamChunk, error) {
	if err := ctx.Err(); err != nil {
		return nil, fmt.Errorf("context cancelled: %w", err)
	}

	session.Append(models.NewUserMessage(userMessage))
	startCount := session.MessagesCount()

	out := make(chan models.StreamChunk)
	go func() {
		defer close(out)
		for iteration := 0; ; iteration++ {
			if iteration >= a.maxIterations {
				session.Rollback(startCount)
				out <- models.StreamChunk{Err: fmt.Errorf("max tool-call iterations exceeded (%d)", a.maxIterations), Done: true}
				return
			}
			if err := ctx.Err(); err != nil {
				session.Rollback(startCount)
				out <- models.StreamChunk{Err: fmt.Errorf("context cancelled: %w", err), Done: true}
				return
			}

			msgs := a.buildContext(session)
			req := a.newChatRequest(ctx, msgs)

			llmCh, err := a.provider.ChatStream(ctx, req)
			if err != nil {
				session.Rollback(startCount)
				out <- models.StreamChunk{Err: fmt.Errorf("provider chat stream: %w", err), Done: true}
				return
			}

			var fullContent string
			var toolCalls []models.ToolCall
			var streamErr error
			for chunk := range llmCh {
				if chunk.Err != nil {
					streamErr = chunk.Err
					out <- models.StreamChunk{Err: chunk.Err, Done: true}
					break
				}
				fullContent += chunk.Delta
				toolCalls = append(toolCalls, chunk.ToolCalls...)
				if len(chunk.ToolCalls) == 0 {
					out <- models.StreamChunk{
						Delta: chunk.Delta,
						Done:  chunk.Done && len(toolCalls) == 0,
						Usage: chunk.Usage,
					}
				}
			}

			if streamErr != nil {
				session.Rollback(startCount)
				return
			}

			if len(toolCalls) == 0 {
				session.Append(models.NewAssistantMessage(fullContent))
				return
			}

			// Tool call loop: execute tools and continue
			assistantMsg := models.NewAssistantMessage(fullContent)
			assistantMsg.ToolCalls = toolCalls
			session.Append(assistantMsg)

			for _, tc := range toolCalls {
				result, _ := a.toolRegistry.Execute(ctx, tc.Name, tc.Arguments)
				session.Append(models.NewToolResultMessage(tc.ID, result))
			}
		}
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

func (a *Agent) newChatRequest(ctx context.Context, msgs []models.Message) *llm.ChatRequest {
	return &llm.ChatRequest{
		Messages:    msgs,
		Tools:       a.toolSchemasForContext(ctx),
		Temperature: a.temperature,
		MaxTokens:   a.maxTokens,
	}
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
