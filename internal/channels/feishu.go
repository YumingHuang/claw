package channels

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"strings"
	"sync"

	lark "github.com/larksuite/oapi-sdk-go/v3"
	larkim "github.com/larksuite/oapi-sdk-go/v3/service/im/v1"
	"github.com/YumingHuang/claw/internal/config"
	"github.com/YumingHuang/claw/internal/gateway"
)

const feishuMaxMsgLen = 4000

// Feishu webhook event structures.

type feishuWebhookBody struct {
	Schema    string             `json:"schema"`
	Header    *feishuEventHeader `json:"header"`
	Event     json.RawMessage    `json:"event"`
	Challenge string             `json:"challenge"`
	Token     string             `json:"token"`
	Type      string             `json:"type"`
}

type feishuEventHeader struct {
	EventID   string `json:"event_id"`
	EventType string `json:"event_type"`
	Token     string `json:"token"`
	AppID     string `json:"app_id"`
}

type feishuMsgEvent struct {
	Sender  feishuEventSender `json:"sender"`
	Message feishuEventMsg    `json:"message"`
}

type feishuEventSender struct {
	SenderID struct {
		OpenID string `json:"open_id"`
	} `json:"sender_id"`
	SenderType string `json:"sender_type"`
}

type feishuEventMsg struct {
	MessageID   string          `json:"message_id"`
	ChatID      string          `json:"chat_id"`
	ChatType    string          `json:"chat_type"`
	MessageType string          `json:"message_type"`
	Content     string          `json:"content"`
	Mentions    []feishuMention `json:"mentions"`
}

type feishuMention struct {
	Key  string `json:"key"`
	Name string `json:"name"`
}

type feishuTextContent struct {
	Text string `json:"text"`
}

// feishuSender abstracts message sending for testability.
type feishuSender interface {
	SendText(ctx context.Context, chatID, text string) error
}

// larkSender sends messages via the Feishu/Lark IM API.
type larkSender struct {
	client *lark.Client
}

func (s *larkSender) SendText(ctx context.Context, chatID, text string) error {
	content, _ := json.Marshal(feishuTextContent{Text: text})
	req := larkim.NewCreateMessageReqBuilder().
		ReceiveIdType("chat_id").
		Body(larkim.NewCreateMessageReqBodyBuilder().
			ReceiveId(chatID).
			MsgType("text").
			Content(string(content)).
			Build()).
		Build()

	resp, err := s.client.Im.Message.Create(ctx, req)
	if err != nil {
		return fmt.Errorf("feishu send: %w", err)
	}
	if !resp.Success() {
		return fmt.Errorf("feishu API: code=%d msg=%s", resp.Code, resp.Msg)
	}
	return nil
}

// FeishuChannel receives messages from Feishu via webhook and replies via IM API.
type FeishuChannel struct {
	gateway *gateway.Gateway
	sender  feishuSender
	handler http.Handler
	seen    sync.Map // event_id deduplication
}

func (f *FeishuChannel) Name() string { return "feishu" }

// NewFeishuChannel creates a Feishu channel with the given gateway and config.
func NewFeishuChannel(gw *gateway.Gateway, cfg config.FeishuChannelConfig) *FeishuChannel {
	client := lark.NewClient(cfg.AppID, cfg.AppSecret)
	ch := &FeishuChannel{
		gateway: gw,
		sender:  &larkSender{client: client},
	}
	mux := http.NewServeMux()
	mux.HandleFunc("/v1/feishu/webhook", ch.handleWebhook)
	ch.handler = mux
	return ch
}

// Handler returns the HTTP handler for the Feishu webhook endpoint.
func (f *FeishuChannel) Handler() http.Handler {
	return f.handler
}

// Start is a no-op; the webhook handler is mounted on the HTTP channel.
func (f *FeishuChannel) Start(_ context.Context) error {
	slog.Info("feishu channel enabled")
	return nil
}

// Stop is a no-op; connections close when the HTTP server shuts down.
func (f *FeishuChannel) Stop(_ context.Context) error {
	slog.Info("feishu channel stopping")
	return nil
}

