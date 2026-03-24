package channels

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"sync/atomic"
	"time"

	"github.com/YumingHuang/claw/internal/audit"
	"github.com/YumingHuang/claw/internal/config"
	"github.com/YumingHuang/claw/internal/gateway"
	"github.com/YumingHuang/claw/internal/metrics"
	"github.com/YumingHuang/claw/internal/models"
	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
)

const version = "0.1.0"

// HTTPChannel serves the HTTP/SSE API.
type HTTPChannel struct {
	gateway *gateway.Gateway
	server  *http.Server
	router  chi.Router
	addr    string
	auth    config.AuthConfig
	rate    config.RateLimitConfig
	auditor *audit.Logger
	metrics *metrics.Collector
	ready   atomic.Bool
}

func (h *HTTPChannel) Name() string { return "http" }

// NewHTTPChannel creates an HTTP channel bound to the given address.
func NewHTTPChannel(gw *gateway.Gateway, addr string, auth config.AuthConfig, rate config.RateLimitConfig, auditor *audit.Logger, collector *metrics.Collector) *HTTPChannel {
	h := &HTTPChannel{gateway: gw, addr: addr, auth: auth, rate: rate, auditor: auditor, metrics: collector}
	h.ready.Store(true)
	if collector != nil {
		collector.SetReady(true)
	}
	h.router = h.buildRouter()
	h.server = &http.Server{
		Addr:    addr,
		Handler: h.router,
	}
	return h
}

// MountHandler mounts an external HTTP handler at the given pattern.
// This is used to attach the WebSocket endpoint onto the same server.
func (h *HTTPChannel) MountHandler(pattern string, handler http.Handler) {
	h.router.Handle(pattern, handler)
}

// Router returns the chi router (primarily for testing).
func (h *HTTPChannel) Router() chi.Router {
	return h.router
}

func (h *HTTPChannel) buildRouter() chi.Router {
	r := chi.NewRouter()
	r.Use(h.requestIDMiddleware)
	r.Use(h.loggingMiddleware)
	r.Use(h.recoveryMiddleware)
	r.Use(AuthMiddleware(h.auth, h.auditor))
	r.Use(RateLimitMiddleware(h.rate))

	r.Post("/v1/chat", h.handleChat)
	r.Get("/v1/sessions/{id}", h.handleGetSession)
	r.Delete("/v1/sessions/{id}", h.handleDeleteSession)
	r.Get("/health", h.handleHealth)
	r.Get("/ready", h.handleReady)
	r.Get("/status", h.handleStatus)
	if h.metrics != nil {
		r.Handle("/metrics", h.metrics.Handler())
	}
	return r
}

// Start begins serving HTTP.
func (h *HTTPChannel) Start(_ context.Context) error {
	slog.Info("http channel starting", "addr", h.addr)
	if err := h.server.ListenAndServe(); err != nil && err != http.ErrServerClosed {
		return fmt.Errorf("http listen: %w", err)
	}
	return nil
}

// Stop performs a graceful shutdown.
func (h *HTTPChannel) Stop(ctx context.Context) error {
	slog.Info("http channel stopping")
	h.ready.Store(false)
	if h.metrics != nil {
		h.metrics.SetReady(false)
	}
	return h.server.Shutdown(ctx)
}

// --- Handlers ---

func (h *HTTPChannel) handleChat(w http.ResponseWriter, r *http.Request) {
	var req models.ChatRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, models.NewAPIError(models.ErrInvalidRequest, fmt.Sprintf("invalid JSON: %v", err)))
		return
	}
	if req.Message == "" {
		writeError(w, models.NewAPIError(models.ErrInvalidRequest, "message is required"))
		return
	}
	if req.SessionID == "" {
		req.SessionID = uuid.New().String()
	}

	if req.Stream {
		h.handleChatStream(w, r, req)
		return
	}

	resp, err := h.gateway.HandleMessage(r.Context(), req.SessionID, "http", req.Message)
	if err != nil {
		writeError(w, models.NewAPIError(models.ErrInternal, err.Error()))
		return
	}
	writeJSON(w, http.StatusOK, resp)
}

