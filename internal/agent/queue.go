package agent

import "sync"

// SessionQueue ensures requests for the same session are processed serially
// while different sessions can proceed concurrently.
type SessionQueue struct {
	mu    sync.Mutex
	locks map[string]*queueEntry
}

type queueEntry struct {
	mu       sync.Mutex
	refCount int
}

// NewSessionQueue creates a new SessionQueue.
func NewSessionQueue() *SessionQueue {
	return &SessionQueue{locks: make(map[string]*queueEntry)}
}

// Acquire blocks until the lock for the given session is available.
func (q *SessionQueue) Acquire(sessionID string) {
	q.mu.Lock()
	e, ok := q.locks[sessionID]
	if !ok {
		e = &queueEntry{}
		q.locks[sessionID] = e
	}
	e.refCount++
	q.mu.Unlock()
	e.mu.Lock()
}

// Release releases the lock for the given session and cleans up if no waiters remain.
func (q *SessionQueue) Release(sessionID string) {
	q.mu.Lock()
	e, ok := q.locks[sessionID]
	if !ok {
		q.mu.Unlock()
		return
	}
	e.refCount--
	if e.refCount == 0 {
		delete(q.locks, sessionID)
	}
	q.mu.Unlock()
	e.mu.Unlock()
}
