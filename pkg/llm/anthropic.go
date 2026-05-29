package llm

import (
	"bytes"
	"context"
	"encoding/base64"
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

	// Anthropic has no sampler-level grammar API; structural guarantees come
	// through the JSONSchema (tool-use) path instead. If the caller set
	// Request.Grammar anyway, warn once and proceed — the JSONSchema path
	// (if set) and the agent retry-on-bad-JSON fallback are the backstops.
	if req.Grammar != nil {
		warnGrammarUnsupported(c.Provider())
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
		// --- user message returning tool results ---------------------
		// Serialize as a content array of tool_result blocks (Anthropic
		// requires these to be the sole content of a user message turn).
		if len(m.ToolResults) > 0 {
			blocks := make([]map[string]any, 0, len(m.ToolResults))
			for _, tr := range m.ToolResults {
				blocks = append(blocks, map[string]any{
					"type":        "tool_result",
					"tool_use_id": tr.ToolUseID,
					"content":     tr.Content,
				})
			}
			msgs = append(msgs, map[string]any{
				"role":    string(m.Role),
				"content": blocks,
			})
			continue
		}

		// --- assistant message that called tools ---------------------
		// Serialize text + tool_use blocks so the next user turn can
		// include matching tool_result blocks.
		if len(m.ToolCalls) > 0 {
			blocks := make([]map[string]any, 0, len(m.ToolCalls)+1)
			if m.Content != "" {
				blocks = append(blocks, map[string]any{
					"type": "text",
					"text": m.Content,
				})
			}
			for _, tc := range m.ToolCalls {
				var input any
				if len(tc.Arguments) > 0 {
					_ = json.Unmarshal(tc.Arguments, &input)
				}
				if input == nil {
					input = map[string]any{}
				}
				blocks = append(blocks, map[string]any{
					"type":  "tool_use",
					"id":    tc.ID,
					"name":  tc.Name,
					"input": input,
				})
			}
			msgs = append(msgs, map[string]any{
				"role":    string(m.Role),
				"content": blocks,
			})
			continue
		}

		// --- user message with images --------------------------------
		if len(m.Images) > 0 {
			blocks := make([]map[string]any, 0, len(m.Images)+1)
			for _, img := range m.Images {
				blocks = append(blocks, map[string]any{
					"type": "image",
					"source": map[string]any{
						"type":       "base64",
						"media_type": img.MediaType,
						"data":       base64.StdEncoding.EncodeToString(img.Data),
					},
				})
			}
			if m.Content != "" {
				blocks = append(blocks, map[string]any{
					"type": "text",
					"text": m.Content,
				})
			}
			msgs = append(msgs, map[string]any{
				"role":    string(m.Role),
				"content": blocks,
			})
			continue
		}

		// --- plain text message -------------------------------------
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
	} else if len(req.Tools) > 0 {
		tools := make([]map[string]any, 0, len(req.Tools))
		for _, t := range req.Tools {
			var schema map[string]any
			if err := json.Unmarshal(t.Schema, &schema); err != nil {
				return nil, fmt.Errorf("anthropic: bad tool schema for %q: %w", t.Name, err)
			}
			tools = append(tools, map[string]any{
				"name":         t.Name,
				"description":  t.Description,
				"input_schema": schema,
			})
		}
		payload["tools"] = tools
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
			ID    string          `json:"id,omitempty"`   // tool_use correlation ID
			Name  string          `json:"name,omitempty"` // tool_use function name
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
	var toolCalls []ToolCall
	for _, b := range parsed.Content {
		switch b.Type {
		case "text":
			content += b.Text
		case "tool_use":
			if expectJSON && len(b.Input) > 0 {
				content = string(b.Input)
				continue
			}
			args := b.Input
			if len(args) == 0 {
				args = json.RawMessage("{}")
			}
			// Capture the provider-generated ID so callers can correlate
			// tool_result blocks in the next conversation turn.
			toolCalls = append(toolCalls, ToolCall{ID: b.ID, Name: b.Name, Arguments: args})
		}
	}
	if expectJSON && content == "" {
		return nil, ErrInvalidJSON
	}

	stop := parsed.StopReason
	if len(toolCalls) > 0 {
		stop = "tool_use"
	}

	return &Response{
		Content:      content,
		InputTokens:  parsed.Usage.InputTokens,
		OutputTokens: parsed.Usage.OutputTokens,
		CachedTokens: parsed.Usage.CacheReadInputTokens,
		Model:        parsed.Model,
		Provider:     "anthropic",
		StopReason:   stop,
		ToolCalls:    toolCalls,
	}, nil
}
