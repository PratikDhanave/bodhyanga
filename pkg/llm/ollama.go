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

// OllamaDefaultHost is the local Ollama URL when none is configured.
const OllamaDefaultHost = "http://localhost:11434"

// ollamaChatPath is Ollama's native chat endpoint.
const ollamaChatPath = "/api/chat"

// ollamaClient implements Client against a local (or remote) Ollama server
// using its native /api/chat endpoint. Ollama models are local and run with
// no API key; structured outputs are requested via the `format` field, which
// accepts a JSON Schema document in Ollama 0.5+.
type ollamaClient struct {
	host  string
	model string
	http  *http.Client
}

// NewOllama returns a Client backed by an Ollama server. host should be a
// scheme://host:port URL such as "http://localhost:11434" (empty falls back
// to OllamaDefaultHost). model is required, e.g. "llama3.2", "mistral",
// "qwen2.5:14b".
func NewOllama(host, model string) Client {
	if host == "" {
		host = OllamaDefaultHost
	}
	host = strings.TrimRight(host, "/")
	return &ollamaClient{
		host:  host,
		model: model,
		http:  &http.Client{Timeout: 120 * time.Second},
	}
}

func (c *ollamaClient) Provider() string { return "ollama" }
func (c *ollamaClient) Model() string    { return c.model }

