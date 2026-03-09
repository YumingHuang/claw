package tools

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// --- TimeTool ---

func TestTimeTool_Default(t *testing.T) {
	tool := NewTimeTool()
	result, err := tool.Execute(context.Background(), json.RawMessage(`{}`))
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if result.IsError {
		t.Fatalf("unexpected error result: %s", result.Content)
	}
	if _, err := time.Parse(time.RFC3339, result.Content); err != nil {
		t.Errorf("content %q is not valid RFC3339: %v", result.Content, err)
	}
}

func TestTimeTool_WithTimezone(t *testing.T) {
	tool := NewTimeTool()
	result, err := tool.Execute(context.Background(), json.RawMessage(`{"timezone":"Asia/Shanghai"}`))
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if result.IsError {
		t.Fatalf("unexpected error: %s", result.Content)
	}
	if !strings.Contains(result.Content, "+08:00") {
		t.Errorf("expected +08:00 in %q", result.Content)
	}
}

func TestTimeTool_InvalidTimezone(t *testing.T) {
	tool := NewTimeTool()
	result, err := tool.Execute(context.Background(), json.RawMessage(`{"timezone":"Invalid/Zone"}`))
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if !result.IsError {
		t.Error("expected error for invalid timezone")
	}
}

// --- ReadFileTool ---

func TestReadFile_Normal(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "hello.txt"), []byte("hello world"), 0644); err != nil {
		t.Fatal(err)
	}

	tool := NewReadFileTool(dir, 10000)
	result, err := tool.Execute(context.Background(), json.RawMessage(`{"path":"hello.txt"}`))
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if result.Content != "hello world" {
		t.Errorf("Content = %q, want %q", result.Content, "hello world")
	}
}

func TestReadFile_PathTraversal(t *testing.T) {
	dir := t.TempDir()
	tool := NewReadFileTool(dir, 10000)

	result, err := tool.Execute(context.Background(), json.RawMessage(`{"path":"../../etc/passwd"}`))
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if !result.IsError {
		t.Error("expected error for path traversal")
	}
	if !strings.Contains(result.Content, "outside sandbox") {
		t.Errorf("Content = %q, expected 'outside sandbox'", result.Content)
	}
}

func TestReadFile_NotFound(t *testing.T) {
	dir := t.TempDir()
	tool := NewReadFileTool(dir, 10000)

	result, err := tool.Execute(context.Background(), json.RawMessage(`{"path":"nonexistent.txt"}`))
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if !result.IsError {
		t.Error("expected error for nonexistent file")
	}
}

func TestReadFile_Truncation(t *testing.T) {
	dir := t.TempDir()
	content := strings.Repeat("a", 200)
	if err := os.WriteFile(filepath.Join(dir, "big.txt"), []byte(content), 0644); err != nil {
		t.Fatal(err)
	}

	tool := NewReadFileTool(dir, 50)
	result, err := tool.Execute(context.Background(), json.RawMessage(`{"path":"big.txt"}`))
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if len(result.Content) > 80 {
		t.Errorf("Content len = %d, expected truncated to ~50", len(result.Content))
	}
}

// --- WriteFileTool ---

func TestWriteFile_Normal(t *testing.T) {
	dir := t.TempDir()
	tool := NewWriteFileTool(dir)

	result, err := tool.Execute(context.Background(), json.RawMessage(`{"path":"out.txt","content":"hello"}`))
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if result.IsError {
		t.Fatalf("unexpected error: %s", result.Content)
	}

	data, err := os.ReadFile(filepath.Join(dir, "out.txt"))
	if err != nil {
		t.Fatalf("ReadFile: %v", err)
	}
	if string(data) != "hello" {
		t.Errorf("file content = %q, want %q", string(data), "hello")
	}
}

func TestWriteFile_PathTraversal(t *testing.T) {
	dir := t.TempDir()
	tool := NewWriteFileTool(dir)

	result, err := tool.Execute(context.Background(), json.RawMessage(`{"path":"../../evil.txt","content":"hack"}`))
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if !result.IsError {
		t.Error("expected error for path traversal")
	}
}

func TestWriteFile_CreateSubdir(t *testing.T) {
	dir := t.TempDir()
	tool := NewWriteFileTool(dir)

	result, err := tool.Execute(context.Background(), json.RawMessage(`{"path":"sub/dir/file.txt","content":"nested"}`))
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if result.IsError {
		t.Fatalf("unexpected error: %s", result.Content)
	}

	data, err := os.ReadFile(filepath.Join(dir, "sub", "dir", "file.txt"))
	if err != nil {
		t.Fatalf("ReadFile: %v", err)
	}
	if string(data) != "nested" {
		t.Errorf("content = %q, want %q", string(data), "nested")
	}
}

// --- RunCommandTool ---

func TestRunCommand_Allowed(t *testing.T) {
	tool := NewRunCommandTool([]string{"echo"}, 10000, 5*time.Second)
	result, err := tool.Execute(context.Background(), json.RawMessage(`{"command":"echo","args":["hello"]}`))
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if result.IsError {
		t.Fatalf("unexpected error: %s", result.Content)
	}
	if strings.TrimSpace(result.Content) != "hello" {
		t.Errorf("Content = %q, want %q", strings.TrimSpace(result.Content), "hello")
	}
}

func TestRunCommand_Denied(t *testing.T) {
	tool := NewRunCommandTool([]string{"echo"}, 10000, 5*time.Second)
	result, err := tool.Execute(context.Background(), json.RawMessage(`{"command":"rm","args":["-rf","/"]}`))
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if !result.IsError {
		t.Error("expected error for non-whitelisted command")
	}
	if !strings.Contains(result.Content, "not allowed") {
		t.Errorf("Content = %q, expected 'not allowed'", result.Content)
	}
}

func TestRunCommand_Timeout(t *testing.T) {
	tool := NewRunCommandTool([]string{"sleep"}, 10000, 500*time.Millisecond)
	result, err := tool.Execute(context.Background(), json.RawMessage(`{"command":"sleep","args":["10"]}`))
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if !result.IsError {
		t.Error("expected error for timeout")
	}
}

func TestRunCommand_OutputTruncation(t *testing.T) {
	tool := NewRunCommandTool([]string{"echo"}, 5, 5*time.Second)
	result, err := tool.Execute(context.Background(), json.RawMessage(`{"command":"echo","args":["this is a long string"]}`))
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if len(result.Content) > 30 {
		t.Errorf("Content len = %d, expected truncated", len(result.Content))
	}
}
