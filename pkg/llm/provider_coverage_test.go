package llm

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

// ---------------------------------------------------------------------------
// Anthropic provider tests
// ---------------------------------------------------------------------------

func TestAnthropicClient_ProviderModel(t *testing.T) {
	c := NewAnthropic("key", "claude-sonnet-4-6")
	ac := c.(*anthropicClient)
	if ac.Provider() != "anthropic" {
		t.Fatalf("expected anthropic, got %s", ac.Provider())
	}
	if ac.Model() != "claude-sonnet-4-6" {
		t.Fatalf("expected claude-sonnet-4-6, got %s", ac.Model())
	}
}

func TestAnthropicClient_CompleteNoMessages(t *testing.T) {
	c := NewAnthropic("key", "model")
	_, err := c.Complete(context.Background(), Request{})
	if err != ErrNoMessages {
		t.Fatalf("expected ErrNoMessages, got %v", err)
	}
}

func TestAnthropicClient_CompleteHTTP500(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(500)
		w.Write([]byte("internal error"))
	}))
	defer srv.Close()

	c := &anthropicClient{
		apiKey:   "key",
		model:    "test",
		endpoint: srv.URL,
		http:     &http.Client{Timeout: 5 * time.Second},
	}

	_, err := c.Complete(context.Background(), Request{
		Messages: []Message{{Role: RoleUser, Content: "hi"}},
	})
	if err == nil {
		t.Fatal("expected error for HTTP 500")
	}
	if !strings.Contains(err.Error(), "500") {
		t.Fatalf("expected 500 in error, got: %v", err)
	}
}

func TestAnthropicClient_CompleteSuccess(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		_ = body
		resp := `{
			"content": [{"type": "text", "text": "Hello!"}],
			"stop_reason": "end_turn",
			"model": "test-model",
			"usage": {"input_tokens": 10, "output_tokens": 5, "cache_read_input_tokens": 2}
		}`
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(resp))
	}))
	defer srv.Close()

	c := &anthropicClient{
		apiKey:   "key",
		model:    "test",
		endpoint: srv.URL,
		http:     &http.Client{Timeout: 5 * time.Second},
	}

	resp, err := c.Complete(context.Background(), Request{
		Messages:    []Message{{Role: RoleUser, Content: "hi"}},
		MaxTokens:   100,
		Temperature: 0.5,
		System:      "You are helpful",
		CacheSystem: true,
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if resp.Content != "Hello!" {
		t.Fatalf("expected Hello!, got %q", resp.Content)
	}
	if resp.CachedTokens != 2 {
		t.Fatalf("expected 2 cached tokens, got %d", resp.CachedTokens)
	}
}

func TestAnthropicClient_CompleteWithTools(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		resp := `{
			"content": [
				{"type": "text", "text": "Calling tool."},
				{"type": "tool_use", "id": "call_1", "name": "get_data", "input": {"key": "value"}}
			],
			"stop_reason": "tool_use",
			"model": "test",
			"usage": {"input_tokens": 10, "output_tokens": 5}
		}`
		w.Write([]byte(resp))
	}))
	defer srv.Close()

	c := &anthropicClient{
		apiKey:   "key",
		model:    "test",
		endpoint: srv.URL,
		http:     &http.Client{Timeout: 5 * time.Second},
	}

	resp, err := c.Complete(context.Background(), Request{
		Messages:  []Message{{Role: RoleUser, Content: "get data"}},
		MaxTokens: 100,
		Tools: []Tool{
			{Name: "get_data", Description: "Gets data", Schema: json.RawMessage(`{"type":"object"}`)},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(resp.ToolCalls) != 1 {
		t.Fatalf("expected 1 tool call, got %d", len(resp.ToolCalls))
	}
	if resp.StopReason != "tool_use" {
		t.Fatalf("expected tool_use stop reason, got %s", resp.StopReason)
	}
}

func TestAnthropicClient_CompleteWithJSONSchema(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		resp := `{
			"content": [{"type": "tool_use", "id": "t1", "name": "result", "input": {"answer": "42"}}],
			"stop_reason": "end_turn",
			"model": "test",
			"usage": {"input_tokens": 10, "output_tokens": 5}
		}`
		w.Write([]byte(resp))
	}))
	defer srv.Close()

	c := &anthropicClient{apiKey: "key", model: "test", endpoint: srv.URL, http: &http.Client{Timeout: 5 * time.Second}}

	resp, err := c.Complete(context.Background(), Request{
		Messages:  []Message{{Role: RoleUser, Content: "what?"}},
		MaxTokens: 100,
		JSONSchema: &JSONSchema{
			Name:        "result",
			Description: "test",
			Schema:      json.RawMessage(`{"type":"object","properties":{"answer":{"type":"string"}}}`),
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if resp.Content == "" {
		t.Fatal("expected non-empty content from JSON schema response")
	}
}

func TestAnthropicClient_CompleteJSONSchemaEmpty(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		resp := `{
			"content": [{"type": "text", "text": ""}],
			"stop_reason": "end_turn",
			"model": "test",
			"usage": {"input_tokens": 10, "output_tokens": 5}
		}`
		w.Write([]byte(resp))
	}))
	defer srv.Close()

	c := &anthropicClient{apiKey: "key", model: "test", endpoint: srv.URL, http: &http.Client{Timeout: 5 * time.Second}}

	_, err := c.Complete(context.Background(), Request{
		Messages:  []Message{{Role: RoleUser, Content: "hi"}},
		MaxTokens: 100,
		JSONSchema: &JSONSchema{
			Name:   "result",
			Schema: json.RawMessage(`{"type":"object"}`),
		},
	})
	if err != ErrInvalidJSON {
		t.Fatalf("expected ErrInvalidJSON, got %v", err)
	}
}

