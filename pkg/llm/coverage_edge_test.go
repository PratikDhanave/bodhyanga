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
// Streaming error mid-chunk — partial stream then error
// ---------------------------------------------------------------------------

func TestAnthropicStream_DecodeMidChunk(t *testing.T) {
	// Anthropic SSE stream where the second content_block_delta has invalid JSON.
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(200)
		flusher, _ := w.(http.Flusher)
		// Valid first delta
		fmt.Fprintln(w, "event: content_block_delta")
		fmt.Fprintln(w, `data: {"delta":{"type":"text_delta","text":"Hello"}}`)
		fmt.Fprintln(w)
		// Invalid JSON in second delta
		fmt.Fprintln(w, "event: content_block_delta")
		fmt.Fprintln(w, `data: {not valid json`)
		fmt.Fprintln(w)
		flusher.Flush()
	}))
	defer srv.Close()

	c := &anthropicClient{apiKey: "k", model: "test", endpoint: srv.URL, http: &http.Client{Timeout: 5 * time.Second}}
	ch, err := c.Stream(context.Background(), Request{
		Messages: []Message{{Role: RoleUser, Content: "hi"}},
	})
	if err != nil {
		t.Fatalf("Stream: %v", err)
	}

	var gotError bool
	var gotDelta bool
	for chunk := range ch {
		if chunk.Delta == "Hello" {
			gotDelta = true
		}
		if chunk.Err != nil {
			gotError = true
			if !strings.Contains(chunk.Err.Error(), "decode") {
				t.Errorf("expected decode error, got: %v", chunk.Err)
			}
		}
	}
	if !gotDelta {
		t.Error("expected at least one valid delta before error")
	}
	if !gotError {
		t.Error("expected a decode error chunk")
	}
}

func TestAnthropicStream_ContextCancelledMidStream(t *testing.T) {
	// Server sends one chunk then blocks, allowing us to cancel the context.
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(200)
		flusher, _ := w.(http.Flusher)
		fmt.Fprintln(w, "event: content_block_delta")
		fmt.Fprintln(w, `data: {"delta":{"type":"text_delta","text":"part"}}`)
		fmt.Fprintln(w)
		flusher.Flush()
		// Block until context is done so the goroutine sees ctx.Done()
		<-r.Context().Done()
	}))
	defer srv.Close()

	ctx, cancel := context.WithCancel(context.Background())
	c := &anthropicClient{apiKey: "k", model: "test", endpoint: srv.URL, http: &http.Client{Timeout: 30 * time.Second}}
	ch, err := c.Stream(ctx, Request{
		Messages: []Message{{Role: RoleUser, Content: "hi"}},
	})
	if err != nil {
		t.Fatalf("Stream: %v", err)
	}

	// Read the first chunk.
	chunk := <-ch
	if chunk.Delta != "part" {
		t.Fatalf("expected delta 'part', got %q", chunk.Delta)
	}

	// Cancel the context.
	cancel()

	// Drain remaining chunks — should see a context error or channel close.
	for chunk := range ch {
		if chunk.Err != nil {
			// Got context.Canceled error — expected.
			return
		}
	}
	// Channel closed — also acceptable since context cancellation can close the body.
}

func TestAnthropicStream_EndsWithoutMessageStop(t *testing.T) {
	// Server sends content but closes without message_stop event.
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(200)
		flusher, _ := w.(http.Flusher)
		fmt.Fprintln(w, "event: content_block_delta")
		fmt.Fprintln(w, `data: {"delta":{"type":"text_delta","text":"partial"}}`)
		fmt.Fprintln(w)
		flusher.Flush()
		// Close without message_stop
	}))
	defer srv.Close()

	c := &anthropicClient{apiKey: "k", model: "test", endpoint: srv.URL, http: &http.Client{Timeout: 5 * time.Second}}
	ch, err := c.Stream(context.Background(), Request{
		Messages: []Message{{Role: RoleUser, Content: "hi"}},
	})
	if err != nil {
		t.Fatalf("Stream: %v", err)
	}

	var gotDone bool
	var fullContent string
	for chunk := range ch {
		if chunk.Err != nil {
			t.Fatalf("unexpected error: %v", chunk.Err)
		}
		if chunk.Done {
			gotDone = true
			fullContent = chunk.FullContent
		}
	}
	if !gotDone {
		t.Error("expected a final done chunk even without message_stop")
	}
	if fullContent != "partial" {
		t.Errorf("expected FullContent='partial', got %q", fullContent)
	}
}

func TestAnthropicStream_NonTextDeltaIgnored(t *testing.T) {
	// Server sends a content_block_delta with type != text_delta.
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(200)
		flusher, _ := w.(http.Flusher)
		// Non-text delta (e.g. input_json_delta for tool use streaming)
		fmt.Fprintln(w, "event: content_block_delta")
		fmt.Fprintln(w, `data: {"delta":{"type":"input_json_delta","partial_json":"{\"key\":"}}`)
		fmt.Fprintln(w)
		// Followed by text
		fmt.Fprintln(w, "event: content_block_delta")
		fmt.Fprintln(w, `data: {"delta":{"type":"text_delta","text":"hello"}}`)
		fmt.Fprintln(w)
		fmt.Fprintln(w, "event: message_stop")
		fmt.Fprintln(w, `data: {}`)
		fmt.Fprintln(w)
		flusher.Flush()
	}))
	defer srv.Close()

	c := &anthropicClient{apiKey: "k", model: "test", endpoint: srv.URL, http: &http.Client{Timeout: 5 * time.Second}}
	ch, err := c.Stream(context.Background(), Request{
		Messages: []Message{{Role: RoleUser, Content: "hi"}},
	})
	if err != nil {
		t.Fatal(err)
	}

	var deltas []string
	for chunk := range ch {
		if chunk.Err != nil {
			t.Fatal(chunk.Err)
		}
		if chunk.Delta != "" {
			deltas = append(deltas, chunk.Delta)
		}
	}
	// Only the text_delta should appear as a delta.
	if len(deltas) != 1 || deltas[0] != "hello" {
		t.Errorf("expected only 'hello' delta, got %v", deltas)
	}
}

func TestAnthropicStream_SkipsNonDataLines(t *testing.T) {
	// SSE stream with comment lines and retry directives.
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(200)
		flusher, _ := w.(http.Flusher)
		// Comment line (starts with ":")
		fmt.Fprintln(w, ": keep-alive ping")
		fmt.Fprintln(w, "retry: 30000")
		fmt.Fprintln(w, "event: content_block_delta")
		fmt.Fprintln(w, `data: {"delta":{"type":"text_delta","text":"ok"}}`)
		fmt.Fprintln(w)
		fmt.Fprintln(w, "event: message_stop")
		fmt.Fprintln(w, `data: {}`)
		fmt.Fprintln(w)
		flusher.Flush()
	}))
	defer srv.Close()

	c := &anthropicClient{apiKey: "k", model: "test", endpoint: srv.URL, http: &http.Client{Timeout: 5 * time.Second}}
	ch, err := c.Stream(context.Background(), Request{
		Messages: []Message{{Role: RoleUser, Content: "hi"}},
	})
	if err != nil {
		t.Fatal(err)
	}

	var finalContent string
	for chunk := range ch {
		if chunk.Err != nil {
			t.Fatal(chunk.Err)
		}
		if chunk.Done {
			finalContent = chunk.FullContent
		}
	}
	if finalContent != "ok" {
		t.Errorf("expected 'ok', got %q", finalContent)
	}
}

// ---------------------------------------------------------------------------
// OpenAI streaming edge cases
// ---------------------------------------------------------------------------

