package llm

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"io"
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

func TestAnthropicImageMessageEncodesContentBlocks(t *testing.T) {
	imgBytes := []byte{0x89, 'P', 'N', 'G', 0x0d, 0x0a, 0x1a, 0x0a}
	wantB64 := base64.StdEncoding.EncodeToString(imgBytes)

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var sent map[string]any
		if err := json.NewDecoder(r.Body).Decode(&sent); err != nil {
			t.Fatalf("decode req: %v", err)
		}
		msgs, _ := sent["messages"].([]any)
		if len(msgs) != 1 {
			t.Fatalf("expected 1 message, got %d", len(msgs))
		}
		m0, _ := msgs[0].(map[string]any)
		blocks, ok := m0["content"].([]any)
		if !ok {
			t.Fatalf("content not a list: %T %v", m0["content"], m0["content"])
		}
		if len(blocks) != 2 {
			t.Fatalf("expected 2 blocks, got %d", len(blocks))
		}
		b0, _ := blocks[0].(map[string]any)
		if b0["type"] != "image" {
			t.Fatalf("block 0 type: %v", b0["type"])
		}
		src, _ := b0["source"].(map[string]any)
		if src["media_type"] != "image/png" {
			t.Fatalf("media_type: %v", src["media_type"])
		}
		if src["data"] != wantB64 {
			t.Fatalf("data: %v want %v", src["data"], wantB64)
		}
		b1, _ := blocks[1].(map[string]any)
		if b1["type"] != "text" || b1["text"] != "what is this" {
			t.Fatalf("text block: %+v", b1)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"content":[{"type":"text","text":"a logo"}],"stop_reason":"end_turn","model":"claude-test","usage":{"input_tokens":1,"output_tokens":1}}`))
	}))
	defer srv.Close()

	c := &anthropicClient{apiKey: "k", model: "claude-test", endpoint: srv.URL, http: http.DefaultClient}
	_, err := c.Complete(context.Background(), Request{
		Messages: []Message{{
			Role:    RoleUser,
			Content: "what is this",
			Images:  []Image{{MediaType: "image/png", Data: imgBytes}},
		}},
		MaxTokens: 16,
	})
	if err != nil {
		t.Fatalf("Complete: %v", err)
	}
}

func TestAnthropicToolUseRoundTrip(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var sent map[string]any
		_ = json.NewDecoder(r.Body).Decode(&sent)
		tools, _ := sent["tools"].([]any)
		if len(tools) != 1 {
			t.Fatalf("expected 1 tool, got %d", len(tools))
		}
		t0, _ := tools[0].(map[string]any)
		if t0["name"] != "get_weather" {
			t.Fatalf("tool name: %v", t0["name"])
		}
		if _, ok := t0["input_schema"]; !ok {
			t.Fatalf("missing input_schema on tool")
		}
		if _, ok := sent["tool_choice"]; ok {
			t.Fatalf("tool_choice should not be forced when not using JSONSchema")
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{
			"content":[
				{"type":"text","text":"let me check"},
				{"type":"tool_use","name":"get_weather","input":{"city":"sf"}}
			],
			"stop_reason":"tool_use",
			"model":"claude-test",
			"usage":{"input_tokens":5,"output_tokens":2}
		}`))
	}))
	defer srv.Close()

	c := &anthropicClient{apiKey: "k", model: "claude-test", endpoint: srv.URL, http: http.DefaultClient}
	got, err := c.Complete(context.Background(), Request{
		Messages:  []Message{{Role: RoleUser, Content: "weather in sf?"}},
		MaxTokens: 32,
		Tools: []Tool{{
			Name:        "get_weather",
			Description: "fetch the weather",
			Schema:      json.RawMessage(`{"type":"object","properties":{"city":{"type":"string"}}}`),
		}},
	})
	if err != nil {
		t.Fatalf("Complete: %v", err)
	}
	if got.StopReason != "tool_use" {
		t.Fatalf("stop_reason: %s", got.StopReason)
	}
	if len(got.ToolCalls) != 1 {
		t.Fatalf("expected 1 tool call, got %d", len(got.ToolCalls))
	}
	if got.ToolCalls[0].Name != "get_weather" {
		t.Fatalf("tool name: %s", got.ToolCalls[0].Name)
	}
	var args map[string]string
	if err := json.Unmarshal(got.ToolCalls[0].Arguments, &args); err != nil {
		t.Fatalf("args parse: %v", err)
	}
	if args["city"] != "sf" {
		t.Fatalf("args: %v", args)
	}
	if got.Content != "let me check" {
		t.Fatalf("content: %q", got.Content)
	}
}

