package llm

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"

	"github.com/YumingHuang/claw/internal/config"
	"github.com/YumingHuang/claw/internal/models"
)

const anthropicAPIVersion = "2023-06-01"

// AnthropicProvider implements Provider for the Anthropic Messages API.
type AnthropicProvider struct {
	name    string
	baseURL string
	apiKey  string
	model   string
	client  *http.Client
}

// NewAnthropicProvider creates a provider for the Anthropic Messages API.
func NewAnthropicProvider(cfg config.ProviderConfig) (*AnthropicProvider, error) {
	return &AnthropicProvider{
		name:    cfg.Name,
		baseURL: strings.TrimRight(cfg.BaseURL, "/"),
		apiKey:  cfg.APIKey,
		model:   cfg.Model,
		client:  &http.Client{Timeout: cfg.Timeout},
	}, nil
}

func (p *AnthropicProvider) Name() string { return p.name }

// Chat sends a non-streaming request to the Anthropic Messages API.
func (p *AnthropicProvider) Chat(ctx context.Context, req *ChatRequest) (*ChatResponse, error) {
	body := p.buildRequestBody(req, false)

	respBody, err := p.doRequest(ctx, body)
	if err != nil {
		return nil, err
	}
	defer respBody.Close()

	var anthResp anthropicResponse
	if err := json.NewDecoder(respBody).Decode(&anthResp); err != nil {
		return nil, models.NewAPIError(models.ErrProviderError, fmt.Sprintf("decode response: %v", err))
	}

	return p.parseResponse(&anthResp), nil
}

// ChatStream sends a streaming request to the Anthropic Messages API.
func (p *AnthropicProvider) ChatStream(ctx context.Context, req *ChatRequest) (<-chan StreamChunk, error) {
	body := p.buildRequestBody(req, true)

	respBody, err := p.doRequest(ctx, body)
	if err != nil {
		return nil, err
	}

	ch := make(chan StreamChunk)
	go func() {
		defer close(ch)
		defer respBody.Close()
		p.readStream(ctx, respBody, ch)
	}()

	return ch, nil
}

func (p *AnthropicProvider) buildRequestBody(req *ChatRequest, stream bool) map[string]interface{} {
	model := req.Model
	if model == "" {
		model = p.model
	}

	msgs, systemPrompt := toAnthropicMessages(req.Messages)

	body := map[string]interface{}{
		"model":    model,
		"messages": msgs,
		"stream":   stream,
	}

	if systemPrompt != "" {
		body["system"] = systemPrompt
	}
	if req.MaxTokens > 0 {
		body["max_tokens"] = req.MaxTokens
	} else {
		body["max_tokens"] = 4096
	}
	if req.Temperature > 0 {
		body["temperature"] = req.Temperature
	}
	if len(req.Tools) > 0 {
		body["tools"] = toAnthropicTools(req.Tools)
	}

	return body
}

func (p *AnthropicProvider) doRequest(ctx context.Context, body map[string]interface{}) (io.ReadCloser, error) {
	jsonBody, err := json.Marshal(body)
	if err != nil {
		return nil, models.NewAPIError(models.ErrInternal, fmt.Sprintf("marshal request: %v", err))
	}

	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, p.baseURL+"/v1/messages", bytes.NewReader(jsonBody))
	if err != nil {
		return nil, models.NewAPIError(models.ErrInternal, fmt.Sprintf("create request: %v", err))
	}

	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set("x-api-key", p.apiKey)
	httpReq.Header.Set("anthropic-version", anthropicAPIVersion)

	resp, err := p.client.Do(httpReq)
	if err != nil {
		if ctx.Err() != nil {
			return nil, models.NewAPIError(models.ErrProviderTimeout, fmt.Sprintf("request cancelled: %v", ctx.Err()))
		}
		return nil, models.NewAPIError(models.ErrProviderTimeout, fmt.Sprintf("request failed: %v", err))
	}

	if resp.StatusCode != http.StatusOK {
		defer resp.Body.Close()
		return nil, p.handleErrorResponse(resp)
	}

	return resp.Body, nil
}

