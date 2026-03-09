package channels

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/mingminliu/claw/internal/agent"
	"github.com/mingminliu/claw/internal/gateway"
	"github.com/mingminliu/claw/internal/llm"
	"github.com/mingminliu/claw/internal/tools"
)

type mockFeishuSender struct {
	mu       sync.Mutex
	messages []feishuSentMsg
	ch       chan struct{}
}

type feishuSentMsg struct {
	ChatID string
	Text   string
}

func newMockFeishuSender() *mockFeishuSender {
	return &mockFeishuSender{ch: make(chan struct{}, 100)}
}

func (m *mockFeishuSender) SendText(_ context.Context, chatID, text string) error {
	m.mu.Lock()
	m.messages = append(m.messages, feishuSentMsg{ChatID: chatID, Text: text})
	m.mu.Unlock()
	m.ch <- struct{}{}
	return nil
}

func (m *mockFeishuSender) waitMessages(n int, timeout time.Duration) []feishuSentMsg {
	timer := time.NewTimer(timeout)
	defer timer.Stop()
loop:
	for i := 0; i < n; i++ {
		select {
		case <-m.ch:
		case <-timer.C:
			break loop
		}
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	result := make([]feishuSentMsg, len(m.messages))
	copy(result, m.messages)
	return result
}

func newTestFeishuChannel(t *testing.T) (*FeishuChannel, *mockFeishuSender) {
	t.Helper()
	provider := &fakeProvider{response: &llm.ChatResponse{Content: "feishu reply", FinishReason: "stop"}}
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

	sender := newMockFeishuSender()
	ch := &FeishuChannel{
		gateway: gw,
		sender:  sender,
	}
	mux := http.NewServeMux()
	mux.HandleFunc("/v1/feishu/webhook", ch.handleWebhook)
	ch.handler = mux
	return ch, sender
}

func makeFeishuMsgEvent(eventID, chatID, text string, mentions []feishuMention) []byte {
	content, _ := json.Marshal(feishuTextContent{Text: text})
	eventBody, _ := json.Marshal(feishuMsgEvent{
		Message: feishuEventMsg{
			MessageID:   "msg_" + eventID,
			ChatID:      chatID,
			ChatType:    "p2p",
			MessageType: "text",
			Content:     string(content),
			Mentions:    mentions,
		},
	})
	body, _ := json.Marshal(feishuWebhookBody{
		Schema: "2.0",
		Header: &feishuEventHeader{
			EventID:   eventID,
			EventType: "im.message.receive_v1",
		},
		Event: eventBody,
	})
	return body
}

func TestFeishuWebhook_Challenge(t *testing.T) {
	ch, _ := newTestFeishuChannel(t)

	body, _ := json.Marshal(map[string]string{
		"type":      "url_verification",
		"challenge": "test-challenge-token",
		"token":     "v-token",
	})
	req := httptest.NewRequest(http.MethodPost, "/v1/feishu/webhook", strings.NewReader(string(body)))
	w := httptest.NewRecorder()

	ch.Handler().ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", w.Code, http.StatusOK)
	}
	var resp map[string]string
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if resp["challenge"] != "test-challenge-token" {
		t.Errorf("challenge = %q, want %q", resp["challenge"], "test-challenge-token")
	}
}

func TestFeishuWebhook_TextMessage(t *testing.T) {
	ch, sender := newTestFeishuChannel(t)

	body := makeFeishuMsgEvent("evt_001", "oc_chat1", "hello bot", nil)
	req := httptest.NewRequest(http.MethodPost, "/v1/feishu/webhook", strings.NewReader(string(body)))
	w := httptest.NewRecorder()

	ch.Handler().ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", w.Code, http.StatusOK)
	}

	msgs := sender.waitMessages(1, 5*time.Second)
	if len(msgs) == 0 {
		t.Fatal("expected at least 1 message sent")
	}
	if msgs[0].ChatID != "oc_chat1" {
		t.Errorf("chatID = %q, want %q", msgs[0].ChatID, "oc_chat1")
	}
	if msgs[0].Text != "feishu reply" {
		t.Errorf("text = %q, want %q", msgs[0].Text, "feishu reply")
	}
}

func TestFeishuWebhook_MentionStrip(t *testing.T) {
	ch, sender := newTestFeishuChannel(t)

	mentions := []feishuMention{{Key: "@_user_1", Name: "Bot"}}
	body := makeFeishuMsgEvent("evt_002", "oc_chat2", "@_user_1 hello", mentions)
	req := httptest.NewRequest(http.MethodPost, "/v1/feishu/webhook", strings.NewReader(string(body)))
	w := httptest.NewRecorder()

	ch.Handler().ServeHTTP(w, req)

	msgs := sender.waitMessages(1, 5*time.Second)
	if len(msgs) == 0 {
		t.Fatal("expected reply")
	}
	if msgs[0].Text != "feishu reply" {
		t.Errorf("text = %q, want %q", msgs[0].Text, "feishu reply")
	}
}

func TestFeishuWebhook_DuplicateEvent(t *testing.T) {
	ch, sender := newTestFeishuChannel(t)

	body := makeFeishuMsgEvent("evt_dup", "oc_chat3", "hi", nil)

	req := httptest.NewRequest(http.MethodPost, "/v1/feishu/webhook", strings.NewReader(string(body)))
	w := httptest.NewRecorder()
	ch.Handler().ServeHTTP(w, req)

	sender.waitMessages(1, 5*time.Second)

	// Send same event again
	req = httptest.NewRequest(http.MethodPost, "/v1/feishu/webhook", strings.NewReader(string(body)))
	w = httptest.NewRecorder()
	ch.Handler().ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", w.Code, http.StatusOK)
	}

	time.Sleep(200 * time.Millisecond)

	sender.mu.Lock()
	count := len(sender.messages)
	sender.mu.Unlock()

	if count != 1 {
		t.Errorf("message count = %d, want 1 (dedup should prevent second)", count)
	}
}

