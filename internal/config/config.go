package config

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"time"

	"gopkg.in/yaml.v3"
)

// Config represents the complete application configuration.
type Config struct {
	Server       ServerConfig    `yaml:"server"`
	Log          LogConfig       `yaml:"log"`
	SystemPrompt string          `yaml:"system_prompt"`
	Session      SessionConfig   `yaml:"session"`
	Providers    ProvidersConfig `yaml:"providers"`
	Tools        ToolsConfig     `yaml:"tools"`
	Channels     ChannelsConfig  `yaml:"channels"`
	Auth         AuthConfig      `yaml:"auth"`
	RateLimit    RateLimitConfig `yaml:"rate_limit"`
}

// ServerConfig holds server-related configuration.
type ServerConfig struct {
	Host            string        `yaml:"host"`
	Port            int           `yaml:"port"`
	ReadTimeout     time.Duration `yaml:"read_timeout"`
	WriteTimeout    time.Duration `yaml:"write_timeout"`
	ShutdownTimeout time.Duration `yaml:"shutdown_timeout"`
}

// LogConfig holds logging configuration.
type LogConfig struct {
	Level  string `yaml:"level"`  // debug|info|warn|error
	Format string `yaml:"format"` // json|text
	Output string `yaml:"output"` // stdout|stderr|filepath
}

// SessionConfig holds session management configuration.
type SessionConfig struct {
	TTL             time.Duration `yaml:"ttl"`
	MaxHistory      int           `yaml:"max_history"`
	CleanupInterval time.Duration `yaml:"cleanup_interval"`
}

// ProviderConfig holds a single LLM provider configuration.
type ProviderConfig struct {
	Name          string        `yaml:"name"`
	Type          string        `yaml:"type"`
	BaseURL       string        `yaml:"base_url"`
	APIKey        string        `yaml:"api_key"`
	Model         string        `yaml:"model"`
	MaxTokens     int           `yaml:"max_tokens"`
	Temperature   float64       `yaml:"temperature"`
	Timeout       time.Duration `yaml:"timeout"`
	ContextWindow int           `yaml:"context_window"`
}

// ProvidersConfig holds LLM provider configuration.
type ProvidersConfig struct {
	Default       string           `yaml:"default"`
	List          []ProviderConfig `yaml:"list"`
	FallbackOrder []string         `yaml:"fallback_order"`
	Retry         RetryConfig      `yaml:"retry"`
}

// RetryConfig holds retry configuration for providers.
type RetryConfig struct {
	MaxAttempts int           `yaml:"max_attempts"`
	Backoff     time.Duration `yaml:"backoff"`
}

// ToolsConfig holds tools execution configuration.
type ToolsConfig struct {
	Workdir         string        `yaml:"workdir"`
	AllowedCommands []string      `yaml:"allowed_commands"`
	MaxOutputChars  int           `yaml:"max_output_chars"`
	Timeout         time.Duration `yaml:"timeout"`
	MaxIterations   int           `yaml:"max_iterations"`
}

// ChannelsConfig holds communication channel configuration.
type ChannelsConfig struct {
	HTTP      HTTPChannelConfig      `yaml:"http"`
	WebSocket WebSocketChannelConfig `yaml:"websocket"`
	Feishu    FeishuChannelConfig    `yaml:"feishu"`
}

// HTTPChannelConfig holds HTTP channel configuration.
type HTTPChannelConfig struct {
	Enabled bool `yaml:"enabled"`
}

// WebSocketChannelConfig holds WebSocket channel configuration.
type WebSocketChannelConfig struct {
	Enabled      bool          `yaml:"enabled"`
	PingInterval time.Duration `yaml:"ping_interval"`
}

// FeishuChannelConfig holds Feishu channel configuration.
type FeishuChannelConfig struct {
	Enabled  bool   `yaml:"enabled"`
	AppID    string `yaml:"app_id"`
	AppSecret string `yaml:"app_secret"`
}

// AuthConfig holds authentication configuration.
type AuthConfig struct {
	Enabled bool     `yaml:"enabled"`
	APIKeys []string `yaml:"api_keys"`
}

// RateLimitConfig holds rate limiting configuration.
type RateLimitConfig struct {
	Enabled           bool `yaml:"enabled"`
	RequestsPerMinute int  `yaml:"requests_per_minute"`
	Burst             int  `yaml:"burst"`
}