func (p *AnthropicProvider) handleErrorResponse(resp *http.Response) error {
	bodyBytes, _ := io.ReadAll(resp.Body)

	var anthErr struct {
		Error struct {
			Type    string `json:"type"`
			Message string `json:"message"`
		} `json:"error"`
	}
	_ = json.Unmarshal(bodyBytes, &anthErr)
	msg := anthErr.Error.Message
	if msg == "" {
		msg = fmt.Sprintf("HTTP %d", resp.StatusCode)
	}

	switch {
	case resp.StatusCode == http.StatusTooManyRequests:
		return models.NewAPIError(models.ErrRateLimited, msg)
	case resp.StatusCode >= 500:
		return models.NewAPIError(models.ErrProviderError, msg)
	default:
		return models.NewAPIError(models.ErrInvalidRequest, msg)
	}
}

func (p *AnthropicProvider) parseResponse(resp *anthropicResponse) *ChatResponse {
	var content string
	var toolCalls []models.ToolCall

	for _, block := range resp.Content {
		switch block.Type {
		case "text":
			content += block.Text
		case "tool_use":
			args, _ := json.Marshal(block.Input)
			toolCalls = append(toolCalls, models.ToolCall{
				ID:        block.ID,
				Name:      block.Name,
				Arguments: args,
			})
		}
	}

	return &ChatResponse{
		Content:   content,
		ToolCalls: toolCalls,
		Usage: models.Usage{
			PromptTokens:     resp.Usage.InputTokens,
			CompletionTokens: resp.Usage.OutputTokens,
			TotalTokens:      resp.Usage.InputTokens + resp.Usage.OutputTokens,
		},
		FinishReason: resp.StopReason,
	}
}

func (p *AnthropicProvider) readStream(ctx context.Context, body io.Reader, ch chan<- StreamChunk) {
	var currentToolID string
	var currentToolName string
	var toolInputBuf string
	var usage *models.Usage

	scanner := bufio.NewScanner(body)
	for scanner.Scan() {
		line := scanner.Text()

		if line == "" {
			continue
		}

		if strings.HasPrefix(line, "event: ") {
			eventType := strings.TrimPrefix(line, "event: ")
			if eventType == "message_stop" {
				if usage != nil {
					select {
					case ch <- StreamChunk{Usage: usage, Done: true}:
					case <-ctx.Done():
					}
				}
				return
			}
			continue
		}

		if !strings.HasPrefix(line, "data: ") {
			continue
		}
		data := strings.TrimPrefix(line, "data: ")

		var event anthropicStreamEvent
		if err := json.Unmarshal([]byte(data), &event); err != nil {
			ch <- StreamChunk{Err: fmt.Errorf("decode stream event: %w", err)}
			return
		}

		switch event.Type {
		case "message_start":
			if event.Message != nil && event.Message.Usage != nil {
				usage = &models.Usage{
					PromptTokens: event.Message.Usage.InputTokens,
				}
			}

		case "content_block_start":
			if event.ContentBlock != nil && event.ContentBlock.Type == "tool_use" {
				currentToolID = event.ContentBlock.ID
				currentToolName = event.ContentBlock.Name
				toolInputBuf = ""
			}

		case "content_block_delta":
			if event.Delta != nil {
				switch event.Delta.Type {
				case "text_delta":
					select {
					case ch <- StreamChunk{Delta: event.Delta.Text}:
					case <-ctx.Done():
						ch <- StreamChunk{Err: ctx.Err()}
						return
					}
				case "input_json_delta":
					toolInputBuf += event.Delta.PartialJSON
				}
			}

		case "content_block_stop":
			if currentToolID != "" {
				tc := models.ToolCall{
					ID:        currentToolID,
					Name:      currentToolName,
					Arguments: json.RawMessage(toolInputBuf),
				}
				select {
				case ch <- StreamChunk{ToolCalls: []models.ToolCall{tc}}:
				case <-ctx.Done():
					ch <- StreamChunk{Err: ctx.Err()}
					return
				}
				currentToolID = ""
				currentToolName = ""
				toolInputBuf = ""
			}

		case "message_delta":
			if event.Usage != nil && usage != nil {
				usage.CompletionTokens = event.Usage.OutputTokens
				usage.TotalTokens = usage.PromptTokens + usage.CompletionTokens
			}
		}
	}

	if err := scanner.Err(); err != nil {
		ch <- StreamChunk{Err: fmt.Errorf("read stream: %w", err)}
	}
}

