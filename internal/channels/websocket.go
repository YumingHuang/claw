package channels

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"sync"
	"time"

	"github.com/coder/websocket"
	"github.com/coder/websocket/wsjson"
	"github.com/google/uuid"
	"github.com/YumingHuang/claw/internal/gateway"
	"github.com/YumingHuang/claw/internal/models"
)

// WebSocket message types exchanged between client and server.
type wsIncoming struct {
	Type    string `json:"type"`    // "message" | "ping"
	Content string `json:"content"` // for "message" type
}

type wsOutgoing struct {
	Type      string          `json:"type"`                // "chunk" | "status" | "pong" | "error" | "done"
	Delta     string          `json:"delta,omitempty"`     // for "chunk"
	Done      bool            `json:"done,omitempty"`      // for "chunk"
	SessionID string          `json:"session_id,omitempty"`
	Status    string          `json:"status,omitempty"`    // for "status": "thinking", "calling_tool"
	Tool      string          `json:"tool,omitempty"`      // for "status" when calling_tool
	Code      string          `json:"code,omitempty"`      // for "error"
	Message   string          `json:"message,omitempty"`   // for "error"
	Usage     *models.Usage   `json:"usage,omitempty"`
	ToolCalls []models.ToolCall `json:"tool_calls,omitempty"`
}

// WebSocketChannel serves the WebSocket API at /v1/ws.
type WebSocketChannel struct {
	gateway      *gateway.Gateway
	pingInterval time.Duration
	handler      http.Handler
}

func (ws *WebSocketChannel) Name() string { return "websocket" }

// NewWebSocketChannel creates a WebSocket channel.
func NewWebSocketChannel(gw *gateway.Gateway, pingInterval time.Duration) *WebSocketChannel {
	if pingInterval <= 0 {
		pingInterval = 30 * time.Second
	}
	ws := &WebSocketChannel{
		gateway:      gw,
		pingInterval: pingInterval,
	}
	mux := http.NewServeMux()
	mux.HandleFunc("/v1/ws", ws.handleUpgrade)
	ws.handler = mux
	return ws
}

// Handler returns the HTTP handler for the WebSocket endpoint,
// allowing integration with an existing HTTP server/router.
func (ws *WebSocketChannel) Handler() http.Handler {
	return ws.handler
}

// Start is a no-op for WebSocketChannel because its handler is
// mounted on the HTTPChannel's router. Satisfies the Channel interface.
func (ws *WebSocketChannel) Start(_ context.Context) error {
	slog.Info("websocket channel enabled")
	return nil
}

// Stop is a no-op; connections are closed when the parent HTTP server shuts down.
func (ws *WebSocketChannel) Stop(_ context.Context) error {
	slog.Info("websocket channel stopping")
	return nil
}

func (ws *WebSocketChannel) handleUpgrade(w http.ResponseWriter, r *http.Request) {
	conn, err := websocket.Accept(w, r, &websocket.AcceptOptions{
		InsecureSkipVerify: true, // allow any origin for development
	})
	if err != nil {
		slog.Error("websocket accept failed", "error", err)
		return
	}

	sessionID := r.URL.Query().Get("session_id")
	if sessionID == "" {
		sessionID = uuid.New().String()
	}

	slog.Info("websocket connected", "session_id", sessionID, "remote", r.RemoteAddr)

	ctx := r.Context()
	ws.serveConn(ctx, conn, sessionID)
}

func (ws *WebSocketChannel) serveConn(ctx context.Context, conn *websocket.Conn, sessionID string) {
	defer conn.CloseNow()

	ctx, cancel := context.WithCancel(ctx)
	defer cancel()

	var writeMu sync.Mutex
	sendJSON := func(msg wsOutgoing) error {
		writeMu.Lock()
		defer writeMu.Unlock()
		writeCtx, writeCancel := context.WithTimeout(ctx, 5*time.Second)
		defer writeCancel()
		return wsjson.Write(writeCtx, conn, msg)
	}

	// Heartbeat goroutine
	go func() {
		ticker := time.NewTicker(ws.pingInterval)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				if err := conn.Ping(ctx); err != nil {
					slog.Debug("websocket ping failed", "session_id", sessionID, "error", err)
					cancel()
					return
				}
			}
		}
	}()

	// Read loop
	for {
		var msg wsIncoming
		if err := wsjson.Read(ctx, conn, &msg); err != nil {
			if websocket.CloseStatus(err) == websocket.StatusNormalClosure ||
				websocket.CloseStatus(err) == websocket.StatusGoingAway {
				slog.Info("websocket closed normally", "session_id", sessionID)
			} else if ctx.Err() != nil {
				slog.Debug("websocket read cancelled", "session_id", sessionID)
			} else {
				slog.Warn("websocket read error", "session_id", sessionID, "error", err)
			}
			return
		}

		switch msg.Type {
		case "ping":
			if err := sendJSON(wsOutgoing{Type: "pong"}); err != nil {
				slog.Warn("websocket pong failed", "session_id", sessionID, "error", err)
				return
			}

		case "message":
			if msg.Content == "" {
				_ = sendJSON(wsOutgoing{
					Type:    "error",
					Code:    "invalid_request",
					Message: "content is required",
				})
				continue
			}
			ws.handleMessage(ctx, conn, sessionID, msg.Content, sendJSON)

		default:
			_ = sendJSON(wsOutgoing{
				Type:    "error",
				Code:    "invalid_request",
				Message: fmt.Sprintf("unknown message type: %q", msg.Type),
			})
		}
	}
}

func (ws *WebSocketChannel) handleMessage(
	ctx context.Context,
	_ *websocket.Conn,
	sessionID, content string,
	sendJSON func(wsOutgoing) error,
) {
	// Notify client we're processing
	_ = sendJSON(wsOutgoing{Type: "status", Status: "thinking", SessionID: sessionID})

	ch, err := ws.gateway.HandleMessageStream(ctx, sessionID, "websocket", content)
	if err != nil {
		_ = sendJSON(wsOutgoing{
			Type:    "error",
			Code:    "internal_error",
			Message: err.Error(),
		})
		return
	}

	for chunk := range ch {
		if chunk.Err != nil {
			_ = sendJSON(wsOutgoing{
				Type:    "error",
				Code:    "internal_error",
				Message: chunk.Err.Error(),
			})
			return
		}

		if len(chunk.ToolCalls) > 0 {
			for _, tc := range chunk.ToolCalls {
				_ = sendJSON(wsOutgoing{
					Type:   "status",
					Status: "calling_tool",
					Tool:   tc.Name,
				})
			}
		}

		if chunk.Delta != "" || chunk.Done {
			msg := wsOutgoing{
				Type:  "chunk",
				Delta: chunk.Delta,
				Done:  chunk.Done,
			}
			if chunk.Usage != nil {
				msg.Usage = chunk.Usage
			}
			if err := sendJSON(msg); err != nil {
				slog.Warn("websocket write failed", "session_id", sessionID, "error", err)
				return
			}
		}
	}

	// Final done message with session_id
	_ = sendJSON(wsOutgoing{
		Type:      "done",
		SessionID: sessionID,
	})
}

// --- JSON helpers for serializing tool calls (for potential direct use) ---

func marshalToolCalls(tcs []models.ToolCall) json.RawMessage {
	if len(tcs) == 0 {
		return nil
	}
	b, _ := json.Marshal(tcs)
	return b
}