func TestAnthropicClient_CompleteWithGrammar(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		resp := `{
			"content": [{"type": "text", "text": "ok"}],
			"stop_reason": "end_turn",
			"model": "test",
			"usage": {"input_tokens": 1, "output_tokens": 1}
		}`
		w.Write([]byte(resp))
	}))
	defer srv.Close()

	c := &anthropicClient{apiKey: "key", model: "test", endpoint: srv.URL, http: &http.Client{Timeout: 5 * time.Second}}

	// Grammar is set but Anthropic doesn't support it — should log warning and proceed.
	_, err := c.Complete(context.Background(), Request{
		Messages:  []Message{{Role: RoleUser, Content: "hi"}},
		MaxTokens: 100,
		Grammar:   &GrammarSpec{Format: GrammarGBNF, Body: "root ::= \"test\""},
	})
	if err != nil {
		t.Fatal(err)
	}
}

func TestAnthropicClient_BuildPayloadWithToolCalls(t *testing.T) {
	c := &anthropicClient{apiKey: "key", model: "test", endpoint: "http://localhost", http: &http.Client{}}

	req := Request{
		Messages: []Message{
			{Role: RoleUser, Content: "call tool"},
			{
				Role:    RoleAssistant,
				Content: "thinking...",
				ToolCalls: []ToolCall{
					{ID: "tc1", Name: "calc", Arguments: json.RawMessage(`{"x":1}`)},
				},
			},
			{
				Role: RoleUser,
				ToolResults: []ToolResult{
					{ToolUseID: "tc1", Content: "42"},
				},
			},
		},
		MaxTokens: 100,
	}

	payload, err := c.buildPayload(req)
	if err != nil {
		t.Fatal(err)
	}
	msgs := payload["messages"].([]map[string]any)
	if len(msgs) != 3 {
		t.Fatalf("expected 3 messages, got %d", len(msgs))
	}
}

func TestAnthropicClient_BuildPayloadWithImages(t *testing.T) {
	c := &anthropicClient{apiKey: "key", model: "test", endpoint: "http://localhost", http: &http.Client{}}

	req := Request{
		Messages: []Message{
			{
				Role:    RoleUser,
				Content: "what is this?",
				Images: []Image{
					{MediaType: "image/png", Data: []byte("fake-png-data")},
				},
			},
		},
		MaxTokens: 100,
	}

	payload, err := c.buildPayload(req)
	if err != nil {
		t.Fatal(err)
	}
	msgs := payload["messages"].([]map[string]any)
	if len(msgs) != 1 {
		t.Fatalf("expected 1 message, got %d", len(msgs))
	}
}

func TestAnthropicClient_BuildPayloadBadSchema(t *testing.T) {
	c := &anthropicClient{apiKey: "key", model: "test", endpoint: "http://localhost", http: &http.Client{}}

	_, err := c.buildPayload(Request{
		Messages:  []Message{{Role: RoleUser, Content: "hi"}},
		MaxTokens: 100,
		JSONSchema: &JSONSchema{
			Name:   "bad",
			Schema: json.RawMessage(`not valid json`),
		},
	})
	if err == nil {
		t.Fatal("expected error for bad JSON schema")
	}
}

func TestAnthropicClient_BuildPayloadBadToolSchema(t *testing.T) {
	c := &anthropicClient{apiKey: "key", model: "test", endpoint: "http://localhost", http: &http.Client{}}

	_, err := c.buildPayload(Request{
		Messages:  []Message{{Role: RoleUser, Content: "hi"}},
		MaxTokens: 100,
		Tools: []Tool{
			{Name: "bad_tool", Schema: json.RawMessage(`invalid`)},
		},
	})
	if err == nil {
		t.Fatal("expected error for bad tool schema")
	}
}

func TestAnthropicClient_ParseResponseBadJSON(t *testing.T) {
	c := &anthropicClient{}
	_, err := c.parseResponse([]byte("not json"), false)
	if err == nil {
		t.Fatal("expected decode error")
	}
}

func TestAnthropicClient_ToolUseEmptyInput(t *testing.T) {
	c := &anthropicClient{}
	body := `{
		"content": [{"type": "tool_use", "id": "t1", "name": "fn"}],
		"stop_reason": "tool_use",
		"model": "test",
		"usage": {"input_tokens": 1, "output_tokens": 1}
	}`
	resp, err := c.parseResponse([]byte(body), false)
	if err != nil {
		t.Fatal(err)
	}
	if len(resp.ToolCalls) != 1 {
		t.Fatal("expected 1 tool call")
	}
	if string(resp.ToolCalls[0].Arguments) != "{}" {
		t.Fatalf("expected empty arguments {}, got %s", string(resp.ToolCalls[0].Arguments))
	}
}