// --- Message format conversion (models → Anthropic) ---

// toAnthropicMessages converts internal messages to Anthropic format.
// System messages are extracted and returned separately since Anthropic
// uses a top-level "system" field rather than a system role in messages.
func toAnthropicMessages(msgs []models.Message) ([]map[string]interface{}, string) {
	var systemPrompt string
	out := make([]map[string]interface{}, 0, len(msgs))

	for _, m := range msgs {
		switch m.Role {
		case "system":
			if systemPrompt != "" {
				systemPrompt += "\n"
			}
			systemPrompt += m.Content

		case "user":
			out = append(out, map[string]interface{}{
				"role":    "user",
				"content": m.Content,
			})

		case "assistant":
			msg := map[string]interface{}{
				"role": "assistant",
			}
			if len(m.ToolCalls) > 0 {
				content := make([]map[string]interface{}, 0)
				if m.Content != "" {
					content = append(content, map[string]interface{}{
						"type": "text",
						"text": m.Content,
					})
				}
				for _, tc := range m.ToolCalls {
					var input interface{}
					_ = json.Unmarshal(tc.Arguments, &input)
					content = append(content, map[string]interface{}{
						"type":  "tool_use",
						"id":    tc.ID,
						"name":  tc.Name,
						"input": input,
					})
				}
				msg["content"] = content
			} else {
				msg["content"] = m.Content
			}
			out = append(out, msg)

		case "tool":
			out = append(out, map[string]interface{}{
				"role": "user",
				"content": []map[string]interface{}{
					{
						"type":        "tool_result",
						"tool_use_id": m.ToolCallID,
						"content":     m.Content,
					},
				},
			})
		}
	}

	return out, systemPrompt
}

// toAnthropicTools converts OpenAI-style ToolSchema to Anthropic tool format.
func toAnthropicTools(tools []ToolSchema) []map[string]interface{} {
	out := make([]map[string]interface{}, 0, len(tools))
	for _, t := range tools {
		tool := map[string]interface{}{
			"name":        t.Function.Name,
			"description": t.Function.Description,
		}
		if t.Function.Parameters != nil {
			var schema interface{}
			if err := json.Unmarshal(t.Function.Parameters, &schema); err == nil {
				tool["input_schema"] = schema
			}
		}
		out = append(out, tool)
	}
	return out
}

// --- Anthropic API response structures ---

type anthropicResponse struct {
	ID         string                 `json:"id"`
	Type       string                 `json:"type"`
	Role       string                 `json:"role"`
	Content    []anthropicContentBlock `json:"content"`
	StopReason string                 `json:"stop_reason"`
	Usage      anthropicUsage         `json:"usage"`
}

type anthropicContentBlock struct {
	Type  string          `json:"type"`
	Text  string          `json:"text,omitempty"`
	ID    string          `json:"id,omitempty"`
	Name  string          `json:"name,omitempty"`
	Input json.RawMessage `json:"input,omitempty"`
}

type anthropicUsage struct {
	InputTokens  int `json:"input_tokens"`
	OutputTokens int `json:"output_tokens"`
}

// --- Anthropic streaming event structures ---

type anthropicStreamEvent struct {
	Type         string                      `json:"type"`
	Message      *anthropicStreamMessage     `json:"message,omitempty"`
	ContentBlock *anthropicStreamContentBlock `json:"content_block,omitempty"`
	Delta        *anthropicStreamDelta       `json:"delta,omitempty"`
	Usage        *anthropicUsage             `json:"usage,omitempty"`
	Index        int                         `json:"index,omitempty"`
}

type anthropicStreamMessage struct {
	ID    string          `json:"id"`
	Usage *anthropicUsage `json:"usage,omitempty"`
}

type anthropicStreamContentBlock struct {
	Type string `json:"type"`
	ID   string `json:"id,omitempty"`
	Name string `json:"name,omitempty"`
	Text string `json:"text,omitempty"`
}

type anthropicStreamDelta struct {
	Type        string `json:"type"`
	Text        string `json:"text,omitempty"`
	PartialJSON string `json:"partial_json,omitempty"`
}
