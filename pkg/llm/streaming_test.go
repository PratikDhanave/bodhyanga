package llm

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// collect drains the channel into a slice and returns it. Bound the wait
// with the supplied context — tests pass a short timeout so a hung stream
// doesn't deadlock the suite.
func collect(t *testing.T, ctx context.Context, ch <-chan StreamChunk) []StreamChunk {
	t.Helper()
	var out []StreamChunk
	for {
		select {
		case <-ctx.Done():
			t.Fatalf("collect: context done with %d chunks: %v", len(out), ctx.Err())
		case c, ok := <-ch:
			if !ok {
				return out
			}
			out = append(out, c)
			if c.Done {
				// Drain anything that arrives after Done (shouldn't be any).
				// We don't return immediately so a misbehaving stream that
				// sends extra chunks is detectable.
				for c2 := range ch {
					out = append(out, c2)
				}
				return out
			}
		}
	}
}

func TestMockStream_EmitsContentChunksThenDone(t *testing.T) {
	want := &Response{Content: "hello world there", Provider: "mock", Model: "mock-model", OutputTokens: 3}
	m := &Mock{Resp: want}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	ch, err := m.Stream(ctx, Request{Messages: []Message{{Role: RoleUser, Content: "hi"}}})
	if err != nil {
		t.Fatalf("Stream: %v", err)
	}
	chunks := collect(t, ctx, ch)

	// We expect 3 word chunks plus a Done chunk.
	if len(chunks) != 4 {
		t.Fatalf("expected 4 chunks, got %d: %+v", len(chunks), chunks)
	}
	var assembled strings.Builder
	for _, c := range chunks[:3] {
		if c.Done {
			t.Fatalf("non-final chunk had Done=true: %+v", c)
		}
		assembled.WriteString(c.Content)
	}
	if assembled.String() != "hello world there" {
		t.Fatalf("reassembled: %q", assembled.String())
	}
	last := chunks[3]
	if !last.Done {
		t.Fatalf("last chunk not Done: %+v", last)
	}
	if last.Final == nil || last.Final.Content != "hello world there" {
		t.Fatalf("Final not populated: %+v", last.Final)
	}
	if len(m.Calls) != 1 {
		t.Fatalf("expected 1 call recorded, got %d", len(m.Calls))
	}
}

func TestAnthropicStream_ParsesContentBlockDeltas(t *testing.T) {
	// A canned SSE stream: message_start, content_block_start (text),
	// two text_deltas, content_block_stop, message_delta with usage/stop,
	// message_stop.
	sse := strings.Join([]string{
		`event: message_start`,
		`data: {"type":"message_start","message":{"id":"m1","model":"claude-test","usage":{"input_tokens":7,"cache_read_input_tokens":2}}}`,
		``,
		`event: content_block_start`,
		`data: {"type":"content_block_start","index":0,"content_block":{"type":"text","text":""}}`,
		``,
		`event: content_block_delta`,
		`data: {"type":"content_block_delta","index":0,"delta":{"type":"text_delta","text":"hello "}}`,
		``,
		`event: content_block_delta`,
		`data: {"type":"content_block_delta","index":0,"delta":{"type":"text_delta","text":"world"}}`,
		``,
		`event: content_block_stop`,
		`data: {"type":"content_block_stop","index":0}`,
		``,
		`event: message_delta`,
		`data: {"type":"message_delta","delta":{"stop_reason":"end_turn"},"usage":{"output_tokens":4}}`,
		``,
		`event: message_stop`,
		`data: {"type":"message_stop"}`,
		``,
	}, "\n")

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = w.Write([]byte(sse))
	}))
	defer srv.Close()

	c := &anthropicClient{apiKey: "k", model: "claude-test", endpoint: srv.URL, http: http.DefaultClient}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	ch, err := c.Stream(ctx, Request{
		Messages:  []Message{{Role: RoleUser, Content: "go"}},
		MaxTokens: 32,
	})
	if err != nil {
		t.Fatalf("Stream: %v", err)
	}
	chunks := collect(t, ctx, ch)
	var assembled strings.Builder
	var done *StreamChunk
	for i := range chunks {
		c := chunks[i]
		if c.Done {
			done = &chunks[i]
			continue
		}
		assembled.WriteString(c.Content)
	}
	if assembled.String() != "hello world" {
		t.Fatalf("assembled: %q", assembled.String())
	}
	if done == nil {
		t.Fatalf("missing Done chunk: %+v", chunks)
	}
	if done.Final == nil {
		t.Fatalf("Done chunk missing Final: %+v", done)
	}
	if done.Final.Content != "hello world" {
		t.Fatalf("Final.Content: %q", done.Final.Content)
	}
	if done.Final.InputTokens != 7 || done.Final.OutputTokens != 4 || done.Final.CachedTokens != 2 {
		t.Fatalf("Final usage: %+v", done.Final)
	}
	if done.Final.StopReason != "end_turn" {
		t.Fatalf("Final stop: %s", done.Final.StopReason)
	}
	if done.Final.Provider != "anthropic" {
		t.Fatalf("Final provider: %s", done.Final.Provider)
	}
}

