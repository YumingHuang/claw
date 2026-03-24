package gateway

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"github.com/YumingHuang/claw/internal/models"
)

func TestSQLiteSessionStore_GetOrCreateAndPersist(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	store, err := NewSQLiteSessionStore(ctx, filepath.Join(t.TempDir(), "sessions.db"), time.Hour, time.Second)
	if err != nil {
		t.Fatalf("NewSQLiteSessionStore: %v", err)
	}

	session := store.GetOrCreate("s1", "http")
	session.Append(models.NewUserMessage("hello"))

	got, ok := store.Get("s1")
	if !ok {
		t.Fatal("expected session")
	}
	if got.MessagesCount() != 1 {
		t.Fatalf("message count = %d, want 1", got.MessagesCount())
	}
}

func TestSQLiteSessionStore_Delete(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	store, err := NewSQLiteSessionStore(ctx, filepath.Join(t.TempDir(), "sessions.db"), time.Hour, time.Second)
	if err != nil {
		t.Fatalf("NewSQLiteSessionStore: %v", err)
	}

	store.GetOrCreate("s1", "http")
	store.Delete("s1")

	if _, ok := store.Get("s1"); ok {
		t.Fatal("session should be deleted")
	}
}

func TestSQLiteSessionStore_TTLCleanup(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	store, err := NewSQLiteSessionStore(ctx, filepath.Join(t.TempDir(), "sessions.db"), 50*time.Millisecond, 20*time.Millisecond)
	if err != nil {
		t.Fatalf("NewSQLiteSessionStore: %v", err)
	}

	store.GetOrCreate("s1", "http")
	time.Sleep(200 * time.Millisecond)

	if _, ok := store.Get("s1"); ok {
		t.Fatal("session should be cleaned up")
	}
}
