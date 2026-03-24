package config

import (
	"os"
	"testing"
	"time"
)

// TestLoad_ValidConfig tests loading a complete valid configuration.
func TestLoad_ValidConfig(t *testing.T) {
	configPath := "testdata/valid_config.yaml"
	cfg, err := Load(configPath)

	if err != nil {
		t.Fatalf("Load() failed: %v", err)
	}

	// Verify server config
	if cfg.Server.Host != "127.0.0.1" {
		t.Errorf("expected host '127.0.0.1', got '%s'", cfg.Server.Host)
	}
	if cfg.Server.Port != 9000 {
		t.Errorf("expected port 9000, got %d", cfg.Server.Port)
	}
	if cfg.Server.ReadTimeout != 30*time.Second {
		t.Errorf("expected read_timeout 30s, got %v", cfg.Server.ReadTimeout)
	}

	// Verify log config
	if cfg.Log.Level != "debug" {
		t.Errorf("expected log level 'debug', got '%s'", cfg.Log.Level)
	}
	if cfg.Log.Format != "text" {
		t.Errorf("expected log format 'text', got '%s'", cfg.Log.Format)
	}

	// Verify providers config
	if cfg.Providers.Default != "openai" {
		t.Errorf("expected default provider 'openai', got '%s'", cfg.Providers.Default)
	}
	if len(cfg.Providers.List) != 2 {
		t.Errorf("expected 2 providers, got %d", len(cfg.Providers.List))
	}
	if cfg.Providers.List[0].Name != "openai" {
		t.Errorf("expected first provider name 'openai', got '%s'", cfg.Providers.List[0].Name)
	}

	// Verify tools config
	if cfg.Tools.MaxOutputChars != 5000 {
		t.Errorf("expected max_output_chars 5000, got %d", cfg.Tools.MaxOutputChars)
	}

	// Verify channels config
	if !cfg.Channels.HTTP.Enabled {
		t.Error("expected HTTP channel to be enabled")
	}
	if !cfg.Channels.WebSocket.Enabled {
		t.Error("expected WebSocket channel to be enabled")
	}
}

// TestLoad_Defaults tests that default values are applied.
func TestLoad_Defaults(t *testing.T) {
	configPath := "testdata/minimal_config.yaml"
	cfg, err := Load(configPath)

	if err != nil {
		t.Fatalf("Load() failed: %v", err)
	}

	// Verify server defaults
	if cfg.Server.Host != "0.0.0.0" {
		t.Errorf("expected default host '0.0.0.0', got '%s'", cfg.Server.Host)
	}
	if cfg.Server.Port != 8080 {
		t.Errorf("expected default port 8080, got %d", cfg.Server.Port)
	}
	if cfg.Server.ReadTimeout != 30*time.Second {
		t.Errorf("expected default read_timeout 30s, got %v", cfg.Server.ReadTimeout)
	}

	// Verify log defaults
	if cfg.Log.Level != "info" {
		t.Errorf("expected default log level 'info', got '%s'", cfg.Log.Level)
	}
	if cfg.Log.Format != "json" {
		t.Errorf("expected default log format 'json', got '%s'", cfg.Log.Format)
	}
	if cfg.Log.Output != "stdout" {
		t.Errorf("expected default log output 'stdout', got '%s'", cfg.Log.Output)
	}

	// Verify session defaults
	if cfg.Session.TTL != 1*time.Hour {
		t.Errorf("expected default session ttl 1h, got %v", cfg.Session.TTL)
	}
	if cfg.Session.MaxHistory != 100 {
		t.Errorf("expected default max_history 100, got %d", cfg.Session.MaxHistory)
	}

	// Verify tools defaults
	if cfg.Tools.MaxOutputChars != 10000 {
		t.Errorf("expected default max_output_chars 10000, got %d", cfg.Tools.MaxOutputChars)
	}
	if cfg.Tools.Timeout != 30*time.Second {
		t.Errorf("expected default timeout 30s, got %v", cfg.Tools.Timeout)
	}
	if cfg.Tools.MaxIterations != 10 {
		t.Errorf("expected default max_iterations 10, got %d", cfg.Tools.MaxIterations)
	}

	// Verify HTTP channel is enabled by default
	if !cfg.Channels.HTTP.Enabled {
		t.Error("expected HTTP channel to be enabled by default")
	}

	// Verify retry defaults
	if cfg.Providers.Retry.MaxAttempts != 2 {
		t.Errorf("expected default retry max_attempts 2, got %d", cfg.Providers.Retry.MaxAttempts)
	}
	if cfg.Providers.Retry.Backoff != 1*time.Second {
		t.Errorf("expected default retry backoff 1s, got %v", cfg.Providers.Retry.Backoff)
	}
}

