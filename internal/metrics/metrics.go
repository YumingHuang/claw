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
	}

	c.registry.MustRegister(
		c.httpRequestsTotal,
		c.httpRequestDuration,
		c.gatewayRequestsTotal,
		c.gatewayRequestDuration,
		c.toolExecutionsTotal,
		c.ready,
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
