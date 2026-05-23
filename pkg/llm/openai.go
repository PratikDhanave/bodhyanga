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
				Content string `json:"content"`
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
	content := parsed.Choices[0].Message.Content
	if expectJSON && content == "" {
		return nil, ErrInvalidJSON
	}
	if expectJSON {
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
		StopReason:   parsed.Choices[0].FinishReason,
	}, nil
}
