package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/YumingHuang/claw/internal/audit"
	"github.com/YumingHuang/claw/internal/models"
)

// --- ReadFileTool ---

// ReadFileTool reads files within a sandboxed directory.
type ReadFileTool struct {
	workdir        string
	maxOutputChars int
}

func NewReadFileTool(workdir string, maxOutputChars int) *ReadFileTool {
	return &ReadFileTool{workdir: workdir, maxOutputChars: maxOutputChars}
}

func (t *ReadFileTool) Name() string        { return "read_file" }
func (t *ReadFileTool) Description() string { return "Read the contents of a file" }
func (t *ReadFileTool) Parameters() json.RawMessage {
	return json.RawMessage(`{"type":"object","properties":{"path":{"type":"string","description":"File path relative to workdir"}},"required":["path"]}`)
}

func (t *ReadFileTool) Execute(_ context.Context, params json.RawMessage) (models.ToolResult, error) {
	var p struct {
		Path string `json:"path"`
	}
	if err := json.Unmarshal(params, &p); err != nil {
		return models.ToolResult{Content: fmt.Sprintf("invalid parameters: %v", err), IsError: true}, nil
	}

	absPath, err := safePath(t.workdir, p.Path)
	if err != nil {
		return models.ToolResult{Content: err.Error(), IsError: true}, nil
	}

	data, err := os.ReadFile(absPath)
	if err != nil {
		return models.ToolResult{Content: fmt.Sprintf("read error: %v", err), IsError: true}, nil
	}

	content := string(data)
	if len(content) > t.maxOutputChars {
		content = content[:t.maxOutputChars] + "\n... [truncated]"
	}

	return models.ToolResult{Content: content}, nil
}

// --- WriteFileTool ---

// WriteFileTool writes files within a sandboxed directory.
type WriteFileTool struct {
	workdir string
	auditor *audit.Logger
}

func NewWriteFileTool(workdir string, auditor *audit.Logger) *WriteFileTool {
	return &WriteFileTool{workdir: workdir, auditor: auditor}
}

func (t *WriteFileTool) Name() string        { return "write_file" }
func (t *WriteFileTool) Description() string { return "Write content to a file" }
func (t *WriteFileTool) Parameters() json.RawMessage {
	return json.RawMessage(`{"type":"object","properties":{"path":{"type":"string","description":"File path relative to workdir"},"content":{"type":"string","description":"Content to write"}},"required":["path","content"]}`)
}

func (t *WriteFileTool) Execute(ctx context.Context, params json.RawMessage) (models.ToolResult, error) {
	var p struct {
		Path    string `json:"path"`
		Content string `json:"content"`
	}
	if err := json.Unmarshal(params, &p); err != nil {
		return models.ToolResult{Content: fmt.Sprintf("invalid parameters: %v", err), IsError: true}, nil
	}

	absPath, err := safePath(t.workdir, p.Path)
	if err != nil {
		return models.ToolResult{Content: err.Error(), IsError: true}, nil
	}

	if err := os.MkdirAll(filepath.Dir(absPath), 0755); err != nil {
		return models.ToolResult{Content: fmt.Sprintf("mkdir error: %v", err), IsError: true}, nil
	}

	if err := os.WriteFile(absPath, []byte(p.Content), 0644); err != nil {
		return models.ToolResult{Content: fmt.Sprintf("write error: %v", err), IsError: true}, nil
	}
	if t.auditor != nil {
		t.auditor.LogFileWritten(ctx, p.Path, p.Content)
	}

	return models.ToolResult{Content: fmt.Sprintf("written %d bytes to %s", len(p.Content), p.Path)}, nil
}

// safePath validates that the requested path stays within the sandbox,
// resolving symlinks to prevent escape via symbolic links.
func safePath(workdir, reqPath string) (string, error) {
	cleaned := filepath.Clean(reqPath)
	absPath := filepath.Join(workdir, cleaned)

	// Check the logical path first (before symlink resolution).
	rel, err := filepath.Rel(workdir, absPath)
	if err != nil || strings.HasPrefix(rel, "..") {
		return "", fmt.Errorf("path outside sandbox: %s", reqPath)
	}

	// Resolve symlinks on the workdir itself.
	realWorkdir, err := filepath.EvalSymlinks(workdir)
	if err != nil {
		return "", fmt.Errorf("resolve workdir: %w", err)
	}

	// Try to resolve the full path. If it doesn't exist, walk up to find
	// the deepest existing ancestor and verify that.
	realPath, err := filepath.EvalSymlinks(absPath)
	if err == nil {
		rel, err = filepath.Rel(realWorkdir, realPath)
		if err != nil || strings.HasPrefix(rel, "..") {
			return "", fmt.Errorf("path outside sandbox: %s", reqPath)
		}
		return absPath, nil
	}

	// Path doesn't exist yet — find the deepest existing ancestor.
	check := absPath
	for check != realWorkdir && check != filepath.Dir(check) {
		check = filepath.Dir(check)
		resolved, err := filepath.EvalSymlinks(check)
		if err != nil {
			continue
		}
		rel, err = filepath.Rel(realWorkdir, resolved)
		if err != nil || strings.HasPrefix(rel, "..") {
			return "", fmt.Errorf("path outside sandbox: %s", reqPath)
		}
		return absPath, nil
	}

	return absPath, nil
}