func TestAnthropicClient_SystemNonCached(t *testing.T) {
	c := &anthropicClient{apiKey: "key", model: "test", endpoint: "http://localhost", http: &http.Client{}}

	payload, err := c.buildPayload(Request{
		Messages:  []Message{{Role: RoleUser, Content: "hi"}},
		MaxTokens: 100,
		System:    "You are helpful",
	})
	if err != nil {
		t.Fatal(err)
	}
	// System should be a string, not array.
	sys, ok := payload["system"].(string)
	if !ok {
		t.Fatal("expected system to be a string for non-cached system prompt")
	}
	if sys != "You are helpful" {
		t.Fatalf("expected system prompt, got %q", sys)
	}
}

// ---------------------------------------------------------------------------
// OpenAI provider tests
// ---------------------------------------------------------------------------

func TestOpenAIClient_ProviderModel(t *testing.T) {
	c := NewOpenAI("key", "gpt-4o")
	oc := c.(*openaiClient)
	if oc.Provider() != "openai" {
		t.Fatalf("expected openai, got %s", oc.Provider())
	}
	if oc.Model() != "gpt-4o" {
		t.Fatalf("expected gpt-4o, got %s", oc.Model())
	}
}

func TestOpenAIClient_CompleteNoMessages(t *testing.T) {
	c := NewOpenAI("key", "model")
	_, err := c.Complete(context.Background(), Request{})
	if err != ErrNoMessages {
		t.Fatalf("expected ErrNoMessages, got %v", err)
	}
}

func TestOpenAIClient_CompleteHTTP500(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(500)
		w.Write([]byte("server error"))
	}))
	defer srv.Close()

	c := &openaiClient{apiKey: "key", model: "test", endpoint: srv.URL, http: &http.Client{Timeout: 5 * time.Second}}

	_, err := c.Complete(context.Background(), Request{
		Messages: []Message{{Role: RoleUser, Content: "hi"}},
	})
	if err == nil {
		t.Fatal("expected error")
	}
}

func TestOpenAIClient_CompleteSuccess(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		resp := `{
			"model": "test",
			"choices": [{
				"message": {"content": "Hi there!", "tool_calls": []},
				"finish_reason": "stop"
			}],
			"usage": {"prompt_tokens": 10, "completion_tokens": 5, "prompt_tokens_details": {"cached_tokens": 3}}
		}`
		w.Write([]byte(resp))
	}))
	defer srv.Close()

	c := &openaiClient{apiKey: "key", model: "test", endpoint: srv.URL, http: &http.Client{Timeout: 5 * time.Second}}

	resp, err := c.Complete(context.Background(), Request{
		Messages:    []Message{{Role: RoleUser, Content: "hi"}},
		MaxTokens:   100,
		Temperature: 0.3,
		System:      "Be helpful",
	})
	if err != nil {
		t.Fatal(err)
	}
	if resp.Content != "Hi there!" {
		t.Fatalf("expected content 'Hi there!', got %q", resp.Content)
	}
	if resp.CachedTokens != 3 {
		t.Fatalf("expected 3 cached, got %d", resp.CachedTokens)
	}
}

func TestOpenAIClient_CompleteNoChoices(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(`{"model": "test", "choices": [], "usage": {}}`))
	}))
	defer srv.Close()

	c := &openaiClient{apiKey: "key", model: "test", endpoint: srv.URL, http: &http.Client{Timeout: 5 * time.Second}}

	_, err := c.Complete(context.Background(), Request{
		Messages: []Message{{Role: RoleUser, Content: "hi"}},
	})
	if err == nil || !strings.Contains(err.Error(), "no choices") {
		t.Fatalf("expected no choices error, got: %v", err)
	}
}

func TestOpenAIClient_CompleteWithJSONSchema(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		resp := `{
			"model": "test",
			"choices": [{"message": {"content": "{\"answer\": 42}"}, "finish_reason": "stop"}],
			"usage": {"prompt_tokens": 10, "completion_tokens": 5}
		}`
		w.Write([]byte(resp))
	}))
	defer srv.Close()

	c := &openaiClient{apiKey: "key", model: "test", endpoint: srv.URL, http: &http.Client{Timeout: 5 * time.Second}}

	resp, err := c.Complete(context.Background(), Request{
		Messages:  []Message{{Role: RoleUser, Content: "answer"}},
		MaxTokens: 100,
		JSONSchema: &JSONSchema{
			Name:   "result",
			Schema: json.RawMessage(`{"type":"object"}`),
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if resp.Content == "" {
		t.Fatal("expected non-empty content")
	}
}

func TestOpenAIClient_CompleteJSONSchemaEmpty(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		resp := `{
			"model": "test",
			"choices": [{"message": {"content": ""}, "finish_reason": "stop"}],
			"usage": {}
		}`
		w.Write([]byte(resp))
	}))
	defer srv.Close()

	c := &openaiClient{apiKey: "key", model: "test", endpoint: srv.URL, http: &http.Client{Timeout: 5 * time.Second}}

	_, err := c.Complete(context.Background(), Request{
		Messages:   []Message{{Role: RoleUser, Content: "hi"}},
		JSONSchema: &JSONSchema{Name: "r", Schema: json.RawMessage(`{"type":"object"}`)},
	})
	if err != ErrInvalidJSON {
		t.Fatalf("expected ErrInvalidJSON, got %v", err)
	}
}

