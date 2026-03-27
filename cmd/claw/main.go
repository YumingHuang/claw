package main

import (
	"context"
	"flag"
	"fmt"
	"log/slog"
	"net"
	"os"
	"os/signal"
	"strings"
	"syscall"

	"github.com/YumingHuang/claw/internal/agent"
	"github.com/YumingHuang/claw/internal/audit"
	"github.com/YumingHuang/claw/internal/channels"
	"github.com/YumingHuang/claw/internal/config"
	clawcron "github.com/YumingHuang/claw/internal/cron"
	"github.com/YumingHuang/claw/internal/gateway"
	"github.com/YumingHuang/claw/internal/llm"
	"github.com/YumingHuang/claw/internal/mcp"
	"github.com/YumingHuang/claw/internal/metrics"
	"github.com/YumingHuang/claw/internal/skills"
	"github.com/YumingHuang/claw/internal/tools"
)

const version = "0.1.0"

func main() {
	configPath := flag.String("config", "configs/config.yaml", "path to config file")
	showVersion := flag.Bool("version", false, "print version and exit")
	flag.Parse()

	if *showVersion {
		fmt.Printf("claw %s\n", version)
		return
	}

	cfg, err := config.Load(*configPath)
	if err != nil {
		fmt.Fprintf(os.Stderr, "failed to load config: %v\n", err)
		os.Exit(1)
	}

	logger := setupLogger(cfg.Log)
	slog.SetDefault(logger)
	auditLogger, err := setupAuditLogger(cfg.Audit)
	if err != nil {
		slog.Error("failed to create audit logger", "error", err)
		os.Exit(1)
	}
	collector := metrics.New()

	// --- Providers ---
	providers, err := createProviders(cfg)
	if err != nil {
		slog.Error("failed to create providers", "error", err)
		os.Exit(1)
	}

	fallbackOrder := cfg.Providers.FallbackOrder
	if len(fallbackOrder) == 0 {
		fallbackOrder = []string{cfg.Providers.Default}
	}

	provider := llm.NewProviderManager(providers, fallbackOrder, cfg.Providers.Retry)

	// --- Tools ---
	registry := tools.NewRegistry()
	registry.SetAuditor(auditLogger)
	registry.SetMetrics(collector)
	if err := registry.Register(tools.NewTimeTool()); err != nil {
		slog.Error("register tool", "error", err)
		os.Exit(1)
	}
	if cfg.Tools.Workdir != "" {
		if err := registry.Register(tools.NewReadFileTool(cfg.Tools.Workdir, cfg.Tools.MaxOutputChars)); err != nil {
			slog.Error("register tool", "error", err)
			os.Exit(1)
		}
		if err := registry.Register(tools.NewWriteFileTool(cfg.Tools.Workdir, auditLogger)); err != nil {
			slog.Error("register tool", "error", err)
			os.Exit(1)
		}
	}
	if len(cfg.Tools.AllowedCommands) > 0 {
		if err := registry.Register(tools.NewRunCommandTool(cfg.Tools.AllowedCommands, cfg.Tools.MaxOutputChars, cfg.Tools.Timeout, auditLogger)); err != nil {
			slog.Error("register tool", "error", err)
			os.Exit(1)
		}
	}
	if cfg.Tools.TavilyAPIKey != "" {
		if err := registry.Register(tools.NewSearchTool(cfg.Tools.TavilyAPIKey, "", nil)); err != nil {
			slog.Error("register tool", "error", err)
			os.Exit(1)
		}
	}
	// --- Memory tools (must be registered before SetProfiles) ---
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	memoryStore, err := createMemoryStore(ctx, cfg)
	if err != nil {
		slog.Error("failed to create memory store", "error", err)
		os.Exit(1)
	}
	if err := registry.Register(tools.NewMemoryGetTool(memoryStore)); err != nil {
		slog.Error("register tool", "error", err)
		os.Exit(1)
	}
	if err := registry.Register(tools.NewMemorySetTool(memoryStore)); err != nil {
		slog.Error("register tool", "error", err)
		os.Exit(1)
	}
	if err := registry.Register(tools.NewMemoryListTool(memoryStore)); err != nil {
		slog.Error("register tool", "error", err)
		os.Exit(1)
	}

	// --- MCP tools ---
	mcpTools := mcp.LoadTools(ctx, cfg.MCP)
	for _, mcpTool := range mcpTools {
		if err := registry.Register(mcpTool); err != nil {
			slog.Warn("mcp: failed to register tool", "name", mcpTool.Name(), "error", err)
		}
	}
	// Auto-add MCP tools to all profiles so the agent can use them.
	for profile, names := range cfg.Tools.Profiles {
		for _, t := range mcpTools {
			names = append(names, t.Name())
		}
		cfg.Tools.Profiles[profile] = names
	}

	if err := registry.SetProfiles(cfg.Tools.Profiles, cfg.Tools.DefaultProfile); err != nil {
		slog.Error("configure tool profiles", "error", err)
		os.Exit(1)
	}

	// --- Agent ---
	systemPrompt := cfg.SystemPrompt
	if skillsPrompt, err := skills.Load("skills", registry); err != nil {
		slog.Warn("load skills", "error", err)
	} else if skillsPrompt != "" {
		systemPrompt = systemPrompt + "\n\n" + skillsPrompt
	}

	defaultProvider := findDefaultProvider(cfg)
	a := agent.NewAgent(provider, registry, agent.AgentOptions{
		SystemPrompt:  systemPrompt,
		MaxIterations: cfg.Tools.MaxIterations,
		ContextWindow: findContextWindow(cfg),
		Temperature:   defaultProvider.Temperature,
		MaxTokens:     defaultProvider.MaxTokens,
	})

	sessionStore, err := createSessionStore(ctx, cfg)
	if err != nil {
		slog.Error("failed to create session store", "error", err)
		os.Exit(1)
	}
	queue := agent.NewSessionQueue()
	gw := gateway.NewGateway(a, sessionStore, queue)
	gw.SetToolProfile(cfg.Tools.DefaultProfile)
	gw.SetMetrics(collector)

	// --- Channels ---
	addr := net.JoinHostPort(cfg.Server.Host, fmt.Sprintf("%d", cfg.Server.Port))
	httpCh := channels.NewHTTPChannel(gw, addr, cfg.Auth, cfg.RateLimit, auditLogger, collector)

	var activeChannels []channels.Channel
	activeChannels = append(activeChannels, httpCh)

	if cfg.Channels.WebSocket.Enabled {
		wsCh := channels.NewWebSocketChannel(gw, cfg.Channels.WebSocket.PingInterval)
		httpCh.MountHandler("/v1/ws", wsCh.Handler())
		activeChannels = append(activeChannels, wsCh)
	}

	var feishuCh *channels.FeishuChannel
	if cfg.Channels.Feishu.Enabled {
		feishuCh = channels.NewFeishuChannel(gw, cfg.Channels.Feishu)
		if !cfg.Channels.Feishu.LongConnection {
			httpCh.MountHandler("/v1/feishu/webhook", feishuCh.Handler())
		}
		activeChannels = append(activeChannels, feishuCh)
	}

	for _, ch := range activeChannels {
		ch := ch
		go func() {
			if err := ch.Start(ctx); err != nil {
				slog.Error("channel start error", "channel", ch.Name(), "error", err)
				stop()
			}
		}()
	}

	slog.Info("claw started", "version", version, "addr", addr)

	// --- Cron ---
	if len(cfg.Cron.Jobs) > 0 {
		notifier := clawcron.NewMultiNotifier()
		if feishuCh != nil {
			notifier.Register("feishu", feishuCh.Sender())
		}
		scheduler, err := clawcron.New(gw, cfg.Cron.Jobs, notifier)
		if err != nil {
			slog.Error("cron setup failed", "error", err)
			os.Exit(1)
		}
		scheduler.Start()
		defer scheduler.Stop()
	}

	<-ctx.Done()
	slog.Info("shutting down...")

	shutdownCtx, cancel := context.WithTimeout(context.Background(), cfg.Server.ShutdownTimeout)
	defer cancel()

	for _, ch := range activeChannels {
		if err := ch.Stop(shutdownCtx); err != nil {
			slog.Error("channel stop error", "channel", ch.Name(), "error", err)
		}
	}

	slog.Info("claw stopped")
}

