package audit

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/YumingHuang/claw/internal/models"
	"github.com/YumingHuang/claw/internal/requestctx"
)

func TestNewLogger_Disabled(t *testing.T) {
	l, err := NewLogger("", 256)
	if err != nil {
		t.Fatal(err)
	}
	if l.Enabled() {
		t.Fatal("expected disabled logger")
	}
}

func TestNewLogger_Enabled(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "audit.log")
	l, err := NewLogger(path, 100)
	if err != nil {
		t.Fatal(err)
	}
	if !l.Enabled() {
		t.Fatal("expected enabled logger")
	}
}

func TestNewLogger_InvalidPath(t *testing.T) {
	_, err := NewLogger("/dev/null/impossible/path/audit.log", 256)
	if err == nil {
		t.Fatal("expected error for invalid path")
	}
}

func TestNilLogger(t *testing.T) {
	var l *Logger
	if l.Enabled() {
		t.Fatal("nil logger should not be enabled")
	}
	// These should not panic on nil receiver.
	l.LogAuthFailed(context.Background(), "key")
	l.LogToolExecuted(context.Background(), "tool", models.ToolResult{})
	l.LogFileWritten(context.Background(), "path", "content")
	l.LogCommandRun(context.Background(), "cmd", nil, models.ToolResult{})
}

func TestLogMethods(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "audit.log")
	l, err := NewLogger(path, 50)
	if err != nil {
		t.Fatal(err)
	}

	ctx := context.WithValue(context.Background(), requestctx.RequestIDKey, "req-1")
	ctx = context.WithValue(ctx, requestctx.SessionIDKey, "sess-1")

	l.LogAuthFailed(ctx, "sk-test-key-12345678")
	l.LogToolExecuted(ctx, "read_file", models.ToolResult{Content: "file content", IsError: false})
	l.LogFileWritten(ctx, "test.txt", "hello world")
	l.LogCommandRun(ctx, "echo", []string{"hello"}, models.ToolResult{Content: "hello\n"})

	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	content := string(data)
	if !strings.Contains(content, "auth_failed") {
		t.Error("expected auth_failed event")
	}
	if !strings.Contains(content, "tool_executed") {
		t.Error("expected tool_executed event")
	}
	if !strings.Contains(content, "file_written") {
		t.Error("expected file_written event")
	}
	if !strings.Contains(content, "command_run") {
		t.Error("expected command_run event")
	}
}

func TestTruncate(t *testing.T) {
	l := &Logger{maxValueChars: 10, enabled: true}
	short := l.truncate("hello")
	if short != "hello" {
		t.Errorf("expected 'hello', got %q", short)
	}
	long := l.truncate("this is a very long string")
	if !strings.HasSuffix(long, "...[truncated]") {
		t.Errorf("expected truncated suffix, got %q", long)
	}
	if len(long) > 10+len("...[truncated]") {
		t.Errorf("truncated string too long: %q", long)
	}
}

func TestRedactSecret(t *testing.T) {
	l := &Logger{enabled: true}
	if l.redactSecret("") != "" {
		t.Error("empty should stay empty")
	}
	if l.redactSecret("ab") != "****" {
		t.Error("short secret should be fully redacted")
	}
	r := l.redactSecret("sk-test-key-12345")
	if !strings.HasPrefix(r, "sk-t") || !strings.HasSuffix(r, "****") {
		t.Errorf("unexpected redaction: %q", r)
	}
}
