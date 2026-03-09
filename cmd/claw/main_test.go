package main

import (
	"log/slog"
	"testing"

	"github.com/YumingHuang/claw/internal/config"
)

func TestSetupLogger(t *testing.T) {
	tests := []struct {
		name   string
		cfg    config.LogConfig
		wantOk bool
	}{
		{
			name:   "json format info level",
			cfg:    config.LogConfig{Level: "info", Format: "json", Output: "stdout"},
			wantOk: true,
		},
		{
			name:   "text format debug level",
			cfg:    config.LogConfig{Level: "debug", Format: "text", Output: "stdout"},
			wantOk: true,
		},
		{
			name:   "warn level",
			cfg:    config.LogConfig{Level: "warn", Format: "json", Output: "stdout"},
			wantOk: true,
		},
		{
			name:   "error level",
			cfg:    config.LogConfig{Level: "error", Format: "text", Output: "stderr"},
			wantOk: true,
		},
		{
			name:   "invalid level defaults to info",
			cfg:    config.LogConfig{Level: "unknown", Format: "json", Output: "stdout"},
			wantOk: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			logger := setupLogger(tt.cfg)
			if logger == nil {
				t.Fatal("setupLogger returned nil")
			}
			if !tt.wantOk {
				t.Error("expected failure")
			}
		})
	}
}

func TestParseLogLevel(t *testing.T) {
	tests := []struct {
		input string
		want  slog.Level
	}{
		{"debug", slog.LevelDebug},
		{"info", slog.LevelInfo},
		{"warn", slog.LevelWarn},
		{"error", slog.LevelError},
		{"DEBUG", slog.LevelDebug},
		{"INFO", slog.LevelInfo},
		{"unknown", slog.LevelInfo},
		{"", slog.LevelInfo},
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			got := parseLogLevel(tt.input)
			if got != tt.want {
				t.Errorf("parseLogLevel(%q) = %v, want %v", tt.input, got, tt.want)
			}
		})
	}
}