func TestOpenAIClient_CompleteJSONSchemaInvalidJSON(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		resp := `{
			"model": "test",
			"choices": [{"message": {"content": "not json"}, "finish_reason": "stop"}],
			"usage": {}
		}`
		w.Write([]byte(resp))
	}))
	defer srv.Close()

	c := &openaiClient{apiKey: "key", model: "test", endpoint: srv.URL, http: &http.Client{Timeout: 5 * time.Second}}

	_, err := c.Complete(context.Background(), Request{
		Messages:   []Message{{Role: RoleUser, Content: "hi"}},
		JSONSchema: &JSONSchema{Name: "r", Schema: json.RawMessage(`{"type":"object"}`)},
	})
	if err != ErrInvalidJSON {
		t.Fatalf("expected ErrInvalidJSON, got %v", err)
	}
}

func TestOpenAIClient_BuildPayloadBadSchema(t *testing.T) {
	c := &openaiClient{apiKey: "key", model: "test"}
	_, err := c.buildPayload(Request{
		Messages:   []Message{{Role: RoleUser, Content: "hi"}},
		JSONSchema: &JSONSchema{Name: "bad", Schema: json.RawMessage(`not json`)},
	})
	if err == nil {
		t.Fatal("expected error for bad schema")
	}
}

func TestOpenAIClient_BuildPayloadBadToolSchema(t *testing.T) {
	c := &openaiClient{apiKey: "key", model: "test"}
	_, err := c.buildPayload(Request{
		Messages: []Message{{Role: RoleUser, Content: "hi"}},
		Tools:    []Tool{{Name: "bad", Schema: json.RawMessage(`invalid`)}},
	})
	if err == nil {
		t.Fatal("expected error for bad tool schema")
	}
}

func TestOpenAIClient_BuildPayloadWithImages(t *testing.T) {
	c := &openaiClient{apiKey: "key", model: "test"}
	payload, err := c.buildPayload(Request{
		Messages: []Message{
			{
				Role:    RoleUser,
				Content: "what?",
				Images:  []Image{{MediaType: "image/png", Data: []byte("data")}},
			},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	msgs := payload["messages"].([]map[string]any)
	if len(msgs) != 1 {
		t.Fatal("expected 1 message")
	}
}

func TestOpenAIClient_ParseResponseBadJSON(t *testing.T) {
	c := &openaiClient{}
	_, err := c.parseResponse([]byte("bad"), false)
	if err == nil {
		t.Fatal("expected decode error")
	}
}

func TestOpenAIClient_WithToolCalls(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		resp := `{
			"model": "test",
			"choices": [{
				"message": {
					"content": "",
					"tool_calls": [{"id": "c1", "type": "function", "function": {"name": "calc", "arguments": "{\"x\":1}"}}]
				},
				"finish_reason": "tool_calls"
			}],
			"usage": {"prompt_tokens": 5, "completion_tokens": 3}
		}`
		w.Write([]byte(resp))
	}))
	defer srv.Close()

	c := &openaiClient{apiKey: "key", model: "test", endpoint: srv.URL, http: &http.Client{Timeout: 5 * time.Second}}

	resp, err := c.Complete(context.Background(), Request{
		Messages: []Message{{Role: RoleUser, Content: "calc"}},
		Tools:    []Tool{{Name: "calc", Schema: json.RawMessage(`{"type":"object"}`)}},
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(resp.ToolCalls) != 1 {
		t.Fatalf("expected 1 tool call, got %d", len(resp.ToolCalls))
	}
}

func TestOpenAIClient_CompleteWithGrammar(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		resp := `{
			"model": "test",
			"choices": [{"message": {"content": "ok"}, "finish_reason": "stop"}],
			"usage": {}
		}`
		w.Write([]byte(resp))
	}))
	defer srv.Close()

	c := &openaiClient{apiKey: "key", model: "test", endpoint: srv.URL, http: &http.Client{Timeout: 5 * time.Second}}

	_, err := c.Complete(context.Background(), Request{
		Messages: []Message{{Role: RoleUser, Content: "hi"}},
		Grammar:  &GrammarSpec{Format: GrammarGBNF, Body: "root ::= \"ok\""},
	})
	if err != nil {
		t.Fatal(err)
	}
}

// ---------------------------------------------------------------------------
// Ollama provider tests
// ---------------------------------------------------------------------------