func (h *HTTPChannel) handleChatStream(w http.ResponseWriter, r *http.Request, req models.ChatRequest) {
	flusher, ok := w.(http.Flusher)
	if !ok {
		writeError(w, models.NewAPIError(models.ErrInternal, "streaming not supported"))
		return
	}

	ch, err := h.gateway.HandleMessageStream(r.Context(), req.SessionID, "http", req.Message)
	if err != nil {
		writeError(w, models.NewAPIError(models.ErrInternal, err.Error()))
		return
	}

	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")

	for chunk := range ch {
		if chunk.Err != nil {
			fmt.Fprintf(w, "event: error\ndata: %s\n\n", chunk.Err.Error())
			flusher.Flush()
			return
		}

		data, _ := json.Marshal(map[string]interface{}{
			"delta": chunk.Delta,
			"done":  chunk.Done,
		})
		fmt.Fprintf(w, "event: chunk\ndata: %s\n\n", data)
		flusher.Flush()
	}

	doneData, _ := json.Marshal(map[string]interface{}{
		"session_id": req.SessionID,
	})
	fmt.Fprintf(w, "event: done\ndata: %s\n\n", doneData)
	flusher.Flush()
}

func (h *HTTPChannel) handleGetSession(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	session, ok := h.gateway.GetSession(id)
	if !ok {
		writeError(w, models.NewAPIError(models.ErrSessionNotFound, fmt.Sprintf("session %s not found", id)))
		return
	}

	writeJSON(w, http.StatusOK, map[string]interface{}{
		"session_id":    session.ID,
		"messages":      session.Messages(),
		"created_at":    session.CreatedAt,
		"message_count": session.MessagesCount(),
	})
}

func (h *HTTPChannel) handleDeleteSession(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	h.gateway.DeleteSession(id)
	w.WriteHeader(http.StatusNoContent)
}

func (h *HTTPChannel) handleHealth(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, map[string]interface{}{
		"status":  "ok",
		"version": version,
	})
}

func (h *HTTPChannel) handleStatus(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, map[string]interface{}{
		"active_sessions": h.gateway.SessionCount(),
		"tools":           h.gateway.ToolNames(),
	})
}

func (h *HTTPChannel) handleReady(w http.ResponseWriter, _ *http.Request) {
	if !h.ready.Load() {
		writeJSON(w, http.StatusServiceUnavailable, map[string]interface{}{
			"status": "not_ready",
		})
		return
	}
	writeJSON(w, http.StatusOK, map[string]interface{}{
		"status": "ready",
	})
}

// --- Middleware ---

func (h *HTTPChannel) requestIDMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requestID := uuid.New().String()
		ctx := context.WithValue(r.Context(), gateway.ContextKeyRequestID, requestID)
		w.Header().Set("X-Request-ID", requestID)
		next.ServeHTTP(w, r.WithContext(ctx))
	})
}

func (h *HTTPChannel) loggingMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		ww := &statusWriter{ResponseWriter: w, status: http.StatusOK}
		next.ServeHTTP(ww, r)
		route := r.URL.Path
		if routeContext := chi.RouteContext(r.Context()); routeContext != nil {
			if pattern := routeContext.RoutePattern(); pattern != "" {
				route = pattern
			}
		}
		if h.metrics != nil {
			h.metrics.ObserveHTTPRequest(r.Method, route, ww.status, time.Since(start))
		}
		slog.Info("http request",
			"method", r.Method,
			"path", route,
			"status", ww.status,
			"latency", time.Since(start),
			"request_id", gateway.RequestIDFromContext(r.Context()),
		)
	})
}

func (h *HTTPChannel) recoveryMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		defer func() {
			if rv := recover(); rv != nil {
				slog.Error("panic recovered", "error", rv,
					"request_id", gateway.RequestIDFromContext(r.Context()))
				writeError(w, models.NewAPIError(models.ErrInternal, "internal server error"))
			}
		}()
		next.ServeHTTP(w, r)
	})
}

// --- Helpers ---

type statusWriter struct {
	http.ResponseWriter
	status int
}

func (w *statusWriter) WriteHeader(status int) {
	w.status = status
	w.ResponseWriter.WriteHeader(status)
}

// Unwrap lets http.ResponseController access the underlying ResponseWriter,
// preserving interfaces like http.Hijacker needed for WebSocket upgrades.
func (w *statusWriter) Unwrap() http.ResponseWriter {
	return w.ResponseWriter
}

func writeJSON(w http.ResponseWriter, status int, data interface{}) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(data)
}

func writeError(w http.ResponseWriter, apiErr *models.APIError) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(apiErr.HTTPStatus)
	_ = json.NewEncoder(w).Encode(map[string]interface{}{
		"error": apiErr,
	})
}
