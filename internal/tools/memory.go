package tools

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/YumingHuang/claw/internal/models"
	"github.com/YumingHuang/claw/internal/requestctx"
	_ "modernc.org/sqlite"
)

// MemoryStore defines the backing store used by memory tools.
type MemoryStore interface {
	Get(sessionID, key string) (string, bool, error)
	Set(sessionID, key, value string) error
	List(sessionID string) (map[string]string, error)
}

// InMemoryStore keeps per-session key/value memory in process memory.
type InMemoryStore struct {
	mu   sync.RWMutex
	data map[string]map[string]string
}

func NewMemoryStore() *InMemoryStore {
	return &InMemoryStore{data: make(map[string]map[string]string)}
}

func (s *InMemoryStore) Get(sessionID, key string) (string, bool, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	sessionData, ok := s.data[sessionID]
	if !ok {
		return "", false, nil
	}
	value, ok := sessionData[key]
	return value, ok, nil
}

func (s *InMemoryStore) Set(sessionID, key, value string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, ok := s.data[sessionID]; !ok {
		s.data[sessionID] = make(map[string]string)
	}
	s.data[sessionID][key] = value
	return nil
}

func (s *InMemoryStore) List(sessionID string) (map[string]string, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	sessionData := s.data[sessionID]
	result := make(map[string]string, len(sessionData))
	for key, value := range sessionData {
		result[key] = value
	}
	return result, nil
}

// SQLiteMemoryStore persists per-session key/value memory in SQLite.
type SQLiteMemoryStore struct {
	db *sql.DB
}

func NewSQLiteMemoryStore(ctx context.Context, dbPath string) (*SQLiteMemoryStore, error) {
	if err := os.MkdirAll(filepath.Dir(dbPath), 0755); err != nil {
		return nil, fmt.Errorf("create sqlite dir: %w", err)
	}
	db, err := sql.Open("sqlite", dbPath)
	if err != nil {
		return nil, fmt.Errorf("open sqlite: %w", err)
	}
	store := &SQLiteMemoryStore{db: db}
	if err := store.initSchema(); err != nil {
		_ = db.Close()
		return nil, err
	}
	go func() {
		<-ctx.Done()
		_ = db.Close()
	}()
	return store, nil
}

func (s *SQLiteMemoryStore) initSchema() error {
	_, err := s.db.Exec(`
		CREATE TABLE IF NOT EXISTS memories (
			session_id TEXT NOT NULL,
			key TEXT NOT NULL,
			value TEXT NOT NULL,
			updated_at DATETIME NOT NULL,
			PRIMARY KEY (session_id, key)
		)
	`)
	if err != nil {
		return fmt.Errorf("create memories table: %w", err)
	}
	return nil
}

func (s *SQLiteMemoryStore) Get(sessionID, key string) (string, bool, error) {
	row := s.db.QueryRow(`SELECT value FROM memories WHERE session_id = ? AND key = ?`, sessionID, key)
	var value string
	if err := row.Scan(&value); err != nil {
		if err == sql.ErrNoRows {
			return "", false, nil
		}
		return "", false, fmt.Errorf("query memory: %w", err)
	}
	return value, true, nil
}

func (s *SQLiteMemoryStore) Set(sessionID, key, value string) error {
	_, err := s.db.Exec(`
		INSERT INTO memories (session_id, key, value, updated_at)
		VALUES (?, ?, ?, ?)
		ON CONFLICT(session_id, key) DO UPDATE SET
			value = excluded.value,
			updated_at = excluded.updated_at
	`, sessionID, key, value, time.Now())
	if err != nil {
		return fmt.Errorf("upsert memory: %w", err)
	}
	return nil
}

func (s *SQLiteMemoryStore) List(sessionID string) (map[string]string, error) {
	rows, err := s.db.Query(`SELECT key, value FROM memories WHERE session_id = ? ORDER BY key`, sessionID)
	if err != nil {
		return nil, fmt.Errorf("list memories: %w", err)
	}
	defer rows.Close()

	result := make(map[string]string)
	for rows.Next() {
		var key string
		var value string
		if err := rows.Scan(&key, &value); err != nil {
			return nil, fmt.Errorf("scan memory: %w", err)
		}
		result[key] = value
	}
	return result, nil
}

type MemoryGetTool struct {
	store MemoryStore
}

type MemorySetTool struct {
	store MemoryStore
}

type MemoryListTool struct {
	store MemoryStore
}