func TestOpenAIImageEmbedsDataURL(t *testing.T) {
	imgBytes := []byte{0xff, 0xd8, 0xff, 0xe0}
	wantB64 := base64.StdEncoding.EncodeToString(imgBytes)

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var sent map[string]any
		_ = json.NewDecoder(r.Body).Decode(&sent)
		msgs, _ := sent["messages"].([]any)
		// system + user
		if len(msgs) != 1 {
			t.Fatalf("expected 1 message, got %d", len(msgs))
		}
		m0, _ := msgs[0].(map[string]any)
		parts, ok := m0["content"].([]any)
		if !ok {
			t.Fatalf("content not a list: %T", m0["content"])
		}
		if len(parts) != 2 {
			t.Fatalf("expected 2 parts, got %d", len(parts))
		}
		// part 0 is text, part 1 is image_url
		p1, _ := parts[1].(map[string]any)
		if p1["type"] != "image_url" {
			t.Fatalf("part 1 type: %v", p1["type"])
		}
		urlObj, _ := p1["image_url"].(map[string]any)
		got, _ := urlObj["url"].(string)
		wantPrefix := "data:image/jpeg;base64,"
		if !strings.HasPrefix(got, wantPrefix) {
			t.Fatalf("url prefix: %q", got)
		}
		if !strings.HasSuffix(got, wantB64) {
			t.Fatalf("url payload: %q", got)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"model":"gpt-test","choices":[{"message":{"content":"a cat"},"finish_reason":"stop"}],"usage":{}}`))
	}))
	defer srv.Close()

	c := &openaiClient{apiKey: "k", model: "gpt-test", endpoint: srv.URL, http: http.DefaultClient}
	_, err := c.Complete(context.Background(), Request{
		Messages: []Message{{
			Role:    RoleUser,
			Content: "describe this",
			Images:  []Image{{MediaType: "image/jpeg", Data: imgBytes}},
		}},
		MaxTokens: 16,
	})
	if err != nil {
		t.Fatalf("Complete: %v", err)
	}
}

func TestOpenAIToolCallParsed(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var sent map[string]any
		_ = json.NewDecoder(r.Body).Decode(&sent)
		tools, _ := sent["tools"].([]any)
		if len(tools) != 1 {
			t.Fatalf("expected 1 tool, got %d", len(tools))
		}
		t0, _ := tools[0].(map[string]any)
		if t0["type"] != "function" {
			t.Fatalf("tool type: %v", t0["type"])
		}
		fn, _ := t0["function"].(map[string]any)
		if fn["name"] != "lookup" {
			t.Fatalf("function name: %v", fn["name"])
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{
			"model":"gpt-test",
			"choices":[{"message":{"content":"","tool_calls":[{"id":"call_1","type":"function","function":{"name":"lookup","arguments":"{\"q\":\"x\"}"}}]},"finish_reason":"tool_calls"}],
			"usage":{}
		}`))
	}))
	defer srv.Close()

	c := &openaiClient{apiKey: "k", model: "gpt-test", endpoint: srv.URL, http: http.DefaultClient}
	got, err := c.Complete(context.Background(), Request{
		Messages:  []Message{{Role: RoleUser, Content: "look it up"}},
		MaxTokens: 16,
		Tools: []Tool{{
			Name:        "lookup",
			Description: "look something up",
			Schema:      json.RawMessage(`{"type":"object","properties":{"q":{"type":"string"}}}`),
		}},
	})
	if err != nil {
		t.Fatalf("Complete: %v", err)
	}
	if len(got.ToolCalls) != 1 {
		t.Fatalf("expected 1 tool call, got %d", len(got.ToolCalls))
	}
	if got.ToolCalls[0].Name != "lookup" {
		t.Fatalf("name: %s", got.ToolCalls[0].Name)
	}
	var args map[string]string
	if err := json.Unmarshal(got.ToolCalls[0].Arguments, &args); err != nil {
		t.Fatalf("args parse: %v (%s)", err, got.ToolCalls[0].Arguments)
	}
	if args["q"] != "x" {
		t.Fatalf("args: %v", args)
	}
}

