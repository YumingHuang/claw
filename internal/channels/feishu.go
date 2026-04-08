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
	"time"

	"github.com/YumingHuang/claw/internal/config"
	"github.com/YumingHuang/claw/internal/gateway"
	lark "github.com/larksuite/oapi-sdk-go/v3"
	larkdispatcher "github.com/larksuite/oapi-sdk-go/v3/event/dispatcher"
	larkim "github.com/larksuite/oapi-sdk-go/v3/service/im/v1"
	larkws "github.com/larksuite/oapi-sdk-go/v3/ws"
)

const feishuMaxMsgLen = 4000

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

type feishuSender interface {
	SendText(ctx context.Context, chatID, text string) error
	SendMarkdown(ctx context.Context, chatID, markdown string) error
	ReplyText(ctx context.Context, messageID, text string) error
	ReplyMarkdown(ctx context.Context, messageID, markdown string) error
	AddReaction(ctx context.Context, messageID, emojiType string) (reactionID string, err error)
	RemoveReaction(ctx context.Context, messageID, reactionID string) error
}

type feishuLongConnRunner interface {
	Start(ctx context.Context) error
}

type larkSender struct {
	client *lark.Client
}

func (s *larkSender) SendText(ctx context.Context, chatID, text string) error {
	content, _ := json.Marshal(feishuTextContent{Text: text})
	return s.sendMessage(ctx, chatID, "text", string(content))
}

func (s *larkSender) SendMarkdown(ctx context.Context, chatID, markdown string) error {
	card := map[string]interface{}{
		"type": "template",
		"data": map[string]interface{}{
			"template_variable": map[string]string{
				"content": markdown,
			},
			"template_id": "",
			"config": map[string]interface{}{
				"update_multi": true,
			},
		},
		"elements": []map[string]interface{}{
			{
				"tag":     "markdown",
				"content": markdown,
			},
		},
	}
	content, _ := json.Marshal(card)
	return s.sendMessage(ctx, chatID, "interactive", string(content))
}

func (s *larkSender) sendMessage(ctx context.Context, chatID, msgType, content string) error {
	req := larkim.NewCreateMessageReqBuilder().
		ReceiveIdType("chat_id").
		Body(larkim.NewCreateMessageReqBodyBuilder().
			ReceiveId(chatID).
			MsgType(msgType).
			Content(content).
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

func (s *larkSender) replyMessage(ctx context.Context, messageID, msgType, content string) error {
	req := larkim.NewReplyMessageReqBuilder().
		MessageId(messageID).
		Body(larkim.NewReplyMessageReqBodyBuilder().
			MsgType(msgType).
			Content(content).
			Build()).
		Build()

	resp, err := s.client.Im.Message.Reply(ctx, req)
	if err != nil {
		return fmt.Errorf("feishu reply: %w", err)
	}
	if !resp.Success() {
		return fmt.Errorf("feishu API: code=%d msg=%s", resp.Code, resp.Msg)
	}
	return nil
}

func (s *larkSender) ReplyText(ctx context.Context, messageID, text string) error {
	content, _ := json.Marshal(feishuTextContent{Text: text})
	return s.replyMessage(ctx, messageID, "text", string(content))
}

func (s *larkSender) ReplyMarkdown(ctx context.Context, messageID, markdown string) error {
	card := map[string]interface{}{
		"type": "template",
		"data": map[string]interface{}{
			"template_variable": map[string]string{
				"content": markdown,
			},
			"template_id": "",
			"config": map[string]interface{}{
				"update_multi": true,
			},
		},
		"elements": []map[string]interface{}{
			{
				"tag":     "markdown",
				"content": markdown,
			},
		},
	}
	content, _ := json.Marshal(card)
	return s.replyMessage(ctx, messageID, "interactive", string(content))
}

func (s *larkSender) AddReaction(ctx context.Context, messageID, emojiType string) (string, error) {
	req := larkim.NewCreateMessageReactionReqBuilder().
		MessageId(messageID).
		Body(larkim.NewCreateMessageReactionReqBodyBuilder().
			ReactionType(&larkim.Emoji{EmojiType: &emojiType}).
			Build()).
		Build()
	resp, err := s.client.Im.MessageReaction.Create(ctx, req)
	if err != nil {
		return "", fmt.Errorf("feishu add reaction: %w", err)
	}
	if !resp.Success() {
		return "", fmt.Errorf("feishu API: code=%d msg=%s", resp.Code, resp.Msg)
	}
	if resp.Data != nil && resp.Data.ReactionId != nil {
		return *resp.Data.ReactionId, nil
	}
	return "", nil
}

func (s *larkSender) RemoveReaction(ctx context.Context, messageID, reactionID string) error {
	req := larkim.NewDeleteMessageReactionReqBuilder().
		MessageId(messageID).
		ReactionId(reactionID).
		Build()
	resp, err := s.client.Im.MessageReaction.Delete(ctx, req)
	if err != nil {
		return fmt.Errorf("feishu remove reaction: %w", err)
	}
	if !resp.Success() {
		return fmt.Errorf("feishu API: code=%d msg=%s", resp.Code, resp.Msg)
	}
	return nil
}

// FeishuChannel supports both webhook delivery and active long-connection delivery.
type FeishuChannel struct {
	gateway        *gateway.Gateway
	sender         feishuSender
	handler        http.Handler
	seen           sync.Map // eventID -> time.Time
	config         config.FeishuChannelConfig
	longConnRunner feishuLongConnRunner
	cancelLongConn context.CancelFunc
}

// seenTTL controls how long event IDs are kept for deduplication.
const seenTTL = 10 * time.Minute

// dedupEvent returns true if the event was already seen.
func (f *FeishuChannel) dedupEvent(eventID string) bool {
	if eventID == "" {
		return false
	}
	if _, loaded := f.seen.LoadOrStore(eventID, time.Now()); loaded {
		return true
	}
	return false
}

// cleanupSeen removes expired entries from the seen map.
func (f *FeishuChannel) cleanupSeen(ctx context.Context) {
	ticker := time.NewTicker(seenTTL)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			now := time.Now()
			f.seen.Range(func(key, value any) bool {
				if now.Sub(value.(time.Time)) > seenTTL {
					f.seen.Delete(key)
				}
				return true
			})
		}
	}
}

