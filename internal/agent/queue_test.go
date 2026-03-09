package agent

import (
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func TestSessionQueue_ConcurrentDifferentSessions(t *testing.T) {
	q := NewSessionQueue()
	var wg sync.WaitGroup
	var count atomic.Int32

	for i := 0; i < 10; i++ {
		wg.Add(1)
		go func(id string) {
			defer wg.Done()
			q.Acquire(id)
			count.Add(1)
			time.Sleep(10 * time.Millisecond)
			q.Release(id)
		}(string(rune('a' + i)))
	}

	wg.Wait()
	if count.Load() != 10 {
		t.Errorf("count = %d, want 10", count.Load())
	}
}

func TestSessionQueue_SameSessionSerial(t *testing.T) {
	q := NewSessionQueue()
	var sequence []int
	var mu sync.Mutex
	var wg sync.WaitGroup

	for i := 0; i < 3; i++ {
		wg.Add(1)
		go func(idx int) {
			defer wg.Done()
			q.Acquire("same-session")
			mu.Lock()
			sequence = append(sequence, idx)
			mu.Unlock()
			time.Sleep(20 * time.Millisecond)
			q.Release("same-session")
		}(i)
		time.Sleep(5 * time.Millisecond) // stagger starts
	}

	wg.Wait()
	if len(sequence) != 3 {
		t.Fatalf("sequence len = %d, want 3", len(sequence))
	}
}
