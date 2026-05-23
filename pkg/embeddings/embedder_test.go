package embeddings

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestMockRecordsCallsReturnsResp(t *testing.T) {
	want := [][]float32{{1, 2, 3}, {4, 5, 6}}
	m := &Mock{Resp: want}

	got, err := m.Embed(context.Background(), []string{"a", "b"})
	if err != nil {
		t.Fatalf("Embed: %v", err)
	}
	if len(got) != 2 || got[0][0] != 1 || got[1][2] != 6 {
		t.Fatalf("got %+v, want %+v", got, want)
	}
	if len(m.Calls) != 1 {
		t.Fatalf("expected 1 call, got %d", len(m.Calls))
	}
	if m.Calls[0][0] != "a" || m.Calls[0][1] != "b" {
		t.Fatalf("inputs not captured: %+v", m.Calls[0])
	}
	if m.Provider() != "mock" || m.Model() != "mock-embed" || m.Dimension() != 8 {
		t.Fatalf("default identity wrong: %s/%s dim=%d", m.Provider(), m.Model(), m.Dimension())
	}
}

func TestMockReturnsErr(t *testing.T) {
	want := errors.New("boom")
	m := &Mock{Err: want}
	_, err := m.Embed(context.Background(), []string{"x"})
	if !errors.Is(err, want) {
		t.Fatalf("expected %v, got %v", want, err)
	}
	// Calls should still record the inputs, so tests can assert what reached
	// the embedder before it failed.
	if len(m.Calls) != 1 || m.Calls[0][0] != "x" {
		t.Fatalf("expected call to be recorded, got %+v", m.Calls)
	}
}

func TestMockGeneratesDeterministicEmbeddings(t *testing.T) {
	m := &Mock{Dim: 4}
	got, err := m.Embed(context.Background(), []string{"alpha", "beta", "gamma"})
	if err != nil {
		t.Fatalf("Embed: %v", err)
	}
	if len(got) != 3 {
		t.Fatalf("expected 3 vectors, got %d", len(got))
	}
	for i, vec := range got {
		if len(vec) != 4 {
			t.Fatalf("vector %d wrong dim: %d", i, len(vec))
		}
		if vec[0] != float32(i) {
			t.Fatalf("vector %d[0] = %v, want %v", i, vec[0], float32(i))
		}
		for j := 1; j < len(vec); j++ {
			if vec[j] != 0 {
				t.Fatalf("vector %d[%d] = %v, want 0", i, j, vec[j])
			}
		}
	}
}

func TestOpenAIHappyPath(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/embeddings" {
			t.Errorf("wrong path: %s", r.URL.Path)
		}
		if r.Header.Get("Authorization") != "Bearer sk-test" {
			t.Errorf("missing/auth header wrong: %q", r.Header.Get("Authorization"))
		}
		var sent map[string]any
		if err := json.NewDecoder(r.Body).Decode(&sent); err != nil {
			t.Errorf("decode req: %v", err)
		}
		if sent["model"] != "text-embedding-3-small" {
			t.Errorf("wrong model: %v", sent["model"])
		}
		inputs, _ := sent["input"].([]any)
		if len(inputs) != 2 {
			t.Errorf("expected 2 inputs, got %d", len(inputs))
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{
			"data":[
				{"embedding":[0.1,0.2,0.3],"index":0},
				{"embedding":[0.4,0.5,0.6],"index":1}
			]
		}`))
	}))
	defer srv.Close()

	e := &openaiEmbedder{
		apiKey:   "sk-test",
		model:    "text-embedding-3-small",
		endpoint: srv.URL + "/v1/embeddings",
		dim:      openaiDimensions["text-embedding-3-small"],
		http:     http.DefaultClient,
	}
	got, err := e.Embed(context.Background(), []string{"foo", "bar"})
	if err != nil {
		t.Fatalf("Embed: %v", err)
	}
	if len(got) != 2 || len(got[0]) != 3 {
		t.Fatalf("shape: %+v", got)
	}
	if got[0][0] != 0.1 || got[1][2] != 0.6 {
		t.Fatalf("content: %+v", got)
	}
	if e.Provider() != "openai" {
		t.Fatalf("provider: %s", e.Provider())
	}
	if e.Dimension() != 1536 {
		t.Fatalf("expected 1536 dim for text-embedding-3-small, got %d", e.Dimension())
	}
}

func TestOpenAIHTTPError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(500)
		_, _ = w.Write([]byte(`{"error":"server"}`))
	}))
	defer srv.Close()
	e := &openaiEmbedder{apiKey: "k", model: "m", endpoint: srv.URL, http: http.DefaultClient}
	_, err := e.Embed(context.Background(), []string{"x"})
	if err == nil || !strings.Contains(err.Error(), "500") {
		t.Fatalf("expected 500 error, got %v", err)
	}
}

func TestVoyageHappyPath(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer pa-test" {
			t.Errorf("missing/auth header wrong: %q", r.Header.Get("Authorization"))
		}
		var sent map[string]any
		_ = json.NewDecoder(r.Body).Decode(&sent)
		if sent["model"] != "voyage-3" {
			t.Errorf("wrong model: %v", sent["model"])
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{
			"data":[
				{"embedding":[0.11,0.22],"index":0},
				{"embedding":[0.33,0.44],"index":1}
			]
		}`))
	}))
	defer srv.Close()

	e := &voyageEmbedder{
		apiKey:   "pa-test",
		model:    "voyage-3",
		endpoint: srv.URL,
		dim:      voyageDimensions["voyage-3"],
		http:     http.DefaultClient,
	}
	got, err := e.Embed(context.Background(), []string{"foo", "bar"})
	if err != nil {
		t.Fatalf("Embed: %v", err)
	}
	if len(got) != 2 || got[0][0] != 0.11 || got[1][1] != 0.44 {
		t.Fatalf("content: %+v", got)
	}
	if e.Provider() != "voyage" {
		t.Fatalf("provider: %s", e.Provider())
	}
}

