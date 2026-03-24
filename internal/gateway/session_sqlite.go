package gateway

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"time"

	_ "modernc.org/sqlite"

	"github.com/YumingHuang/claw/internal/agent"
	"github.com/YumingHuang/claw/internal/models"
)

type SQLiteSessionStore struct {
	db  *sql.DB
	ttl time.Duration
}

func NewSQLiteSessionStore(ctx context.Context, dbPath string, ttl time.Duration, cleanupInterval time.Duration) (*SQLiteSessionStore, error) {
	if err := os.MkdirAll(filepath.Dir(dbPath), 0755); err != nil {
		return nil, fmt.Errorf("create sqlite dir: %w", err)
	}
	db, err := sql.Open("sqlite", dbPath)
	if err != nil {
		return nil, fmt.Errorf("open sqlite: %w", err)
	}
	store := &SQLiteSessionStore{db: db, ttl: ttl}
	if err := store.initSchema(); err != nil {
		_ = db.Close()
		return nil, err
	}
	go store.cleanupLoop(ctx, cleanupInterval)
	go func() {
		<-ctx.Done()
		_ = db.Close()
	}()
	return store, nil
}

func (s *SQLiteSessionStore) initSchema() error {
	_, err := s.db.Exec(`
		CREATE TABLE IF NOT EXISTS sessions (
			id TEXT PRIMARY KEY,
			channel TEXT NOT NULL,
			messages TEXT NOT NULL,
			created_at DATETIME NOT NULL,
			updated_at DATETIME NOT NULL
		)
	`)
	if err != nil {
		return fmt.Errorf("create sessions table: %w", err)
	}
	return nil
}

func (s *SQLiteSessionStore) Get(id string) (*agent.Session, bool) {
	row := s.db.QueryRow(`SELECT channel, messages, created_at, updated_at FROM sessions WHERE id = ?`, id)
	var (
		channel   string
		raw       string
		createdAt time.Time
		updatedAt time.Time
	)
	if err := row.Scan(&channel, &raw, &createdAt, &updatedAt); err != nil {
		if err == sql.ErrNoRows {
			return nil, false
		}
		slog.Error("sqlite session get", "id", id, "error", err)
		return nil, false
	}
	session, err := hydrateSession(id, channel, raw, createdAt, updatedAt, s)
	if err != nil {
		slog.Error("sqlite session hydrate", "id", id, "error", err)
		return nil, false
	}
	return session, true
}

func (s *SQLiteSessionStore) GetOrCreate(id string, channel string) *agent.Session {
	if session, ok := s.Get(id); ok {
		return session
	}

	session := agent.NewSession(id, channel)
	session.SetOnUpdate(func(sess *agent.Session) {
		if err := s.saveSession(sess); err != nil {
			slog.Error("sqlite session save", "id", sess.ID, "error", err)
		}
	})
	if err := s.saveSession(session); err != nil {
		slog.Error("sqlite session create", "id", id, "error", err)
	}
	return session
}

func (s *SQLiteSessionStore) Delete(id string) {
	if _, err := s.db.Exec(`DELETE FROM sessions WHERE id = ?`, id); err != nil {
		slog.Error("sqlite session delete", "id", id, "error", err)
	}
}

func (s *SQLiteSessionStore) List() []*agent.Session {
	rows, err := s.db.Query(`SELECT id, channel, messages, created_at, updated_at FROM sessions`)
	if err != nil {
		slog.Error("sqlite session list", "error", err)
		return nil
	}
	defer rows.Close()

	var out []*agent.Session
	for rows.Next() {
		var (
			id        string
			channel   string
			raw       string
			createdAt time.Time
			updatedAt time.Time
		)
		if err := rows.Scan(&id, &channel, &raw, &createdAt, &updatedAt); err != nil {
			slog.Error("sqlite session scan", "error", err)
			continue
		}
		session, err := hydrateSession(id, channel, raw, createdAt, updatedAt, s)
		if err != nil {
			slog.Error("sqlite session hydrate", "id", id, "error", err)
			continue
		}
		out = append(out, session)
	}
	return out
}

func (s *SQLiteSessionStore) Count() int {
	row := s.db.QueryRow(`SELECT COUNT(*) FROM sessions`)
	var count int
	if err := row.Scan(&count); err != nil {
		slog.Error("sqlite session count", "error", err)
		return 0
	}
	return count
}

func (s *SQLiteSessionStore) saveSession(session *agent.Session) error {
	payload, err := json.Marshal(session.Messages())
	if err != nil {
		return fmt.Errorf("marshal messages: %w", err)
	}
	_, err = s.db.Exec(`
		INSERT INTO sessions (id, channel, messages, created_at, updated_at)
		VALUES (?, ?, ?, ?, ?)
		ON CONFLICT(id) DO UPDATE SET
			channel = excluded.channel,
			messages = excluded.messages,
			created_at = excluded.created_at,
			updated_at = excluded.updated_at
	`, session.ID, session.Channel, string(payload), session.CreatedAt, session.UpdatedAt)
	if err != nil {
		return fmt.Errorf("upsert session: %w", err)
	}
	return nil
}

func (s *SQLiteSessionStore) cleanupLoop(ctx context.Context, interval time.Duration) {
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			if _, err := s.db.Exec(`DELETE FROM sessions WHERE updated_at < ?`, time.Now().Add(-s.ttl)); err != nil {
				slog.Error("sqlite session cleanup", "error", err)
			}
		}
	}
}

func hydrateSession(id, channel, raw string, createdAt, updatedAt time.Time, store *SQLiteSessionStore) (*agent.Session, error) {
	session := agent.NewSession(id, channel)
	var messages []models.Message
	if err := json.Unmarshal([]byte(raw), &messages); err != nil {
		return nil, fmt.Errorf("unmarshal messages: %w", err)
	}
	for _, msg := range messages {
		session.Append(msg)
	}
	session.CreatedAt = createdAt
	session.UpdatedAt = updatedAt
	session.SetOnUpdate(func(sess *agent.Session) {
		if err := store.saveSession(sess); err != nil {
			slog.Error("sqlite session save", "id", sess.ID, "error", err)
		}
	})
	return session, nil
}
