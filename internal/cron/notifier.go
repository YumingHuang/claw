package cron

import (
	"context"
	"fmt"
	"strings"
)

// Sender abstracts sending a message to a chat (e.g. feishu).
type Sender interface {
	SendMarkdown(ctx context.Context, chatID, markdown string) error
	SendText(ctx context.Context, chatID, text string) error
}

// MultiNotifier routes notifications by prefix (e.g. "feishu:<chat_id>").
type MultiNotifier struct {
	senders map[string]Sender // key: channel name, e.g. "feishu"
}

// NewMultiNotifier creates a notifier with named senders.
func NewMultiNotifier() *MultiNotifier {
	return &MultiNotifier{senders: make(map[string]Sender)}
}

// Register adds a sender for a channel name.
func (n *MultiNotifier) Register(name string, s Sender) {
	n.senders[name] = s
}

// Notify parses target as "channel:id" and sends content.
func (n *MultiNotifier) Notify(ctx context.Context, target, content string) error {
	parts := strings.SplitN(target, ":", 2)
	if len(parts) != 2 {
		return fmt.Errorf("invalid notify target %q, expected channel:id", target)
	}
	name, id := parts[0], parts[1]
	s, ok := n.senders[name]
	if !ok {
		return fmt.Errorf("unknown notify channel %q", name)
	}
	if err := s.SendMarkdown(ctx, id, content); err != nil {
		return s.SendText(ctx, id, content)
	}
	return nil
}