func TestOpenAIStream_DecodeMidChunk(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(200)
		flusher, _ := w.(http.Flusher)
		fmt.Fprintln(w, `data: {"choices":[{"delta":{"content":"Hi"},"finish_reason":null}]}`)
		// Invalid JSON chunk
		fmt.Fprintln(w, `data: {broken json`)
		flusher.Flush()
	}))
	defer srv.Close()

	c := &openaiClient{apiKey: "k", model: "test", endpoint: srv.URL, http: &http.Client{Timeout: 5 * time.Second}}
	ch, err := c.Stream(context.Background(), Request{
		Messages: []Message{{Role: RoleUser, Content: "hi"}},
	})
	if err != nil {
		t.Fatal(err)
	}

	var gotError bool
	var gotDelta bool
	for chunk := range ch {
		if chunk.Delta == "Hi" {
			gotDelta = true
		}
		if chunk.Err != nil {
			gotError = true
			if !strings.Contains(chunk.Err.Error(), "decode") {
				t.Errorf("expected decode error, got: %v", chunk.Err)
			}
		}
	}
	if !gotDelta {
		t.Error("expected at least one valid delta before error")
	}
	if !gotError {
		t.Error("expected a decode error chunk")
	}
}

func TestOpenAIStream_ContextCancelledMidStream(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(200)
		flusher, _ := w.(http.Flusher)
		fmt.Fprintln(w, `data: {"choices":[{"delta":{"content":"part"},"finish_reason":null}]}`)
		flusher.Flush()
		// Block
		<-r.Context().Done()
	}))
	defer srv.Close()

	ctx, cancel := context.WithCancel(context.Background())
	c := &openaiClient{apiKey: "k", model: "test", endpoint: srv.URL, http: &http.Client{Timeout: 30 * time.Second}}
	ch, err := c.Stream(ctx, Request{
		Messages: []Message{{Role: RoleUser, Content: "hi"}},
	})
	if err != nil {
		t.Fatal(err)
	}

	chunk := <-ch
	if chunk.Delta != "part" {
		t.Fatalf("expected delta 'part', got %q", chunk.Delta)
	}

	cancel()

	for chunk := range ch {
		if chunk.Err != nil {
			return // expected
		}
	}
	// Channel closed — acceptable.
}

func TestOpenAIStream_FinishReasonMidStream(t *testing.T) {
	// Test the finish_reason path (not [DONE]).
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(200)
		flusher, _ := w.(http.Flusher)
		fmt.Fprintln(w, `data: {"choices":[{"delta":{"content":"hello"},"finish_reason":null}]}`)
		// Use finish_reason to end the stream instead of [DONE].
		stop := "stop"
		_ = stop
		fmt.Fprintln(w, `data: {"choices":[{"delta":{"content":""},"finish_reason":"stop"}]}`)
		flusher.Flush()
	}))
	defer srv.Close()

	c := &openaiClient{apiKey: "k", model: "test", endpoint: srv.URL, http: &http.Client{Timeout: 5 * time.Second}}
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
		t.Error("expected done chunk via finish_reason")
	}
}

func TestOpenAIStream_EmptyChoicesSkipped(t *testing.T) {
	// Test that empty choices arrays are skipped gracefully.
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(200)
		flusher, _ := w.(http.Flusher)
		// Empty choices event (sometimes sent at stream start)
		fmt.Fprintln(w, `data: {"choices":[]}`)
		fmt.Fprintln(w, `data: {"choices":[{"delta":{"content":"ok"},"finish_reason":null}]}`)
		fmt.Fprintln(w, `data: [DONE]`)
		flusher.Flush()
	}))
	defer srv.Close()

	c := &openaiClient{apiKey: "k", model: "test", endpoint: srv.URL, http: &http.Client{Timeout: 5 * time.Second}}
	ch, err := c.Stream(context.Background(), Request{
		Messages: []Message{{Role: RoleUser, Content: "hi"}},
	})
	if err != nil {
		t.Fatal(err)
	}

	var gotDone bool
	var content string
	for chunk := range ch {
		if chunk.Err != nil {
			t.Fatal(chunk.Err)
		}
		if chunk.Delta != "" {
			content += chunk.Delta
		}
		if chunk.Done {
			gotDone = true
		}
	}
	if !gotDone {
		t.Error("expected done chunk")
	}
	if content != "ok" {
		t.Errorf("expected 'ok', got %q", content)
	}
}

func TestOpenAIStream_EndsWithoutDONE(t *testing.T) {
	// Stream ends (server closes connection) without sending [DONE].
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(200)
		flusher, _ := w.(http.Flusher)
		fmt.Fprintln(w, `data: {"choices":[{"delta":{"content":"partial"},"finish_reason":null}]}`)
		flusher.Flush()
		// Close without [DONE]
	}))
	defer srv.Close()

	c := &openaiClient{apiKey: "k", model: "test", endpoint: srv.URL, http: &http.Client{Timeout: 5 * time.Second}}
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
		t.Error("expected done chunk even without [DONE]")
	}
}

func TestOpenAIStream_NonDataLinesSkipped(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(200)
		flusher, _ := w.(http.Flusher)
		fmt.Fprintln(w, ": comment line")
		fmt.Fprintln(w, "event: something")
		fmt.Fprintln(w, `data: {"choices":[{"delta":{"content":"ok"},"finish_reason":null}]}`)
		fmt.Fprintln(w, `data: [DONE]`)
		flusher.Flush()
	}))
	defer srv.Close()

	c := &openaiClient{apiKey: "k", model: "test", endpoint: srv.URL, http: &http.Client{Timeout: 5 * time.Second}}
	ch, err := c.Stream(context.Background(), Request{
		Messages: []Message{{Role: RoleUser, Content: "hi"}},
	})
	if err != nil {
		t.Fatal(err)
	}

	var content string
	for chunk := range ch {
		if chunk.Err != nil {
			t.Fatal(chunk.Err)
		}
		content += chunk.Delta
	}
	if content != "ok" {
		t.Errorf("expected 'ok', got %q", content)
	}
}

// ---------------------------------------------------------------------------
// Ollama streaming edge cases
// ---------------------------------------------------------------------------

func TestOllamaStream_DecodeMidChunk(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/x-ndjson")
		flusher, _ := w.(http.Flusher)
		fmt.Fprintln(w, `{"message":{"content":"ok"},"done":false}`)
		fmt.Fprintln(w, `{broken json`)
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

	var gotError bool
	var gotDelta bool
	for chunk := range ch {
		if chunk.Delta == "ok" {
			gotDelta = true
		}
		if chunk.Err != nil {
			gotError = true
			if !strings.Contains(chunk.Err.Error(), "decode") {
				t.Errorf("expected decode error, got: %v", chunk.Err)
			}
		}
	}
	if !gotDelta {
		t.Error("expected at least one valid delta")
	}
	if !gotError {
		t.Error("expected decode error")
	}
}

func TestOllamaStream_EmptyLinesSkipped(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/x-ndjson")
		flusher, _ := w.(http.Flusher)
		fmt.Fprintln(w, "")
		fmt.Fprintln(w, `{"message":{"content":"hi"},"done":false}`)
		fmt.Fprintln(w, "")
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

	var fullContent string
	for chunk := range ch {
		if chunk.Err != nil {
			t.Fatal(chunk.Err)
		}
		if chunk.Done {
			fullContent = chunk.FullContent
		}
	}
	if fullContent != "hi there" {
		t.Errorf("expected 'hi there', got %q", fullContent)
	}
}

// ---------------------------------------------------------------------------
// HTTP post error paths
// ---------------------------------------------------------------------------