func TestOllamaClient_ProviderModel(t *testing.T) {
	c := NewOllama("", "llama3.2")
	oc := c.(*ollamaClient)
	if oc.Provider() != "ollama" {
		t.Fatalf("expected ollama, got %s", oc.Provider())
	}
	if oc.Model() != "llama3.2" {
		t.Fatalf("expected llama3.2, got %s", oc.Model())
	}
}

func TestOllamaClient_CompleteNoMessages(t *testing.T) {
	c := NewOllama("http://localhost:11434", "llama3.2")
	_, err := c.Complete(context.Background(), Request{})
	if err != ErrNoMessages {
		t.Fatalf("expected ErrNoMessages, got %v", err)
	}
}

func TestOllamaClient_CompleteHTTP500(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(500)
		w.Write([]byte("error"))
	}))
	defer srv.Close()

	c := &ollamaClient{host: srv.URL, model: "test", http: &http.Client{Timeout: 5 * time.Second}}

	_, err := c.Complete(context.Background(), Request{
		Messages: []Message{{Role: RoleUser, Content: "hi"}},
	})
	if err == nil {
		t.Fatal("expected error")
	}
}

func TestOllamaClient_CompleteSuccess(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		resp := `{
			"model": "llama3.2",
			"message": {"role": "assistant", "content": "Hello!"},
			"done_reason": "stop",
			"prompt_eval_count": 10,
			"eval_count": 5
		}`
		w.Write([]byte(resp))
	}))
	defer srv.Close()

	c := &ollamaClient{host: srv.URL, model: "llama3.2", http: &http.Client{Timeout: 5 * time.Second}}

	resp, err := c.Complete(context.Background(), Request{
		Messages:    []Message{{Role: RoleUser, Content: "hi"}},
		MaxTokens:   100,
		Temperature: 0.3,
		System:      "Be helpful",
	})
	if err != nil {
		t.Fatal(err)
	}
	if resp.Content != "Hello!" {
		t.Fatalf("expected Hello!, got %q", resp.Content)
	}
}

func TestOllamaClient_CompleteWithToolCalls(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		resp := `{
			"model": "test",
			"message": {
				"role": "assistant",
				"content": "",
				"tool_calls": [{"function": {"name": "calc", "arguments": {"x": 1}}}]
			},
			"prompt_eval_count": 5,
			"eval_count": 3
		}`
		w.Write([]byte(resp))
	}))
	defer srv.Close()

	c := &ollamaClient{host: srv.URL, model: "test", http: &http.Client{Timeout: 5 * time.Second}}

	resp, err := c.Complete(context.Background(), Request{
		Messages: []Message{{Role: RoleUser, Content: "calc"}},
		Tools:    []Tool{{Name: "calc", Schema: json.RawMessage(`{"type":"object"}`)}},
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(resp.ToolCalls) != 1 {
		t.Fatalf("expected 1 tool call, got %d", len(resp.ToolCalls))
	}
	if resp.StopReason != "tool_use" {
		t.Fatalf("expected tool_use, got %s", resp.StopReason)
	}
}

func TestOllamaClient_CompleteJSONSchemaInvalid(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		resp := `{
			"model": "test",
			"message": {"role": "assistant", "content": ""},
			"prompt_eval_count": 5,
			"eval_count": 3
		}`
		w.Write([]byte(resp))
	}))
	defer srv.Close()

	c := &ollamaClient{host: srv.URL, model: "test", http: &http.Client{Timeout: 5 * time.Second}}

	_, err := c.Complete(context.Background(), Request{
		Messages:   []Message{{Role: RoleUser, Content: "hi"}},
		JSONSchema: &JSONSchema{Name: "r", Schema: json.RawMessage(`{"type":"object"}`)},
	})
	if err != ErrInvalidJSON {
		t.Fatalf("expected ErrInvalidJSON, got %v", err)
	}
}

func TestOllamaClient_BuildPayloadBadToolSchema(t *testing.T) {
	c := &ollamaClient{host: "http://localhost", model: "test"}
	_, err := c.buildPayload(Request{
		Messages: []Message{{Role: RoleUser, Content: "hi"}},
		Tools:    []Tool{{Name: "bad", Schema: json.RawMessage(`invalid`)}},
	}, false)
	if err == nil {
		t.Fatal("expected error for bad tool schema")
	}
}

func TestOllamaClient_BuildPayloadBadJSONSchema(t *testing.T) {
	c := &ollamaClient{host: "http://localhost", model: "test"}
	_, err := c.buildPayload(Request{
		Messages:   []Message{{Role: RoleUser, Content: "hi"}},
		JSONSchema: &JSONSchema{Name: "bad", Schema: json.RawMessage(`not json`)},
	}, false)
	if err == nil {
		t.Fatal("expected error for bad JSON schema")
	}
}

func TestOllamaClient_BuildPayloadGrammarJSONSchema(t *testing.T) {
	c := &ollamaClient{host: "http://localhost", model: "test"}

	// Valid GrammarJSONSchema
	payload, err := c.buildPayload(Request{
		Messages: []Message{{Role: RoleUser, Content: "hi"}},
		Grammar:  &GrammarSpec{Format: GrammarJSONSchema, Body: `{"type":"object"}`},
	}, false)
	if err != nil {
		t.Fatal(err)
	}
	if payload["format"] == nil {
		t.Fatal("expected format to be set")
	}
}

