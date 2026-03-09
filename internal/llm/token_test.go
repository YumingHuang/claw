package llm

import (
	"testing"

	"github.com/YumingHuang/claw/internal/models"
)

func TestEstimateTokens_English(t *testing.T) {
	tests := []struct {
		name string
		text string
		want int
	}{
		{"empty", "", 0},
		{"short", "hello", 2},              // 5 chars / 4 ≈ 1, but at least round up → 2
		{"sentence", "The quick brown fox", 5}, // 19 chars / 4 ≈ 5
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := EstimateTokens(tt.text)
			if got != tt.want {
				t.Errorf("EstimateTokens(%q) = %d, want %d", tt.text, got, tt.want)
			}
		})
	}
}

func TestEstimateTokens_Chinese(t *testing.T) {
	tests := []struct {
		name string
		text string
		want int
	}{
		{"single char", "你", 2},
		{"short", "你好", 4},         // 2 chars × 2 = 4
		{"sentence", "你好世界", 8}, // 4 chars × 2 = 8
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := EstimateTokens(tt.text)
			if got != tt.want {
				t.Errorf("EstimateTokens(%q) = %d, want %d", tt.text, got, tt.want)
			}
		})
	}
}

func TestEstimateTokens_Mixed(t *testing.T) {
	// "Hello你好" = 5 English chars + 2 Chinese chars
	// English: 5/4 ≈ 1 (rounded), Chinese: 2×2 = 4 → total ≈ 5
	text := "Hello你好"
	got := EstimateTokens(text)
	if got < 3 || got > 10 {
		t.Errorf("EstimateTokens(%q) = %d, expected between 3 and 10", text, got)
	}
}

func TestEstimateMessagesTokens(t *testing.T) {
	msgs := []models.Message{
		models.NewSystemMessage("You are helpful."),
		models.NewUserMessage("Hi"),
		models.NewAssistantMessage("Hello!"),
	}
	got := EstimateMessagesTokens(msgs)
	if got <= 0 {
		t.Errorf("EstimateMessagesTokens = %d, want > 0", got)
	}

	// Each message has per-message overhead, so total should be more than
	// just summing content tokens
	contentOnly := EstimateTokens("You are helpful.") + EstimateTokens("Hi") + EstimateTokens("Hello!")
	if got <= contentOnly {
		t.Errorf("EstimateMessagesTokens = %d, should be > content-only %d (due to overhead)", got, contentOnly)
	}
}

func TestTruncateMessages_NoTruncation(t *testing.T) {
	msgs := []models.Message{
		models.NewSystemMessage("system"),
		models.NewUserMessage("hi"),
		models.NewAssistantMessage("hello"),
	}

	result := TruncateMessages(msgs, 100000)
	if len(result) != 3 {
		t.Errorf("len = %d, want 3 (no truncation needed)", len(result))
	}
}

func TestTruncateMessages_KeepSystemPrompt(t *testing.T) {
	msgs := []models.Message{
		models.NewSystemMessage("system prompt"),
		models.NewUserMessage("first message"),
		models.NewAssistantMessage("first reply"),
		models.NewUserMessage("second message"),
		models.NewAssistantMessage("second reply"),
		models.NewUserMessage("third message"),
		models.NewAssistantMessage("third reply"),
	}

	// Use a very small limit to force truncation
	result := TruncateMessages(msgs, 20)

	if len(result) == 0 {
		t.Fatal("result should not be empty")
	}

	// System message must always be preserved
	if result[0].Role != "system" {
		t.Errorf("first message role = %q, want %q", result[0].Role, "system")
	}
	if result[0].Content != "system prompt" {
		t.Errorf("system prompt content changed")
	}

	// Should have fewer messages than original
	if len(result) >= len(msgs) {
		t.Errorf("len = %d, expected < %d after truncation", len(result), len(msgs))
	}
}

func TestTruncateMessages_RemoveOldest(t *testing.T) {
	msgs := []models.Message{
		models.NewSystemMessage("sys"),
		models.NewUserMessage("old question"),
		models.NewAssistantMessage("old answer"),
		models.NewUserMessage("new question"),
		models.NewAssistantMessage("new answer"),
	}

	// Set limit so only system + one pair fits
	result := TruncateMessages(msgs, 30)

	if len(result) < 2 {
		t.Fatalf("len = %d, need at least system + something", len(result))
	}

	// System must be first
	if result[0].Role != "system" {
		t.Fatalf("first role = %q, want system", result[0].Role)
	}

	// The newest messages should be preserved, oldest removed
	last := result[len(result)-1]
	if last.Content != "new answer" {
		t.Errorf("last message = %q, want %q (newest should be kept)", last.Content, "new answer")
	}
}

func TestTruncateMessages_NoSystemMessage(t *testing.T) {
	msgs := []models.Message{
		models.NewUserMessage("first"),
		models.NewAssistantMessage("reply1"),
		models.NewUserMessage("second"),
		models.NewAssistantMessage("reply2"),
	}

	result := TruncateMessages(msgs, 20)

	// Should still work without system message
	if len(result) == 0 {
		t.Fatal("result should not be empty")
	}

	// Newest messages should be kept
	last := result[len(result)-1]
	if last.Content != "reply2" {
		t.Errorf("last message = %q, want %q", last.Content, "reply2")
	}
}

func TestTruncateMessages_OnlySystem(t *testing.T) {
	msgs := []models.Message{
		models.NewSystemMessage("sys"),
	}

	result := TruncateMessages(msgs, 5)
	if len(result) != 1 {
		t.Errorf("len = %d, want 1", len(result))
	}
	if result[0].Role != "system" {
		t.Errorf("role = %q, want system", result[0].Role)
	}
}