// TestLoad_EnvSubstitution tests environment variable substitution.
func TestLoad_EnvSubstitution(t *testing.T) {
	// Set test environment variables
	testAPIKey := "test-api-key-12345"
	testBaseURL := "https://api.test.com"
	os.Setenv("TEST_API_KEY", testAPIKey)
	os.Setenv("TEST_BASE_URL", testBaseURL)
	defer func() {
		os.Unsetenv("TEST_API_KEY")
		os.Unsetenv("TEST_BASE_URL")
	}()

	configPath := "testdata/env_substitution_config.yaml"
	cfg, err := Load(configPath)

	if err != nil {
		t.Fatalf("Load() failed: %v", err)
	}

	// Verify environment variables were substituted
	if cfg.Providers.List[0].APIKey != testAPIKey {
		t.Errorf("expected api_key '%s', got '%s'", testAPIKey, cfg.Providers.List[0].APIKey)
	}
	if cfg.Providers.List[0].BaseURL != testBaseURL {
		t.Errorf("expected base_url '%s', got '%s'", testBaseURL, cfg.Providers.List[0].BaseURL)
	}
}

// TestLoad_MissingProvider tests that missing providers list causes an error.
func TestLoad_MissingProvider(t *testing.T) {
	configPath := "testdata/missing_provider_config.yaml"
	_, err := Load(configPath)

	if err == nil {
		t.Error("expected error for missing providers, got nil")
	}
}

// TestLoad_InvalidDefaultProvider tests that an invalid default provider name causes an error.
func TestLoad_InvalidDefaultProvider(t *testing.T) {
	configPath := "testdata/invalid_default_provider_config.yaml"
	_, err := Load(configPath)

	if err == nil {
		t.Error("expected error for invalid default provider, got nil")
	}
}

// TestLoad_FileNotFound tests that a non-existent file returns an error.
func TestLoad_FileNotFound(t *testing.T) {
	configPath := "testdata/nonexistent_config.yaml"
	_, err := Load(configPath)

	if err == nil {
		t.Error("expected error for non-existent file, got nil")
	}
}

// TestLoad_MissingAPIKey tests that missing api_key causes an error.
func TestLoad_MissingAPIKey(t *testing.T) {
	configPath := "testdata/missing_api_key_config.yaml"
	_, err := Load(configPath)

	if err == nil {
		t.Error("expected error for missing api_key, got nil")
	}
}

// TestLoad_RelativeWorkdir tests that relative workdir path causes an error.
func TestLoad_RelativeWorkdir(t *testing.T) {
	configPath := "testdata/relative_workdir_config.yaml"
	_, err := Load(configPath)

	if err == nil {
		t.Error("expected error for relative workdir path, got nil")
	}
}

func TestLoad_InvalidDefaultToolProfile(t *testing.T) {
	configPath := "testdata/invalid_default_tool_profile.yaml"
	_, err := Load(configPath)

	if err == nil {
		t.Error("expected error for invalid default tool profile, got nil")
	}
}

func TestLoad_InvalidAuthConfig(t *testing.T) {
	configPath := "testdata/invalid_auth_config.yaml"
	_, err := Load(configPath)

	if err == nil {
		t.Error("expected error for invalid auth config, got nil")
	}
}

func TestLoad_RelativeSQLitePath(t *testing.T) {
	configPath := "testdata/relative_sqlite_path_config.yaml"
	_, err := Load(configPath)

	if err == nil {
		t.Error("expected error for relative sqlite path, got nil")
	}
}

