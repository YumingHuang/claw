package agent

import (
	"sync"
	"time"

	"github.com/YumingHuang/claw/internal/models"
)

// Session holds the conversation state for a single user session.
type Session struct {
	ID        string
	Channel   string
	messages  []models.Message
	CreatedAt time.Time
	UpdatedAt time.Time
	mu        sync.RWMutex
	onUpdate  func(*Session)
}

// NewSession creates a new session with the given ID and channel.
func NewSession(id, channel string) *Session {
	now := time.Now()
	return &Session{
		ID:        id,
		Channel:   channel,
		CreatedAt: now,
		UpdatedAt: now,
	}
}

// Lock acquires the session mutex.
func (s *Session) Lock() { s.mu.Lock() }

// Unlock releases the session mutex.
func (s *Session) Unlock() { s.mu.Unlock() }

// Append adds a message and updates the timestamp.
func (s *Session) Append(msg models.Message) {
	s.mu.Lock()
	s.messages = append(s.messages, msg)
	s.UpdatedAt = time.Now()
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

func (s *Session) SetOnUpdate(fn func(*Session)) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.onUpdate = fn
}
