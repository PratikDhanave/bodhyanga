package llm

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// fakeOllamaServer records every request body it receives and returns canned responses.
type fakeOllamaServer struct {
	Bodies  []map[string]any
	Respond func(w http.ResponseWriter, r *http.Request, body map[string]any)
}

func newFakeOllama(t *testing.T, respond func(w http.ResponseWriter, r *http.Request, body map[string]any)) *fakeOllamaServer {
	t.Helper()
	f := &fakeOllamaServer{Respond: respond}
	return f
}

func (f *fakeOllamaServer) handler() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body map[string]any
		_ = json.NewDecoder(r.Body).Decode(&body)
		f.Bodies = append(f.Bodies, body)
		f.Respond(w, r, body)
	})
}

// fakeOllamaResponse returns a minimal non-streaming Ollama chat response.
func fakeOllamaResponse(content string) map[string]any {
	return map[string]any{
		"model": "test",
		"message": map[string]any{
			"role":    "assistant",
			"content": content,
		},
		"done":        true,
		"done_reason": "stop",
	}
}

// ── GBNF Grammar wiring ──────────────────────────────────────────────────────

// TestOllamaGBNFGrammarWired verifies that a GrammarGBNF spec is forwarded
// to Ollama via options.grammar — the same field llama.cpp reads to enforce
// sampler-level constraints.
func TestOllamaGBNFGrammarWired(t *testing.T) {
	grammar := GBNFJSONObject()

	var capturedOptions map[string]any
	srv := httptest.NewServer(newFakeOllama(t, func(w http.ResponseWriter, r *http.Request, body map[string]any) {
		if opts, ok := body["options"].(map[string]any); ok {
			capturedOptions = opts
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(fakeOllamaResponse(`{"ok":true}`))
	}).handler())
	defer srv.Close()

	c := NewOllama(srv.URL, "llama3.2")
	_, err := c.Complete(context.Background(), Request{
		Messages: []Message{{Role: RoleUser, Content: "test"}},
		Grammar:  &grammar,
	})
	if err != nil {
		t.Fatalf("Complete: %v", err)
	}
	if capturedOptions == nil {
		t.Fatal("options not set in request")
	}
	got, ok := capturedOptions["grammar"].(string)
	if !ok {
		t.Fatalf("options.grammar missing or wrong type: %v", capturedOptions)
	}
	if !strings.Contains(got, "root") {
		t.Errorf("options.grammar = %q; want GBNF with root rule", got)
	}
}

// TestOllamaGBNFDoesNotWarnLog confirms that GBNF grammar on Ollama does NOT
// emit the "provider does not support grammar" warning (unlike anthropic/openai).
func TestOllamaGBNFDoesNotWarnLog(t *testing.T) {
	resetGrammarWarnSeenForTest()
	grammar := GBNFEnum([]string{"approve", "reject"})

	var logBuf bytes.Buffer
	prev := log.Writer()
	log.SetOutput(&logBuf)
	defer log.SetOutput(prev)

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(fakeOllamaResponse(`"approve"`))
	}))
	defer srv.Close()

	c := NewOllama(srv.URL, "llama3.2")
	_, err := c.Complete(context.Background(), Request{
		Messages: []Message{{Role: RoleUser, Content: "decide"}},
		Grammar:  &grammar,
	})
	if err != nil {
		t.Fatalf("Complete: %v", err)
	}
	if strings.Contains(logBuf.String(), "does not support grammar") {
		t.Errorf("got unexpected grammar warning for ollama:\n%s", logBuf.String())
	}
}

// ── Full JSON Schema in format ────────────────────────────────────────────────

// TestOllamaJSONSchemaPassedAsObject verifies that the full JSON Schema object
// is sent in format rather than the legacy bare "json" string. This enables
// Ollama 0.5+ field-level validation.
func TestOllamaJSONSchemaPassedAsObject(t *testing.T) {
	schema := &JSONSchema{
		Name: "diagnosis",
		Schema: json.RawMessage(`{
			"type": "object",
			"properties": {
				"diagnosis": {"type": "string"},
				"confidence": {"type": "number"}
			},
			"required": ["diagnosis", "confidence"]
		}`),
	}

	var capturedFormat any
	srv := httptest.NewServer(newFakeOllama(t, func(w http.ResponseWriter, r *http.Request, body map[string]any) {
		capturedFormat = body["format"]
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(fakeOllamaResponse(`{"diagnosis":"flu","confidence":0.9}`))
	}).handler())
	defer srv.Close()

	c := NewOllama(srv.URL, "qwen2.5:7b")
	_, err := c.Complete(context.Background(), Request{
		Messages:   []Message{{Role: RoleUser, Content: "diagnose"}},
		JSONSchema: schema,
	})
	if err != nil {
		t.Fatalf("Complete: %v", err)
	}

	// format must be a map (the JSON Schema object), not the string "json".
	schemaMap, ok := capturedFormat.(map[string]any)
	if !ok {
		t.Fatalf("format = %T(%v); want map[string]any (full JSON Schema)", capturedFormat, capturedFormat)
	}
	if schemaMap["type"] != "object" {
		t.Errorf("format.type = %v; want object", schemaMap["type"])
	}
}