func TestOllamaHappyPath(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != ollamaEmbedPath {
			t.Errorf("wrong path: %s", r.URL.Path)
		}
		var sent map[string]any
		_ = json.NewDecoder(r.Body).Decode(&sent)
		if sent["model"] != "nomic-embed-text" {
			t.Errorf("wrong model: %v", sent["model"])
		}
		inputs, _ := sent["input"].([]any)
		if len(inputs) != 2 {
			t.Errorf("expected 2 inputs, got %d", len(inputs))
		}
		w.Header().Set("Content-Type", "application/json")
		// Note: plural "embeddings" — this is the format that needs to decode.
		_, _ = w.Write([]byte(`{
			"embeddings":[
				[0.01,0.02,0.03],
				[0.04,0.05,0.06]
			]
		}`))
	}))
	defer srv.Close()

	e := &ollamaEmbedder{
		host:  srv.URL,
		model: "nomic-embed-text",
		dim:   ollamaDimensions["nomic-embed-text"],
		http:  http.DefaultClient,
	}
	got, err := e.Embed(context.Background(), []string{"foo", "bar"})
	if err != nil {
		t.Fatalf("Embed: %v", err)
	}
	if len(got) != 2 || got[0][0] != 0.01 || got[1][2] != 0.06 {
		t.Fatalf("content: %+v", got)
	}
	if e.Provider() != "ollama" {
		t.Fatalf("provider: %s", e.Provider())
	}
}

func TestNewOllamaFillsDefaultHost(t *testing.T) {
	e := NewOllama("", "nomic-embed-text").(*ollamaEmbedder)
	if e.host != OllamaDefaultHost {
		t.Fatalf("expected default host %q, got %q", OllamaDefaultHost, e.host)
	}
	// Trailing slash should be stripped.
	e2 := NewOllama("http://example.com:11434/", "nomic-embed-text").(*ollamaEmbedder)
	if e2.host != "http://example.com:11434" {
		t.Fatalf("trailing slash not stripped: %q", e2.host)
	}
}

func TestEmptyInputReturnsErr(t *testing.T) {
	e := NewOpenAI("k", "text-embedding-3-small")
	if _, err := e.Embed(context.Background(), nil); !errors.Is(err, ErrEmptyInput) {
		t.Fatalf("openai: want ErrEmptyInput, got %v", err)
	}
	e = NewVoyage("k", "voyage-3")
	if _, err := e.Embed(context.Background(), []string{}); !errors.Is(err, ErrEmptyInput) {
		t.Fatalf("voyage: want ErrEmptyInput, got %v", err)
	}
	e = NewOllama("", "nomic-embed-text")
	if _, err := e.Embed(context.Background(), nil); !errors.Is(err, ErrEmptyInput) {
		t.Fatalf("ollama: want ErrEmptyInput, got %v", err)
	}
}
