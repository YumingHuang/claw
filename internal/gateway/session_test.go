package gateway

import (
	"context"
	"sync"
	"testing"
	"time"
)

func TestGetOrCreate_New(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	store := NewMemorySessionStore(ctx, 1*time.Hour, 100, 5*time.Minute)

	s := store.GetOrCreate("s1", "http")
	if s.ID != "s1" {
		t.Errorf("ID = %q, want %q", s.ID, "s1")
	}
	if s.Channel != "http" {
		t.Errorf("Channel = %q, want %q", s.Channel, "http")
	}
}

func TestGetOrCreate_Existing(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	store := NewMemorySessionStore(ctx, 1*time.Hour, 100, 5*time.Minute)

	s1 := store.GetOrCreate("s1", "http")
	s2 := store.GetOrCreate("s1", "http")
	if s1 != s2 {
		t.Error("expected same session pointer for same ID")
	}
}

func TestGet_NotFound(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	store := NewMemorySessionStore(ctx, 1*time.Hour, 100, 5*time.Minute)

	_, ok := store.Get("nonexistent")
	if ok {
		t.Error("expected false for nonexistent session")
	}
}

func TestDelete(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	store := NewMemorySessionStore(ctx, 1*time.Hour, 100, 5*time.Minute)

	store.GetOrCreate("s1", "http")
	store.Delete("s1")

	_, ok := store.Get("s1")
	if ok {
		t.Error("expected false after delete")
	}
}

func TestCount(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	store := NewMemorySessionStore(ctx, 1*time.Hour, 100, 5*time.Minute)

	store.GetOrCreate("s1", "http")
	store.GetOrCreate("s2", "http")

	if store.Count() != 2 {
		t.Errorf("Count = %d, want 2", store.Count())
	}
}

func TestTTLCleanup(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	store := NewMemorySessionStore(ctx, 50*time.Millisecond, 100, 30*time.Millisecond)

	store.GetOrCreate("expire-me", "http")

	time.Sleep(150 * time.Millisecond)

	_, ok := store.Get("expire-me")
	if ok {
		t.Error("expected session to be cleaned up after TTL")
	}
}

func TestConcurrentAccess(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	store := NewMemorySessionStore(ctx, 1*time.Hour, 100, 5*time.Minute)

	var wg sync.WaitGroup
	for i := 0; i < 50; i++ {
		wg.Add(1)
		go func(id string) {
			defer wg.Done()
			store.GetOrCreate(id, "http")
		}(string(rune('a' + i%26)))
	}
	wg.Wait()

	if store.Count() == 0 {
		t.Error("expected some sessions after concurrent access")
	}
}