func TestAnthropicStream_AccumulatesToolUseBlock(t *testing.T) {
	// A canned stream with a tool_use content block: input_json_delta arrives
	// in two fragments then content_block_stop closes it.
	sse := strings.Join([]string{
		`event: message_start`,
		`data: {"type":"message_start","message":{"id":"m1","model":"claude-test","usage":{"input_tokens":3}}}`,
		``,
		`event: content_block_start`,
		`data: {"type":"content_block_start","index":0,"content_block":{"type":"tool_use","id":"toolu_1","name":"get_weather","input":{}}}`,
		``,
		`event: content_block_delta`,
		`data: {"type":"content_block_delta","index":0,"delta":{"type":"input_json_delta","partial_json":"{\"city\":"}}`,
		``,
		`event: content_block_delta`,
		`data: {"type":"content_block_delta","index":0,"delta":{"type":"input_json_delta","partial_json":"\"sf\"}"}}`,
		``,
		`event: content_block_stop`,
		`data: {"type":"content_block_stop","index":0}`,
		``,
		`event: message_delta`,
		`data: {"type":"message_delta","delta":{"stop_reason":"tool_use"},"usage":{"output_tokens":5}}`,
		``,
		`event: message_stop`,
		`data: {"type":"message_stop"}`,
		``,
	}, "\n")
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(sse))
	}))
	defer srv.Close()

	c := &anthropicClient{apiKey: "k", model: "claude-test", endpoint: srv.URL, http: http.DefaultClient}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	ch, err := c.Stream(ctx, Request{
		Messages:  []Message{{Role: RoleUser, Content: "weather in sf?"}},
		MaxTokens: 32,
	})
	if err != nil {
		t.Fatalf("Stream: %v", err)
	}
	chunks := collect(t, ctx, ch)
	var toolCallChunks int
	var done *StreamChunk
	for i := range chunks {
		c := chunks[i]
		if c.ToolCall != nil {
			toolCallChunks++
			if c.ToolCall.Name != "get_weather" {
				t.Errorf("tool name: %s", c.ToolCall.Name)
			}
			if string(c.ToolCall.Arguments) != `{"city":"sf"}` {
				t.Errorf("tool args: %s", string(c.ToolCall.Arguments))
			}
		}
		if c.Done {
			done = &chunks[i]
		}
	}
	if toolCallChunks != 1 {
		t.Fatalf("expected 1 tool-call chunk, got %d", toolCallChunks)
	}
	if done == nil || done.Final == nil {
		t.Fatalf("missing Done chunk: %+v", chunks)
	}
	if len(done.Final.ToolCalls) != 1 {
		t.Fatalf("Final.ToolCalls: %+v", done.Final.ToolCalls)
	}
	if done.Final.StopReason != "tool_use" {
		t.Fatalf("Final stop: %s", done.Final.StopReason)
	}
}

func TestOpenAIStream_HandlesDONE(t *testing.T) {
	body := strings.Join([]string{
		`data: {"model":"gpt-test","choices":[{"delta":{"content":"hello "}}]}`,
		``,
		`data: {"model":"gpt-test","choices":[{"delta":{"content":"world"}}]}`,
		``,
		`data: {"model":"gpt-test","choices":[{"delta":{},"finish_reason":"stop"}],"usage":{"prompt_tokens":4,"completion_tokens":2,"prompt_tokens_details":{"cached_tokens":1}}}`,
		``,
		`data: [DONE]`,
		``,
	}, "\n")

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer sk-test" {
			t.Errorf("missing auth header")
		}
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = w.Write([]byte(body))
	}))
	defer srv.Close()

	c := &openaiClient{apiKey: "sk-test", model: "gpt-test", endpoint: srv.URL, http: http.DefaultClient}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	ch, err := c.Stream(ctx, Request{
		Messages:  []Message{{Role: RoleUser, Content: "hi"}},
		MaxTokens: 8,
	})
	if err != nil {
		t.Fatalf("Stream: %v", err)
	}
	chunks := collect(t, ctx, ch)
	var assembled strings.Builder
	var done *StreamChunk
	for i := range chunks {
		c := chunks[i]
		if c.Done {
			done = &chunks[i]
			continue
		}
		assembled.WriteString(c.Content)
		if c.Content == "[DONE]" {
			t.Fatalf("[DONE] sentinel leaked as Content chunk")
		}
	}
	if assembled.String() != "hello world" {
		t.Fatalf("assembled: %q", assembled.String())
	}
	if done == nil || done.Final == nil {
		t.Fatalf("missing Done chunk")
	}
	if done.Final.Content != "hello world" {
		t.Fatalf("Final.Content: %q", done.Final.Content)
	}
	if done.Final.InputTokens != 4 || done.Final.OutputTokens != 2 || done.Final.CachedTokens != 1 {
		t.Fatalf("Final usage: %+v", done.Final)
	}
	if done.Final.StopReason != "stop" {
		t.Fatalf("Final stop: %s", done.Final.StopReason)
	}
	if done.Final.Provider != "openai" {
		t.Fatalf("Final provider: %s", done.Final.Provider)
	}
}