func TestAnthropicPost_ContextCancelled(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Block until context cancelled
		<-r.Context().Done()
	}))
	defer srv.Close()

	ctx, cancel := context.WithCancel(context.Background())
	cancel() // cancel immediately

	c := &anthropicClient{apiKey: "k", model: "m", endpoint: srv.URL, http: &http.Client{Timeout: 5 * time.Second}}
	_, err := c.Complete(ctx, Request{
		Messages: []Message{{Role: RoleUser, Content: "hi"}},
	})
	if err == nil {
		t.Fatal("expected error for cancelled context")
	}
}

func TestOpenAIPost_ContextCancelled(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		<-r.Context().Done()
	}))
	defer srv.Close()

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	c := &openaiClient{apiKey: "k", model: "m", endpoint: srv.URL, http: &http.Client{Timeout: 5 * time.Second}}
	_, err := c.Complete(ctx, Request{
		Messages: []Message{{Role: RoleUser, Content: "hi"}},
	})
	if err == nil {
		t.Fatal("expected error for cancelled context")
	}
}

func TestOllamaPost_ContextCancelled(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		<-r.Context().Done()
	}))
	defer srv.Close()

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	c := &ollamaClient{host: srv.URL, model: "m", http: &http.Client{Timeout: 5 * time.Second}}
	_, err := c.Complete(ctx, Request{
		Messages: []Message{{Role: RoleUser, Content: "hi"}},
	})
	if err == nil {
		t.Fatal("expected error for cancelled context")
	}
}

// ---------------------------------------------------------------------------
// HTTP error response body included in error message
// ---------------------------------------------------------------------------

func TestAnthropicPost_ErrorBodyIncluded(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(429)
		w.Write([]byte(`{"error":{"message":"rate limited","type":"rate_limit_error"}}`))
	}))
	defer srv.Close()

	c := &anthropicClient{apiKey: "k", model: "m", endpoint: srv.URL, http: &http.Client{Timeout: 5 * time.Second}}
	_, err := c.Complete(context.Background(), Request{
		Messages: []Message{{Role: RoleUser, Content: "hi"}},
	})
	if err == nil {
		t.Fatal("expected error")
	}
	if !strings.Contains(err.Error(), "429") {
		t.Errorf("error should contain status code: %v", err)
	}
	if !strings.Contains(err.Error(), "rate_limit_error") {
		t.Errorf("error should contain response body: %v", err)
	}
}

func TestOpenAIPost_ErrorBodyIncluded(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(401)
		w.Write([]byte(`{"error":{"message":"invalid api key"}}`))
	}))
	defer srv.Close()

	c := &openaiClient{apiKey: "bad", model: "m", endpoint: srv.URL, http: &http.Client{Timeout: 5 * time.Second}}
	_, err := c.Complete(context.Background(), Request{
		Messages: []Message{{Role: RoleUser, Content: "hi"}},
	})
	if err == nil {
		t.Fatal("expected error")
	}
	if !strings.Contains(err.Error(), "401") {
		t.Errorf("error should contain 401: %v", err)
	}
	if !strings.Contains(err.Error(), "invalid api key") {
		t.Errorf("error should contain body: %v", err)
	}
}

func TestOllamaPost_ErrorBodyIncluded(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(404)
		w.Write([]byte(`{"error":"model not found: nonexistent"}`))
	}))
	defer srv.Close()

	c := &ollamaClient{host: srv.URL, model: "nonexistent", http: &http.Client{Timeout: 5 * time.Second}}
	_, err := c.Complete(context.Background(), Request{
		Messages: []Message{{Role: RoleUser, Content: "hi"}},
	})
	if err == nil {
		t.Fatal("expected error")
	}
	if !strings.Contains(err.Error(), "404") {
		t.Errorf("error should contain 404: %v", err)
	}
	if !strings.Contains(err.Error(), "model not found") {
		t.Errorf("error should contain body: %v", err)
	}
}

// ---------------------------------------------------------------------------
// Model-not-found scenarios (simulated via HTTP 404)
// ---------------------------------------------------------------------------

func TestOllama_ModelNotFoundError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(404)
		w.Write([]byte(`{"error":"model \"nonexistent\" not found, try pulling it first"}`))
	}))
	defer srv.Close()

	c := NewOllama(srv.URL, "nonexistent")
	_, err := c.Complete(context.Background(), Request{
		Messages: []Message{{Role: RoleUser, Content: "hi"}},
	})
	if err == nil {
		t.Fatal("expected error for model not found")
	}
	if !strings.Contains(err.Error(), "404") {
		t.Errorf("expected 404 in error: %v", err)
	}
}

func TestAnthropic_InvalidModelReturns400(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var req map[string]any
		json.NewDecoder(r.Body).Decode(&req)
		w.WriteHeader(400)
		fmt.Fprintf(w, `{"error":{"message":"model %s does not exist","type":"invalid_request_error"}}`, req["model"])
	}))
	defer srv.Close()

	c := &anthropicClient{apiKey: "k", model: "nonexistent-model", endpoint: srv.URL, http: &http.Client{Timeout: 5 * time.Second}}
	_, err := c.Complete(context.Background(), Request{
		Messages: []Message{{Role: RoleUser, Content: "hi"}},
	})
	if err == nil {
		t.Fatal("expected error for invalid model")
	}
	if !strings.Contains(err.Error(), "400") {
		t.Errorf("expected 400 in error: %v", err)
	}
}

func TestOpenAI_InvalidModelReturns404(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(404)
		w.Write([]byte(`{"error":{"message":"The model 'nonexistent' does not exist","type":"invalid_request_error"}}`))
	}))
	defer srv.Close()

	c := &openaiClient{apiKey: "k", model: "nonexistent", endpoint: srv.URL, http: &http.Client{Timeout: 5 * time.Second}}
	_, err := c.Complete(context.Background(), Request{
		Messages: []Message{{Role: RoleUser, Content: "hi"}},
	})
	if err == nil {
		t.Fatal("expected error for invalid model")
	}
	if !strings.Contains(err.Error(), "404") {
		t.Errorf("expected 404 in error: %v", err)
	}
}

// ---------------------------------------------------------------------------
// Content parsing edge cases
// ---------------------------------------------------------------------------

func TestAnthropicParseResponse_MultipleTextBlocks(t *testing.T) {
	// Anthropic can return multiple text content blocks.
	c := &anthropicClient{}
	body := `{
		"content": [
			{"type": "text", "text": "part1"},
			{"type": "text", "text": "part2"}
		],
		"stop_reason": "end_turn",
		"model": "test",
		"usage": {"input_tokens": 1, "output_tokens": 1}
	}`
	resp, err := c.parseResponse([]byte(body), false)
	if err != nil {
		t.Fatal(err)
	}
	// Content should be concatenated.
	if resp.Content != "part1part2" {
		t.Errorf("expected concatenated 'part1part2', got %q", resp.Content)
	}
}

func TestAnthropicParseResponse_ToolUseWithEmptyInputJSONSchemaMode(t *testing.T) {
	// When expectJSON=true and tool_use has empty input, it should return ErrInvalidJSON.
	c := &anthropicClient{}
	body := `{
		"content": [{"type": "tool_use", "id": "t1", "name": "emit"}],
		"stop_reason": "tool_use",
		"model": "test",
		"usage": {"input_tokens": 1, "output_tokens": 1}
	}`
	_, err := c.parseResponse([]byte(body), true)
	if err != ErrInvalidJSON {
		t.Fatalf("expected ErrInvalidJSON for empty tool_use input in JSON schema mode, got %v", err)
	}
}