// Load reads and parses the configuration file at the given path.
// It performs environment variable substitution on ${VAR} patterns.
func Load(path string) (*Config, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("failed to read config file: %w", err)
	}

	// Perform environment variable substitution
	content := expandEnv(string(data))

	var cfg Config
	if err := yaml.Unmarshal([]byte(content), &cfg); err != nil {
		return nil, fmt.Errorf("failed to parse config: %w", err)
	}

	// Set defaults
	cfg.setDefaults()

	// Validate configuration
	if err := cfg.validate(); err != nil {
		return nil, fmt.Errorf("config validation failed: %w", err)
	}

	return &cfg, nil
}

// expandEnv replaces ${VAR} patterns with environment variable values.
func expandEnv(s string) string {
	re := regexp.MustCompile(`\$\{([A-Za-z_][A-Za-z0-9_]*)\}`)
	return re.ReplaceAllStringFunc(s, func(match string) string {
		varName := match[2 : len(match)-1] // Strip ${ and }
		if val := os.Getenv(varName); val != "" {
			return val
		}
		return match
	})
}

// setDefaults fills in default values for unspecified configuration fields.
func (c *Config) setDefaults() {
	// Server defaults
	if c.Server.Host == "" {
		c.Server.Host = "0.0.0.0"
	}
	if c.Server.Port == 0 {
		c.Server.Port = 8080
	}
	if c.Server.ReadTimeout == 0 {
		c.Server.ReadTimeout = 30 * time.Second
	}
	if c.Server.WriteTimeout == 0 {
		c.Server.WriteTimeout = 120 * time.Second
	}
	if c.Server.ShutdownTimeout == 0 {
		c.Server.ShutdownTimeout = 15 * time.Second
	}

	// Log defaults
	if c.Log.Level == "" {
		c.Log.Level = "info"
	}
	if c.Log.Format == "" {
		c.Log.Format = "json"
	}
	if c.Log.Output == "" {
		c.Log.Output = "stdout"
	}

	// Session defaults
	if c.Session.TTL == 0 {
		c.Session.TTL = 1 * time.Hour
	}
	if c.Session.MaxHistory == 0 {
		c.Session.MaxHistory = 100
	}
	if c.Session.CleanupInterval == 0 {
		c.Session.CleanupInterval = 5 * time.Minute
	}

	// Tools defaults
	if c.Tools.MaxOutputChars == 0 {
		c.Tools.MaxOutputChars = 10000
	}
	if c.Tools.Timeout == 0 {
		c.Tools.Timeout = 30 * time.Second
	}
	if c.Tools.MaxIterations == 0 {
		c.Tools.MaxIterations = 10
	}

	// Channels defaults
	c.Channels.HTTP.Enabled = true

	// Providers defaults
	if c.Providers.Retry.MaxAttempts == 0 {
		c.Providers.Retry.MaxAttempts = 2
	}
	if c.Providers.Retry.Backoff == 0 {
		c.Providers.Retry.Backoff = 1 * time.Second
	}
}

// validate checks the configuration for required fields and valid values.
func (c *Config) validate() error {
	// Validate providers
	if len(c.Providers.List) == 0 {
		return fmt.Errorf("providers.list cannot be empty")
	}

	if c.Providers.Default == "" {
		return fmt.Errorf("providers.default is required")
	}

	// Check that default provider exists
	defaultFound := false
	for _, p := range c.Providers.List {
		if p.Name == c.Providers.Default {
			defaultFound = true
			break
		}
	}
	if !defaultFound {
		return fmt.Errorf("providers.default '%s' not found in providers.list", c.Providers.Default)
	}

	// Validate each provider
	for _, p := range c.Providers.List {
		if p.Name == "" {
			return fmt.Errorf("provider name cannot be empty")
		}
		if p.BaseURL == "" {
			return fmt.Errorf("provider '%s': base_url cannot be empty", p.Name)
		}
		if p.APIKey == "" {
			return fmt.Errorf("provider '%s': api_key cannot be empty", p.Name)
		}
	}

	// Validate tools workdir
	if c.Tools.Workdir != "" {
		if !filepath.IsAbs(c.Tools.Workdir) {
			return fmt.Errorf("tools.workdir must be an absolute path")
		}
	}

	return nil
}