func TestOpenAIStream_AccumulatesToolCallArgs(t *testing.T) {
	body := strings.Join([]string{
		`data: {"model":"gpt-test","choices":[{"delta":{"tool_calls":[{"index":0,"id":"call_1","function":{"name":"lookup","arguments":"{\"q\":"}}]}}]}`,
		``,
		`data: {"model":"gpt-test","choices":[{"delta":{"tool_calls":[{"index":0,"function":{"arguments":"\"x\"}"}}]}}]}`,
		``,
		`data: {"model":"gpt-test","choices":[{"delta":{},"finish_reason":"tool_calls"}],"usage":{"prompt_tokens":3,"completion_tokens":4}}`,
		``,
		`data: [DONE]`,
		``,
	}, "\n")
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(body))
	}))
	defer srv.Close()

	c := &openaiClient{apiKey: "k", model: "gpt-test", endpoint: srv.URL, http: http.DefaultClient}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	ch, err := c.Stream(ctx, Request{
		Messages:  []Message{{Role: RoleUser, Content: "look it up"}},
		MaxTokens: 16,
	})
	if err != nil {
		t.Fatalf("Stream: %v", err)
	}
	chunks := collect(t, ctx, ch)
	var toolChunks int
	var done *StreamChunk
	for i := range chunks {
		c := chunks[i]
		if c.ToolCall != nil {
			toolChunks++
			if c.ToolCall.Name != "lookup" {
				t.Errorf("name: %s", c.ToolCall.Name)
			}
			if string(c.ToolCall.Arguments) != `{"q":"x"}` {
				t.Errorf("args: %s", string(c.ToolCall.Arguments))
			}
		}
		if c.Done {
			done = &chunks[i]
		}
	}
	if toolChunks != 1 {
		t.Fatalf("expected 1 ToolCall chunk, got %d", toolChunks)
	}
	if done == nil || done.Final == nil || len(done.Final.ToolCalls) != 1 {
		t.Fatalf("Done/Final/ToolCalls: %+v", done)
	}
	if done.Final.StopReason != "tool_calls" {
		t.Fatalf("Final stop: %s", done.Final.StopReason)
	}
}

func TestOllamaStream_NDJSONReassembly(t *testing.T) {
	lines := []string{
		`{"model":"llama-test","message":{"role":"assistant","content":"hello "},"done":false}`,
		`{"model":"llama-test","message":{"role":"assistant","content":"world"},"done":false}`,
		`{"model":"llama-test","message":{"role":"assistant","content":""},"done":true,"done_reason":"stop","prompt_eval_count":9,"eval_count":2}`,
	}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != ollamaChatPath {
			t.Errorf("path: %s", r.URL.Path)
		}
		for _, l := range lines {
			fmt.Fprintln(w, l)
		}
	}))
	defer srv.Close()

	c := &ollamaClient{host: srv.URL, model: "llama-test", http: http.DefaultClient}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	ch, err := c.Stream(ctx, Request{
		Messages:  []Message{{Role: RoleUser, Content: "hi"}},
		MaxTokens: 8,
	})
	if err != nil {
		t.Fatalf("Stream: %v", err)
	}
	chunks := collect(t, ctx, ch)
	var assembled strings.Builder
	var done *StreamChunk
	for i := range chunks {
		c := chunks[i]
		if c.Done {
			done = &chunks[i]
			continue
		}
		assembled.WriteString(c.Content)
	}
	if assembled.String() != "hello world" {
		t.Fatalf("assembled: %q", assembled.String())
	}
	if done == nil || done.Final == nil {
		t.Fatalf("missing Done/Final: %+v", chunks)
	}
	if done.Final.Content != "hello world" {
		t.Fatalf("Final.Content: %q", done.Final.Content)
	}
	if done.Final.InputTokens != 9 || done.Final.OutputTokens != 2 {
		t.Fatalf("Final usage: %+v", done.Final)
	}
	if done.Final.StopReason != "stop" {
		t.Fatalf("Final stop: %s", done.Final.StopReason)
	}
	if done.Final.Provider != "ollama" {
		t.Fatalf("Final provider: %s", done.Final.Provider)
	}
}

func TestCanStream_DetectsMockAndAdapters(t *testing.T) {
	cases := []struct {
		name string
		c    Client
	}{
		{"mock", &Mock{}},
		{"anthropic", NewAnthropic("k", "m")},
		{"openai", NewOpenAI("k", "m")},
		{"ollama", NewOllama("", "m")},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if !CanStream(tc.c) {
				t.Fatalf("expected %s to implement Streamer", tc.name)
			}
			if _, ok := tc.c.(Streamer); !ok {
				t.Fatalf("type assertion to Streamer failed for %s", tc.name)
			}
		})
	}
}
