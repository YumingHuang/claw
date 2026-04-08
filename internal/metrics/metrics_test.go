package metrics

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestNew(t *testing.T) {
	c := New()
	if c == nil {
		t.Fatal("expected non-nil collector")
	}
	if c.registry == nil {
		t.Fatal("expected non-nil registry")
	}
}

func TestHandler(t *testing.T) {
	c := New()
	h := c.Handler()
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest("GET", "/metrics", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rec.Code)
	}
	if !strings.Contains(rec.Body.String(), "claw_app_ready") {
		t.Fatal("expected claw_app_ready metric in output")
	}
}

func TestHandler_Nil(t *testing.T) {
	var c *Collector
	h := c.Handler()
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest("GET", "/metrics", nil))
	if rec.Code != http.StatusNotFound {
		t.Fatalf("expected 404 for nil collector, got %d", rec.Code)
	}
}

func TestSetReady(t *testing.T) {
	c := New()
	c.SetReady(true)
	c.SetReady(false)
	// no panic = pass

	var nilC *Collector
	nilC.SetReady(true) // should not panic
}

func TestObserveHTTPRequest(t *testing.T) {
	c := New()
	c.ObserveHTTPRequest("GET", "/health", 200, 10*time.Millisecond)
	c.ObserveHTTPRequest("POST", "", 500, 100*time.Millisecond)

	var nilC *Collector
	nilC.ObserveHTTPRequest("GET", "/", 200, time.Millisecond) // should not panic
}

func TestObserveGatewayRequest(t *testing.T) {
	c := New()
	c.ObserveGatewayRequest("http", true, "success", 1*time.Second)
	c.ObserveGatewayRequest("", false, "", 1*time.Second)

	var nilC *Collector
	nilC.ObserveGatewayRequest("http", false, "ok", time.Second)
}

func TestObserveToolExecution(t *testing.T) {
	c := New()
	c.ObserveToolExecution("read_file", "success")
	c.ObserveToolExecution("", "")

	var nilC *Collector
	nilC.ObserveToolExecution("x", "ok")
}

func TestObserveLLMRequest(t *testing.T) {
	c := New()
	c.ObserveLLMRequest("openai", "gpt-4", "success", 2*time.Second)
	c.ObserveLLMRequest("", "", "error", time.Second)

	var nilC *Collector
	nilC.ObserveLLMRequest("x", "y", "ok", time.Second)
}

func TestObserveLLMTokens(t *testing.T) {
	c := New()
	c.ObserveLLMTokens("openai", "gpt-4", 100, 50)
	c.ObserveLLMTokens("", "", 0, 0) // zero tokens should not increment

	var nilC *Collector
	nilC.ObserveLLMTokens("x", "y", 10, 5)
}

func TestSetActiveSessions(t *testing.T) {
	c := New()
	c.SetActiveSessions(5)
	c.SetActiveSessions(0)

	var nilC *Collector
	nilC.SetActiveSessions(1)
}