func TestOllamaImageRoundTrip(t *testing.T) {
	imgBytes := []byte{0x01, 0x02, 0x03, 0x04}
	wantB64 := base64.StdEncoding.EncodeToString(imgBytes)

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var sent map[string]any
		_ = json.NewDecoder(r.Body).Decode(&sent)
		msgs, _ := sent["messages"].([]any)
		if len(msgs) != 1 {
			t.Fatalf("expected 1 message, got %d", len(msgs))
		}
		m0, _ := msgs[0].(map[string]any)
		imgs, ok := m0["images"].([]any)
		if !ok {
			t.Fatalf("images missing or wrong type: %T %v", m0["images"], m0["images"])
		}
		if len(imgs) != 1 {
			t.Fatalf("expected 1 image, got %d", len(imgs))
		}
		if imgs[0] != wantB64 {
			t.Fatalf("image b64: %v want %v", imgs[0], wantB64)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"model":"llava","message":{"role":"assistant","content":"a logo"},"done_reason":"stop"}`))
	}))
	defer srv.Close()

	c := &ollamaClient{host: srv.URL, model: "llava", http: http.DefaultClient}
	_, err := c.Complete(context.Background(), Request{
		Messages: []Message{{
			Role:    RoleUser,
			Content: "what is this",
			Images:  []Image{{MediaType: "image/png", Data: imgBytes}},
		}},
		MaxTokens: 16,
	})
	if err != nil {
		t.Fatalf("Complete: %v", err)
	}
}

func TestOllamaToolCallParsed(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var sent map[string]any
		_ = json.NewDecoder(r.Body).Decode(&sent)
		tools, _ := sent["tools"].([]any)
		if len(tools) != 1 {
			t.Fatalf("expected 1 tool, got %d", len(tools))
		}
		t0, _ := tools[0].(map[string]any)
		if t0["type"] != "function" {
			t.Fatalf("tool type: %v", t0["type"])
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{
			"model":"llama3.1",
			"message":{"role":"assistant","content":"","tool_calls":[{"function":{"name":"lookup","arguments":{"q":"x"}}}]},
			"done_reason":"stop"
		}`))
	}))
	defer srv.Close()

	c := &ollamaClient{host: srv.URL, model: "llama3.1", http: http.DefaultClient}
	got, err := c.Complete(context.Background(), Request{
		Messages:  []Message{{Role: RoleUser, Content: "look it up"}},
		MaxTokens: 16,
		Tools: []Tool{{
			Name:        "lookup",
			Description: "look something up",
			Schema:      json.RawMessage(`{"type":"object","properties":{"q":{"type":"string"}}}`),
		}},
	})
	if err != nil {
		t.Fatalf("Complete: %v", err)
	}
	if got.StopReason != "tool_use" {
		t.Fatalf("stop_reason: %s", got.StopReason)
	}
	if len(got.ToolCalls) != 1 {
		t.Fatalf("expected 1 tool call, got %d", len(got.ToolCalls))
	}
	if got.ToolCalls[0].Name != "lookup" {
		t.Fatalf("name: %s", got.ToolCalls[0].Name)
	}
	var args map[string]string
	if err := json.Unmarshal(got.ToolCalls[0].Arguments, &args); err != nil {
		t.Fatalf("args parse: %v (%s)", err, got.ToolCalls[0].Arguments)
	}
	if args["q"] != "x" {
		t.Fatalf("args: %v", args)
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

// TestOllamaMultiTurnToolResultMessages verifies that a multi-turn request
// (assistant with tool_calls + user with tool_results) is serialised correctly
// in the Ollama /api/chat wire format.
func TestOllamaMultiTurnToolResultMessages(t *testing.T) {
	var capturedBody []byte
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		capturedBody, _ = io.ReadAll(r.Body)
		w.Header().Set("Content-Type", "application/json")
		// Return a final text response — no more tool calls.
		_, _ = w.Write([]byte(`{
			"model":"llama3.2",
			"message":{"role":"assistant","content":"The API endpoint is https://api.example.com/v1"},
			"done_reason":"stop"
		}`))
	}))
	defer srv.Close()

	c := &ollamaClient{host: srv.URL, model: "llama3.2", http: http.DefaultClient}
	_, err := c.Complete(context.Background(), Request{
		Messages: []Message{
			// Turn 1: user asks a question.
			{Role: RoleUser, Content: "What is the API endpoint in config.json?"},
			// Turn 2: assistant called readFile (carried from previous response).
			{
				Role:    RoleAssistant,
				Content: "",
				ToolCalls: []ToolCall{
					{ID: "toolu_1", Name: "readFile", Arguments: json.RawMessage(`{"path":"config.json"}`)},
				},
			},
			// Turn 3: user returns tool result.
			{
				Role: RoleUser,
				ToolResults: []ToolResult{
					{ToolUseID: "toolu_1", Content: `{"apiEndpoint":"https://api.example.com/v1"}`},
				},
			},
		},
		MaxTokens: 256,
	})
	if err != nil {
		t.Fatalf("Complete: %v", err)
	}

	var payload map[string]any
	if err := json.Unmarshal(capturedBody, &payload); err != nil {
		t.Fatalf("decode captured body: %v", err)
	}
	msgs, _ := payload["messages"].([]any)

	// Expect 3 messages: user, assistant (tool_calls), tool.
	if len(msgs) != 3 {
		t.Fatalf("want 3 messages in wire payload, got %d: %v", len(msgs), msgs)
	}

	// Message 0: user text.
	m0, _ := msgs[0].(map[string]any)
	if m0["role"] != "user" {
		t.Errorf("msg[0] role: want user, got %v", m0["role"])
	}

	// Message 1: assistant with tool_calls.
	m1, _ := msgs[1].(map[string]any)
	if m1["role"] != "assistant" {
		t.Errorf("msg[1] role: want assistant, got %v", m1["role"])
	}
	calls, _ := m1["tool_calls"].([]any)
	if len(calls) != 1 {
		t.Fatalf("msg[1] tool_calls: want 1, got %d", len(calls))
	}
	call0, _ := calls[0].(map[string]any)
	fn, _ := call0["function"].(map[string]any)
	if fn["name"] != "readFile" {
		t.Errorf("tool call name: want readFile, got %v", fn["name"])
	}

	// Message 2: tool result (Ollama uses role "tool").
	m2, _ := msgs[2].(map[string]any)
	if m2["role"] != "tool" {
		t.Errorf("msg[2] role: want tool, got %v", m2["role"])
	}
	if m2["content"] == "" {
		t.Error("msg[2] content should not be empty")
	}
}