func TestOpenAIParseResponse_ToolCallWithEmptyArgs(t *testing.T) {
	c := &openaiClient{}
	body := `{
		"model": "test",
		"choices": [{
			"message": {
				"content": "",
				"tool_calls": [{"id":"c1","type":"function","function":{"name":"fn","arguments":""}}]
			},
			"finish_reason": "tool_calls"
		}],
		"usage": {}
	}`
	resp, err := c.parseResponse([]byte(body), false)
	if err != nil {
		t.Fatal(err)
	}
	if len(resp.ToolCalls) != 1 {
		t.Fatalf("expected 1 tool call, got %d", len(resp.ToolCalls))
	}
	// Empty arguments string should be preserved as-is (empty json.RawMessage).
	if string(resp.ToolCalls[0].Arguments) != "{}" {
		t.Errorf("expected '{}' for empty args, got %q", string(resp.ToolCalls[0].Arguments))
	}
}

func TestOllamaParseResponse_JSONSchemaContentNotJSON(t *testing.T) {
	c := &ollamaClient{}
	body := `{
		"model": "test",
		"message": {"role": "assistant", "content": "not valid json"},
		"done_reason": "stop"
	}`
	_, err := c.parseResponse([]byte(body), true)
	if err != ErrInvalidJSON {
		t.Fatalf("expected ErrInvalidJSON, got %v", err)
	}
}

// ---------------------------------------------------------------------------
// Anthropic buildPayload edge cases
// ---------------------------------------------------------------------------

func TestAnthropicBuildPayload_AssistantToolCallEmptyContent(t *testing.T) {
	// Assistant message with tool calls but empty Content string.
	c := &anthropicClient{apiKey: "k", model: "test", endpoint: "http://localhost", http: &http.Client{}}
	payload, err := c.buildPayload(Request{
		Messages: []Message{
			{Role: RoleUser, Content: "hello"},
			{
				Role:    RoleAssistant,
				Content: "", // empty — should NOT produce a text block
				ToolCalls: []ToolCall{
					{ID: "t1", Name: "fn", Arguments: json.RawMessage(`{}`)},
				},
			},
		},
		MaxTokens: 100,
	})
	if err != nil {
		t.Fatal(err)
	}
	msgs := payload["messages"].([]map[string]any)
	if len(msgs) != 2 {
		t.Fatalf("expected 2 messages, got %d", len(msgs))
	}
	// The assistant message content should be blocks, no text block since content is empty.
	blocks := msgs[1]["content"].([]map[string]any)
	for _, blk := range blocks {
		if blk["type"] == "text" {
			t.Error("should not have a text block when content is empty")
		}
	}
	if len(blocks) != 1 {
		t.Errorf("expected 1 block (tool_use only), got %d", len(blocks))
	}
}

func TestAnthropicBuildPayload_AssistantToolCallNilArguments(t *testing.T) {
	// Tool call with nil/empty Arguments — should default to empty object.
	c := &anthropicClient{apiKey: "k", model: "test", endpoint: "http://localhost", http: &http.Client{}}
	payload, err := c.buildPayload(Request{
		Messages: []Message{
			{Role: RoleUser, Content: "hello"},
			{
				Role: RoleAssistant,
				ToolCalls: []ToolCall{
					{ID: "t1", Name: "fn", Arguments: nil}, // nil args
				},
			},
		},
		MaxTokens: 100,
	})
	if err != nil {
		t.Fatal(err)
	}
	msgs := payload["messages"].([]map[string]any)
	blocks := msgs[1]["content"].([]map[string]any)
	for _, blk := range blocks {
		if blk["type"] == "tool_use" {
			input := blk["input"]
			// input should default to empty map, not nil.
			if input == nil {
				t.Error("tool_use input should not be nil")
			}
		}
	}
}

func TestAnthropicBuildPayload_TemperatureZeroOmitted(t *testing.T) {
	c := &anthropicClient{apiKey: "k", model: "test", endpoint: "http://localhost", http: &http.Client{}}
	payload, err := c.buildPayload(Request{
		Messages:    []Message{{Role: RoleUser, Content: "hi"}},
		MaxTokens:   100,
		Temperature: 0, // zero should be omitted
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := payload["temperature"]; ok {
		t.Error("temperature=0 should not be included in payload")
	}
}

func TestAnthropicBuildPayload_TemperatureSet(t *testing.T) {
	c := &anthropicClient{apiKey: "k", model: "test", endpoint: "http://localhost", http: &http.Client{}}
	payload, err := c.buildPayload(Request{
		Messages:    []Message{{Role: RoleUser, Content: "hi"}},
		MaxTokens:   100,
		Temperature: 0.7,
	})
	if err != nil {
		t.Fatal(err)
	}
	temp, ok := payload["temperature"].(float64)
	if !ok {
		t.Fatal("expected temperature in payload")
	}
	if temp != 0.7 {
		t.Errorf("expected 0.7, got %f", temp)
	}
}

// ---------------------------------------------------------------------------
// Provider fallback (BudgetRouter) with zero limit
// ---------------------------------------------------------------------------

func TestBudgetRouter_ZeroLimitUsesDefaultTier(t *testing.T) {
	// When MonthlyLimitUSD is 0, the hard limit and warning checks should not trigger.
	expensive := &Mock{Resp: &Response{Content: "ok", Provider: "anthropic", Model: "claude"}}
	tiers := []TierConfig{
		{Name: "cheap", Provider: "mock", Model: "small", Client: &Mock{Resp: &Response{Content: "ok", Provider: "mock"}}},
		{Name: "expensive", Provider: "anthropic", Model: "claude", Client: expensive},
	}

	ledger := &coverageLedger{spend: 500} // spend is high but limit is zero
	router := NewBudgetRouter(BudgetConfig{
		MonthlyLimitUSD:  0, // no limit
		WarnThresholdPct: 0.8,
		TierOrder:        tiers,
		DefaultTier:      1,
	}, ledger)

	resp, err := router.Complete(context.Background(), Request{
		Messages: []Message{{Role: RoleUser, Content: "hi"}},
	})
	if err != nil {
		t.Fatal(err)
	}
	// Should use default tier even though spend is high, because limit is zero.
	if resp.Provider != "anthropic" {
		t.Errorf("expected anthropic (default tier), got %s", resp.Provider)
	}
}

func TestBudgetRouter_ZeroWarnThresholdPct(t *testing.T) {
	// When WarnThresholdPct is 0, warning check should not trigger.
	expensive := &Mock{Resp: &Response{Content: "ok", Provider: "anthropic", Model: "claude"}}
	tiers := []TierConfig{
		{Name: "cheap", Provider: "mock", Model: "small", Client: &Mock{Resp: &Response{Content: "ok", Provider: "mock"}}},
		{Name: "expensive", Provider: "anthropic", Model: "claude", Client: expensive},
	}

	ledger := &coverageLedger{spend: 50}
	router := NewBudgetRouter(BudgetConfig{
		MonthlyLimitUSD:  100,
		WarnThresholdPct: 0, // no warn threshold
		TierOrder:        tiers,
		DefaultTier:      1,
	}, ledger)

	resp, err := router.Complete(context.Background(), Request{
		Messages: []Message{{Role: RoleUser, Content: "hi"}},
	})
	if err != nil {
		t.Fatal(err)
	}
	if resp.Provider != "anthropic" {
		t.Errorf("expected anthropic, got %s", resp.Provider)
	}
}

// ---------------------------------------------------------------------------
// Ollama stream HTTP error includes body
// ---------------------------------------------------------------------------

func TestOllamaStream_HTTPErrorIncludesBody(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(404)
		w.Write([]byte(`model "nonexistent" not found`))
	}))
	defer srv.Close()

	c := &ollamaClient{host: srv.URL, model: "nonexistent", http: &http.Client{Timeout: 5 * time.Second}}
	_, err := c.Stream(context.Background(), Request{
		Messages: []Message{{Role: RoleUser, Content: "hi"}},
	})
	if err == nil {
		t.Fatal("expected error")
	}
	if !strings.Contains(err.Error(), "404") {
		t.Errorf("error should contain 404: %v", err)
	}
	if !strings.Contains(err.Error(), "not found") {
		t.Errorf("error should contain body: %v", err)
	}
}

