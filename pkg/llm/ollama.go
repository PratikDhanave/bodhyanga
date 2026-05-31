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

	// Ollama passes grammars directly to llama.cpp via options.grammar.
	// No warning needed — GBNF is natively supported.
	// JSON Schema grammars (GrammarJSONSchema) are handled via the format field.

	payload, err := c.buildPayload(req, false /* stream */)
	if err != nil {
		return nil, err
	}

	body, err := c.post(ctx, payload)
	if err != nil {
		return nil, err
	}

	return c.parseResponse(body, req.JSONSchema != nil)
}

// Stream implements the Streamer interface for token-by-token output.
// Ollama streams newline-delimited JSON objects; each has a message.content
// delta and a done boolean. The channel is closed after the final chunk.
func (c *ollamaClient) Stream(ctx context.Context, req Request) (<-chan StreamChunk, error) {
	if len(req.Messages) == 0 {
		return nil, ErrNoMessages
	}
	payload, err := c.buildPayload(req, true /* stream */)
	if err != nil {
		return nil, err
	}

	buf, err := json.Marshal(payload)
	if err != nil {
		return nil, err
	}
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost,
		c.host+ollamaChatPath, bytes.NewReader(buf))
	if err != nil {
		return nil, err
	}
	httpReq.Header.Set("Content-Type", "application/json")

	resp, err := c.http.Do(httpReq)
	if err != nil {
		return nil, fmt.Errorf("ollama stream: %w", err)
	}
	if resp.StatusCode >= 400 {
		body, _ := io.ReadAll(resp.Body)
		resp.Body.Close()
		return nil, fmt.Errorf("ollama stream: http %d: %s", resp.StatusCode, string(body))
	}

	ch := make(chan StreamChunk, 32)
	go func() {
		defer resp.Body.Close()
		defer close(ch)

		var full strings.Builder
		scanner := bufio.NewScanner(resp.Body)
		for scanner.Scan() {
			line := scanner.Text()
			if line == "" {
				continue
			}
			var event struct {
				Message struct {
					Content string `json:"content"`
				} `json:"message"`
				Done bool `json:"done"`
			}
			if err := json.Unmarshal([]byte(line), &event); err != nil {
				ch <- StreamChunk{Err: fmt.Errorf("ollama stream: decode: %w", err)}
				return
			}
			full.WriteString(event.Message.Content)
			if event.Done {
				ch <- StreamChunk{Delta: event.Message.Content, Done: true, FullContent: full.String()}
				return
			}
			ch <- StreamChunk{Delta: event.Message.Content}
		}
		if err := scanner.Err(); err != nil {
			ch <- StreamChunk{Err: fmt.Errorf("ollama stream: scan: %w", err)}
		}
	}()
	return ch, nil
}

func (c *ollamaClient) buildPayload(req Request, stream bool) (map[string]any, error) {
	msgs := make([]map[string]any, 0, len(req.Messages)+1)
	if req.System != "" {
		msgs = append(msgs, map[string]any{
			"role":    "system",
			"content": req.System,
		})
	}
	for _, m := range req.Messages {
		// --- tool result turn (user role, returning tool outputs) ----------
		// Ollama uses a dedicated "tool" role for each result — one message
		// per tool call (unlike Anthropic's batched tool_result content blocks).
		if len(m.ToolResults) > 0 {
			for _, tr := range m.ToolResults {
				msgs = append(msgs, map[string]any{
					"role":    "tool",
					"content": tr.Content,
				})
			}
			continue
		}

		// --- assistant turn that called tools ------------------------------
		// Serialize as an assistant message with a tool_calls array.
		if len(m.ToolCalls) > 0 {
			calls := make([]map[string]any, 0, len(m.ToolCalls))
			for _, tc := range m.ToolCalls {
				var args any
				if len(tc.Arguments) > 0 {
					_ = json.Unmarshal(tc.Arguments, &args)
				}
				if args == nil {
					args = map[string]any{}
				}
				calls = append(calls, map[string]any{
					"function": map[string]any{
						"name":      tc.Name,
						"arguments": args,
					},
				})
			}
			entry := map[string]any{
				"role":       "assistant",
				"content":    m.Content,
				"tool_calls": calls,
			}
			msgs = append(msgs, entry)
			continue
		}

		// --- plain text / image message ------------------------------------
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

	// GBNF grammar — Ollama tunnels options.grammar straight to llama.cpp's
	// sampler, giving a sampler-level guarantee that every token produced
	// conforms to the grammar. This is the same mechanism as the kronk
	// (in-process llama.cpp) provider, just accessed over HTTP.
	// Only GBNF format is wired here; GrammarJSONSchema is handled via the
	// format field below.
	if req.Grammar != nil && req.Grammar.Format == GrammarGBNF {
		options["grammar"] = req.Grammar.Body
	}

	payload := map[string]any{
		"model":    c.model,
		"messages": msgs,
		"stream":   stream,
	}
	if len(options) > 0 {
		payload["options"] = options
	}

	// JSON Schema structured output — Ollama 0.5+ accepts the full JSON Schema
	// object in the `format` field, giving field-level validation in addition
	// to the GBNF structural guarantee (when both are set). Older Ollama
	// servers (0.4.x) only accept format="json"; we send the full schema
	// since Ollama 0.5 is now long-standing (2026).
	if req.JSONSchema != nil {
		var schema map[string]any
		if err := json.Unmarshal(req.JSONSchema.Schema, &schema); err != nil {
			return nil, fmt.Errorf("ollama: bad JSONSchema: %w", err)
		}
		payload["format"] = schema
	} else if req.Grammar != nil && req.Grammar.Format == GrammarJSONSchema {
		// JSON Schema grammar passed as a GrammarSpec rather than via
		// JSONSchema field — honour it the same way.
		var schema map[string]any
		if err := json.Unmarshal([]byte(req.Grammar.Body), &schema); err != nil {
			return nil, fmt.Errorf("ollama: bad GrammarJSONSchema body: %w", err)
		}
		payload["format"] = schema
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
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost,
		c.host+ollamaChatPath, bytes.NewReader(buf))
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
