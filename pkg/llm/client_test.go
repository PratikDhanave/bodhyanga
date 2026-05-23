package llm

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestMockRecordsCallsAndReturnsResponse(t *testing.T) {
	want := &Response{Content: "hi", InputTokens: 5, OutputTokens: 1, Provider: "mock", Model: "mock-model"}
	m := &Mock{Resp: want}

	got, err := m.Complete(context.Background(), Request{
		System:    "be terse",
		Messages:  []Message{{Role: RoleUser, Content: "hello"}},
		MaxTokens: 16,
	})
	if err != nil {
		t.Fatalf("Complete: %v", err)
	}
	if got != want {
		t.Fatalf("got %+v, want %+v", got, want)
	}
	if len(m.Calls) != 1 {
		t.Fatalf("expected 1 call, got %d", len(m.Calls))
	}
	if m.Calls[0].System != "be terse" {
		t.Fatalf("system not captured: %+v", m.Calls[0])
	}
}

func TestMockReturnsErrorWhenSet(t *testing.T) {
	want := errors.New("boom")
	m := &Mock{Err: want}
	_, err := m.Complete(context.Background(), Request{
		Messages: []Message{{Role: RoleUser, Content: "x"}},
	})
	if !errors.Is(err, want) {
		t.Fatalf("expected %v, got %v", want, err)
	}
}

func TestAnthropicHappyPathWithJSONSchema(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("x-api-key") != "sk-test" {
			t.Errorf("missing api key header")
		}
		if r.Header.Get("anthropic-version") != AnthropicVersion {
			t.Errorf("missing version header")
		}
		var sent map[string]any
		if err := json.NewDecoder(r.Body).Decode(&sent); err != nil {
			t.Errorf("decode req: %v", err)
		}
		if sent["model"] != "claude-test" {
			t.Errorf("wrong model: %v", sent["model"])
		}
		tools, _ := sent["tools"].([]any)
		if len(tools) != 1 {
			t.Errorf("expected one tool, got %d", len(tools))
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{
			"content": [
				{"type":"tool_use","name":"emit","input":{"answer":"42"}}
			],
			"stop_reason":"tool_use",
			"model":"claude-test",
			"usage":{"input_tokens":12,"output_tokens":4,"cache_read_input_tokens":8}
		}`))
	}))
	defer srv.Close()

	c := &anthropicClient{apiKey: "sk-test", model: "claude-test", endpoint: srv.URL, http: http.DefaultClient}
	got, err := c.Complete(context.Background(), Request{
		System:    "system",
		Messages:  []Message{{Role: RoleUser, Content: "go"}},
		MaxTokens: 64,
		JSONSchema: &JSONSchema{
			Name:        "emit",
			Description: "emit",
			Schema:      json.RawMessage(`{"type":"object","properties":{"answer":{"type":"string"}}}`),
		},
		CacheSystem: true,
	})
	if err != nil {
		t.Fatalf("Complete: %v", err)
	}
	if got.Content != `{"answer":"42"}` {
		t.Fatalf("content: %q", got.Content)
	}
	if got.InputTokens != 12 || got.OutputTokens != 4 || got.CachedTokens != 8 {
		t.Fatalf("usage: %+v", got)
	}
	if got.Provider != "anthropic" {
		t.Fatalf("provider: %s", got.Provider)
	}
}

func TestAnthropicHTTPErrorBubblesUp(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(500)
		_, _ = w.Write([]byte(`{"error":"overloaded"}`))
	}))
	defer srv.Close()
	c := &anthropicClient{apiKey: "k", model: "m", endpoint: srv.URL, http: http.DefaultClient}
	_, err := c.Complete(context.Background(), Request{
		Messages: []Message{{Role: RoleUser, Content: "x"}}, MaxTokens: 1,
	})
	if err == nil || !strings.Contains(err.Error(), "500") {
		t.Fatalf("expected 500 error, got %v", err)
	}
}

func TestOpenAIHappyPathWithJSONSchema(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer sk-test" {
			t.Errorf("missing auth header")
		}
		var sent map[string]any
		_ = json.NewDecoder(r.Body).Decode(&sent)
		if _, ok := sent["response_format"]; !ok {
			t.Errorf("missing response_format")
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{
			"model":"gpt-test",
			"choices":[{"message":{"content":"{\"answer\":\"42\"}"},"finish_reason":"stop"}],
			"usage":{"prompt_tokens":11,"completion_tokens":3,"prompt_tokens_details":{"cached_tokens":7}}
		}`))
	}))
	defer srv.Close()

	c := &openaiClient{apiKey: "sk-test", model: "gpt-test", endpoint: srv.URL, http: http.DefaultClient}
	got, err := c.Complete(context.Background(), Request{
		System:    "system",
		Messages:  []Message{{Role: RoleUser, Content: "go"}},
		MaxTokens: 64,
		JSONSchema: &JSONSchema{
			Name:   "emit",
			Schema: json.RawMessage(`{"type":"object","properties":{"answer":{"type":"string"}}}`),
		},
	})
	if err != nil {
		t.Fatalf("Complete: %v", err)
	}
	if got.Content != `{"answer":"42"}` {
		t.Fatalf("content: %q", got.Content)
	}
	if got.InputTokens != 11 || got.OutputTokens != 3 || got.CachedTokens != 7 {
		t.Fatalf("usage: %+v", got)
	}
}