// ---------------------------------------------------------------------------
// Streaming with buildPayload error
// ---------------------------------------------------------------------------

func TestAnthropicStream_BuildPayloadError(t *testing.T) {
	c := &anthropicClient{apiKey: "k", model: "test", endpoint: "http://localhost", http: &http.Client{Timeout: 5 * time.Second}}
	_, err := c.Stream(context.Background(), Request{
		Messages: []Message{{Role: RoleUser, Content: "hi"}},
		JSONSchema: &JSONSchema{
			Name:   "bad",
			Schema: json.RawMessage(`not valid json`),
		},
	})
	if err == nil {
		t.Fatal("expected error from buildPayload")
	}
	if !strings.Contains(err.Error(), "bad JSONSchema") {
		t.Errorf("expected JSONSchema error, got: %v", err)
	}
}

func TestOpenAIStream_BuildPayloadError(t *testing.T) {
	c := &openaiClient{apiKey: "k", model: "test", endpoint: "http://localhost", http: &http.Client{Timeout: 5 * time.Second}}
	_, err := c.Stream(context.Background(), Request{
		Messages: []Message{{Role: RoleUser, Content: "hi"}},
		JSONSchema: &JSONSchema{
			Name:   "bad",
			Schema: json.RawMessage(`not valid`),
		},
	})
	if err == nil {
		t.Fatal("expected error from buildPayload")
	}
}

func TestOllamaStream_BuildPayloadError(t *testing.T) {
	c := &ollamaClient{host: "http://localhost", model: "test", http: &http.Client{Timeout: 5 * time.Second}}
	_, err := c.Stream(context.Background(), Request{
		Messages: []Message{{Role: RoleUser, Content: "hi"}},
		JSONSchema: &JSONSchema{
			Name:   "bad",
			Schema: json.RawMessage(`invalid`),
		},
	})
	if err == nil {
		t.Fatal("expected error from buildPayload")
	}
}

// ---------------------------------------------------------------------------
// AsStreamer with non-streaming client
// ---------------------------------------------------------------------------

func TestAsStreamer_MockDoesNotStream(t *testing.T) {
	m := &Mock{}
	_, ok := AsStreamer(m)
	if ok {
		t.Error("Mock should not implement Streamer")
	}
}

// ---------------------------------------------------------------------------
// Anthropic Complete with grammar warning — verify it doesn't error
// ---------------------------------------------------------------------------

func TestAnthropicComplete_GrammarWarningDoesNotBlock(t *testing.T) {
	resetGrammarWarnSeenForTest()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Verify grammar is NOT sent in the payload (Anthropic ignores it).
		var body map[string]any
		json.NewDecoder(r.Body).Decode(&body)
		if _, ok := body["grammar"]; ok {
			t.Error("grammar should not appear in Anthropic API payload")
		}
		w.Write([]byte(`{
			"content":[{"type":"text","text":"ok"}],
			"stop_reason":"end_turn",
			"model":"test",
			"usage":{"input_tokens":1,"output_tokens":1}
		}`))
	}))
	defer srv.Close()

	c := &anthropicClient{apiKey: "k", model: "test", endpoint: srv.URL, http: &http.Client{Timeout: 5 * time.Second}}
	grammar := GBNFJSONObject()
	resp, err := c.Complete(context.Background(), Request{
		Messages: []Message{{Role: RoleUser, Content: "hi"}},
		Grammar:  &grammar,
	})
	if err != nil {
		t.Fatal(err)
	}
	if resp.Content != "ok" {
		t.Errorf("expected 'ok', got %q", resp.Content)
	}
}

func TestOpenAIComplete_GrammarWarningDoesNotBlock(t *testing.T) {
	resetGrammarWarnSeenForTest()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(`{
			"model":"test",
			"choices":[{"message":{"content":"ok"},"finish_reason":"stop"}],
			"usage":{}
		}`))
	}))
	defer srv.Close()

	c := &openaiClient{apiKey: "k", model: "test", endpoint: srv.URL, http: &http.Client{Timeout: 5 * time.Second}}
	grammar := GBNFEnum([]string{"yes", "no"})
	resp, err := c.Complete(context.Background(), Request{
		Messages: []Message{{Role: RoleUser, Content: "hi"}},
		Grammar:  &grammar,
	})
	if err != nil {
		t.Fatal(err)
	}
	if resp.Content != "ok" {
		t.Errorf("expected 'ok', got %q", resp.Content)
	}
}

// ---------------------------------------------------------------------------
// Anthropic/OpenAI headers verification
// ---------------------------------------------------------------------------

func TestAnthropicPost_Headers(t *testing.T) {
	var gotHeaders http.Header
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotHeaders = r.Header
		w.Write([]byte(`{
			"content":[{"type":"text","text":"ok"}],
			"stop_reason":"end_turn","model":"test",
			"usage":{"input_tokens":1,"output_tokens":1}
		}`))
	}))
	defer srv.Close()

	c := &anthropicClient{apiKey: "test-key-123", model: "test", endpoint: srv.URL, http: &http.Client{Timeout: 5 * time.Second}}
	_, err := c.Complete(context.Background(), Request{
		Messages: []Message{{Role: RoleUser, Content: "hi"}},
	})
	if err != nil {
		t.Fatal(err)
	}
	if gotHeaders.Get("x-api-key") != "test-key-123" {
		t.Errorf("expected x-api-key=test-key-123, got %q", gotHeaders.Get("x-api-key"))
	}
	if gotHeaders.Get("anthropic-version") != AnthropicVersion {
		t.Errorf("expected anthropic-version=%s, got %q", AnthropicVersion, gotHeaders.Get("anthropic-version"))
	}
	if gotHeaders.Get("Content-Type") != "application/json" {
		t.Errorf("expected Content-Type=application/json, got %q", gotHeaders.Get("Content-Type"))
	}
}

func TestOpenAIPost_Headers(t *testing.T) {
	var gotHeaders http.Header
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotHeaders = r.Header
		w.Write([]byte(`{
			"model":"test",
			"choices":[{"message":{"content":"ok"},"finish_reason":"stop"}],
			"usage":{}
		}`))
	}))
	defer srv.Close()

	c := &openaiClient{apiKey: "sk-test-456", model: "test", endpoint: srv.URL, http: &http.Client{Timeout: 5 * time.Second}}
	_, err := c.Complete(context.Background(), Request{
		Messages: []Message{{Role: RoleUser, Content: "hi"}},
	})
	if err != nil {
		t.Fatal(err)
	}
	if gotHeaders.Get("Authorization") != "Bearer sk-test-456" {
		t.Errorf("expected Authorization header, got %q", gotHeaders.Get("Authorization"))
	}
}

// ---------------------------------------------------------------------------
// Ollama trims trailing slash from host
// ---------------------------------------------------------------------------

func TestNewOllama_TrimsTrailingSlash(t *testing.T) {
	c := NewOllama("http://localhost:11434/", "llama3.2").(*ollamaClient)
	if strings.HasSuffix(c.host, "/") {
		t.Errorf("host should not have trailing slash, got %q", c.host)
	}
}

// ---------------------------------------------------------------------------
// Anthropic buildPayload with CacheSystem off (plain system string)
// ---------------------------------------------------------------------------

func TestAnthropicBuildPayload_SystemNotCached(t *testing.T) {
	c := &anthropicClient{apiKey: "k", model: "test", endpoint: "http://localhost", http: &http.Client{}}
	payload, err := c.buildPayload(Request{
		Messages:    []Message{{Role: RoleUser, Content: "hi"}},
		MaxTokens:   100,
		System:      "Be helpful",
		CacheSystem: false,
	})
	if err != nil {
		t.Fatal(err)
	}
	// Should be a string, not an array.
	_, ok := payload["system"].(string)
	if !ok {
		t.Fatalf("expected system to be a string, got %T", payload["system"])
	}
}

