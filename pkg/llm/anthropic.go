package llm

import (
	"bufio"
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
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
			toolCalls = append(toolCalls, ToolCall{Name: b.Name, Arguments: args})
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

// Stream implements Streamer for Anthropic via Messages SSE.
//
// It sends the same payload as Complete with "stream": true and parses the
// resulting Server-Sent Events stream. Text deltas (content_block_delta with
// delta.type=text_delta) are forwarded as Content chunks immediately. tool_use
// blocks accumulate their input_json_delta fragments and are emitted as a
// single ToolCall when the matching content_block_stop arrives. The final
// chunk (Done=true) carries a synthesized Response with accumulated text,
// token usage, and stop_reason from the message_delta / message_stop events.
func (c *anthropicClient) Stream(ctx context.Context, req Request) (<-chan StreamChunk, error) {
	if len(req.Messages) == 0 {
		return nil, ErrNoMessages
	}
	payload, err := c.buildPayload(req)
	if err != nil {
		return nil, err
	}
	payload["stream"] = true

	buf, err := json.Marshal(payload)
	if err != nil {
		return nil, err
	}
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, c.endpoint, bytes.NewReader(buf))
	if err != nil {
		return nil, err
	}
	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set("Accept", "text/event-stream")
	httpReq.Header.Set("x-api-key", c.apiKey)
	httpReq.Header.Set("anthropic-version", AnthropicVersion)

	resp, err := c.http.Do(httpReq)
	if err != nil {
		return nil, fmt.Errorf("anthropic: %w", err)
	}
	if resp.StatusCode >= 400 {
		body, _ := io.ReadAll(resp.Body)
		resp.Body.Close()
		return nil, fmt.Errorf("anthropic: http %d: %s", resp.StatusCode, string(body))
	}

	ch := make(chan StreamChunk)
	go func() {
		defer resp.Body.Close()
		defer close(ch)

		scanner := bufio.NewScanner(resp.Body)
		// Anthropic events can exceed the default 64KB Scanner buffer when
		// tool inputs are large or when many blocks are joined; bump it.
		scanner.Buffer(make([]byte, 0, 64*1024), 1024*1024)

		// Per-block accumulators for tool_use blocks. Anthropic provides a
		// content_block_start with the tool name and id, then a series of
		// input_json_delta events, then a content_block_stop. We hold the
		// partial JSON keyed by block index.
		type toolAcc struct {
			name string
			args strings.Builder
		}
		tools := map[int]*toolAcc{}

		var (
			textBuf      strings.Builder
			toolCalls    []ToolCall
			usageIn      int
			usageOut     int
			usageCached  int
			stopReason   string
			modelName    = c.model
			currentEvent string
		)

		emit := func(chunk StreamChunk) bool {
			select {
			case <-ctx.Done():
				return false
			case ch <- chunk:
				return true
			}
		}

		for scanner.Scan() {
			if ctx.Err() != nil {
				return
			}
			line := scanner.Text()
			if line == "" {
				currentEvent = ""
				continue
			}
			if strings.HasPrefix(line, "event:") {
				currentEvent = strings.TrimSpace(strings.TrimPrefix(line, "event:"))
				continue
			}
			if !strings.HasPrefix(line, "data:") {
				continue
			}
			data := strings.TrimSpace(strings.TrimPrefix(line, "data:"))
			if data == "" {
				continue
			}

			switch currentEvent {
			case "message_start":
				var ev struct {
					Message struct {
						Model string `json:"model"`
						Usage struct {
							InputTokens          int `json:"input_tokens"`
							CacheReadInputTokens int `json:"cache_read_input_tokens"`
						} `json:"usage"`
					} `json:"message"`
				}
				if err := json.Unmarshal([]byte(data), &ev); err == nil {
					if ev.Message.Model != "" {
						modelName = ev.Message.Model
					}
					if ev.Message.Usage.InputTokens > 0 {
						usageIn = ev.Message.Usage.InputTokens
					}
					if ev.Message.Usage.CacheReadInputTokens > 0 {
						usageCached = ev.Message.Usage.CacheReadInputTokens
					}
				}
			case "content_block_start":
				var ev struct {
					Index        int `json:"index"`
					ContentBlock struct {
						Type string `json:"type"`
						Name string `json:"name"`
					} `json:"content_block"`
				}
				if err := json.Unmarshal([]byte(data), &ev); err == nil {
					if ev.ContentBlock.Type == "tool_use" {
						tools[ev.Index] = &toolAcc{name: ev.ContentBlock.Name}
					}
				}
			case "content_block_delta":
				var ev struct {
					Index int `json:"index"`
					Delta struct {
						Type        string `json:"type"`
						Text        string `json:"text"`
						PartialJSON string `json:"partial_json"`
					} `json:"delta"`
				}
				if err := json.Unmarshal([]byte(data), &ev); err != nil {
					continue
				}
				switch ev.Delta.Type {
				case "text_delta":
					if ev.Delta.Text != "" {
						textBuf.WriteString(ev.Delta.Text)
						if !emit(StreamChunk{Content: ev.Delta.Text}) {
							return
						}
					}
				case "input_json_delta":
					if t, ok := tools[ev.Index]; ok {
						t.args.WriteString(ev.Delta.PartialJSON)
					}
				}
			case "content_block_stop":
				var ev struct {
					Index int `json:"index"`
				}
				if err := json.Unmarshal([]byte(data), &ev); err != nil {
					continue
				}
				if t, ok := tools[ev.Index]; ok {
					args := json.RawMessage(t.args.String())
					if len(args) == 0 {
						args = json.RawMessage("{}")
					}
					tc := ToolCall{Name: t.name, Arguments: args}
					toolCalls = append(toolCalls, tc)
					delete(tools, ev.Index)
					if !emit(StreamChunk{ToolCall: &tc}) {
						return
					}
				}
			case "message_delta":
				var ev struct {
					Delta struct {
						StopReason string `json:"stop_reason"`
					} `json:"delta"`
					Usage struct {
						OutputTokens int `json:"output_tokens"`
					} `json:"usage"`
				}
				if err := json.Unmarshal([]byte(data), &ev); err == nil {
					if ev.Delta.StopReason != "" {
						stopReason = ev.Delta.StopReason
					}
					if ev.Usage.OutputTokens > 0 {
						usageOut = ev.Usage.OutputTokens
					}
				}
			case "message_stop":
				if len(toolCalls) > 0 && stopReason == "" {
					stopReason = "tool_use"
				}
				final := &Response{
					Content:      textBuf.String(),
					InputTokens:  usageIn,
					OutputTokens: usageOut,
					CachedTokens: usageCached,
					Model:        modelName,
					Provider:     "anthropic",
					StopReason:   stopReason,
					ToolCalls:    toolCalls,
				}
				emit(StreamChunk{Done: true, Final: final})
				return
			}
		}

		if err := scanner.Err(); err != nil && ctx.Err() == nil {
			emit(StreamChunk{Done: true, Err: fmt.Errorf("anthropic: stream read: %w", err)})
		}
	}()

	return ch, nil
}
