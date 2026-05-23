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

// OpenAIEndpoint is the default OpenAI chat completions URL.
const OpenAIEndpoint = "https://api.openai.com/v1/chat/completions"

// openaiClient implements Client against the OpenAI chat completions API.
type openaiClient struct {
	apiKey   string
	model    string
	endpoint string
	http     *http.Client
}

// NewOpenAI returns a Client backed by the OpenAI chat completions API.
// model is required (e.g. "gpt-4o"). apiKey is required.
func NewOpenAI(apiKey, model string) Client {
	return &openaiClient{
		apiKey:   apiKey,
		model:    model,
		endpoint: OpenAIEndpoint,
		http:     &http.Client{Timeout: 60 * time.Second},
	}
}

func (c *openaiClient) Provider() string { return "openai" }
func (c *openaiClient) Model() string    { return c.model }

func (c *openaiClient) Complete(ctx context.Context, req Request) (*Response, error) {
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

func (c *openaiClient) buildPayload(req Request) (map[string]any, error) {
	msgs := make([]map[string]any, 0, len(req.Messages)+1)
	if req.System != "" {
		msgs = append(msgs, map[string]any{
			"role":    "system",
			"content": req.System,
		})
	}
	for _, m := range req.Messages {
		if len(m.Images) > 0 {
			parts := make([]map[string]any, 0, len(m.Images)+1)
			if m.Content != "" {
				parts = append(parts, map[string]any{
					"type": "text",
					"text": m.Content,
				})
			}
			for _, img := range m.Images {
				url := "data:" + img.MediaType + ";base64," + base64.StdEncoding.EncodeToString(img.Data)
				parts = append(parts, map[string]any{
					"type":      "image_url",
					"image_url": map[string]any{"url": url},
				})
			}
			msgs = append(msgs, map[string]any{
				"role":    string(m.Role),
				"content": parts,
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
		"messages":   msgs,
		"max_tokens": req.MaxTokens,
	}
	if req.Temperature > 0 {
		payload["temperature"] = req.Temperature
	}

	if req.JSONSchema != nil {
		var schema map[string]any
		if err := json.Unmarshal(req.JSONSchema.Schema, &schema); err != nil {
			return nil, fmt.Errorf("openai: bad JSONSchema: %w", err)
		}
		payload["response_format"] = map[string]any{
			"type": "json_schema",
			"json_schema": map[string]any{
				"name":        req.JSONSchema.Name,
				"description": req.JSONSchema.Description,
				"schema":      schema,
				"strict":      true,
			},
		}
	}

	if len(req.Tools) > 0 {
		tools := make([]map[string]any, 0, len(req.Tools))
		for _, t := range req.Tools {
			var schema map[string]any
			if err := json.Unmarshal(t.Schema, &schema); err != nil {
				return nil, fmt.Errorf("openai: bad tool schema for %q: %w", t.Name, err)
			}
			tools = append(tools, map[string]any{
				"type": "function",
				"function": map[string]any{
					"name":        t.Name,
					"description": t.Description,
					"parameters":  schema,
				},
			})
		}
		payload["tools"] = tools
	}
	return payload, nil
}

func (c *openaiClient) post(ctx context.Context, payload map[string]any) ([]byte, error) {
	buf, err := json.Marshal(payload)
	if err != nil {
		return nil, err
	}
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, c.endpoint, bytes.NewReader(buf))
	if err != nil {
		return nil, err
	}
	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set("Authorization", "Bearer "+c.apiKey)

	resp, err := c.http.Do(httpReq)
	if err != nil {
		return nil, fmt.Errorf("openai: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}
	if resp.StatusCode >= 400 {
		return nil, fmt.Errorf("openai: http %d: %s", resp.StatusCode, string(body))
	}
	return body, nil
}

func (c *openaiClient) parseResponse(body []byte, expectJSON bool) (*Response, error) {
	var parsed struct {
		Model   string `json:"model"`
		Choices []struct {
			Message struct {
				Content   string `json:"content"`
				ToolCalls []struct {
					ID       string `json:"id"`
					Type     string `json:"type"`
					Function struct {
						Name      string `json:"name"`
						Arguments string `json:"arguments"`
					} `json:"function"`
				} `json:"tool_calls"`
			} `json:"message"`
			FinishReason string `json:"finish_reason"`
		} `json:"choices"`
		Usage struct {
			PromptTokens        int `json:"prompt_tokens"`
			CompletionTokens    int `json:"completion_tokens"`
			PromptTokensDetails struct {
				CachedTokens int `json:"cached_tokens"`
			} `json:"prompt_tokens_details"`
		} `json:"usage"`
	}
	if err := json.Unmarshal(body, &parsed); err != nil {
		return nil, fmt.Errorf("openai: decode: %w", err)
	}
	if len(parsed.Choices) == 0 {
		return nil, fmt.Errorf("openai: no choices in response")
	}
	choice := parsed.Choices[0]
	content := choice.Message.Content

	var toolCalls []ToolCall
	for _, tc := range choice.Message.ToolCalls {
		args := json.RawMessage(tc.Function.Arguments)
		if len(args) == 0 {
			args = json.RawMessage("{}")
		}
		toolCalls = append(toolCalls, ToolCall{Name: tc.Function.Name, Arguments: args})
	}

	if expectJSON {
		if content == "" {
			return nil, ErrInvalidJSON
		}
		var tmp any
		if err := json.Unmarshal([]byte(content), &tmp); err != nil {
			return nil, ErrInvalidJSON
		}
	}
	return &Response{
		Content:      content,
		InputTokens:  parsed.Usage.PromptTokens,
		OutputTokens: parsed.Usage.CompletionTokens,
		CachedTokens: parsed.Usage.PromptTokensDetails.CachedTokens,
		Model:        parsed.Model,
		Provider:     "openai",
		StopReason:   choice.FinishReason,
		ToolCalls:    toolCalls,
	}, nil
}

// Stream implements Streamer against the OpenAI chat completions SSE API.
//
// The payload is identical to Complete but with "stream": true and
// stream_options.include_usage=true so the final delta carries token counts.
// Each data: line is a JSON chat.completion.chunk with choices[0].delta.
// Text deltas (delta.content) are forwarded as Content chunks; tool_call
// argument fragments are accumulated keyed by index and emitted as ToolCalls
// when finish_reason="tool_calls" arrives. The sentinel "data: [DONE]" closes
// the stream and triggers a Done chunk with usage stats.
func (c *openaiClient) Stream(ctx context.Context, req Request) (<-chan StreamChunk, error) {
	if len(req.Messages) == 0 {
		return nil, ErrNoMessages
	}
	payload, err := c.buildPayload(req)
	if err != nil {
		return nil, err
	}
	payload["stream"] = true
	payload["stream_options"] = map[string]any{"include_usage": true}

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
	httpReq.Header.Set("Authorization", "Bearer "+c.apiKey)

	resp, err := c.http.Do(httpReq)
	if err != nil {
		return nil, fmt.Errorf("openai: %w", err)
	}
	if resp.StatusCode >= 400 {
		body, _ := io.ReadAll(resp.Body)
		resp.Body.Close()
		return nil, fmt.Errorf("openai: http %d: %s", resp.StatusCode, string(body))
	}

	ch := make(chan StreamChunk)
	go func() {
		defer resp.Body.Close()
		defer close(ch)

		scanner := bufio.NewScanner(resp.Body)
		scanner.Buffer(make([]byte, 0, 64*1024), 1024*1024)

		type toolAcc struct {
			name string
			args strings.Builder
		}
		// OpenAI sends tool_call fragments indexed by position.
		tools := map[int]*toolAcc{}

		var (
			textBuf      strings.Builder
			toolCalls    []ToolCall
			finishReason string
			modelName    = c.model
			usageIn      int
			usageOut     int
			usageCached  int
		)

		emit := func(chunk StreamChunk) bool {
			select {
			case <-ctx.Done():
				return false
			case ch <- chunk:
				return true
			}
		}

		flushTools := func() {
			// Sort by ascending index so order is stable.
			indexes := make([]int, 0, len(tools))
			for i := range tools {
				indexes = append(indexes, i)
			}
			// Tiny in-place sort to avoid an extra import.
			for i := 1; i < len(indexes); i++ {
				for j := i; j > 0 && indexes[j-1] > indexes[j]; j-- {
					indexes[j-1], indexes[j] = indexes[j], indexes[j-1]
				}
			}
			for _, idx := range indexes {
				t := tools[idx]
				args := json.RawMessage(t.args.String())
				if len(args) == 0 {
					args = json.RawMessage("{}")
				}
				tc := ToolCall{Name: t.name, Arguments: args}
				toolCalls = append(toolCalls, tc)
				emit(StreamChunk{ToolCall: &tc})
			}
			tools = map[int]*toolAcc{}
		}

		for scanner.Scan() {
			if ctx.Err() != nil {
				return
			}
			line := scanner.Text()
			if line == "" || !strings.HasPrefix(line, "data:") {
				continue
			}
			data := strings.TrimSpace(strings.TrimPrefix(line, "data:"))
			if data == "" {
				continue
			}
			if data == "[DONE]" {
				if len(tools) > 0 {
					flushTools()
				}
				if len(toolCalls) > 0 && finishReason == "" {
					finishReason = "tool_calls"
				}
				final := &Response{
					Content:      textBuf.String(),
					InputTokens:  usageIn,
					OutputTokens: usageOut,
					CachedTokens: usageCached,
					Model:        modelName,
					Provider:     "openai",
					StopReason:   finishReason,
					ToolCalls:    toolCalls,
				}
				emit(StreamChunk{Done: true, Final: final})
				return
			}

			var chunk struct {
				Model   string `json:"model"`
				Choices []struct {
					Delta struct {
						Content   string `json:"content"`
						ToolCalls []struct {
							Index    int    `json:"index"`
							ID       string `json:"id"`
							Function struct {
								Name      string `json:"name"`
								Arguments string `json:"arguments"`
							} `json:"function"`
						} `json:"tool_calls"`
					} `json:"delta"`
					FinishReason string `json:"finish_reason"`
				} `json:"choices"`
				Usage *struct {
					PromptTokens        int `json:"prompt_tokens"`
					CompletionTokens    int `json:"completion_tokens"`
					PromptTokensDetails struct {
						CachedTokens int `json:"cached_tokens"`
					} `json:"prompt_tokens_details"`
				} `json:"usage"`
			}
			if err := json.Unmarshal([]byte(data), &chunk); err != nil {
				continue
			}
			if chunk.Model != "" {
				modelName = chunk.Model
			}
			if chunk.Usage != nil {
				usageIn = chunk.Usage.PromptTokens
				usageOut = chunk.Usage.CompletionTokens
				usageCached = chunk.Usage.PromptTokensDetails.CachedTokens
			}
			for _, ch0 := range chunk.Choices {
				if ch0.Delta.Content != "" {
					textBuf.WriteString(ch0.Delta.Content)
					if !emit(StreamChunk{Content: ch0.Delta.Content}) {
						return
					}
				}
				for _, tc := range ch0.Delta.ToolCalls {
					acc, ok := tools[tc.Index]
					if !ok {
						acc = &toolAcc{}
						tools[tc.Index] = acc
					}
					if tc.Function.Name != "" {
						acc.name = tc.Function.Name
					}
					if tc.Function.Arguments != "" {
						acc.args.WriteString(tc.Function.Arguments)
					}
				}
				if ch0.FinishReason != "" {
					finishReason = ch0.FinishReason
					if ch0.FinishReason == "tool_calls" {
						flushTools()
					}
				}
			}
		}

		if err := scanner.Err(); err != nil && ctx.Err() == nil {
			emit(StreamChunk{Done: true, Err: fmt.Errorf("openai: stream read: %w", err)})
		}
	}()

	return ch, nil
}
