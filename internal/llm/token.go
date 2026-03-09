package llm

import (
	"unicode"

	"github.com/mingminliu/claw/internal/models"
)

const perMessageOverhead = 4 // role, separators, etc.

// EstimateTokens returns a rough token count for a text string.
// English: ~4 chars per token. CJK: ~2 tokens per character.
func EstimateTokens(text string) int {
	var tokens int
	for _, r := range text {
		if unicode.Is(unicode.Han, r) ||
			unicode.Is(unicode.Hiragana, r) ||
			unicode.Is(unicode.Katakana, r) ||
			unicode.Is(unicode.Hangul, r) {
			tokens += 2
		} else {
			// Non-CJK characters contribute ~0.25 tokens each (4 chars ≈ 1 token),
			// but we accumulate fractional parts via integer math below.
			tokens++
		}
	}

	// For non-CJK characters we counted 1 per char above;
	// re-walk to split CJK vs ASCII contribution properly.
	var cjkTokens, asciiChars int
	for _, r := range text {
		if unicode.Is(unicode.Han, r) ||
			unicode.Is(unicode.Hiragana, r) ||
			unicode.Is(unicode.Katakana, r) ||
			unicode.Is(unicode.Hangul, r) {
			cjkTokens += 2
		} else {
			asciiChars++
		}
	}

	// ASCII chars: divide by 4, round up
	asciiTokens := (asciiChars + 3) / 4

	return cjkTokens + asciiTokens
}

// EstimateMessagesTokens returns the estimated total token count for a
// slice of messages, including per-message overhead.
func EstimateMessagesTokens(messages []models.Message) int {
	total := 0
	for _, m := range messages {
		total += EstimateTokens(m.Content) + perMessageOverhead
	}
	return total
}

// TruncateMessages removes the oldest non-system messages so the total
// estimated tokens stays within maxTokens. The first system message (if
// present) is always preserved. Messages are removed from the oldest
// non-system position first.
func TruncateMessages(messages []models.Message, maxTokens int) []models.Message {
	if len(messages) == 0 {
		return messages
	}

	if EstimateMessagesTokens(messages) <= maxTokens {
		return messages
	}

	// Separate system message from the rest
	var system *models.Message
	var rest []models.Message

	if messages[0].Role == "system" {
		sys := messages[0]
		system = &sys
		rest = make([]models.Message, len(messages)-1)
		copy(rest, messages[1:])
	} else {
		rest = make([]models.Message, len(messages))
		copy(rest, messages)
	}

	// Remove oldest messages until we fit
	for len(rest) > 0 {
		var result []models.Message
		if system != nil {
			result = append([]models.Message{*system}, rest...)
		} else {
			result = rest
		}

		if EstimateMessagesTokens(result) <= maxTokens {
			return result
		}

		// Remove the oldest message from rest
		rest = rest[1:]
	}

	// Only system message left (or empty)
	if system != nil {
		return []models.Message{*system}
	}
	return nil
}
