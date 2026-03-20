package tools

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strings"
	"testing"
)

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(req *http.Request) (*http.Response, error) {
	return f(req)
}

func newTestHTTPClient(fn roundTripFunc) *http.Client {
	return &http.Client{Transport: fn}
}

func TestSearchTool_Success(t *testing.T) {
	client := newTestHTTPClient(func(req *http.Request) (*http.Response, error) {
		if req.Method != http.MethodPost {
			t.Fatalf("method = %s, want POST", req.Method)
		}
		if req.URL.String() != "https://search.example/api" {
			t.Fatalf("url = %s", req.URL.String())
		}

		body, err := io.ReadAll(req.Body)
		if err != nil {
			t.Fatalf("ReadAll: %v", err)
		}
		var payload map[string]any
		if err := json.Unmarshal(body, &payload); err != nil {
			t.Fatalf("Unmarshal request: %v", err)
		}
		if payload["query"] != "golang claw" {
			t.Fatalf("query = %v", payload["query"])
		}
		if payload["max_results"] != float64(2) {
			t.Fatalf("max_results = %v", payload["max_results"])
		}
		if payload["api_key"] != "test-key" {
			t.Fatalf("api_key = %v", payload["api_key"])
		}

		return &http.Response{
			StatusCode: http.StatusOK,
			Body: io.NopCloser(strings.NewReader(`{
				"results": [
					{"title":"Claw Repo","url":"https://example.com/claw","content":"A Go AI assistant project."},
					{"title":"Go Docs","url":"https://go.dev","content":"The Go programming language."}
				]
			}`)),
			Header: make(http.Header),
		}, nil
	})

	tool := NewSearchTool("test-key", "https://search.example/api", client)
	result, err := tool.Execute(context.Background(), json.RawMessage(`{"query":"golang claw","num_results":2}`))
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if result.IsError {
		t.Fatalf("unexpected error: %s", result.Content)
	}
	if !strings.Contains(result.Content, "1. Claw Repo") {
		t.Fatalf("unexpected content: %s", result.Content)
	}
	if !strings.Contains(result.Content, "https://example.com/claw") {
		t.Fatalf("missing url: %s", result.Content)
	}
}

func TestSearchTool_DefaultsNumResults(t *testing.T) {
	client := newTestHTTPClient(func(req *http.Request) (*http.Response, error) {
		body, err := io.ReadAll(req.Body)
		if err != nil {
			t.Fatalf("ReadAll: %v", err)
		}
		var payload map[string]any
		if err := json.Unmarshal(body, &payload); err != nil {
			t.Fatalf("Unmarshal request: %v", err)
		}
		if payload["max_results"] != float64(5) {
			t.Fatalf("max_results = %v, want 5", payload["max_results"])
		}
		return &http.Response{
			StatusCode: http.StatusOK,
			Body:       io.NopCloser(strings.NewReader(`{"results":[]}`)),
			Header:     make(http.Header),
		}, nil
	})

	tool := NewSearchTool("test-key", "https://search.example/api", client)
	result, err := tool.Execute(context.Background(), json.RawMessage(`{"query":"golang"}`))
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if result.IsError {
		t.Fatalf("unexpected error: %s", result.Content)
	}
	if result.Content != "no search results found" {
		t.Fatalf("content = %q", result.Content)
	}
}

func TestSearchTool_MissingQuery(t *testing.T) {
	tool := NewSearchTool("test-key", "", newTestHTTPClient(func(req *http.Request) (*http.Response, error) {
		t.Fatal("request should not be sent")
		return nil, nil
	}))

	result, err := tool.Execute(context.Background(), json.RawMessage(`{"num_results":3}`))
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if !result.IsError {
		t.Fatal("expected error")
	}
	if !strings.Contains(result.Content, "query is required") {
		t.Fatalf("content = %q", result.Content)
	}
}

func TestSearchTool_RequestFailure(t *testing.T) {
	client := newTestHTTPClient(func(req *http.Request) (*http.Response, error) {
		return nil, errors.New("network down")
	})

	tool := NewSearchTool("test-key", "https://search.example/api", client)
	result, err := tool.Execute(context.Background(), json.RawMessage(`{"query":"golang"}`))
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if !result.IsError {
		t.Fatal("expected error")
	}
	if !strings.Contains(result.Content, "search request failed") {
		t.Fatalf("content = %q", result.Content)
	}
}

func TestSearchTool_HTTPError(t *testing.T) {
	client := newTestHTTPClient(func(req *http.Request) (*http.Response, error) {
		return &http.Response{
			StatusCode: http.StatusUnauthorized,
			Body:       io.NopCloser(strings.NewReader(`{"error":"bad api key"}`)),
			Header:     make(http.Header),
		}, nil
	})

	tool := NewSearchTool("test-key", "https://search.example/api", client)
	result, err := tool.Execute(context.Background(), json.RawMessage(`{"query":"golang"}`))
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if !result.IsError {
		t.Fatal("expected error")
	}
	if !strings.Contains(result.Content, "status 401") {
		t.Fatalf("content = %q", result.Content)
	}
}