// ── Streaming ─────────────────────────────────────────────────────────────────

// TestOllamaStreamReceivesChunks verifies that Stream() reads the Ollama
// newline-delimited JSON format and delivers delta chunks to the caller.
func TestOllamaStreamReceivesChunks(t *testing.T) {
	// Build an Ollama streaming response: 3 content chunks + done.
	words := []string{"Hello", " world", "!"}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/x-ndjson")
		flusher, _ := w.(http.Flusher)
		for i, word := range words {
			event := map[string]any{
				"message": map[string]any{"role": "assistant", "content": word},
				"done":    i == len(words)-1,
			}
			b, _ := json.Marshal(event)
			fmt.Fprintf(w, "%s\n", b)
			if flusher != nil {
				flusher.Flush()
			}
		}
	}))
	defer srv.Close()

	c := NewOllama(srv.URL, "llama3.2")
	s, ok := AsStreamer(c)
	if !ok {
		t.Fatal("ollamaClient does not implement Streamer")
	}

	ch, err := s.Stream(context.Background(), Request{
		Messages: []Message{{Role: RoleUser, Content: "hi"}},
	})
	if err != nil {
		t.Fatalf("Stream: %v", err)
	}

	var deltas []string
	var finalContent string
	for chunk := range ch {
		if chunk.Err != nil {
			t.Fatalf("chunk error: %v", chunk.Err)
		}
		deltas = append(deltas, chunk.Delta)
		if chunk.Done {
			finalContent = chunk.FullContent
		}
	}

	if len(deltas) != len(words) {
		t.Errorf("got %d deltas, want %d: %v", len(deltas), len(words), deltas)
	}
	if finalContent != strings.Join(words, "") {
		t.Errorf("FullContent = %q; want %q", finalContent, strings.Join(words, ""))
	}
}

// TestOllamaStreamGBNFSentInStreamingMode verifies that when Stream() is
// called with a GBNF grammar, options.grammar is still wired into the request.
func TestOllamaStreamGBNFSentInStreamingMode(t *testing.T) {
	grammar := GBNFEnum([]string{"yes", "no"})
	var capturedOptions map[string]any

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body map[string]any
		_ = json.NewDecoder(r.Body).Decode(&body)
		if opts, ok := body["options"].(map[string]any); ok {
			capturedOptions = opts
		}
		w.Header().Set("Content-Type", "application/x-ndjson")
		event := map[string]any{
			"message": map[string]any{"role": "assistant", "content": `"yes"`},
			"done":    true,
		}
		b, _ := json.Marshal(event)
		fmt.Fprintf(w, "%s\n", b)
	}))
	defer srv.Close()

	c := NewOllama(srv.URL, "llama3.2")
	s, _ := AsStreamer(c)
	ch, err := s.Stream(context.Background(), Request{
		Messages: []Message{{Role: RoleUser, Content: "yes or no?"}},
		Grammar:  &grammar,
	})
	if err != nil {
		t.Fatalf("Stream: %v", err)
	}
	for range ch {
	}

	if capturedOptions == nil {
		t.Fatal("options not set in streaming request")
	}
	got, ok := capturedOptions["grammar"].(string)
	if !ok || !strings.Contains(got, "root") {
		t.Errorf("options.grammar in streaming mode = %v; want GBNF body", capturedOptions["grammar"])
	}
}

// TestOllamaStreamFlagTrue verifies that the streaming request sends "stream":true.
func TestOllamaStreamFlagTrue(t *testing.T) {
	var capturedStream any
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body map[string]any
		_ = json.NewDecoder(r.Body).Decode(&body)
		capturedStream = body["stream"]
		// Return minimal valid stream response.
		event := map[string]any{
			"message": map[string]any{"role": "assistant", "content": "hi"},
			"done":    true,
		}
		b, _ := json.Marshal(event)
		fmt.Fprintf(w, "%s\n", b)
	}))
	defer srv.Close()

	c := NewOllama(srv.URL, "llama3.2")
	s, _ := AsStreamer(c)
	ch, _ := s.Stream(context.Background(), Request{
		Messages: []Message{{Role: RoleUser, Content: "hi"}},
	})
	for range ch {
	}
	if capturedStream != true {
		t.Errorf("stream flag = %v; want true", capturedStream)
	}
}

// TestOllamaCompleteStreamFlagFalse verifies that Complete (non-streaming)
// sends "stream":false.
func TestOllamaCompleteStreamFlagFalse(t *testing.T) {
	var capturedStream any
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body map[string]any
		_ = json.NewDecoder(r.Body).Decode(&body)
		capturedStream = body["stream"]
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(fakeOllamaResponse("pong"))
	}))
	defer srv.Close()

	c := NewOllama(srv.URL, "llama3.2")
	_, err := c.Complete(context.Background(), Request{
		Messages: []Message{{Role: RoleUser, Content: "ping"}},
	})
	if err != nil {
		t.Fatalf("Complete: %v", err)
	}
	if capturedStream != false {
		t.Errorf("stream flag = %v; want false", capturedStream)
	}
}

// ensure bufio and bufio.Scanner are referenced to prevent import-cycle lint noise
var _ = bufio.NewScanner
