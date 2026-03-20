package tools

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/YumingHuang/claw/internal/models"
)

const defaultTavilySearchURL = "https://api.tavily.com/search"

// SearchTool queries a web search API and returns a compact text summary.
type SearchTool struct {
	apiKey     string
	baseURL    string
	httpClient *http.Client
}

// NewSearchTool creates a web search tool backed by Tavily-compatible APIs.
func NewSearchTool(apiKey, baseURL string, httpClient *http.Client) *SearchTool {
	if strings.TrimSpace(baseURL) == "" {
		baseURL = defaultTavilySearchURL
	}
	if httpClient == nil {
		httpClient = &http.Client{Timeout: 15 * time.Second}
	}
	return &SearchTool{
		apiKey:     apiKey,
		baseURL:    baseURL,
		httpClient: httpClient,
	}
}

func (t *SearchTool) Name() string { return "web_search" }

func (t *SearchTool) Description() string {
	return "Search the web and return the most relevant results with titles, summaries, and URLs"
}

func (t *SearchTool) Parameters() json.RawMessage {
	return json.RawMessage(`{"type":"object","properties":{"query":{"type":"string","description":"Search query"},"num_results":{"type":"integer","description":"Maximum number of results to return","minimum":1,"maximum":10}},"required":["query"]}`)
}

func (t *SearchTool) Execute(ctx context.Context, params json.RawMessage) (models.ToolResult, error) {
	var p struct {
		Query      string `json:"query"`
		NumResults int    `json:"num_results"`
	}
	if err := json.Unmarshal(params, &p); err != nil {
		return models.ToolResult{Content: fmt.Sprintf("invalid parameters: %v", err), IsError: true}, nil
	}
	if strings.TrimSpace(p.Query) == "" {
		return models.ToolResult{Content: "query is required", IsError: true}, nil
	}
	if p.NumResults <= 0 {
		p.NumResults = 5
	}
	if p.NumResults > 10 {
		p.NumResults = 10
	}
	if strings.TrimSpace(t.apiKey) == "" {
		return models.ToolResult{Content: "search API key is not configured", IsError: true}, nil
	}

	reqBody := map[string]any{
		"api_key":     t.apiKey,
		"query":       p.Query,
		"max_results": p.NumResults,
	}
	body, err := json.Marshal(reqBody)
	if err != nil {
		return models.ToolResult{Content: fmt.Sprintf("build request: %v", err), IsError: true}, nil
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, t.baseURL, bytes.NewReader(body))
	if err != nil {
		return models.ToolResult{Content: fmt.Sprintf("build request: %v", err), IsError: true}, nil
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := t.httpClient.Do(req)
	if err != nil {
		return models.ToolResult{Content: fmt.Sprintf("search request failed: %v", err), IsError: true}, nil
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return models.ToolResult{Content: fmt.Sprintf("read search response: %v", err), IsError: true}, nil
	}

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return models.ToolResult{
			Content: fmt.Sprintf("search API returned status %d: %s", resp.StatusCode, strings.TrimSpace(string(respBody))),
			IsError: true,
		}, nil
	}

	var searchResp struct {
		Results []struct {
			Title   string `json:"title"`
			URL     string `json:"url"`
			Content string `json:"content"`
		} `json:"results"`
	}
	if err := json.Unmarshal(respBody, &searchResp); err != nil {
		return models.ToolResult{Content: fmt.Sprintf("invalid search response: %v", err), IsError: true}, nil
	}
	if len(searchResp.Results) == 0 {
		return models.ToolResult{Content: "no search results found"}, nil
	}

	var b strings.Builder
	for i, result := range searchResp.Results {
		if i > 0 {
			b.WriteString("\n\n")
		}
		fmt.Fprintf(&b, "%d. %s\n", i+1, strings.TrimSpace(result.Title))
		if summary := strings.TrimSpace(result.Content); summary != "" {
			b.WriteString(summary)
			b.WriteString("\n")
		}
		if url := strings.TrimSpace(result.URL); url != "" {
			b.WriteString(url)
		}
	}

	return models.ToolResult{Content: b.String()}, nil
}