func NewMemoryGetTool(store MemoryStore) *MemoryGetTool   { return &MemoryGetTool{store: store} }
func NewMemorySetTool(store MemoryStore) *MemorySetTool   { return &MemorySetTool{store: store} }
func NewMemoryListTool(store MemoryStore) *MemoryListTool { return &MemoryListTool{store: store} }

func (t *MemoryGetTool) Name() string { return "memory_get" }
func (t *MemoryGetTool) Description() string {
	return "Read a remembered value for the current session by key"
}
func (t *MemoryGetTool) Parameters() json.RawMessage {
	return json.RawMessage(`{"type":"object","properties":{"key":{"type":"string","description":"Memory key to read"}},"required":["key"]}`)
}

func (t *MemoryGetTool) Execute(ctx context.Context, params json.RawMessage) (models.ToolResult, error) {
	var p struct {
		Key string `json:"key"`
	}
	if err := json.Unmarshal(params, &p); err != nil {
		return models.ToolResult{Content: fmt.Sprintf("invalid parameters: %v", err), IsError: true}, nil
	}
	sessionID, err := requireSessionID(ctx)
	if err != nil {
		return models.ToolResult{Content: err.Error(), IsError: true}, nil
	}
	if strings.TrimSpace(p.Key) == "" {
		return models.ToolResult{Content: "key is required", IsError: true}, nil
	}

	value, ok, err := t.store.Get(sessionID, p.Key)
	if err != nil {
		return models.ToolResult{Content: fmt.Sprintf("memory lookup failed: %v", err), IsError: true}, nil
	}
	if !ok {
		return models.ToolResult{Content: fmt.Sprintf("memory key not found: %s", p.Key), IsError: true}, nil
	}
	return models.ToolResult{Content: value}, nil
}

func (t *MemorySetTool) Name() string { return "memory_set" }
func (t *MemorySetTool) Description() string {
	return "Store a remembered value for the current session by key"
}
func (t *MemorySetTool) Parameters() json.RawMessage {
	return json.RawMessage(`{"type":"object","properties":{"key":{"type":"string","description":"Memory key to write"},"value":{"type":"string","description":"Value to store"}},"required":["key","value"]}`)
}

func (t *MemorySetTool) Execute(ctx context.Context, params json.RawMessage) (models.ToolResult, error) {
	var p struct {
		Key   string `json:"key"`
		Value string `json:"value"`
	}
	if err := json.Unmarshal(params, &p); err != nil {
		return models.ToolResult{Content: fmt.Sprintf("invalid parameters: %v", err), IsError: true}, nil
	}
	sessionID, err := requireSessionID(ctx)
	if err != nil {
		return models.ToolResult{Content: err.Error(), IsError: true}, nil
	}
	if strings.TrimSpace(p.Key) == "" {
		return models.ToolResult{Content: "key is required", IsError: true}, nil
	}

	if err := t.store.Set(sessionID, p.Key, p.Value); err != nil {
		return models.ToolResult{Content: fmt.Sprintf("memory store failed: %v", err), IsError: true}, nil
	}
	return models.ToolResult{Content: fmt.Sprintf("stored memory key %q", p.Key)}, nil
}

func (t *MemoryListTool) Name() string { return "memory_list" }
func (t *MemoryListTool) Description() string {
	return "List all remembered key/value pairs for the current session"
}
func (t *MemoryListTool) Parameters() json.RawMessage {
	return json.RawMessage(`{"type":"object","properties":{},"required":[]}`)
}

func (t *MemoryListTool) Execute(ctx context.Context, _ json.RawMessage) (models.ToolResult, error) {
	sessionID, err := requireSessionID(ctx)
	if err != nil {
		return models.ToolResult{Content: err.Error(), IsError: true}, nil
	}

	items, err := t.store.List(sessionID)
	if err != nil {
		return models.ToolResult{Content: fmt.Sprintf("memory list failed: %v", err), IsError: true}, nil
	}
	if len(items) == 0 {
		return models.ToolResult{Content: "no memory entries found"}, nil
	}

	keys := make([]string, 0, len(items))
	for key := range items {
		keys = append(keys, key)
	}
	sort.Strings(keys)

	var b strings.Builder
	for i, key := range keys {
		if i > 0 {
			b.WriteString("\n")
		}
		fmt.Fprintf(&b, "%s=%s", key, items[key])
	}

	return models.ToolResult{Content: b.String()}, nil
}

func requireSessionID(ctx context.Context) (string, error) {
	sessionID := requestctx.SessionIDFromContext(ctx)
	if strings.TrimSpace(sessionID) == "" {
		return "", fmt.Errorf("session_id is required in context")
	}
	return sessionID, nil
}