// TestExpandEnv tests the expandEnv function.
func TestExpandEnv(t *testing.T) {
	os.Setenv("TEST_VAR", "test-value")
	defer os.Unsetenv("TEST_VAR")

	tests := []struct {
		name     string
		input    string
		expected string
	}{
		{
			name:     "simple substitution",
			input:    "value: ${TEST_VAR}",
			expected: "value: test-value",
		},
		{
			name:     "multiple substitutions",
			input:    "${TEST_VAR}/${TEST_VAR}",
			expected: "test-value/test-value",
		},
		{
			name:     "no substitution",
			input:    "plain value",
			expected: "plain value",
		},
		{
			name:     "missing env var",
			input:    "value: ${MISSING_VAR}",
			expected: "value: ${MISSING_VAR}",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := expandEnv(tt.input)
			if result != tt.expected {
				t.Errorf("expandEnv(%q) = %q, want %q", tt.input, result, tt.expected)
			}
		})
	}
}

// TestLoad_TokenReplacement tests token substitution in config files.
func TestLoad_TokenReplacement(t *testing.T) {
	// Get the current directory to use in test
	cwd, _ := os.Getwd()

	// Set test environment variables with current working directory
	os.Setenv("CLAW_WORKDIR", cwd)
	os.Setenv("CLAW_API_KEY", "sk-test-key-12345")
	defer func() {
		os.Unsetenv("CLAW_WORKDIR")
		os.Unsetenv("CLAW_API_KEY")
	}()

	configPath := "testdata/token_replacement_config.yaml"
	cfg, err := Load(configPath)

	if err != nil {
		t.Fatalf("Load() failed: %v", err)
	}

	// Verify workdir was replaced
	if cfg.Tools.Workdir != cwd {
		t.Errorf("expected workdir '%s', got '%s'", cwd, cfg.Tools.Workdir)
	}

	// Verify API key was replaced
	if cfg.Providers.List[0].APIKey != "sk-test-key-12345" {
		t.Errorf("expected api_key 'sk-test-key-12345', got '%s'", cfg.Providers.List[0].APIKey)
	}
}

// TestValidate_ProviderEmptyName tests that provider with empty name fails validation.
func TestValidate_ProviderEmptyName(t *testing.T) {
	configPath := "testdata/empty_provider_name_config.yaml"
	_, err := Load(configPath)

	if err == nil {
		t.Error("expected error for empty provider name, got nil")
	}
}

// TestLoad_ExampleConfig tests that the example config file can be loaded.
func TestLoad_ExampleConfig(t *testing.T) {
	// Set required environment variables
	os.Setenv("OPENAI_API_KEY", "sk-test-key")
	os.Setenv("BACKUP_API_KEY", "sk-backup-key")
	os.Setenv("FEISHU_APP_ID", "test-app-id")
	os.Setenv("FEISHU_APP_SECRET", "test-app-secret")
	os.Setenv("CLAW_API_KEY", "claw-api-key")
	defer func() {
		os.Unsetenv("OPENAI_API_KEY")
		os.Unsetenv("BACKUP_API_KEY")
		os.Unsetenv("FEISHU_APP_ID")
		os.Unsetenv("FEISHU_APP_SECRET")
		os.Unsetenv("CLAW_API_KEY")
	}()

	configPath := "../../configs/config.example.yaml"
	cfg, err := Load(configPath)

	if err != nil {
		t.Fatalf("Load() failed: %v", err)
	}

	// Verify some key values
	if cfg.Server.Host != "0.0.0.0" {
		t.Errorf("expected host '0.0.0.0', got '%s'", cfg.Server.Host)
	}
	if cfg.Server.Port != 8080 {
		t.Errorf("expected port 8080, got %d", cfg.Server.Port)
	}
	if cfg.Providers.Default != "openai" {
		t.Errorf("expected default provider 'openai', got '%s'", cfg.Providers.Default)
	}
	if len(cfg.Providers.List) != 2 {
		t.Errorf("expected 2 providers, got %d", len(cfg.Providers.List))
	}
	if cfg.Providers.List[0].Name != "openai" {
		t.Errorf("expected first provider name 'openai', got '%s'", cfg.Providers.List[0].Name)
	}

	// Verify environment variables were substituted
	if cfg.Providers.List[0].APIKey != "sk-test-key" {
		t.Errorf("expected api_key 'sk-test-key', got '%s'", cfg.Providers.List[0].APIKey)
	}
	if cfg.Providers.List[1].APIKey != "sk-backup-key" {
		t.Errorf("expected second provider api_key 'sk-backup-key', got '%s'", cfg.Providers.List[1].APIKey)
	}
}