func TestOpenAIRejectsNonJSONWhenSchemaRequested(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`{"model":"m","choices":[{"message":{"content":"not json"},"finish_reason":"stop"}],"usage":{}}`))
	}))
	defer srv.Close()
	c := &openaiClient{apiKey: "k", model: "m", endpoint: srv.URL, http: http.DefaultClient}
	_, err := c.Complete(context.Background(), Request{
		Messages:   []Message{{Role: RoleUser, Content: "x"}},
		MaxTokens:  4,
		JSONSchema: &JSONSchema{Name: "emit", Schema: json.RawMessage(`{}`)},
	})
	if !errors.Is(err, ErrInvalidJSON) {
		t.Fatalf("expected ErrInvalidJSON, got %v", err)
	}
}

func TestOllamaHappyPathWithJSONSchema(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != ollamaChatPath {
			t.Errorf("wrong path: %s", r.URL.Path)
		}
		var sent map[string]any
		_ = json.NewDecoder(r.Body).Decode(&sent)
		if sent["model"] != "llama-test" {
			t.Errorf("model: %v", sent["model"])
		}
		if sent["stream"] != false {
			t.Errorf("expected stream:false, got %v", sent["stream"])
		}
		if _, ok := sent["format"]; !ok {
			t.Errorf("missing format (JSON schema) field")
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{
			"model":"llama-test",
			"message":{"role":"assistant","content":"{\"answer\":\"42\"}"},
			"done_reason":"stop",
			"prompt_eval_count":11,
			"eval_count":3
		}`))
	}))
	defer srv.Close()

	c := &ollamaClient{host: srv.URL, model: "llama-test", http: http.DefaultClient}
	got, err := c.Complete(context.Background(), Request{
		System:   "system",
		Messages: []Message{{Role: RoleUser, Content: "go"}},
		MaxTokens: 64,
		JSONSchema: &JSONSchema{
			Name:   "emit",
			Schema: json.RawMessage(`{"type":"object","properties":{"answer":{"type":"string"}}}`),
		},
	})
	if err != nil {
		t.Fatalf("Complete: %v", err)
	}
	if got.Content != `{"answer":"42"}` {
		t.Fatalf("content: %q", got.Content)
	}
	if got.InputTokens != 11 || got.OutputTokens != 3 {
		t.Fatalf("usage: %+v", got)
	}
	if got.Provider != "ollama" {
		t.Fatalf("provider: %s", got.Provider)
	}
}

func TestOllamaRejectsNonJSONWhenSchemaRequested(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`{"model":"m","message":{"role":"assistant","content":"not json"},"done_reason":"stop"}`))
	}))
	defer srv.Close()
	c := &ollamaClient{host: srv.URL, model: "m", http: http.DefaultClient}
	_, err := c.Complete(context.Background(), Request{
		Messages:   []Message{{Role: RoleUser, Content: "x"}},
		MaxTokens:  4,
		JSONSchema: &JSONSchema{Name: "emit", Schema: json.RawMessage(`{}`)},
	})
	if !errors.Is(err, ErrInvalidJSON) {
		t.Fatalf("expected ErrInvalidJSON, got %v", err)
	}
}

func TestNewOllamaFillsDefaultHost(t *testing.T) {
	c := NewOllama("", "llama3.2").(*ollamaClient)
	if c.host != OllamaDefaultHost {
		t.Fatalf("expected default host %q, got %q", OllamaDefaultHost, c.host)
	}
}

func TestEmptyMessagesReturnsErr(t *testing.T) {
	c := NewAnthropic("k", "m")
	if _, err := c.Complete(context.Background(), Request{}); !errors.Is(err, ErrNoMessages) {
		t.Fatalf("anthropic: want ErrNoMessages, got %v", err)
	}
	c = NewOpenAI("k", "m")
	if _, err := c.Complete(context.Background(), Request{}); !errors.Is(err, ErrNoMessages) {
		t.Fatalf("openai: want ErrNoMessages, got %v", err)
	}
	c = NewOllama("", "m")
	if _, err := c.Complete(context.Background(), Request{}); !errors.Is(err, ErrNoMessages) {
		t.Fatalf("ollama: want ErrNoMessages, got %v", err)
	}
}