func TestOllamaClient_BuildPayloadGrammarJSONSchemaInvalid(t *testing.T) {
	c := &ollamaClient{host: "http://localhost", model: "test"}
	_, err := c.buildPayload(Request{
		Messages: []Message{{Role: RoleUser, Content: "hi"}},
		Grammar:  &GrammarSpec{Format: GrammarJSONSchema, Body: `not valid json`},
	}, false)
	if err == nil {
		t.Fatal("expected error for bad GrammarJSONSchema body")
	}
}

func TestOllamaClient_BuildPayloadGBNF(t *testing.T) {
	c := &ollamaClient{host: "http://localhost", model: "test"}
	payload, err := c.buildPayload(Request{
		Messages: []Message{{Role: RoleUser, Content: "hi"}},
		Grammar:  &GrammarSpec{Format: GrammarGBNF, Body: "root ::= \"test\""},
	}, false)
	if err != nil {
		t.Fatal(err)
	}
	opts, ok := payload["options"].(map[string]any)
	if !ok {
		t.Fatal("expected options")
	}
	if opts["grammar"] == nil {
		t.Fatal("expected grammar in options")
	}
}

func TestOllamaClient_ParseResponseBadJSON(t *testing.T) {
	c := &ollamaClient{}
	_, err := c.parseResponse([]byte("bad"), false)
	if err == nil {
		t.Fatal("expected decode error")
	}
}

func TestOllamaClient_ParseResponseNoDoneReason(t *testing.T) {
	c := &ollamaClient{}
	body := `{
		"model": "test",
		"message": {"role": "assistant", "content": "ok"},
		"prompt_eval_count": 1,
		"eval_count": 1
	}`
	resp, err := c.parseResponse([]byte(body), false)
	if err != nil {
		t.Fatal(err)
	}
	if resp.StopReason != "stop" {
		t.Fatalf("expected default stop reason 'stop', got %q", resp.StopReason)
	}
}

func TestOllamaClient_ParseResponseEmptyToolArgs(t *testing.T) {
	c := &ollamaClient{}
	body := `{
		"model": "test",
		"message": {
			"role": "assistant",
			"content": "",
			"tool_calls": [{"function": {"name": "fn"}}]
		},
		"prompt_eval_count": 1,
		"eval_count": 1
	}`
	resp, err := c.parseResponse([]byte(body), false)
	if err != nil {
		t.Fatal(err)
	}
	if string(resp.ToolCalls[0].Arguments) != "{}" {
		t.Fatalf("expected {}, got %s", string(resp.ToolCalls[0].Arguments))
	}
}

func TestOllamaClient_BuildPayloadWithToolResults(t *testing.T) {
	c := &ollamaClient{host: "http://localhost", model: "test"}
	payload, err := c.buildPayload(Request{
		Messages: []Message{
			{Role: RoleUser, Content: "calc"},
			{
				Role:      RoleAssistant,
				Content:   "",
				ToolCalls: []ToolCall{{Name: "calc", Arguments: json.RawMessage(`{"x":1}`)}},
			},
			{
				Role:        RoleUser,
				ToolResults: []ToolResult{{ToolUseID: "tc1", Content: "42"}},
			},
		},
	}, false)
	if err != nil {
		t.Fatal(err)
	}
	msgs := payload["messages"].([]map[string]any)
	if len(msgs) < 3 {
		t.Fatalf("expected >= 3 messages, got %d", len(msgs))
	}
}

func TestOllamaClient_BuildPayloadWithImages(t *testing.T) {
	c := &ollamaClient{host: "http://localhost", model: "test"}
	payload, err := c.buildPayload(Request{
		Messages: []Message{
			{Role: RoleUser, Content: "what?", Images: []Image{{MediaType: "image/png", Data: []byte("data")}}},
		},
	}, false)
	if err != nil {
		t.Fatal(err)
	}
	msgs := payload["messages"].([]map[string]any)
	// No system prompt, so only user message at index 0.
	found := false
	for _, m := range msgs {
		if m["images"] != nil {
			found = true
			break
		}
	}
	if !found {
		t.Fatal("expected images in at least one message")
	}
}

// ---------------------------------------------------------------------------
// Mock provider tests
// ---------------------------------------------------------------------------

func TestMock_ProviderModelDefaults(t *testing.T) {
	m := &Mock{}
	if m.Provider() != "mock" {
		t.Fatalf("expected mock, got %s", m.Provider())
	}
	if m.Model() != "mock-model" {
		t.Fatalf("expected mock-model, got %s", m.Model())
	}
}

func TestMock_ProviderModelOverrides(t *testing.T) {
	m := &Mock{ProviderName: "custom", ModelName: "custom-model"}
	if m.Provider() != "custom" {
		t.Fatalf("expected custom, got %s", m.Provider())
	}
	if m.Model() != "custom-model" {
		t.Fatalf("expected custom-model, got %s", m.Model())
	}
}

