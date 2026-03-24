package audit

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"strings"

	"github.com/YumingHuang/claw/internal/models"
	"github.com/YumingHuang/claw/internal/requestctx"
)

type Logger struct {
	logger        *slog.Logger
	enabled       bool
	maxValueChars int
}

func NewLogger(outputPath string, maxValueChars int) (*Logger, error) {
	if strings.TrimSpace(outputPath) == "" {
		return &Logger{}, nil
	}
	if maxValueChars <= 0 {
		maxValueChars = 256
	}
	if err := os.MkdirAll(filepath.Dir(outputPath), 0755); err != nil {
		return nil, fmt.Errorf("create audit dir: %w", err)
	}
	f, err := os.OpenFile(outputPath, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0644)
	if err != nil {
		return nil, fmt.Errorf("open audit log: %w", err)
	}
	return &Logger{
		logger:        slog.New(slog.NewJSONHandler(f, &slog.HandlerOptions{Level: slog.LevelInfo})),
		enabled:       true,
		maxValueChars: maxValueChars,
	}, nil
}

func (l *Logger) Enabled() bool {
	return l != nil && l.enabled && l.logger != nil
}

func (l *Logger) LogAuthFailed(ctx context.Context, credential string) {
	if !l.Enabled() {
		return
	}
	l.logger.Info("audit",
		slog.String("event", "auth_failed"),
		slog.String("request_id", requestctx.RequestIDFromContext(ctx)),
		slog.String("session_id", requestctx.SessionIDFromContext(ctx)),
		slog.String("credential", l.redactSecret(credential)),
	)
}

func (l *Logger) LogToolExecuted(ctx context.Context, toolName string, result models.ToolResult) {
	if !l.Enabled() {
		return
	}
	l.logger.Info("audit",
		slog.String("event", "tool_executed"),
		slog.String("request_id", requestctx.RequestIDFromContext(ctx)),
		slog.String("session_id", requestctx.SessionIDFromContext(ctx)),
		slog.String("tool", toolName),
		slog.Bool("is_error", result.IsError),
		slog.String("content", l.truncate(result.Content)),
	)
}

func (l *Logger) LogFileWritten(ctx context.Context, path string, content string) {
	if !l.Enabled() {
		return
	}
	l.logger.Info("audit",
		slog.String("event", "file_written"),
		slog.String("request_id", requestctx.RequestIDFromContext(ctx)),
		slog.String("session_id", requestctx.SessionIDFromContext(ctx)),
		slog.String("path", path),
		slog.String("content", l.truncate(content)),
	)
}

func (l *Logger) LogCommandRun(ctx context.Context, command string, args []string, result models.ToolResult) {
	if !l.Enabled() {
		return
	}
	l.logger.Info("audit",
		slog.String("event", "command_run"),
		slog.String("request_id", requestctx.RequestIDFromContext(ctx)),
		slog.String("session_id", requestctx.SessionIDFromContext(ctx)),
		slog.String("command", command),
		slog.Any("args", args),
		slog.Bool("is_error", result.IsError),
		slog.String("output", l.truncate(result.Content)),
	)
}

func (l *Logger) truncate(value string) string {
	value = strings.TrimSpace(value)
	if len(value) <= l.maxValueChars {
		return value
	}
	return value[:l.maxValueChars] + "...[truncated]"
}

func (l *Logger) redactSecret(value string) string {
	value = strings.TrimSpace(value)
	if value == "" {
		return ""
	}
	if len(value) <= 4 {
		return "****"
	}
	return value[:4] + "****"
}
