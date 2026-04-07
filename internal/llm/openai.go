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

// OpenAIProvider implements Provider for OpenAI-compatible APIs.
type OpenAIProvider struct {
	name    string
	baseURL string
	apiKey  string
	model   string
	client  *http.Client
}

// NewOpenAIProvider creates a provider configured from the given ProviderConfig.
func NewOpenAIProvider(cfg config.ProviderConfig) (*OpenAIProvider, error) {
	return &OpenAIProvider{
		name:    cfg.Name,
		baseURL: strings.TrimRight(cfg.BaseURL, "/"),
		apiKey:  cfg.APIKey,
		model:   cfg.Model,
		client:  &http.Client{Timeout: cfg.Timeout},
	}, nil
}

func (p *OpenAIProvider) Name() string { return p.name }

// Chat sends a non-streaming chat completion request.
func (p *OpenAIProvider) Chat(ctx context.Context, req *ChatRequest) (*ChatResponse, error) {
	body := p.buildRequestBody(req, false)

	respBody, err := p.doRequest(ctx, body)
	if err != nil {
		return nil, err
	}
	defer respBody.Close()

	var oaiResp openAIResponse
	if err := json.NewDecoder(respBody).Decode(&oaiResp); err != nil {
		return nil, models.NewAPIError(models.ErrProviderError, fmt.Sprintf("decode response: %v", err))
	}

	if len(oaiResp.Choices) == 0 {
		return nil, models.NewAPIError(models.ErrProviderError, "no choices in response")
	}

	choice := oaiResp.Choices[0]
	return &ChatResponse{
		Content:      choice.Message.Content,
		ToolCalls:    convertToolCalls(choice.Message.ToolCalls),
		Usage:        oaiResp.Usage,
		FinishReason: choice.FinishReason,
	}, nil
}

// ChatStream sends a streaming chat completion request, returning chunks via channel.
func (p *OpenAIProvider) ChatStream(ctx context.Context, req *ChatRequest) (<-chan StreamChunk, error) {
	body := p.buildRequestBody(req, true)

	respBody, err := p.doRequest(ctx, body)
	if err != nil {
		return nil, err
	}

	ch := make(chan StreamChunk)
	go func() {
		defer close(ch)
		defer respBody.Close()
		p.readSSEStream(ctx, respBody, ch)
	}()

	return ch, nil
}

func (p *OpenAIProvider) buildRequestBody(req *ChatRequest, stream bool) map[string]interface{} {
	model := req.Model
	if model == "" {
		model = p.model
	}

	body := map[string]interface{}{
		"model":    model,
		"messages": toOpenAIMessages(req.Messages),
		"stream":   stream,
	}

	if req.Temperature > 0 {
		body["temperature"] = req.Temperature
	}
	if req.MaxTokens > 0 {
		body["max_tokens"] = req.MaxTokens
	}
	if len(req.Tools) > 0 {
		body["tools"] = req.Tools
	}
	if stream {
		body["stream_options"] = map[string]interface{}{"include_usage": true}
	}

	return body
}

func (p *OpenAIProvider) doRequest(ctx context.Context, body map[string]interface{}) (io.ReadCloser, error) {
	jsonBody, err := json.Marshal(body)
	if err != nil {
		return nil, models.NewAPIError(models.ErrInternal, fmt.Sprintf("marshal request: %v", err))
	}

	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, p.baseURL+"/chat/completions", bytes.NewReader(jsonBody))
	if err != nil {
		return nil, models.NewAPIError(models.ErrInternal, fmt.Sprintf("create request: %v", err))
	}

	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set("Authorization", "Bearer "+p.apiKey)

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