func (c *ollamaClient) Complete(ctx context.Context, req Request) (*Response, error) {
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

func (c *ollamaClient) buildPayload(req Request) (map[string]any, error) {
	msgs := make([]map[string]any, 0, len(req.Messages)+1)
	if req.System != "" {
		msgs = append(msgs, map[string]any{
			"role":    "system",
			"content": req.System,
		})
	}
	for _, m := range req.Messages {
		entry := map[string]any{
			"role":    string(m.Role),
			"content": m.Content,
		}
		if len(m.Images) > 0 {
			imgs := make([]string, 0, len(m.Images))
			for _, img := range m.Images {
				imgs = append(imgs, base64.StdEncoding.EncodeToString(img.Data))
			}
			entry["images"] = imgs
		}
		msgs = append(msgs, entry)
	}

	options := map[string]any{}
	if req.Temperature > 0 {
		options["temperature"] = req.Temperature
	}
	if req.MaxTokens > 0 {
		options["num_predict"] = req.MaxTokens
	}

	payload := map[string]any{
		"model":    c.model,
		"messages": msgs,
		"stream":   false,
	}
	if len(options) > 0 {
		payload["options"] = options
	}

	if req.JSONSchema != nil {
		// Validate the schema is well-formed JSON, then ask Ollama for a
		// free-form JSON object. Older Ollama servers (0.4.x and earlier)
		// only accept format="json"; newer ones (0.5+) accept a JSON Schema
		// object, but it's not yet universally available. We send "json" for
		// cross-version compatibility; the schema still lives in the system
		// prompt so the model knows the expected shape, and the response is
		// validated client-side in parseResponse.
		if err := json.Unmarshal(req.JSONSchema.Schema, new(map[string]any)); err != nil {
			return nil, fmt.Errorf("ollama: bad JSONSchema: %w", err)
		}
		payload["format"] = "json"
	}

	if len(req.Tools) > 0 {
		tools := make([]map[string]any, 0, len(req.Tools))
		for _, t := range req.Tools {
			var schema map[string]any
			if err := json.Unmarshal(t.Schema, &schema); err != nil {
				return nil, fmt.Errorf("ollama: bad tool schema for %q: %w", t.Name, err)
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

func (c *ollamaClient) post(ctx context.Context, payload map[string]any) ([]byte, error) {
	buf, err := json.Marshal(payload)
	if err != nil {
		return nil, err
	}
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, c.host+ollamaChatPath, bytes.NewReader(buf))
	if err != nil {
		return nil, err
	}
	httpReq.Header.Set("Content-Type", "application/json")

	resp, err := c.http.Do(httpReq)
	if err != nil {
		return nil, fmt.Errorf("ollama: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}
	if resp.StatusCode >= 400 {
		return nil, fmt.Errorf("ollama: http %d: %s", resp.StatusCode, string(body))
	}
	return body, nil
}

func (c *ollamaClient) parseResponse(body []byte, expectJSON bool) (*Response, error) {
	var parsed struct {
		Model   string `json:"model"`
		Message struct {
			Role      string `json:"role"`
			Content   string `json:"content"`
			ToolCalls []struct {
				Function struct {
					Name      string          `json:"name"`
					Arguments json.RawMessage `json:"arguments"`
				} `json:"function"`
			} `json:"tool_calls"`
		} `json:"message"`
		DoneReason      string `json:"done_reason"`
		PromptEvalCount int    `json:"prompt_eval_count"`
		EvalCount       int    `json:"eval_count"`
	}
	if err := json.Unmarshal(body, &parsed); err != nil {
		return nil, fmt.Errorf("ollama: decode: %w", err)
	}
	content := parsed.Message.Content

	var toolCalls []ToolCall
	for _, tc := range parsed.Message.ToolCalls {
		args := tc.Function.Arguments
		if len(args) == 0 {
			args = json.RawMessage("{}")
		} else {
			// Re-marshal to normalize whitespace and confirm it's valid JSON.
			var raw any
			if err := json.Unmarshal(args, &raw); err == nil {
				if buf, err := json.Marshal(raw); err == nil {
					args = buf
				}
			}
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
	stop := parsed.DoneReason
	if stop == "" {
		stop = "stop"
	}
	if len(toolCalls) > 0 {
		stop = "tool_use"
	}
	return &Response{
		Content:      content,
		InputTokens:  parsed.PromptEvalCount,
		OutputTokens: parsed.EvalCount,
		CachedTokens: 0,
		Model:        parsed.Model,
		Provider:     "ollama",
		StopReason:   stop,
		ToolCalls:    toolCalls,
	}, nil
}

// Stream implements Streamer against Ollama's native /api/chat NDJSON stream.
//
// Ollama's chat endpoint streams by default — each newline-delimited JSON
// object is a delta: {"message":{"content":"..."},"done":false} until the
// final {"done":true} which carries the done_reason and aggregate token
// counts. Tool calls arrive on the final object (rather than streamed
// incrementally) and are emitted as ToolCall chunks before the Done chunk.
func (c *ollamaClient) Stream(ctx context.Context, req Request) (<-chan StreamChunk, error) {
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
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, c.host+ollamaChatPath, bytes.NewReader(buf))
	if err != nil {
		return nil, err
	}
	httpReq.Header.Set("Content-Type", "application/json")

	resp, err := c.http.Do(httpReq)
	if err != nil {
		return nil, fmt.Errorf("ollama: %w", err)
	}
	if resp.StatusCode >= 400 {
		body, _ := io.ReadAll(resp.Body)
		resp.Body.Close()
		return nil, fmt.Errorf("ollama: http %d: %s", resp.StatusCode, string(body))
	}

	ch := make(chan StreamChunk)
	go func() {
		defer resp.Body.Close()
		defer close(ch)

		scanner := bufio.NewScanner(resp.Body)
		scanner.Buffer(make([]byte, 0, 64*1024), 1024*1024)

		var (
			textBuf     strings.Builder
			toolCalls   []ToolCall
			usageIn     int
			usageOut    int
			modelName   = c.model
			doneReason  string
			finalEmitted bool
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
			line := bytes.TrimSpace(scanner.Bytes())
			if len(line) == 0 {
				continue
			}
			var ev struct {
				Model   string `json:"model"`
				Message struct {
					Content   string `json:"content"`
					ToolCalls []struct {
						Function struct {
							Name      string          `json:"name"`
							Arguments json.RawMessage `json:"arguments"`
						} `json:"function"`
					} `json:"tool_calls"`
				} `json:"message"`
				Done            bool   `json:"done"`
				DoneReason      string `json:"done_reason"`
				PromptEvalCount int    `json:"prompt_eval_count"`
				EvalCount       int    `json:"eval_count"`
			}
			if err := json.Unmarshal(line, &ev); err != nil {
				continue
			}
			if ev.Model != "" {
				modelName = ev.Model
			}
			if ev.Message.Content != "" {
				textBuf.WriteString(ev.Message.Content)
				if !emit(StreamChunk{Content: ev.Message.Content}) {
					return
				}
			}
			for _, tc := range ev.Message.ToolCalls {
				args := tc.Function.Arguments
				if len(args) == 0 {
					args = json.RawMessage("{}")
				} else {
					var raw any
					if err := json.Unmarshal(args, &raw); err == nil {
						if buf, err := json.Marshal(raw); err == nil {
							args = buf
						}
					}
				}
				call := ToolCall{Name: tc.Function.Name, Arguments: args}
				toolCalls = append(toolCalls, call)
				if !emit(StreamChunk{ToolCall: &call}) {
					return
				}
			}
			if ev.Done {
				if ev.PromptEvalCount > 0 {
					usageIn = ev.PromptEvalCount
				}
				if ev.EvalCount > 0 {
					usageOut = ev.EvalCount
				}
				doneReason = ev.DoneReason
				if doneReason == "" {
					doneReason = "stop"
				}
				if len(toolCalls) > 0 {
					doneReason = "tool_use"
				}
				final := &Response{
					Content:      textBuf.String(),
					InputTokens:  usageIn,
					OutputTokens: usageOut,
					Model:        modelName,
					Provider:     "ollama",
					StopReason:   doneReason,
					ToolCalls:    toolCalls,
				}
				emit(StreamChunk{Done: true, Final: final})
				finalEmitted = true
				return
			}
		}

		if err := scanner.Err(); err != nil && ctx.Err() == nil && !finalEmitted {
			emit(StreamChunk{Done: true, Err: fmt.Errorf("ollama: stream read: %w", err)})
		}
	}()

	return ch, nil
}