func TestMock_CompleteWithErr(t *testing.T) {
	m := &Mock{Err: fmt.Errorf("fail")}
	_, err := m.Complete(context.Background(), Request{Messages: []Message{{Role: RoleUser, Content: "hi"}}})
	if err == nil || err.Error() != "fail" {
		t.Fatalf("expected fail error, got %v", err)
	}
	if len(m.Calls) != 1 {
		t.Fatal("expected 1 call recorded")
	}
}

func TestMock_CompleteDefaultResp(t *testing.T) {
	m := &Mock{}
	resp, err := m.Complete(context.Background(), Request{Messages: []Message{{Role: RoleUser, Content: "hi"}}})
	if err != nil {
		t.Fatal(err)
	}
	if resp.Provider != "mock" {
		t.Fatalf("expected mock provider, got %s", resp.Provider)
	}
}

// ---------------------------------------------------------------------------
// Budget router tests
// ---------------------------------------------------------------------------

func TestBudgetRouter_SelectTierQuiet_DefaultOutOfRange(t *testing.T) {
	cheap := &Mock{Resp: &Response{Content: "ok"}}
	tiers := []TierConfig{
		{Name: "cheap", Provider: "mock", Model: "small", Client: cheap},
	}

	ledger := &coverageLedger{}
	router := NewBudgetRouter(BudgetConfig{
		MonthlyLimitUSD:  100,
		WarnThresholdPct: 0.8,
		TierOrder:        tiers,
		DefaultTier:      99, // out of range
	}, ledger)

	// selectTierQuiet should clamp to 0.
	if router.CurrentTier().Name != "cheap" {
		t.Fatalf("expected cheap tier when default out of range, got %s", router.CurrentTier().Name)
	}
}

func TestBudgetRouter_SelectTierQuiet_NegativeDefault(t *testing.T) {
	cheap := &Mock{Resp: &Response{Content: "ok"}}
	tiers := []TierConfig{
		{Name: "cheap", Provider: "mock", Model: "small", Client: cheap},
	}

	ledger := &coverageLedger{}
	router := NewBudgetRouter(BudgetConfig{
		MonthlyLimitUSD:  100,
		WarnThresholdPct: 0.8,
		TierOrder:        tiers,
		DefaultTier:      -1,
	}, ledger)

	if router.CurrentTier().Name != "cheap" {
		t.Fatalf("expected cheap tier when default < 0, got %s", router.CurrentTier().Name)
	}
}

func TestBudgetRouter_SelectTierQuiet_OverWarning(t *testing.T) {
	cheap := &Mock{Resp: &Response{Content: "ok"}}
	expensive := &Mock{Resp: &Response{Content: "ok"}}
	tiers := []TierConfig{
		{Name: "cheap", Provider: "mock", Model: "small", Client: cheap},
		{Name: "expensive", Provider: "mock", Model: "big", Client: expensive},
	}

	ledger := &coverageLedger{spend: 85}
	router := NewBudgetRouter(BudgetConfig{
		MonthlyLimitUSD:  100,
		WarnThresholdPct: 0.8,
		TierOrder:        tiers,
		DefaultTier:      1,
	}, ledger)

	// Spend is 85/100 = 85% > 80% threshold
	if router.CurrentTier().Name != "cheap" {
		t.Fatalf("expected cheap tier when over warning threshold, got %s", router.CurrentTier().Name)
	}
}

func TestBudgetRouter_SelectTierQuiet_OverLimit(t *testing.T) {
	cheap := &Mock{Resp: &Response{Content: "ok"}}
	expensive := &Mock{Resp: &Response{Content: "ok"}}
	tiers := []TierConfig{
		{Name: "cheap", Provider: "mock", Model: "small", Client: cheap},
		{Name: "expensive", Provider: "mock", Model: "big", Client: expensive},
	}

	ledger := &coverageLedger{spend: 105}
	router := NewBudgetRouter(BudgetConfig{
		MonthlyLimitUSD:  100,
		WarnThresholdPct: 0.8,
		TierOrder:        tiers,
		DefaultTier:      1,
	}, ledger)

	if router.CurrentTier().Name != "cheap" {
		t.Fatalf("expected cheap tier at limit, got %s", router.CurrentTier().Name)
	}
}

// ---------------------------------------------------------------------------
// Streaming tests
// ---------------------------------------------------------------------------

func TestAnthropicStream_NoMessages(t *testing.T) {
	c := NewAnthropic("key", "model")
	s, ok := AsStreamer(c)
	if !ok {
		t.Fatal("expected Streamer")
	}
	_, err := s.Stream(context.Background(), Request{})
	if err != ErrNoMessages {
		t.Fatalf("expected ErrNoMessages, got %v", err)
	}
}

func TestAnthropicStream_HTTP400(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(400)
	}))
	defer srv.Close()

	c := &anthropicClient{apiKey: "key", model: "test", endpoint: srv.URL, http: &http.Client{Timeout: 5 * time.Second}}
	_, err := c.Stream(context.Background(), Request{
		Messages: []Message{{Role: RoleUser, Content: "hi"}},
	})
	if err == nil {
		t.Fatal("expected error for HTTP 400")
	}
}

