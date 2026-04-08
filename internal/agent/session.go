package agent

import (
	"sync"
	"time"

	"github.com/YumingHuang/claw/internal/models"
)

// Session holds the conversation state for a single user session.
type Session struct {
	ID      string
	Channel string

	messages  []models.Message
	createdAt time.Time
	updatedAt time.Time
	mu        sync.RWMutex
	onUpdate  func(*Session)
}

// NewSession creates a new session with the given ID and channel.
func NewSession(id, channel string) *Session {
	now := time.Now()
	return &Session{
		ID:        id,
		Channel:   channel,
		createdAt: now,
		updatedAt: now,
	}
}

// Append adds a message and updates the timestamp.
func (s *Session) Append(msg models.Message) {
	s.mu.Lock()
	s.messages = append(s.messages, msg)
	s.updatedAt = time.Now()
	callback := s.onUpdate
	s.mu.Unlock()
	if callback != nil {
		callback(s)
	}
}

// Messages returns a defensive copy of the messages slice.
func (s *Session) Messages() []models.Message {
	s.mu.RLock()
	defer s.mu.RUnlock()
	result := make([]models.Message, len(s.messages))
	copy(result, s.messages)
	return result
}

// MessagesCount returns the number of messages in the session.
func (s *Session) MessagesCount() int {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return len(s.messages)
}

// Rollback truncates the message history back to the given count,
// discarding messages appended after that point.
func (s *Session) Rollback(count int) {
	s.mu.Lock()
	if count < len(s.messages) {
		s.messages = s.messages[:count]
	}
	callback := s.onUpdate
	s.mu.Unlock()
	if callback != nil {
		callback(s)
	}
}

// CreatedAt returns the session creation time.
func (s *Session) CreatedAt() time.Time {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.createdAt
}

// UpdatedAt returns the last update time.
func (s *Session) UpdatedAt() time.Time {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.updatedAt
}

// SetTimestamps sets both created and updated timestamps (used for hydration from DB).
func (s *Session) SetTimestamps(createdAt, updatedAt time.Time) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.createdAt = createdAt
	s.updatedAt = updatedAt
}

// SetOnUpdate registers a callback invoked after each Append or Rollback.
func (s *Session) SetOnUpdate(fn func(*Session)) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.onUpdate = fn
}