// TestAnthropicMultiTurnToolResultMessages verifies the Anthropic adapter
// serialises multi-turn tool results as tool_use + tool_result content blocks.
func TestAnthropicMultiTurnToolResultMessages(t *testing.T) {
	var capturedBody []byte
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		capturedBody, _ = io.ReadAll(r.Body)
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{
			"content":[{"type":"text","text":"The endpoint is https://api.example.com/v1"}],
			"stop_reason":"end_turn",
			"model":"claude-haiku-4-5",
			"usage":{"input_tokens":10,"output_tokens":20}
		}`))
	}))
	defer srv.Close()

	c := &anthropicClient{
		apiKey:   "test-key",
		model:    "claude-haiku-4-5",
		endpoint: srv.URL,
		http:     http.DefaultClient,
	}
	_, err := c.Complete(context.Background(), Request{
		Messages: []Message{
			{Role: RoleUser, Content: "What is the API endpoint?"},
			// Assistant turn: carried tool_use content blocks.
			{
				Role:    RoleAssistant,
				Content: "I'll read the file.",
				ToolCalls: []ToolCall{
					{ID: "toolu_abc", Name: "readFile", Arguments: json.RawMessage(`{"path":"config.json"}`)},
				},
			},
			// User turn: returning tool_result content blocks.
			{
				Role: RoleUser,
				ToolResults: []ToolResult{
					{ToolUseID: "toolu_abc", Content: `{"apiEndpoint":"https://api.example.com/v1"}`},
				},
			},
		},
		MaxTokens: 256,
	})
	if err != nil {
		t.Fatalf("Complete: %v", err)
	}

	var payload map[string]any
	if err := json.Unmarshal(capturedBody, &payload); err != nil {
		t.Fatalf("decode captured body: %v", err)
	}
	msgs, _ := payload["messages"].([]any)

	// Expect 3 messages: user text, assistant content-block array, user tool_result array.
	if len(msgs) != 3 {
		t.Fatalf("want 3 messages, got %d: %v", len(msgs), msgs)
	}

	// Message 1: assistant content blocks must include tool_use.
	m1, _ := msgs[1].(map[string]any)
	if m1["role"] != "assistant" {
		t.Errorf("msg[1] role: want assistant, got %v", m1["role"])
	}
	m1content, _ := m1["content"].([]any)
	var hasToolUse bool
	for _, blk := range m1content {
		b, _ := blk.(map[string]any)
		if b["type"] == "tool_use" {
			hasToolUse = true
			if b["id"] != "toolu_abc" {
				t.Errorf("tool_use id: want toolu_abc, got %v", b["id"])
			}
		}
	}
	if !hasToolUse {
		t.Error("msg[1] content must contain a tool_use block")
	}

	// Message 2: user content blocks must be tool_result blocks.
	m2, _ := msgs[2].(map[string]any)
	if m2["role"] != "user" {
		t.Errorf("msg[2] role: want user, got %v", m2["role"])
	}
	m2content, _ := m2["content"].([]any)
	if len(m2content) == 0 {
		t.Fatal("msg[2] content must not be empty")
	}
	blk0, _ := m2content[0].(map[string]any)
	if blk0["type"] != "tool_result" {
		t.Errorf("msg[2] block type: want tool_result, got %v", blk0["type"])
	}
	if blk0["tool_use_id"] != "toolu_abc" {
		t.Errorf("tool_result tool_use_id: want toolu_abc, got %v", blk0["tool_use_id"])
	}
}