func TestAnthropicStream_Success(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(200)
		flusher, _ := w.(http.Flusher)
		fmt.Fprintln(w, "event: content_block_delta")
		fmt.Fprintln(w, `data: {"delta":{"type":"text_delta","text":"Hello"}}`)
		fmt.Fprintln(w)
		fmt.Fprintln(w, "event: message_stop")
		fmt.Fprintln(w, `data: {}`)
		fmt.Fprintln(w)
		flusher.Flush()
	}))
	defer srv.Close()

	c := &anthropicClient{apiKey: "key", model: "test", endpoint: srv.URL, http: &http.Client{Timeout: 5 * time.Second}}
	ch, err := c.Stream(context.Background(), Request{
		Messages: []Message{{Role: RoleUser, Content: "hi"}},
	})
	if err != nil {
		t.Fatal(err)
	}
	var gotDone bool
	for chunk := range ch {
		if chunk.Err != nil {
			t.Fatal(chunk.Err)
		}
		if chunk.Done {
			gotDone = true
		}
	}
	if !gotDone {
		t.Fatal("expected done chunk")
	}
}

func TestOpenAIStream_NoMessages(t *testing.T) {
	c := NewOpenAI("key", "model")
	s, ok := AsStreamer(c)
	if !ok {
		t.Fatal("expected Streamer")
	}
	_, err := s.Stream(context.Background(), Request{})
	if err != ErrNoMessages {
		t.Fatalf("expected ErrNoMessages, got %v", err)
	}
}

func TestOpenAIStream_HTTP400(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(400)
	}))
	defer srv.Close()

	c := &openaiClient{apiKey: "key", model: "test", endpoint: srv.URL, http: &http.Client{Timeout: 5 * time.Second}}
	_, err := c.Stream(context.Background(), Request{
		Messages: []Message{{Role: RoleUser, Content: "hi"}},
	})
	if err == nil {
		t.Fatal("expected error for HTTP 400")
	}
}

func TestOpenAIStream_Success(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(200)
		flusher, _ := w.(http.Flusher)
		fmt.Fprintln(w, `data: {"choices":[{"delta":{"content":"Hi"},"finish_reason":null}]}`)
		fmt.Fprintln(w, `data: [DONE]`)
		flusher.Flush()
	}))
	defer srv.Close()

	c := &openaiClient{apiKey: "key", model: "test", endpoint: srv.URL, http: &http.Client{Timeout: 5 * time.Second}}
	ch, err := c.Stream(context.Background(), Request{
		Messages: []Message{{Role: RoleUser, Content: "hi"}},
	})
	if err != nil {
		t.Fatal(err)
	}
	var gotDone bool
	for chunk := range ch {
		if chunk.Err != nil {
			t.Fatal(chunk.Err)
		}
		if chunk.Done {
			gotDone = true
		}
	}
	if !gotDone {
		t.Fatal("expected done chunk")
	}
}

func TestOllamaStream_NoMessages(t *testing.T) {
	c := NewOllama("http://localhost:11434", "llama3.2")
	s, ok := AsStreamer(c)
	if !ok {
		t.Fatal("expected Streamer")
	}
	_, err := s.Stream(context.Background(), Request{})
	if err != ErrNoMessages {
		t.Fatalf("expected ErrNoMessages, got %v", err)
	}
}

func TestOllamaStream_HTTP400(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(400)
		w.Write([]byte("bad request"))
	}))
	defer srv.Close()

	c := &ollamaClient{host: srv.URL, model: "test", http: &http.Client{Timeout: 5 * time.Second}}
	_, err := c.Stream(context.Background(), Request{
		Messages: []Message{{Role: RoleUser, Content: "hi"}},
	})
	if err == nil {
		t.Fatal("expected error for HTTP 400")
	}
}

func TestOllamaStream_Success(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/x-ndjson")
		w.WriteHeader(200)
		flusher, _ := w.(http.Flusher)
		fmt.Fprintln(w, `{"message":{"content":"Hi"},"done":false}`)
		fmt.Fprintln(w, `{"message":{"content":" there"},"done":true}`)
		flusher.Flush()
	}))
	defer srv.Close()

	c := &ollamaClient{host: srv.URL, model: "test", http: &http.Client{Timeout: 5 * time.Second}}
	ch, err := c.Stream(context.Background(), Request{
		Messages: []Message{{Role: RoleUser, Content: "hi"}},
	})
	if err != nil {
		t.Fatal(err)
	}
	var gotDone bool
	for chunk := range ch {
		if chunk.Err != nil {
			t.Fatal(chunk.Err)
		}
		if chunk.Done {
			gotDone = true
		}
	}
	if !gotDone {
		t.Fatal("expected done chunk")
	}
}

// coverageLedger is a test double for SpendLedger (named to avoid collision
// with the existing mockLedger in budget_test.go).
type coverageLedger struct {
	spend float64
}

func (m *coverageLedger) MonthlySpendUSD(_, _ time.Time) float64 { return m.spend }
func (m *coverageLedger) RecordUsage(_, _ string, _, _, _ int)    {}
