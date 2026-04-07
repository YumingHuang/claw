package gateway

import (
	"context"
	"sync"
	"time"

	"github.com/YumingHuang/claw/internal/agent"
)

// SessionStore abstracts session persistence.
type SessionStore interface {
	Get(id string) (*agent.Session, bool)
	GetOrCreate(id string, channel string) *agent.Session
	Delete(id string)
	List() []*agent.Session
	Count() int
}

// MemorySessionStore keeps sessions in memory with TTL-based expiration.
type MemorySessionStore struct {
	sessions sync.Map
	ttl      time.Duration
}

// NewMemorySessionStore creates a store that cleans up expired sessions
// on the given interval. The cleanup goroutine stops when ctx is cancelled.
func NewMemorySessionStore(ctx context.Context, ttl time.Duration, _ int, cleanupInterval time.Duration) *MemorySessionStore {
	s := &MemorySessionStore{ttl: ttl}
	go s.cleanupLoop(ctx, cleanupInterval)
	return s
}

func (s *MemorySessionStore) Get(id string) (*agent.Session, bool) {
	val, ok := s.sessions.Load(id)
	if !ok {
		return nil, false
	}
	return val.(*agent.Session), true
}

func (s *MemorySessionStore) GetOrCreate(id string, channel string) *agent.Session {
	session := agent.NewSession(id, channel)
	actual, _ := s.sessions.LoadOrStore(id, session)
	return actual.(*agent.Session)
}

func (s *MemorySessionStore) Delete(id string) {
	s.sessions.Delete(id)
}

func (s *MemorySessionStore) List() []*agent.Session {
	var list []*agent.Session
	s.sessions.Range(func(_, value interface{}) bool {
		list = append(list, value.(*agent.Session))
		return true
	})
	return list
}

func (s *MemorySessionStore) Count() int {
	count := 0
	s.sessions.Range(func(_, _ interface{}) bool {
		count++
		return true
	})
	return count
}

func (s *MemorySessionStore) cleanupLoop(ctx context.Context, interval time.Duration) {
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			now := time.Now()
			s.sessions.Range(func(key, value interface{}) bool {
				session := value.(*agent.Session)
				session.Lock()
				expired := now.Sub(session.UpdatedAt) > s.ttl
				session.Unlock()
				if expired {
					s.sessions.Delete(key)
				}
				return true
			})
		}
	}
}