func (p *OpenAIProvider) handleErrorResponse(resp *http.Response) error {
	bodyBytes, _ := io.ReadAll(resp.Body)

	var oaiErr struct {
		Error struct {
			Message string `json:"message"`
		} `json:"error"`
	}
	_ = json.Unmarshal(bodyBytes, &oaiErr)
	msg := oaiErr.Error.Message
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

func (p *OpenAIProvider) readSSEStream(ctx context.Context, body io.Reader, ch chan<- StreamChunk) {
	// Accumulates streamed tool calls by index
	toolCallAccum := make(map[int]*openAIToolCall)

	scanner := bufio.NewScanner(body)
	for scanner.Scan() {
		line := scanner.Text()

		if line == "" || strings.HasPrefix(line, "event:") {
			continue
		}

		if !strings.HasPrefix(line, "data: ") {
			continue
		}
		data := strings.TrimPrefix(line, "data: ")

		if data == "[DONE]" {
			// Emit accumulated tool calls once at the end
			if len(toolCallAccum) > 0 {
				var tcs []models.ToolCall
				for i := 0; i < len(toolCallAccum); i++ {
					tc := toolCallAccum[i]
					tcs = append(tcs, models.ToolCall{
						ID:        tc.ID,
						Name:      tc.Function.Name,
						Arguments: json.RawMessage(tc.Function.Arguments),
					})
				}
				select {
				case ch <- StreamChunk{ToolCalls: tcs, Done: true}:
				case <-ctx.Done():
					ch <- StreamChunk{Err: ctx.Err()}
				}
			}
			return
		}

		var chunk openAIStreamChunk
		if err := json.Unmarshal([]byte(data), &chunk); err != nil {
			ch <- StreamChunk{Err: fmt.Errorf("decode stream chunk: %w", err)}
			return
		}

		sc := StreamChunk{}

		if chunk.Usage != nil {
			sc.Usage = chunk.Usage
		}

		if len(chunk.Choices) > 0 {
			delta := chunk.Choices[0].Delta
			sc.Delta = delta.Content

			// Accumulate tool call deltas without emitting yet
			for _, tc := range delta.ToolCalls {
				existing, ok := toolCallAccum[tc.Index]
				if !ok {
					existing = &openAIToolCall{
						ID:   tc.ID,
						Type: tc.Type,
					}
					toolCallAccum[tc.Index] = existing
				}
				if tc.ID != "" {
					existing.ID = tc.ID
				}
				if tc.Function.Name != "" {
					existing.Function.Name = tc.Function.Name
				}
				existing.Function.Arguments += tc.Function.Arguments
			}
		}

		// Only send text deltas and usage; tool calls are batched at [DONE]
		if sc.Delta != "" || sc.Usage != nil {
			select {
			case ch <- sc:
			case <-ctx.Done():
				ch <- StreamChunk{Err: ctx.Err()}
				return
			}
		}
	}

	if err := scanner.Err(); err != nil {
		ch <- StreamChunk{Err: fmt.Errorf("read stream: %w", err)}
	}
}

// --- Message format conversion ---

func toOpenAIMessages(msgs []models.Message) []map[string]interface{} {
	out := make([]map[string]interface{}, 0, len(msgs))
	for _, m := range msgs {
		msg := map[string]interface{}{
			"role":    m.Role,
			"content": m.Content,
		}

		if len(m.ToolCalls) > 0 {
			tcs := make([]map[string]interface{}, 0, len(m.ToolCalls))
			for _, tc := range m.ToolCalls {
				tcs = append(tcs, map[string]interface{}{
					"id":   tc.ID,
					"type": "function",
					"function": map[string]interface{}{
						"name":      tc.Name,
						"arguments": string(tc.Arguments),
					},
				})
			}
			msg["tool_calls"] = tcs
		}

		if m.ToolCallID != "" {
			msg["tool_call_id"] = m.ToolCallID
		}

		out = append(out, msg)
	}
	return out
}

// --- OpenAI API response structures ---

type openAIResponse struct {
	Choices []openAIChoice `json:"choices"`
	Usage   models.Usage   `json:"usage"`
}

type openAIChoice struct {
	Message      openAIMessage `json:"message"`
	FinishReason string        `json:"finish_reason"`
}

type openAIMessage struct {
	Role      string           `json:"role"`
	Content   string           `json:"content"`
	ToolCalls []openAIToolCall `json:"tool_calls,omitempty"`
}

type openAIToolCall struct {
	ID       string             `json:"id"`
	Type     string             `json:"type"`
	Function openAIFunctionCall `json:"function"`
}

type openAIFunctionCall struct {
	Name      string `json:"name"`
	Arguments string `json:"arguments"`
}

type openAIStreamChunk struct {
	Choices []openAIStreamChoice `json:"choices"`
	Usage   *models.Usage        `json:"usage,omitempty"`
}

type openAIStreamChoice struct {
	Delta openAIStreamDelta `json:"delta"`
}

type openAIStreamDelta struct {
	Role      string                   `json:"role,omitempty"`
	Content   string                   `json:"content,omitempty"`
	ToolCalls []openAIStreamToolCall   `json:"tool_calls,omitempty"`
}

type openAIStreamToolCall struct {
	Index    int                `json:"index"`
	ID       string             `json:"id,omitempty"`
	Type     string             `json:"type,omitempty"`
	Function openAIFunctionCall `json:"function"`
}

func convertToolCalls(oaiTCs []openAIToolCall) []models.ToolCall {
	if len(oaiTCs) == 0 {
		return nil
	}
	tcs := make([]models.ToolCall, 0, len(oaiTCs))
	for _, tc := range oaiTCs {
		tcs = append(tcs, models.ToolCall{
			ID:        tc.ID,
			Name:      tc.Function.Name,
			Arguments: json.RawMessage(tc.Function.Arguments),
		})
	}
	return tcs
}
