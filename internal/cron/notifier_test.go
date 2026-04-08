package cron

import (
	"context"
	"fmt"
	"testing"
)

type mockSender struct {
	lastChatID  string
	lastContent string
	failMD      bool
}

func (s *mockSender) SendMarkdown(_ context.Context, chatID, markdown string) error {
	if s.failMD {
		return fmt.Errorf("markdown failed")
	}
	s.lastChatID = chatID
	s.lastContent = markdown
	return nil
}

func (s *mockSender) SendText(_ context.Context, chatID, text string) error {
	s.lastChatID = chatID
	s.lastContent = text
	return nil
}

func TestMultiNotifier_Notify(t *testing.T) {
	n := NewMultiNotifier()
	s := &mockSender{}
	n.Register("feishu", s)

	err := n.Notify(context.Background(), "feishu:oc_123", "hello")
	if err != nil {
		t.Fatal(err)
	}
	if s.lastChatID != "oc_123" {
		t.Errorf("expected oc_123, got %q", s.lastChatID)
	}
	if s.lastContent != "hello" {
		t.Errorf("expected hello, got %q", s.lastContent)
	}
}

func TestMultiNotifier_FallbackToText(t *testing.T) {
	n := NewMultiNotifier()
	s := &mockSender{failMD: true}
	n.Register("feishu", s)

	err := n.Notify(context.Background(), "feishu:oc_456", "content")
	if err != nil {
		t.Fatal(err)
	}
	if s.lastContent != "content" {
		t.Errorf("expected fallback to text, got %q", s.lastContent)
	}
}

func TestMultiNotifier_InvalidTarget(t *testing.T) {
	n := NewMultiNotifier()
	err := n.Notify(context.Background(), "invalid", "msg")
	if err == nil {
		t.Fatal("expected error for invalid target")
	}
}

func TestMultiNotifier_UnknownChannel(t *testing.T) {
	n := NewMultiNotifier()
	err := n.Notify(context.Background(), "slack:chan", "msg")
	if err == nil {
		t.Fatal("expected error for unknown channel")
	}
}