func TestAnthropicBuildPayload_SystemCached(t *testing.T) {
	c := &anthropicClient{apiKey: "k", model: "test", endpoint: "http://localhost", http: &http.Client{}}
	payload, err := c.buildPayload(Request{
		Messages:    []Message{{Role: RoleUser, Content: "hi"}},
		MaxTokens:   100,
		System:      "Be helpful",
		CacheSystem: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	// Should be an array with cache_control.
	arr, ok := payload["system"].([]map[string]any)
	if !ok {
		t.Fatalf("expected system to be []map[string]any, got %T", payload["system"])
	}
	if len(arr) != 1 {
		t.Fatalf("expected 1 system block, got %d", len(arr))
	}
	if arr[0]["text"] != "Be helpful" {
		t.Errorf("expected text='Be helpful', got %q", arr[0]["text"])
	}
	cc, ok := arr[0]["cache_control"].(map[string]any)
	if !ok {
		t.Fatal("expected cache_control map")
	}
	if cc["type"] != "ephemeral" {
		t.Errorf("expected cache_control type=ephemeral, got %v", cc["type"])
	}
}

// ---------------------------------------------------------------------------
// Anthropic Stream headers
// ---------------------------------------------------------------------------

func TestAnthropicStream_Headers(t *testing.T) {
	var gotHeaders http.Header
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotHeaders = r.Header
		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(200)
		flusher, _ := w.(http.Flusher)
		fmt.Fprintln(w, "event: message_stop")
		fmt.Fprintln(w, `data: {}`)
		fmt.Fprintln(w)
		flusher.Flush()
	}))
	defer srv.Close()

	c := &anthropicClient{apiKey: "k", model: "test", endpoint: srv.URL, http: &http.Client{Timeout: 5 * time.Second}}
	ch, err := c.Stream(context.Background(), Request{
		Messages: []Message{{Role: RoleUser, Content: "hi"}},
	})
	if err != nil {
		t.Fatal(err)
	}
	for range ch {
	}

	if gotHeaders.Get("Accept") != "text/event-stream" {
		t.Errorf("expected Accept=text/event-stream, got %q", gotHeaders.Get("Accept"))
	}
	if gotHeaders.Get("x-api-key") != "k" {
		t.Errorf("expected x-api-key=k, got %q", gotHeaders.Get("x-api-key"))
	}
}

func TestOpenAIStream_Headers(t *testing.T) {
	var gotHeaders http.Header
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotHeaders = r.Header
		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(200)
		flusher, _ := w.(http.Flusher)
		fmt.Fprintln(w, `data: [DONE]`)
		flusher.Flush()
	}))
	defer srv.Close()

	c := &openaiClient{apiKey: "sk", model: "test", endpoint: srv.URL, http: &http.Client{Timeout: 5 * time.Second}}
	ch, err := c.Stream(context.Background(), Request{
		Messages: []Message{{Role: RoleUser, Content: "hi"}},
	})
	if err != nil {
		t.Fatal(err)
	}
	for range ch {
	}

	if gotHeaders.Get("Accept") != "text/event-stream" {
		t.Errorf("expected Accept=text/event-stream, got %q", gotHeaders.Get("Accept"))
	}
	if gotHeaders.Get("Authorization") != "Bearer sk" {
		t.Errorf("expected Authorization=Bearer sk, got %q", gotHeaders.Get("Authorization"))
	}
}

// ---------------------------------------------------------------------------
// Ollama Complete with Grammar (GrammarJSONSchema format via grammar field)
// ---------------------------------------------------------------------------

func TestOllamaComplete_GrammarJSONSchemaViaGrammar(t *testing.T) {
	var capturedFormat any
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body map[string]any
		json.NewDecoder(r.Body).Decode(&body)
		capturedFormat = body["format"]
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{
			"model":"test",
			"message":{"role":"assistant","content":"{\"result\":\"ok\"}"},
			"done_reason":"stop",
			"prompt_eval_count":1,
			"eval_count":1
		}`))
	}))
	defer srv.Close()

	c := NewOllama(srv.URL, "test")
	_, err := c.Complete(context.Background(), Request{
		Messages: []Message{{Role: RoleUser, Content: "hi"}},
		Grammar: &GrammarSpec{
			Format: GrammarJSONSchema,
			Body:   `{"type":"object","properties":{"result":{"type":"string"}}}`,
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	// format should have been set to the JSON schema object.
	if capturedFormat == nil {
		t.Fatal("expected format to be set")
	}
	m, ok := capturedFormat.(map[string]any)
	if !ok {
		t.Fatalf("expected format to be map, got %T", capturedFormat)
	}
	if m["type"] != "object" {
		t.Errorf("expected type=object in format, got %v", m["type"])
	}
}

// ---------------------------------------------------------------------------
// Ollama tool results — empty Arguments on ToolCall
// ---------------------------------------------------------------------------

func TestOllamaBuildPayload_ToolCallEmptyArguments(t *testing.T) {
	c := &ollamaClient{host: "http://localhost", model: "test"}
	payload, err := c.buildPayload(Request{
		Messages: []Message{
			{Role: RoleUser, Content: "call it"},
			{
				Role: RoleAssistant,
				ToolCalls: []ToolCall{
					{Name: "fn", Arguments: nil}, // nil args
				},
			},
		},
	}, false)
	if err != nil {
		t.Fatal(err)
	}
	msgs := payload["messages"].([]map[string]any)
	// Find assistant message with tool_calls
	assistantMsg := msgs[1]
	calls, ok := assistantMsg["tool_calls"].([]map[string]any)
	if !ok {
		t.Fatal("expected tool_calls array")
	}
	if len(calls) != 1 {
		t.Fatalf("expected 1 call, got %d", len(calls))
	}
	fn := calls[0]["function"].(map[string]any)
	// nil args should become empty map
	if fn["arguments"] == nil {
		t.Error("arguments should not be nil when input was nil")
	}
}

// ---------------------------------------------------------------------------
// OpenAI buildPayload with images — no text content
// ---------------------------------------------------------------------------

func TestOpenAIBuildPayload_ImageOnlyNoText(t *testing.T) {
	c := &openaiClient{apiKey: "k", model: "test"}
	payload, err := c.buildPayload(Request{
		Messages: []Message{
			{
				Role:   RoleUser,
				Images: []Image{{MediaType: "image/png", Data: []byte("data")}},
				// Content is empty — should not produce a text part
			},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	msgs := payload["messages"].([]map[string]any)
	parts, ok := msgs[0]["content"].([]map[string]any)
	if !ok {
		t.Fatal("expected parts array")
	}
	// Should only have image part, no text part.
	for _, p := range parts {
		if p["type"] == "text" {
			t.Error("should not have text part when Content is empty")
		}
	}
	if len(parts) != 1 {
		t.Errorf("expected 1 part (image only), got %d", len(parts))
	}
}

func TestAnthropicBuildPayload_ImageOnlyNoText(t *testing.T) {
	c := &anthropicClient{apiKey: "k", model: "test", endpoint: "http://localhost", http: &http.Client{}}
	payload, err := c.buildPayload(Request{
		Messages: []Message{
			{
				Role:   RoleUser,
				Images: []Image{{MediaType: "image/png", Data: []byte("data")}},
				// Content is empty
			},
		},
		MaxTokens: 100,
	})
	if err != nil {
		t.Fatal(err)
	}
	msgs := payload["messages"].([]map[string]any)
	blocks, ok := msgs[0]["content"].([]map[string]any)
	if !ok {
		t.Fatal("expected blocks array")
	}
	for _, b := range blocks {
		if b["type"] == "text" {
			t.Error("should not have text block when Content is empty")
		}
	}
	if len(blocks) != 1 {
		t.Errorf("expected 1 block (image only), got %d", len(blocks))
	}
}

// ---------------------------------------------------------------------------
// Anthropic parseResponse — decode error
// ---------------------------------------------------------------------------

func TestAnthropicParseResponse_InvalidBody(t *testing.T) {
	c := &anthropicClient{}
	_, err := c.parseResponse([]byte(""), false)
	if err == nil {
		t.Fatal("expected decode error on empty body")
	}
}

// ---------------------------------------------------------------------------
// Ollama stream context cancellation
// ---------------------------------------------------------------------------

func TestOllamaStream_ContextCancelledBeforeChunkRead(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/x-ndjson")
		flusher, _ := w.(http.Flusher)
		fmt.Fprintln(w, `{"message":{"content":"hi"},"done":false}`)
		flusher.Flush()
		<-r.Context().Done()
	}))
	defer srv.Close()

	ctx, cancel := context.WithCancel(context.Background())
	c := &ollamaClient{host: srv.URL, model: "test", http: &http.Client{Timeout: 30 * time.Second}}
	ch, err := c.Stream(ctx, Request{
		Messages: []Message{{Role: RoleUser, Content: "hi"}},
	})
	if err != nil {
		t.Fatal(err)
	}

	// Read first chunk.
	<-ch
	// Cancel.
	cancel()
	// Drain.
	for range ch {
	}
	// If we get here without hanging, the test passes.
}

// ---------------------------------------------------------------------------
// Anthropic response with malformed body from API (ReadAll failure simulated)
// ---------------------------------------------------------------------------

func TestAnthropicComplete_MalformedResponseJSON(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{not valid json`))
	}))
	defer srv.Close()

	c := &anthropicClient{apiKey: "k", model: "test", endpoint: srv.URL, http: &http.Client{Timeout: 5 * time.Second}}
	_, err := c.Complete(context.Background(), Request{
		Messages: []Message{{Role: RoleUser, Content: "hi"}},
	})
	if err == nil {
		t.Fatal("expected error for malformed JSON response")
	}
	if !strings.Contains(err.Error(), "decode") {
		t.Errorf("expected decode error, got: %v", err)
	}
}