func (f *FeishuChannel) handleWebhook(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	body, err := io.ReadAll(r.Body)
	defer r.Body.Close()
	if err != nil {
		slog.Error("feishu: read body", "error", err)
		http.Error(w, "bad request", http.StatusBadRequest)
		return
	}

	var event feishuWebhookBody
	if err := json.Unmarshal(body, &event); err != nil {
		slog.Error("feishu: parse event", "error", err)
		http.Error(w, "bad request", http.StatusBadRequest)
		return
	}

	// URL verification challenge
	if event.Type == "url_verification" {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]string{"challenge": event.Challenge})
		return
	}

	if event.Header == nil || event.Header.EventType != "im.message.receive_v1" {
		w.WriteHeader(http.StatusOK)
		return
	}

	// Deduplicate by event_id
	if _, loaded := f.seen.LoadOrStore(event.Header.EventID, struct{}{}); loaded {
		slog.Debug("feishu: duplicate event", "event_id", event.Header.EventID)
		w.WriteHeader(http.StatusOK)
		return
	}

	var msgEvent feishuMsgEvent
	if err := json.Unmarshal(event.Event, &msgEvent); err != nil {
		slog.Error("feishu: parse message event", "error", err)
		w.WriteHeader(http.StatusOK)
		return
	}

	if msgEvent.Message.MessageType != "text" {
		slog.Debug("feishu: unsupported message type", "type", msgEvent.Message.MessageType)
		w.WriteHeader(http.StatusOK)
		return
	}

	// Acknowledge immediately; process asynchronously
	w.WriteHeader(http.StatusOK)

	go func() {
		defer func() {
			if rv := recover(); rv != nil {
				slog.Error("feishu: panic in processMessage", "error", rv,
					"chat_id", msgEvent.Message.ChatID)
			}
		}()
		f.processMessage(msgEvent.Message.ChatID, msgEvent.Message.Content, msgEvent.Message.Mentions)
	}()
}

func (f *FeishuChannel) processMessage(chatID, contentJSON string, mentions []feishuMention) {
	text := extractFeishuText(contentJSON, mentions)
	if text == "" {
		return
	}

	ctx := context.Background()

	resp, err := f.gateway.HandleMessage(ctx, chatID, "feishu", text)
	if err != nil {
		slog.Error("feishu: handle message failed", "chat_id", chatID, "error", err)
		_ = f.sender.SendText(ctx, chatID, fmt.Sprintf("处理消息时出错: %v", err))
		return
	}

	reply := resp.Message.Content
	if reply == "" {
		return
	}

	for _, seg := range splitFeishuMessage(reply, feishuMaxMsgLen) {
		if err := f.sender.SendText(ctx, chatID, seg); err != nil {
			slog.Error("feishu: send reply failed", "chat_id", chatID, "error", err)
			return
		}
	}
}

// extractFeishuText extracts plain text from Feishu message content JSON
// and strips bot mention placeholders.
func extractFeishuText(contentJSON string, mentions []feishuMention) string {
	var tc feishuTextContent
	if err := json.Unmarshal([]byte(contentJSON), &tc); err != nil {
		return ""
	}
	text := tc.Text
	for _, m := range mentions {
		text = strings.ReplaceAll(text, m.Key, "")
	}
	return strings.TrimSpace(text)
}

// splitFeishuMessage splits text into segments of at most maxLen runes,
// preferring to break at newline boundaries.
func splitFeishuMessage(text string, maxLen int) []string {
	if maxLen <= 0 {
		maxLen = feishuMaxMsgLen
	}
	runes := []rune(text)
	if len(runes) <= maxLen {
		return []string{text}
	}

	var segments []string
	for len(runes) > 0 {
		if len(runes) <= maxLen {
			segments = append(segments, string(runes))
			break
		}

		cut := maxLen
		for i := maxLen - 1; i > 0; i-- {
			if runes[i] == '\n' {
				cut = i + 1
				break
			}
		}

		segments = append(segments, string(runes[:cut]))
		runes = runes[cut:]
	}

	return segments
}