func createProviders(cfg *config.Config) (map[string]llm.Provider, error) {
	providers := make(map[string]llm.Provider, len(cfg.Providers.List))
	for _, pcfg := range cfg.Providers.List {
		var p llm.Provider
		var err error
		switch pcfg.Type {
		case "anthropic":
			p, err = llm.NewAnthropicProvider(pcfg)
		default: // "openai_compatible" and any other type
			p, err = llm.NewOpenAIProvider(pcfg)
		}
		if err != nil {
			return nil, fmt.Errorf("create provider %q: %w", pcfg.Name, err)
		}
		providers[pcfg.Name] = p
		slog.Info("provider registered", "name", pcfg.Name, "type", pcfg.Type)
	}
	return providers, nil
}

func findDefaultProvider(cfg *config.Config) config.ProviderConfig {
	for _, p := range cfg.Providers.List {
		if p.Name == cfg.Providers.Default {
			return p
		}
	}
	return cfg.Providers.List[0]
}

func findContextWindow(cfg *config.Config) int {
	p := findDefaultProvider(cfg)
	if p.ContextWindow > 0 {
		return p.ContextWindow
	}
	return 128000
}

func parseLogLevel(level string) slog.Level {
	switch strings.ToLower(level) {
	case "debug":
		return slog.LevelDebug
	case "warn":
		return slog.LevelWarn
	case "error":
		return slog.LevelError
	default:
		return slog.LevelInfo
	}
}

func setupLogger(cfg config.LogConfig) *slog.Logger {
	level := parseLogLevel(cfg.Level)

	var output *os.File
	switch strings.ToLower(cfg.Output) {
	case "stderr":
		output = os.Stderr
	default:
		output = os.Stdout
	}

	opts := &slog.HandlerOptions{Level: level}

	var handler slog.Handler
	switch strings.ToLower(cfg.Format) {
	case "text":
		handler = slog.NewTextHandler(output, opts)
	default:
		handler = slog.NewJSONHandler(output, opts)
	}

	return slog.New(handler)
}

func setupAuditLogger(cfg config.AuditConfig) (*audit.Logger, error) {
	if !cfg.Enabled {
		return &audit.Logger{}, nil
	}
	return audit.NewLogger(cfg.Output, cfg.MaxValueChars)
}

func createSessionStore(ctx context.Context, cfg *config.Config) (gateway.SessionStore, error) {
	if cfg.Session.SQLitePath != "" {
		return gateway.NewSQLiteSessionStore(ctx, cfg.Session.SQLitePath, cfg.Session.TTL, cfg.Session.CleanupInterval)
	}
	return gateway.NewMemorySessionStore(ctx, cfg.Session.TTL, cfg.Session.MaxHistory, cfg.Session.CleanupInterval), nil
}

func createMemoryStore(ctx context.Context, cfg *config.Config) (tools.MemoryStore, error) {
	if cfg.Session.SQLitePath != "" {
		return tools.NewSQLiteMemoryStore(ctx, cfg.Session.SQLitePath)
	}
	return tools.NewMemoryStore(), nil
}
