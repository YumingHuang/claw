package gateway

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	"github.com/YumingHuang/claw/internal/agent"
	"github.com/YumingHuang/claw/internal/models"
	"github.com/YumingHuang/claw/internal/requestctx"
	"github.com/google/uuid"
)

// Gateway coordinates sessions, queuing, and the agents.
type Gateway struct {
	agent       *agent.Agent
	sessions    SessionStore
	queue       *agent.SessionQueue
	toolProfile string
}

// NewGateway creates a Gateway.
func NewGateway(a *agent.Agent, sessions SessionStore, queue *agent.SessionQueue) *Gateway {
	return &Gateway{agent: a, sessions: sessions, queue: queue}
}

// SetToolProfile configures the default tool profile applied to incoming requests.
func (g *Gateway) SetToolProfile(profile string) {
	g.toolProfile = profile
}

// HandleMessage processes a non-streaming chat request.
func (g *Gateway) HandleMessage(ctx context.Context, sessionID, channel, message string) (*models.ChatResponse, error) {
	requestID := uuid.New().String()
	ctx = context.WithValue(ctx, ContextKeyRequestID, requestID)
	ctx = context.WithValue(ctx, ContextKeySessionID, sessionID)
	if g.toolProfile != "" {
		ctx = context.WithValue(ctx, requestctx.ToolProfileKey, g.toolProfile)
	}

	start := time.Now()
	session := g.sessions.GetOrCreate(sessionID, channel)

	g.queue.Acquire(sessionID)
	defer g.queue.Release(sessionID)

	result, err := g.agent.Run(ctx, session, message)
	latency := time.Since(start)

	if err != nil {
		slog.Error("handle message failed",
			"request_id", requestID, "session_id", sessionID, "latency", latency, "error", err)
		return nil, fmt.Errorf("agents run: %w", err)
	}

	resp := &models.ChatResponse{
		SessionID: sessionID,
		RequestID: requestID,
		Message:   models.NewAssistantMessage(result),
	}

	slog.Info("handle message",
		"request_id", requestID, "session_id", sessionID, "latency", latency)

	return resp, nil
}

// HandleMessageStream processes a streaming chat request.
func (g *Gateway) HandleMessageStream(ctx context.Context, sessionID, channel, message string) (<-chan models.StreamChunk, error) {
	requestID := uuid.New().String()
	ctx = context.WithValue(ctx, ContextKeyRequestID, requestID)
	ctx = context.WithValue(ctx, ContextKeySessionID, sessionID)
	if g.toolProfile != "" {
		ctx = context.WithValue(ctx, requestctx.ToolProfileKey, g.toolProfile)
	}

	session := g.sessions.GetOrCreate(sessionID, channel)

	g.queue.Acquire(sessionID)

	ch, err := g.agent.RunStream(ctx, session, message)
	if err != nil {
		g.queue.Release(sessionID)
		return nil, fmt.Errorf("agents run stream: %w", err)
	}

	out := make(chan models.StreamChunk)
	go func() {
		defer close(out)
		defer g.queue.Release(sessionID)
		for chunk := range ch {
			out <- chunk
		}
	}()

	return out, nil
}

// GetSession returns a session by ID.
func (g *Gateway) GetSession(id string) (*agent.Session, bool) {
	return g.sessions.Get(id)
}

// DeleteSession removes a session.
func (g *Gateway) DeleteSession(id string) {
	g.sessions.Delete(id)
}

// SessionCount returns the number of active sessions.
func (g *Gateway) SessionCount() int {
	return g.sessions.Count()
}

// ToolNames returns the names of registered tools.
func (g *Gateway) ToolNames() []string {
	return g.agent.ToolNames()
}
