package llm

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"
)

// AnthropicEndpoint is the default Anthropic Messages API URL.
const AnthropicEndpoint = "https://api.anthropic.com/v1/messages"

// AnthropicVersion is the Anthropic API version header value.
const AnthropicVersion = "2023-06-01"

// anthropicClient implements Client against the Anthropic Messages API.
type anthropicClient struct {
	apiKey   string
	model    string
	endpoint string
	http     *http.Client
}

// NewAnthropic returns a Client backed by the Anthropic Messages API.
// model is required (e.g. "claude-sonnet-4-6"). apiKey is required.
func NewAnthropic(apiKey, model string) Client {
	return &anthropicClient{
		apiKey:   apiKey,
		model:    model,
		endpoint: AnthropicEndpoint,
		http:     &http.Client{Timeout: 60 * time.Second},
	}
}

func (c *anthropicClient) Provider() string { return "anthropic" }
func (c *anthropicClient) Model() string    { return c.model }

func (c *anthropicClient) Complete(ctx context.Context, req Request) (*Response, error) {
	if len(req.Messages) == 0 {
		return nil, ErrNoMessages
	}

	payload, err := c.buildPayload(req)
	if err != nil {
		return nil, err
	}

	body, err := c.post(ctx, payload)
	if err != nil {
		return nil, err
	}

	return c.parseResponse(body, req.JSONSchema != nil)
}

func (c *anthropicClient) buildPayload(req Request) (map[string]any, error) {
	msgs := make([]map[string]any, 0, len(req.Messages))
	for _, m := range req.Messages {
		msgs = append(msgs, map[string]any{
			"role":    string(m.Role),
			"content": m.Content,
		})
	}

	payload := map[string]any{
		"model":      c.model,
		"max_tokens": req.MaxTokens,
		"messages":   msgs,
	}
	if req.Temperature > 0 {
		payload["temperature"] = req.Temperature
	}

	if req.System != "" {
		if req.CacheSystem {
			payload["system"] = []map[string]any{{
				"type":          "text",
				"text":          req.System,
				"cache_control": map[string]any{"type": "ephemeral"},
			}}
		} else {
			payload["system"] = req.System
		}
	}

	if req.JSONSchema != nil {
		var schema map[string]any
		if err := json.Unmarshal(req.JSONSchema.Schema, &schema); err != nil {
			return nil, fmt.Errorf("anthropic: bad JSONSchema: %w", err)
		}
		payload["tools"] = []map[string]any{{
			"name":         req.JSONSchema.Name,
			"description":  req.JSONSchema.Description,
			"input_schema": schema,
		}}
		payload["tool_choice"] = map[string]any{
			"type": "tool",
			"name": req.JSONSchema.Name,
		}
	}
	return payload, nil
}

func (c *anthropicClient) post(ctx context.Context, payload map[string]any) ([]byte, error) {
	buf, err := json.Marshal(payload)
	if err != nil {
		return nil, err
	}
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, c.endpoint, bytes.NewReader(buf))
	if err != nil {
		return nil, err
	}
	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set("x-api-key", c.apiKey)
	httpReq.Header.Set("anthropic-version", AnthropicVersion)

	resp, err := c.http.Do(httpReq)
	if err != nil {
		return nil, fmt.Errorf("anthropic: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}
	if resp.StatusCode >= 400 {
		return nil, fmt.Errorf("anthropic: http %d: %s", resp.StatusCode, string(body))
	}
	return body, nil
}

func (c *anthropicClient) parseResponse(body []byte, expectJSON bool) (*Response, error) {
	var parsed struct {
		Content []struct {
			Type  string          `json:"type"`
			Text  string          `json:"text,omitempty"`
			Name  string          `json:"name,omitempty"`
			Input json.RawMessage `json:"input,omitempty"`
		} `json:"content"`
		StopReason string `json:"stop_reason"`
		Model      string `json:"model"`
		Usage      struct {
			InputTokens              int `json:"input_tokens"`
			OutputTokens             int `json:"output_tokens"`
			CacheCreationInputTokens int `json:"cache_creation_input_tokens"`
			CacheReadInputTokens     int `json:"cache_read_input_tokens"`
		} `json:"usage"`
	}
	if err := json.Unmarshal(body, &parsed); err != nil {
		return nil, fmt.Errorf("anthropic: decode: %w", err)
	}

	content := ""
	for _, b := range parsed.Content {
		switch b.Type {
		case "text":
			content += b.Text
		case "tool_use":
			if expectJSON && len(b.Input) > 0 {
				content = string(b.Input)
			}
		}
	}
	if expectJSON && content == "" {
		return nil, ErrInvalidJSON
	}

	return &Response{
		Content:      content,
		InputTokens:  parsed.Usage.InputTokens,
		OutputTokens: parsed.Usage.OutputTokens,
		CachedTokens: parsed.Usage.CacheReadInputTokens,
		Model:        parsed.Model,
		Provider:     "anthropic",
		StopReason:   parsed.StopReason,
	}, nil
}