func TestOpenAIComplete_MalformedResponseJSON(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(`{broken`))
	}))
	defer srv.Close()

	c := &openaiClient{apiKey: "k", model: "test", endpoint: srv.URL, http: &http.Client{Timeout: 5 * time.Second}}
	_, err := c.Complete(context.Background(), Request{
		Messages: []Message{{Role: RoleUser, Content: "hi"}},
	})
	if err == nil {
		t.Fatal("expected error")
	}
}

func TestOllamaComplete_MalformedResponseJSON(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(`{broken`))
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

// ---------------------------------------------------------------------------
// Ollama buildPayload edge cases — tool call empty args unmarshal
// ---------------------------------------------------------------------------

func TestOllamaBuildPayload_ToolCallWithInvalidJSON(t *testing.T) {
	c := &ollamaClient{host: "http://localhost", model: "test"}
	// Invalid JSON in Arguments — unmarshal fails, args default to empty map.
	payload, err := c.buildPayload(Request{
		Messages: []Message{
			{Role: RoleUser, Content: "go"},
			{
				Role: RoleAssistant,
				ToolCalls: []ToolCall{
					{Name: "fn", Arguments: json.RawMessage(`not json`)},
				},
			},
		},
	}, false)
	if err != nil {
		t.Fatal(err)
	}
	msgs := payload["messages"].([]map[string]any)
	calls := msgs[1]["tool_calls"].([]map[string]any)
	fn := calls[0]["function"].(map[string]any)
	if fn["arguments"] == nil {
		t.Error("arguments should default when unmarshal fails")
	}
}

// ---------------------------------------------------------------------------
// Anthropic stream — verify stream flag in payload
// ---------------------------------------------------------------------------

func TestAnthropicStream_StreamFlagSet(t *testing.T) {
	var capturedStream any
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body map[string]any
		json.NewDecoder(r.Body).Decode(&body)
		capturedStream = body["stream"]
		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(200)
		flusher, _ := w.(http.Flusher)
		fmt.Fprintln(w, "event: message_stop")
		fmt.Fprintln(w, `data: {}`)
		fmt.Fprintln(w)
		flusher.Flush()
	}))
	defer srv.Close()

	c := &anthropicClient{apiKey: "k", model: "test", endpoint: srv.URL, http: &http.Client{Timeout: 5 * time.Second}}
	ch, err := c.Stream(context.Background(), Request{
		Messages: []Message{{Role: RoleUser, Content: "hi"}},
	})
	if err != nil {
		t.Fatal(err)
	}
	for range ch {
	}
	if capturedStream != true {
		t.Errorf("expected stream=true in payload, got %v", capturedStream)
	}
}

func TestOpenAIStream_StreamFlagSet(t *testing.T) {
	var capturedStream any
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body map[string]any
		json.NewDecoder(r.Body).Decode(&body)
		capturedStream = body["stream"]
		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(200)
		flusher, _ := w.(http.Flusher)
		fmt.Fprintln(w, `data: [DONE]`)
		flusher.Flush()
	}))
	defer srv.Close()

	c := &openaiClient{apiKey: "k", model: "test", endpoint: srv.URL, http: &http.Client{Timeout: 5 * time.Second}}
	ch, err := c.Stream(context.Background(), Request{
		Messages: []Message{{Role: RoleUser, Content: "hi"}},
	})
	if err != nil {
		t.Fatal(err)
	}
	for range ch {
	}
	if capturedStream != true {
		t.Errorf("expected stream=true in payload, got %v", capturedStream)
	}
}

// ---------------------------------------------------------------------------
// Anthropic buildPayload edge: invalid tool_call Arguments unmarshal
// ---------------------------------------------------------------------------

func TestAnthropicBuildPayload_ToolCallInvalidArguments(t *testing.T) {
	c := &anthropicClient{apiKey: "k", model: "test", endpoint: "http://localhost", http: &http.Client{}}
	payload, err := c.buildPayload(Request{
		Messages: []Message{
			{Role: RoleUser, Content: "go"},
			{
				Role: RoleAssistant,
				ToolCalls: []ToolCall{
					{ID: "t1", Name: "fn", Arguments: json.RawMessage(`invalid json`)},
				},
			},
		},
		MaxTokens: 100,
	})
	if err != nil {
		t.Fatal(err)
	}
	msgs := payload["messages"].([]map[string]any)
	blocks := msgs[1]["content"].([]map[string]any)
	for _, blk := range blocks {
		if blk["type"] == "tool_use" {
			// input should default to empty map since unmarshal failed
			if blk["input"] == nil {
				t.Error("tool_use input should not be nil when Arguments is invalid JSON")
			}
		}
	}
}

// ---------------------------------------------------------------------------
// io.ReadAll failure simulation — use a server that drops connection abruptly
// ---------------------------------------------------------------------------

func TestAnthropicPost_ServerDropsConnection(t *testing.T) {
	// Create a server that accepts the connection then closes it abruptly.
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Read the body to avoid broken pipe on client
		io.ReadAll(r.Body)
		// Hijack and close immediately to simulate connection drop.
		hj, ok := w.(http.Hijacker)
		if !ok {
			// If hijack not supported, write partial response.
			w.Header().Set("Content-Length", "1000")
			w.WriteHeader(200)
			w.Write([]byte("partial"))
			return
		}
		conn, _, _ := hj.Hijack()
		conn.Close()
	}))
	defer srv.Close()

	c := &anthropicClient{apiKey: "k", model: "test", endpoint: srv.URL, http: &http.Client{Timeout: 5 * time.Second}}
	_, err := c.Complete(context.Background(), Request{
		Messages: []Message{{Role: RoleUser, Content: "hi"}},
	})
	if err == nil {
		t.Fatal("expected error when server drops connection")
	}
}

