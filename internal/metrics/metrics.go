package metrics

import (
	"net/http"
	"strconv"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promhttp"
)

// Collector owns the Prometheus registry and application metrics.
type Collector struct {
	registry *prometheus.Registry

	httpRequestsTotal      *prometheus.CounterVec
	httpRequestDuration    *prometheus.HistogramVec
	gatewayRequestsTotal   *prometheus.CounterVec
	gatewayRequestDuration *prometheus.HistogramVec
	toolExecutionsTotal    *prometheus.CounterVec
	ready                  prometheus.Gauge

	llmRequestsTotal   *prometheus.CounterVec
	llmRequestDuration *prometheus.HistogramVec
	llmTokensTotal     *prometheus.CounterVec
	activeSessions     prometheus.Gauge
}

// New creates a collector with an isolated registry.
func New() *Collector {
	c := &Collector{
		registry: prometheus.NewRegistry(),
		httpRequestsTotal: prometheus.NewCounterVec(
			prometheus.CounterOpts{
				Namespace: "claw",
				Subsystem: "http",
				Name:      "requests_total",
				Help:      "Total number of HTTP requests handled by route, method, and status.",
			},
			[]string{"method", "route", "status"},
		),
		httpRequestDuration: prometheus.NewHistogramVec(
			prometheus.HistogramOpts{
				Namespace: "claw",
				Subsystem: "http",
				Name:      "request_duration_seconds",
				Help:      "HTTP request latency by route, method, and status.",
				Buckets:   prometheus.DefBuckets,
			},
			[]string{"method", "route", "status"},
		),
		gatewayRequestsTotal: prometheus.NewCounterVec(
			prometheus.CounterOpts{
				Namespace: "claw",
				Subsystem: "gateway",
				Name:      "requests_total",
				Help:      "Total gateway requests by channel, streaming mode, and status.",
			},
			[]string{"channel", "stream", "status"},
		),
		gatewayRequestDuration: prometheus.NewHistogramVec(
			prometheus.HistogramOpts{
				Namespace: "claw",
				Subsystem: "gateway",
				Name:      "request_duration_seconds",
				Help:      "Gateway request latency by channel, streaming mode, and status.",
				Buckets:   prometheus.DefBuckets,
			},
			[]string{"channel", "stream", "status"},
		),
		toolExecutionsTotal: prometheus.NewCounterVec(
			prometheus.CounterOpts{
				Namespace: "claw",
				Subsystem: "tools",
				Name:      "executions_total",
				Help:      "Total tool executions by tool name and status.",
			},
			[]string{"tool", "status"},
		),
		ready: prometheus.NewGauge(
			prometheus.GaugeOpts{
				Namespace: "claw",
				Subsystem: "app",
				Name:      "ready",
				Help:      "Whether the service is ready to serve requests.",
			},
		),
		llmRequestsTotal: prometheus.NewCounterVec(
			prometheus.CounterOpts{
				Namespace: "claw",
				Subsystem: "llm",
				Name:      "requests_total",
				Help:      "Total LLM requests by provider, model, and status.",
			},
			[]string{"provider", "model", "status"},
		),
		llmRequestDuration: prometheus.NewHistogramVec(
			prometheus.HistogramOpts{
				Namespace: "claw",
				Subsystem: "llm",
				Name:      "request_duration_seconds",
				Help:      "LLM request latency by provider and model.",
				Buckets:   []float64{0.5, 1, 2, 5, 10, 30, 60, 120},
			},
			[]string{"provider", "model"},
		),
		llmTokensTotal: prometheus.NewCounterVec(
			prometheus.CounterOpts{
				Namespace: "claw",
				Subsystem: "llm",
				Name:      "tokens_total",
				Help:      "Total tokens consumed by provider, model, and type (prompt/completion).",
			},
			[]string{"provider", "model", "type"},
		),
		activeSessions: prometheus.NewGauge(
			prometheus.GaugeOpts{
				Namespace: "claw",
				Subsystem: "app",
				Name:      "active_sessions",
				Help:      "Number of active sessions.",
			},
		),
	}

	c.registry.MustRegister(
		c.httpRequestsTotal,
		c.httpRequestDuration,
		c.gatewayRequestsTotal,
		c.gatewayRequestDuration,
		c.toolExecutionsTotal,
		c.ready,
		c.llmRequestsTotal,
		c.llmRequestDuration,
		c.llmTokensTotal,
		c.activeSessions,
	)

	return c
}

// Handler exposes the collector registry in Prometheus text format.
func (c *Collector) Handler() http.Handler {
	if c == nil {
		return http.NotFoundHandler()
	}
	return promhttp.HandlerFor(c.registry, promhttp.HandlerOpts{})
}

// SetReady updates the application readiness gauge.
func (c *Collector) SetReady(ready bool) {
	if c == nil {
		return
	}
	if ready {
		c.ready.Set(1)
		return
	}
	c.ready.Set(0)
}

// ObserveHTTPRequest records a completed HTTP request.
func (c *Collector) ObserveHTTPRequest(method, route string, status int, duration time.Duration) {
	if c == nil {
		return
	}
	if route == "" {
		route = "unknown"
	}
	statusLabel := strconv.Itoa(status)
	c.httpRequestsTotal.WithLabelValues(method, route, statusLabel).Inc()
	c.httpRequestDuration.WithLabelValues(method, route, statusLabel).Observe(duration.Seconds())
}

// ObserveGatewayRequest records a completed gateway request.
func (c *Collector) ObserveGatewayRequest(channel string, stream bool, status string, duration time.Duration) {
	if c == nil {
		return
	}
	if channel == "" {
		channel = "unknown"
	}
	if status == "" {
		status = "unknown"
	}
	streamLabel := strconv.FormatBool(stream)
	c.gatewayRequestsTotal.WithLabelValues(channel, streamLabel, status).Inc()
	c.gatewayRequestDuration.WithLabelValues(channel, streamLabel, status).Observe(duration.Seconds())
}

// ObserveToolExecution records a tool invocation outcome.
func (c *Collector) ObserveToolExecution(toolName, status string) {
	if c == nil {
		return
	}
	if toolName == "" {
		toolName = "unknown"
	}
	if status == "" {
		status = "unknown"
	}
	c.toolExecutionsTotal.WithLabelValues(toolName, status).Inc()
}

// ObserveLLMRequest records a completed LLM provider call.
func (c *Collector) ObserveLLMRequest(provider, model, status string, duration time.Duration) {
	if c == nil {
		return
	}
	if provider == "" {
		provider = "unknown"
	}
	if model == "" {
		model = "unknown"
	}
	c.llmRequestsTotal.WithLabelValues(provider, model, status).Inc()
	c.llmRequestDuration.WithLabelValues(provider, model).Observe(duration.Seconds())
}

// ObserveLLMTokens records token usage from an LLM call.
func (c *Collector) ObserveLLMTokens(provider, model string, promptTokens, completionTokens int) {
	if c == nil {
		return
	}
	if provider == "" {
		provider = "unknown"
	}
	if model == "" {
		model = "unknown"
	}
	if promptTokens > 0 {
		c.llmTokensTotal.WithLabelValues(provider, model, "prompt").Add(float64(promptTokens))
	}
	if completionTokens > 0 {
		c.llmTokensTotal.WithLabelValues(provider, model, "completion").Add(float64(completionTokens))
	}
}

// SetActiveSessions updates the active session gauge.
func (c *Collector) SetActiveSessions(count int) {
	if c == nil {
		return
	}
	c.activeSessions.Set(float64(count))
}