func (f *FeishuChannel) Name() string { return "feishu" }

// Sender returns the underlying sender for external use (e.g. cron notifications).
func (f *FeishuChannel) Sender() feishuSender { return f.sender }

func NewFeishuChannel(gw *gateway.Gateway, cfg config.FeishuChannelConfig) *FeishuChannel {
	client := lark.NewClient(cfg.AppID, cfg.AppSecret)
	ch := &FeishuChannel{
		gateway: gw,
		sender:  &larkSender{client: client},
		config:  cfg,
	}

	mux := http.NewServeMux()
	mux.HandleFunc("/v1/feishu/webhook", ch.handleWebhook)
	ch.handler = mux

	if cfg.LongConnection {
		dispatcher := larkdispatcher.NewEventDispatcher(cfg.VerificationToken, cfg.EncryptKey)
		dispatcher.OnP2MessageReceiveV1(ch.handleLongConnMessage)
		ch.longConnRunner = larkws.NewClient(
			cfg.AppID,
			cfg.AppSecret,
			larkws.WithEventHandler(dispatcher),
		)
	}

	return ch
}

func (f *FeishuChannel) Handler() http.Handler {
	return f.handler
}

func (f *FeishuChannel) Start(ctx context.Context) error {
	if !f.config.LongConnection {
		slog.Info("feishu channel enabled", "mode", "webhook")
		go f.cleanupSeen(ctx)
		return nil
	}
	if f.longConnRunner == nil {
		return fmt.Errorf("feishu long connection runner is not configured")
	}

	slog.Info("feishu channel enabled", "mode", "long_connection")
	longCtx, cancel := context.WithCancel(ctx)
	f.cancelLongConn = cancel
	go f.cleanupSeen(longCtx)
	return f.longConnRunner.Start(longCtx)
}

func (f *FeishuChannel) Stop(_ context.Context) error {
	slog.Info("feishu channel stopping")
	if f.cancelLongConn != nil {
		f.cancelLongConn()
	}
	return nil
}

