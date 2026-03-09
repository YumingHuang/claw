package channels

import (
	"context"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/coder/websocket"
	"github.com/coder/websocket/wsjson"
	"github.com/YumingHuang/claw/internal/agent"
	"github.com/YumingHuang/claw/internal/gateway"
	"github.com/YumingHuang/claw/internal/llm"
	"github.com/YumingHuang/claw/internal/tools"
)

func newTestWebSocketServer(t *testing.T) (*httptest.Server, *WebSocketChannel) {
	t.Helper()
	provider := &fakeProvider{response: &llm.ChatResponse{Content: "ws reply", FinishReason: "stop"}}
	registry := tools.NewRegistry()
	a := agent.NewAgent(provider, registry, agent.AgentOptions{
		SystemPrompt:  "test",
		MaxIterations: 10,
		ContextWindow: 100000,
	})

	ctx := context.Background()
	sessions := gateway.NewMemorySessionStore(ctx, 1*time.Hour, 100, 5*time.Minute)
	queue := agent.NewSessionQueue()
	gw := gateway.NewGateway(a, sessions, queue)

	wsCh := NewWebSocketChannel(gw, 30*time.Second)
	server := httptest.NewServer(wsCh.Handler())
	return server, wsCh
}

func dialWS(t *testing.T, serverURL string, sessionID string) *websocket.Conn {
	t.Helper()
	url := "ws" + serverURL[len("http"):] + "/v1/ws"
	if sessionID != "" {
		url += "?session_id=" + sessionID
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	conn, _, err := websocket.Dial(ctx, url, nil)
	if err != nil {
		t.Fatalf("websocket dial: %v", err)
	}
	return conn
}

func TestWebSocket_PingPong(t *testing.T) {
	server, _ := newTestWebSocketServer(t)
	defer server.Close()

	conn := dialWS(t, server.URL, "s1")
	defer conn.CloseNow()

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	err := wsjson.Write(ctx, conn, wsIncoming{Type: "ping"})
	if err != nil {
		t.Fatalf("write ping: %v", err)
	}

	var resp wsOutgoing
	err = wsjson.Read(ctx, conn, &resp)
	if err != nil {
		t.Fatalf("read pong: %v", err)
	}
	if resp.Type != "pong" {
		t.Errorf("type = %q, want %q", resp.Type, "pong")
	}

	conn.Close(websocket.StatusNormalClosure, "done")
}

func TestWebSocket_Message(t *testing.T) {
	server, _ := newTestWebSocketServer(t)
	defer server.Close()

	conn := dialWS(t, server.URL, "s2")
	defer conn.CloseNow()

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	err := wsjson.Write(ctx, conn, wsIncoming{Type: "message", Content: "hello"})
	if err != nil {
		t.Fatalf("write message: %v", err)
	}

	// Expect a "status: thinking" message first
	var status wsOutgoing
	err = wsjson.Read(ctx, conn, &status)
	if err != nil {
		t.Fatalf("read status: %v", err)
	}
	if status.Type != "status" || status.Status != "thinking" {
		t.Errorf("expected status=thinking, got type=%q status=%q", status.Type, status.Status)
	}

	// Read chunks until "done"
	var content string
	for {
		var msg wsOutgoing
		err = wsjson.Read(ctx, conn, &msg)
		if err != nil {
			t.Fatalf("read: %v", err)
		}
		if msg.Type == "chunk" {
			content += msg.Delta
		}
		if msg.Type == "done" {
			break
		}
	}

	if content != "ws reply" {
		t.Errorf("content = %q, want %q", content, "ws reply")
	}

	conn.Close(websocket.StatusNormalClosure, "done")
}

func TestWebSocket_EmptyContent(t *testing.T) {
	server, _ := newTestWebSocketServer(t)
	defer server.Close()

	conn := dialWS(t, server.URL, "s3")
	defer conn.CloseNow()

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	err := wsjson.Write(ctx, conn, wsIncoming{Type: "message", Content: ""})
	if err != nil {
		t.Fatalf("write: %v", err)
	}

	var resp wsOutgoing
	err = wsjson.Read(ctx, conn, &resp)
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	if resp.Type != "error" {
		t.Errorf("type = %q, want %q", resp.Type, "error")
	}
	if resp.Code != "invalid_request" {
		t.Errorf("code = %q, want %q", resp.Code, "invalid_request")
	}

	conn.Close(websocket.StatusNormalClosure, "done")
}

func TestWebSocket_UnknownType(t *testing.T) {
	server, _ := newTestWebSocketServer(t)
	defer server.Close()

	conn := dialWS(t, server.URL, "s4")
	defer conn.CloseNow()

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	err := wsjson.Write(ctx, conn, wsIncoming{Type: "foobar"})
	if err != nil {
		t.Fatalf("write: %v", err)
	}

	var resp wsOutgoing
	err = wsjson.Read(ctx, conn, &resp)
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	if resp.Type != "error" {
		t.Errorf("type = %q, want %q", resp.Type, "error")
	}

	conn.Close(websocket.StatusNormalClosure, "done")
}

func TestWebSocket_AutoSessionID(t *testing.T) {
	server, _ := newTestWebSocketServer(t)
	defer server.Close()

	// Dial without session_id
	conn := dialWS(t, server.URL, "")
	defer conn.CloseNow()

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	err := wsjson.Write(ctx, conn, wsIncoming{Type: "message", Content: "hello"})
	if err != nil {
		t.Fatalf("write: %v", err)
	}

	// Read until we get a "done" message that has a non-empty session_id
	for {
		var msg wsOutgoing
		err = wsjson.Read(ctx, conn, &msg)
		if err != nil {
			t.Fatalf("read: %v", err)
		}
		if msg.Type == "done" {
			if msg.SessionID == "" {
				t.Error("session_id should be auto-generated")
			}
			break
		}
	}

	conn.Close(websocket.StatusNormalClosure, "done")
}

func TestWebSocket_MultipleMessages(t *testing.T) {
	server, _ := newTestWebSocketServer(t)
	defer server.Close()

	conn := dialWS(t, server.URL, "multi")
	defer conn.CloseNow()

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	for i := 0; i < 3; i++ {
		err := wsjson.Write(ctx, conn, wsIncoming{Type: "message", Content: "msg"})
		if err != nil {
			t.Fatalf("write %d: %v", i, err)
		}

		// Drain until "done"
		for {
			var msg wsOutgoing
			err = wsjson.Read(ctx, conn, &msg)
			if err != nil {
				t.Fatalf("read %d: %v", i, err)
			}
			if msg.Type == "done" {
				break
			}
		}
	}

	conn.Close(websocket.StatusNormalClosure, "done")
}

func TestWebSocket_MountOnHTTPChannel(t *testing.T) {
	provider := &fakeProvider{response: &llm.ChatResponse{Content: "via http mount", FinishReason: "stop"}}
	registry := tools.NewRegistry()
	a := agent.NewAgent(provider, registry, agent.AgentOptions{
		SystemPrompt:  "test",
		MaxIterations: 10,
		ContextWindow: 100000,
	})

	ctx := context.Background()
	sessions := gateway.NewMemorySessionStore(ctx, 1*time.Hour, 100, 5*time.Minute)
	queue := agent.NewSessionQueue()
	gw := gateway.NewGateway(a, sessions, queue)

	httpCh := NewHTTPChannel(gw, ":0")
	wsCh := NewWebSocketChannel(gw, 30*time.Second)
	httpCh.MountHandler("/v1/ws", wsCh.Handler())

	server := httptest.NewServer(httpCh.Router())
	defer server.Close()

	// Test HTTP still works
	// Test WebSocket works through the same server
	wsURL := "ws" + server.URL[len("http"):] + "/v1/ws?session_id=mount-test"
	connCtx, connCancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer connCancel()
	conn, _, err := websocket.Dial(connCtx, wsURL, nil)
	if err != nil {
		t.Fatalf("websocket dial: %v", err)
	}
	defer conn.CloseNow()

	err = wsjson.Write(connCtx, conn, wsIncoming{Type: "message", Content: "hi"})
	if err != nil {
		t.Fatalf("write: %v", err)
	}

	var content string
	for {
		var msg wsOutgoing
		err = wsjson.Read(connCtx, conn, &msg)
		if err != nil {
			t.Fatalf("read: %v", err)
		}
		if msg.Type == "chunk" {
			content += msg.Delta
		}
		if msg.Type == "done" {
			break
		}
	}

	if content != "via http mount" {
		t.Errorf("content = %q, want %q", content, "via http mount")
	}

	conn.Close(websocket.StatusNormalClosure, "done")
}
