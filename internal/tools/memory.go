package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"sort"
	"strings"
	"sync"

	"github.com/YumingHuang/claw/internal/models"
	"github.com/YumingHuang/claw/internal/requestctx"
)

// MemoryStore keeps per-session key/value memory in process memory.
type MemoryStore struct {
	mu   sync.RWMutex
	data map[string]map[string]string
}

func NewMemoryStore() *MemoryStore {
	return &MemoryStore{data: make(map[string]map[string]string)}
}

func (s *MemoryStore) get(sessionID, key string) (string, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	sessionData, ok := s.data[sessionID]
	if !ok {
		return "", false
	}
	value, ok := sessionData[key]
	return value, ok
}

func (s *MemoryStore) set(sessionID, key, value string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, ok := s.data[sessionID]; !ok {
		s.data[sessionID] = make(map[string]string)
	}
	s.data[sessionID][key] = value
}

func (s *MemoryStore) list(sessionID string) map[string]string {
	s.mu.RLock()
	defer s.mu.RUnlock()
	sessionData := s.data[sessionID]
	result := make(map[string]string, len(sessionData))
	for key, value := range sessionData {
		result[key] = value
	}
	return result
}

type MemoryGetTool struct {
	store *MemoryStore
}

type MemorySetTool struct {
	store *MemoryStore
}

type MemoryListTool struct {
	store *MemoryStore
}

func NewMemoryGetTool(store *MemoryStore) *MemoryGetTool   { return &MemoryGetTool{store: store} }
func NewMemorySetTool(store *MemoryStore) *MemorySetTool   { return &MemorySetTool{store: store} }
func NewMemoryListTool(store *MemoryStore) *MemoryListTool { return &MemoryListTool{store: store} }

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

	value, ok := t.store.get(sessionID, p.Key)
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

	t.store.set(sessionID, p.Key, p.Value)
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

	items := t.store.list(sessionID)
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
