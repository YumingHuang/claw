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
	Messages  []models.Message
	CreatedAt time.Time
	UpdatedAt time.Time
	mu        sync.Mutex
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
func (s *Session) Lock()   { s.mu.Lock() }

// Unlock releases the session mutex.
func (s *Session) Unlock() { s.mu.Unlock() }

// Append adds a message and updates the timestamp.
func (s *Session) Append(msg models.Message) {
	s.Messages = append(s.Messages, msg)
	s.UpdatedAt = time.Now()
}
