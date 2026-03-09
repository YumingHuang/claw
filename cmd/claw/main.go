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

	"github.com/mingminliu/claw/internal/agent"
	"github.com/mingminliu/claw/internal/channels"
	"github.com/mingminliu/claw/internal/config"
	"github.com/mingminliu/claw/internal/gateway"
	"github.com/mingminliu/claw/internal/llm"
	"github.com/mingminliu/claw/internal/tools"
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
	if err := registry.Register(tools.NewTimeTool()); err != nil {
		slog.Error("register tool", "error", err)
		os.Exit(1)
	}
	if cfg.Tools.Workdir != "" {
		if err := registry.Register(tools.NewReadFileTool(cfg.Tools.Workdir, cfg.Tools.MaxOutputChars)); err != nil {
			slog.Error("register tool", "error", err)
			os.Exit(1)
		}
		if err := registry.Register(tools.NewWriteFileTool(cfg.Tools.Workdir)); err != nil {
			slog.Error("register tool", "error", err)
			os.Exit(1)
		}
	}
	if len(cfg.Tools.AllowedCommands) > 0 {
		if err := registry.Register(tools.NewRunCommandTool(cfg.Tools.AllowedCommands, cfg.Tools.MaxOutputChars, cfg.Tools.Timeout)); err != nil {
			slog.Error("register tool", "error", err)
			os.Exit(1)
		}
	}

	// --- Agent ---
	a := agent.NewAgent(provider, registry, agent.AgentOptions{
		SystemPrompt:  cfg.SystemPrompt,
		MaxIterations: cfg.Tools.MaxIterations,
		ContextWindow: findContextWindow(cfg),
	})

	// --- Gateway ---
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	sessionStore := gateway.NewMemorySessionStore(ctx, cfg.Session.TTL, cfg.Session.MaxHistory, cfg.Session.CleanupInterval)
	queue := agent.NewSessionQueue()
	gw := gateway.NewGateway(a, sessionStore, queue)

	// --- Channels ---
	addr := net.JoinHostPort(cfg.Server.Host, fmt.Sprintf("%d", cfg.Server.Port))
	httpCh := channels.NewHTTPChannel(gw, addr)

	var activeChannels []channels.Channel
	activeChannels = append(activeChannels, httpCh)

	if cfg.Channels.WebSocket.Enabled {
		wsCh := channels.NewWebSocketChannel(gw, cfg.Channels.WebSocket.PingInterval)
		httpCh.MountHandler("/v1/ws", wsCh.Handler())
		activeChannels = append(activeChannels, wsCh)
	}

	if cfg.Channels.Feishu.Enabled {
		feishuCh := channels.NewFeishuChannel(gw, cfg.Channels.Feishu)
		httpCh.MountHandler("/v1/feishu/webhook", feishuCh.Handler())
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

func findContextWindow(cfg *config.Config) int {
	for _, p := range cfg.Providers.List {
		if p.Name == cfg.Providers.Default && p.ContextWindow > 0 {
			return p.ContextWindow
		}
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