func TestFeishuWebhook_NonTextMessage(t *testing.T) {
	ch, sender := newTestFeishuChannel(t)

	eventBody, _ := json.Marshal(feishuMsgEvent{
		Message: feishuEventMsg{
			MessageID:   "msg_img",
			ChatID:      "oc_chat4",
			ChatType:    "p2p",
			MessageType: "image",
			Content:     `{"image_key":"img_xxx"}`,
		},
	})
	body, _ := json.Marshal(feishuWebhookBody{
		Schema: "2.0",
		Header: &feishuEventHeader{
			EventID:   "evt_003",
			EventType: "im.message.receive_v1",
		},
		Event: eventBody,
	})

	req := httptest.NewRequest(http.MethodPost, "/v1/feishu/webhook", strings.NewReader(string(body)))
	w := httptest.NewRecorder()

	ch.Handler().ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", w.Code, http.StatusOK)
	}

	time.Sleep(200 * time.Millisecond)

	sender.mu.Lock()
	count := len(sender.messages)
	sender.mu.Unlock()

	if count != 0 {
		t.Errorf("should not reply to non-text message, got %d messages", count)
	}
}

func TestFeishuWebhook_InvalidJSON(t *testing.T) {
	ch, _ := newTestFeishuChannel(t)

	req := httptest.NewRequest(http.MethodPost, "/v1/feishu/webhook", strings.NewReader("not json"))
	w := httptest.NewRecorder()

	ch.Handler().ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want %d", w.Code, http.StatusBadRequest)
	}
}

func TestFeishuWebhook_MethodNotAllowed(t *testing.T) {
	ch, _ := newTestFeishuChannel(t)

	req := httptest.NewRequest(http.MethodGet, "/v1/feishu/webhook", nil)
	w := httptest.NewRecorder()

	ch.Handler().ServeHTTP(w, req)

	if w.Code != http.StatusMethodNotAllowed {
		t.Fatalf("status = %d, want %d", w.Code, http.StatusMethodNotAllowed)
	}
}

func TestExtractFeishuText(t *testing.T) {
	tests := []struct {
		name     string
		content  string
		mentions []feishuMention
		want     string
	}{
		{
			name:    "simple text",
			content: `{"text":"hello"}`,
			want:    "hello",
		},
		{
			name:     "with mention",
			content:  `{"text":"@_user_1 hello"}`,
			mentions: []feishuMention{{Key: "@_user_1", Name: "Bot"}},
			want:     "hello",
		},
		{
			name:     "multiple mentions",
			content:  `{"text":"@_user_1 @_user_2 hello"}`,
			mentions: []feishuMention{{Key: "@_user_1"}, {Key: "@_user_2"}},
			want:     "hello",
		},
		{
			name:    "invalid json",
			content: "not json",
			want:    "",
		},
		{
			name:    "empty text",
			content: `{"text":""}`,
			want:    "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := extractFeishuText(tt.content, tt.mentions)
			if got != tt.want {
				t.Errorf("extractFeishuText() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestSplitFeishuMessage(t *testing.T) {
	tests := []struct {
		name   string
		text   string
		maxLen int
		want   int
	}{
		{
			name:   "short message",
			text:   "hello",
			maxLen: 100,
			want:   1,
		},
		{
			name:   "exact limit",
			text:   "hello",
			maxLen: 5,
			want:   1,
		},
		{
			name:   "split at newline",
			text:   "line1\nline2\nline3",
			maxLen: 10,
			want:   3,
		},
		{
			name:   "no newline",
			text:   strings.Repeat("a", 15),
			maxLen: 10,
			want:   2,
		},
		{
			name:   "chinese characters",
			text:   strings.Repeat("你", 20),
			maxLen: 10,
			want:   2,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := splitFeishuMessage(tt.text, tt.maxLen)
			if len(got) != tt.want {
				t.Errorf("splitFeishuMessage() returned %d segments, want %d", len(got), tt.want)
			}
			combined := strings.Join(got, "")
			if combined != tt.text {
				t.Errorf("combined = %q, want %q", combined, tt.text)
			}
		})
	}
}

func TestFeishuWebhook_UnknownEventType(t *testing.T) {
	ch, _ := newTestFeishuChannel(t)

	body, _ := json.Marshal(feishuWebhookBody{
		Schema: "2.0",
		Header: &feishuEventHeader{
			EventID:   "evt_unknown",
			EventType: "im.chat.create_v1",
		},
		Event: json.RawMessage(`{}`),
	})

	req := httptest.NewRequest(http.MethodPost, "/v1/feishu/webhook", strings.NewReader(string(body)))
	w := httptest.NewRecorder()

	ch.Handler().ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", w.Code, http.StatusOK)
	}
}

func TestFeishuChannel_Interface(t *testing.T) {
	ch, _ := newTestFeishuChannel(t)

	if ch.Name() != "feishu" {
		t.Errorf("Name() = %q, want %q", ch.Name(), "feishu")
	}

	if err := ch.Start(context.Background()); err != nil {
		t.Errorf("Start() error = %v", err)
	}

	if err := ch.Stop(context.Background()); err != nil {
		t.Errorf("Stop() error = %v", err)
	}
}
