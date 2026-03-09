package agent

import "sync"

// SessionQueue ensures requests for the same session are processed serially
// while different sessions can proceed concurrently.
type SessionQueue struct {
	locks sync.Map
}

// NewSessionQueue creates a new SessionQueue.
func NewSessionQueue() *SessionQueue {
	return &SessionQueue{}
}

// Acquire blocks until the lock for the given session is available.
func (q *SessionQueue) Acquire(sessionID string) {
	val, _ := q.locks.LoadOrStore(sessionID, &sync.Mutex{})
	val.(*sync.Mutex).Lock()
}

// Release releases the lock for the given session.
func (q *SessionQueue) Release(sessionID string) {
	val, ok := q.locks.Load(sessionID)
	if ok {
		val.(*sync.Mutex).Unlock()
	}
}