func (f *FeishuChannel) handleWebhook(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	body, err := io.ReadAll(io.LimitReader(r.Body, 1<<20)) // 1MB max
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
	if err := verifyFeishuToken(f.config, event); err != nil {
		slog.Error("feishu: verify token", "error", err)
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}

	if event.Type == "url_verification" {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]string{"challenge": event.Challenge})
		return
	}

	if event.Header == nil || event.Header.EventType != "im.message.receive_v1" {
		w.WriteHeader(http.StatusOK)
		return
	}

	var msgEvent feishuMsgEvent
	if err := json.Unmarshal(event.Event, &msgEvent); err != nil {
		slog.Error("feishu: parse message event", "error", err)
		w.WriteHeader(http.StatusOK)
		return
	}

	w.WriteHeader(http.StatusOK)

	go func() {
		defer func() {
			if rv := recover(); rv != nil {
				slog.Error("feishu: panic in processMessage", "error", rv, "chat_id", msgEvent.Message.ChatID)
			}
		}()
		f.handleIncomingMessage(
			event.Header.EventID,
			msgEvent.Message.ChatID,
			msgEvent.Message.MessageID,
			msgEvent.Message.MessageType,
			msgEvent.Message.Content,
			msgEvent.Message.Mentions,
		)
	}()
}

func (f *FeishuChannel) handleLongConnMessage(_ context.Context, event *larkim.P2MessageReceiveV1) error {
	if event == nil || event.Event == nil || event.Event.Message == nil {
		return nil
	}

	msg := event.Event.Message
	mentions := make([]feishuMention, 0, len(msg.Mentions))
	for _, mention := range msg.Mentions {
		if mention == nil {
			continue
		}
		item := feishuMention{}
		if mention.Key != nil {
			item.Key = *mention.Key
		}
		if mention.Name != nil {
			item.Name = *mention.Name
		}
		mentions = append(mentions, item)
	}

	eventID := ""
	if event.EventV2Base != nil && event.EventV2Base.Header != nil {
		eventID = event.EventV2Base.Header.EventID
	}

	f.handleIncomingMessage(
		eventID,
		derefString(msg.ChatId),
		derefString(msg.MessageId),
		derefString(msg.MessageType),
		derefString(msg.Content),
		mentions,
	)
	return nil
}

func (f *FeishuChannel) handleIncomingMessage(eventID, chatID, messageID, messageType, contentJSON string, mentions []feishuMention) {
	if f.dedupEvent(eventID) {
		slog.Debug("feishu: duplicate event", "event_id", eventID)
		return
	}
	if messageType != "text" {
		slog.Debug("feishu: unsupported message type", "type", messageType)
		_ = f.sender.SendText(context.Background(), chatID, "暂时只支持文本消息哦，请发送文字~")
		return
	}

	text := extractFeishuText(contentJSON, mentions)
	if text == "" {
		return
	}

	ctx := context.Background()
	requestTimeout := f.config.RequestTimeout
	if requestTimeout <= 0 {
		requestTimeout = 3 * time.Minute
	}
	ctx, cancel := context.WithTimeout(ctx, requestTimeout)
	defer cancel()

	// Add a thinking emoji reaction to the user's message.
	emojiType := f.config.ThinkingEmoji
	if emojiType == "" {
		emojiType = "THINKING"
	}
	reactionID, err := f.sender.AddReaction(ctx, messageID, emojiType)
	if err != nil {
		slog.Debug("feishu: add thinking reaction failed", "error", err)
	}

	resp, err := f.gateway.HandleMessage(ctx, chatID, "feishu", text)

	// Remove the thinking reaction after processing.
	if reactionID != "" {
		if err := f.sender.RemoveReaction(ctx, messageID, reactionID); err != nil {
			slog.Debug("feishu: remove thinking reaction failed", "error", err)
		}
	}
	if err != nil {
		slog.Error("feishu: handle message failed", "chat_id", chatID, "error", err)
		_ = f.sender.ReplyText(ctx, messageID, fmt.Sprintf("处理消息时出错: %v", err))
		return
	}

	reply := resp.Message.Content
	if reply == "" {
		return
	}

	for _, seg := range splitFeishuMessage(reply, feishuMaxMsgLen) {
		// Try markdown card first; fall back to plain text on error.
		if err := f.sender.ReplyMarkdown(ctx, messageID, seg); err != nil {
			slog.Debug("feishu: markdown reply failed, falling back to text", "error", err)
			if err := f.sender.ReplyText(ctx, messageID, seg); err != nil {
				slog.Error("feishu: send reply failed", "chat_id", chatID, "error", err)
				return
			}
		}
	}
}

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

func derefString(value *string) string {
	if value == nil {
		return ""
	}
	return *value
}