func TestOpenAIPost_ServerDropsConnection(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		io.ReadAll(r.Body)
		hj, ok := w.(http.Hijacker)
		if !ok {
			w.Header().Set("Content-Length", "1000")
			w.WriteHeader(200)
			w.Write([]byte("partial"))
			return
		}
		conn, _, _ := hj.Hijack()
		conn.Close()
	}))
	defer srv.Close()

	c := &openaiClient{apiKey: "k", model: "test", endpoint: srv.URL, http: &http.Client{Timeout: 5 * time.Second}}
	_, err := c.Complete(context.Background(), Request{
		Messages: []Message{{Role: RoleUser, Content: "hi"}},
	})
	if err == nil {
		t.Fatal("expected error when server drops connection")
	}
}

func TestOllamaPost_ServerDropsConnection(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		io.ReadAll(r.Body)
		hj, ok := w.(http.Hijacker)
		if !ok {
			w.Header().Set("Content-Length", "1000")
			w.WriteHeader(200)
			w.Write([]byte("partial"))
			return
		}
		conn, _, _ := hj.Hijack()
		conn.Close()
	}))
	defer srv.Close()

	c := &ollamaClient{host: srv.URL, model: "test", http: &http.Client{Timeout: 5 * time.Second}}
	_, err := c.Complete(context.Background(), Request{
		Messages: []Message{{Role: RoleUser, Content: "hi"}},
	})
	if err == nil {
		t.Fatal("expected error when server drops connection")
	}
}

// ---------------------------------------------------------------------------
// Multiple tool calls in one response
// ---------------------------------------------------------------------------

func TestAnthropicParseResponse_MultipleToolCalls(t *testing.T) {
	c := &anthropicClient{}
	body := `{
		"content": [
			{"type":"text","text":"doing both"},
			{"type":"tool_use","id":"t1","name":"fn1","input":{"a":1}},
			{"type":"tool_use","id":"t2","name":"fn2","input":{"b":2}}
		],
		"stop_reason":"tool_use",
		"model":"test",
		"usage":{"input_tokens":1,"output_tokens":1}
	}`
	resp, err := c.parseResponse([]byte(body), false)
	if err != nil {
		t.Fatal(err)
	}
	if len(resp.ToolCalls) != 2 {
		t.Fatalf("expected 2 tool calls, got %d", len(resp.ToolCalls))
	}
	if resp.ToolCalls[0].Name != "fn1" || resp.ToolCalls[1].Name != "fn2" {
		t.Errorf("tool names: %s, %s", resp.ToolCalls[0].Name, resp.ToolCalls[1].Name)
	}
	if resp.Content != "doing both" {
		t.Errorf("expected text content, got %q", resp.Content)
	}
}

func TestOpenAIParseResponse_MultipleToolCalls(t *testing.T) {
	c := &openaiClient{}
	body := `{
		"model":"test",
		"choices":[{
			"message":{
				"content":"",
				"tool_calls":[
					{"id":"c1","type":"function","function":{"name":"fn1","arguments":"{\"x\":1}"}},
					{"id":"c2","type":"function","function":{"name":"fn2","arguments":"{\"y\":2}"}}
				]
			},
			"finish_reason":"tool_calls"
		}],
		"usage":{"prompt_tokens":1,"completion_tokens":1}
	}`
	resp, err := c.parseResponse([]byte(body), false)
	if err != nil {
		t.Fatal(err)
	}
	if len(resp.ToolCalls) != 2 {
		t.Fatalf("expected 2 tool calls, got %d", len(resp.ToolCalls))
	}
}

// ---------------------------------------------------------------------------
// Anthropic stream — buildPayload with bad tool schema (Complete path)
// ---------------------------------------------------------------------------

func TestAnthropicComplete_BadToolSchemaError(t *testing.T) {
	c := &anthropicClient{apiKey: "k", model: "test", endpoint: "http://localhost", http: &http.Client{Timeout: 5 * time.Second}}
	_, err := c.Complete(context.Background(), Request{
		Messages: []Message{{Role: RoleUser, Content: "hi"}},
		Tools: []Tool{
			{Name: "bad_tool", Schema: json.RawMessage(`invalid json`)},
		},
	})
	if err == nil {
		t.Fatal("expected error for bad tool schema")
	}
	if !strings.Contains(err.Error(), "bad tool schema") {
		t.Errorf("expected tool schema error, got: %v", err)
	}
}

func TestOpenAIComplete_BadToolSchemaError(t *testing.T) {
	c := &openaiClient{apiKey: "k", model: "test", endpoint: "http://localhost", http: &http.Client{Timeout: 5 * time.Second}}
	_, err := c.Complete(context.Background(), Request{
		Messages: []Message{{Role: RoleUser, Content: "hi"}},
		Tools: []Tool{
			{Name: "bad_tool", Schema: json.RawMessage(`invalid json`)},
		},
	})
	if err == nil {
		t.Fatal("expected error for bad tool schema")
	}
}

func TestOllamaComplete_BadToolSchemaError(t *testing.T) {
	c := &ollamaClient{host: "http://localhost", model: "test", http: &http.Client{Timeout: 5 * time.Second}}
	_, err := c.Complete(context.Background(), Request{
		Messages: []Message{{Role: RoleUser, Content: "hi"}},
		Tools: []Tool{
			{Name: "bad_tool", Schema: json.RawMessage(`invalid json`)},
		},
	})
	if err == nil {
		t.Fatal("expected error for bad tool schema")
	}
}

// ---------------------------------------------------------------------------
// OpenAI buildPayload — temperature is omitted when zero
// ---------------------------------------------------------------------------

func TestOpenAIBuildPayload_TemperatureZero(t *testing.T) {
	c := &openaiClient{apiKey: "k", model: "test"}
	payload, err := c.buildPayload(Request{
		Messages:    []Message{{Role: RoleUser, Content: "hi"}},
		Temperature: 0,
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := payload["temperature"]; ok {
		t.Error("temperature=0 should not be in payload")
	}
}

// ---------------------------------------------------------------------------
// Ollama buildPayload — no options when temperature and maxTokens are zero
// ---------------------------------------------------------------------------

func TestOllamaBuildPayload_NoOptionsWhenZero(t *testing.T) {
	c := &ollamaClient{host: "http://localhost", model: "test"}
	payload, err := c.buildPayload(Request{
		Messages:    []Message{{Role: RoleUser, Content: "hi"}},
		Temperature: 0,
		MaxTokens:   0,
	}, false)
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := payload["options"]; ok {
		t.Error("options should not be present when temp and maxTokens are zero")
	}
}

func TestOllamaBuildPayload_OptionsSetWhenNonZero(t *testing.T) {
	c := &ollamaClient{host: "http://localhost", model: "test"}
	payload, err := c.buildPayload(Request{
		Messages:    []Message{{Role: RoleUser, Content: "hi"}},
		Temperature: 0.5,
		MaxTokens:   100,
	}, false)
	if err != nil {
		t.Fatal(err)
	}
	opts, ok := payload["options"].(map[string]any)
	if !ok {
		t.Fatal("expected options map")
	}
	if opts["temperature"] != 0.5 {
		t.Errorf("expected temperature=0.5, got %v", opts["temperature"])
	}
	if opts["num_predict"] != 100 {
		t.Errorf("expected num_predict=100, got %v", opts["num_predict"])
	}
}
