package embeddings

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
)

// ---------------------------------------------------------------------------
// Mock coverage: Provider/Model with overrides
// ---------------------------------------------------------------------------

func TestMock_ProviderOverride(t *testing.T) {
	m := &Mock{ProviderName: "custom"}
	if m.Provider() != "custom" {
		t.Errorf("expected custom, got %q", m.Provider())
	}
}

func TestMock_ProviderDefault(t *testing.T) {
	m := &Mock{}
	if m.Provider() != "mock" {
		t.Errorf("expected mock, got %q", m.Provider())
	}
}

func TestMock_ModelOverride(t *testing.T) {
	m := &Mock{ModelName: "custom-model"}
	if m.Model() != "custom-model" {
		t.Errorf("expected custom-model, got %q", m.Model())
	}
}

func TestMock_ModelDefault(t *testing.T) {
	m := &Mock{}
	if m.Model() != "mock-embed" {
		t.Errorf("expected mock-embed, got %q", m.Model())
	}
}

// ---------------------------------------------------------------------------
// Ollama coverage: Model, Dimension accessors
// ---------------------------------------------------------------------------

func TestOllama_ModelAndDimension(t *testing.T) {
	e := NewOllama("", "nomic-embed-text")
	if e.Model() != "nomic-embed-text" {
		t.Errorf("expected nomic-embed-text, got %q", e.Model())
	}
	if e.Dimension() != 768 {
		t.Errorf("expected 768, got %d", e.Dimension())
	}
}

func TestOllama_Embed_HTTPError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(500)
		w.Write([]byte("internal error"))
	}))
	defer srv.Close()

	e := NewOllama(srv.URL, "nomic-embed-text")
	_, err := e.Embed(context.Background(), []string{"hello"})
	if err == nil {
		t.Fatal("expected error for 500 response")
	}
}

func TestOllama_Embed_MismatchedVectors(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		resp := map[string]any{
			"embeddings": [][]float32{{1, 2, 3}},
		}
		json.NewEncoder(w).Encode(resp)
	}))
	defer srv.Close()

	e := NewOllama(srv.URL, "test-model")
	_, err := e.Embed(context.Background(), []string{"hello", "world"})
	if err == nil {
		t.Fatal("expected error for mismatched vectors")
	}
}

func TestOllama_Embed_Success_DynamicDim(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		resp := map[string]any{
			"embeddings": [][]float32{{1, 2, 3, 4}},
		}
		json.NewEncoder(w).Encode(resp)
	}))
	defer srv.Close()

	e := NewOllama(srv.URL, "unknown-model")
	vecs, err := e.Embed(context.Background(), []string{"hello"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(vecs) != 1 || len(vecs[0]) != 4 {
		t.Errorf("unexpected shape")
	}
	// After a successful call, dimension should be set.
	if e.Dimension() != 4 {
		t.Errorf("expected dimension 4, got %d", e.Dimension())
	}
}

func TestOllama_Embed_BadJSON(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte("not json"))
	}))
	defer srv.Close()

	e := NewOllama(srv.URL, "test-model")
	_, err := e.Embed(context.Background(), []string{"hello"})
	if err == nil {
		t.Fatal("expected error for bad JSON")
	}
}

// ---------------------------------------------------------------------------
// OpenAI coverage: Model accessor, error paths
// ---------------------------------------------------------------------------

func TestOpenAI_ModelAccessor(t *testing.T) {
	e := NewOpenAI("key", "text-embedding-3-small")
	if e.Model() != "text-embedding-3-small" {
		t.Errorf("expected text-embedding-3-small, got %q", e.Model())
	}
}

func TestOpenAI_Embed_HTTPError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(429)
		w.Write([]byte("rate limited"))
	}))
	defer srv.Close()

	e := &openaiEmbedder{
		apiKey:   "key",
		model:    "test",
		endpoint: srv.URL,
		http:     srv.Client(),
	}
	_, err := e.Embed(context.Background(), []string{"hello"})
	if err == nil {
		t.Fatal("expected error for 429 response")
	}
}

func TestOpenAI_Embed_MismatchedVectors(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		resp := map[string]any{
			"data": []map[string]any{
				{"embedding": []float64{1, 2}, "index": 0},
			},
		}
		json.NewEncoder(w).Encode(resp)
	}))
	defer srv.Close()

	e := &openaiEmbedder{
		apiKey:   "key",
		model:    "test",
		endpoint: srv.URL,
		http:     srv.Client(),
	}
	_, err := e.Embed(context.Background(), []string{"hello", "world"})
	if err == nil {
		t.Fatal("expected error for mismatched vectors")
	}
}

func TestOpenAI_Embed_BadIndex(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		resp := map[string]any{
			"data": []map[string]any{
				{"embedding": []float64{1, 2}, "index": 99},
			},
		}
		json.NewEncoder(w).Encode(resp)
	}))
	defer srv.Close()

	e := &openaiEmbedder{
		apiKey:   "key",
		model:    "test",
		endpoint: srv.URL,
		http:     srv.Client(),
	}
	_, err := e.Embed(context.Background(), []string{"hello"})
	if err == nil {
		t.Fatal("expected error for bad index")
	}
}

func TestOpenAI_Embed_SuccessDynamicDim(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		resp := map[string]any{
			"data": []map[string]any{
				{"embedding": []float64{0.1, 0.2, 0.3}, "index": 0},
			},
		}
		json.NewEncoder(w).Encode(resp)
	}))
	defer srv.Close()

	e := &openaiEmbedder{
		apiKey:   "key",
		model:    "custom-model",
		endpoint: srv.URL,
		dim:      0, // unknown initially
		http:     srv.Client(),
	}
	vecs, err := e.Embed(context.Background(), []string{"hello"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(vecs) != 1 {
		t.Fatalf("expected 1 vector, got %d", len(vecs))
	}
	if e.dim != 3 {
		t.Errorf("expected dim 3, got %d", e.dim)
	}
}

// ---------------------------------------------------------------------------
// Voyage coverage: Model, Dimension accessors, error paths
// ---------------------------------------------------------------------------

func TestVoyage_ModelAccessor(t *testing.T) {
	e := NewVoyage("key", "voyage-3")
	if e.Model() != "voyage-3" {
		t.Errorf("expected voyage-3, got %q", e.Model())
	}
}

func TestVoyage_DimensionAccessor(t *testing.T) {
	e := NewVoyage("key", "voyage-3")
	if e.Dimension() != 1024 {
		t.Errorf("expected 1024, got %d", e.Dimension())
	}
}

func TestVoyage_UnknownModel_DefaultDim(t *testing.T) {
	e := NewVoyage("key", "custom-voyage")
	if e.Dimension() != voyageDefaultDim {
		t.Errorf("expected %d, got %d", voyageDefaultDim, e.Dimension())
	}
}

func TestVoyage_Embed_HTTPError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(500)
		fmt.Fprintln(w, "error")
	}))
	defer srv.Close()

	e := &voyageEmbedder{
		apiKey:   "key",
		model:    "test",
		endpoint: srv.URL,
		http:     srv.Client(),
	}
	_, err := e.Embed(context.Background(), []string{"hello"})
	if err == nil {
		t.Fatal("expected error")
	}
}

func TestVoyage_Embed_MismatchedVectors(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		resp := map[string]any{
			"data": []map[string]any{
				{"embedding": []float64{1, 2}, "index": 0},
			},
		}
		json.NewEncoder(w).Encode(resp)
	}))
	defer srv.Close()

	e := &voyageEmbedder{
		apiKey:   "key",
		model:    "test",
		endpoint: srv.URL,
		http:     srv.Client(),
	}
	_, err := e.Embed(context.Background(), []string{"hello", "world"})
	if err == nil {
		t.Fatal("expected error for mismatched")
	}
}

func TestVoyage_Embed_BadIndex(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		resp := map[string]any{
			"data": []map[string]any{
				{"embedding": []float64{1, 2}, "index": -1},
			},
		}
		json.NewEncoder(w).Encode(resp)
	}))
	defer srv.Close()

	e := &voyageEmbedder{
		apiKey:   "key",
		model:    "test",
		endpoint: srv.URL,
		http:     srv.Client(),
	}
	_, err := e.Embed(context.Background(), []string{"hello"})
	if err == nil {
		t.Fatal("expected error for bad index")
	}
}

func TestVoyage_Embed_SuccessUpdatesDim(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		resp := map[string]any{
			"data": []map[string]any{
				{"embedding": []float64{0.1, 0.2, 0.3, 0.4, 0.5}, "index": 0},
			},
		}
		json.NewEncoder(w).Encode(resp)
	}))
	defer srv.Close()

	e := &voyageEmbedder{
		apiKey:   "key",
		model:    "test",
		endpoint: srv.URL,
		dim:      0,
		http:     srv.Client(),
	}
	vecs, err := e.Embed(context.Background(), []string{"hello"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(vecs) != 1 {
		t.Fatalf("expected 1 vector")
	}
	if e.dim != 5 {
		t.Errorf("expected dim 5, got %d", e.dim)
	}
}

func TestVoyage_Embed_FallbackIndex(t *testing.T) {
	// Test the index=0 fallback for second items.
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		resp := map[string]any{
			"data": []map[string]any{
				{"embedding": []float64{0.1, 0.2}, "index": 0},
				{"embedding": []float64{0.3, 0.4}, "index": 0}, // index 0 but it's the second item
			},
		}
		json.NewEncoder(w).Encode(resp)
	}))
	defer srv.Close()

	e := &voyageEmbedder{
		apiKey:   "key",
		model:    "test",
		endpoint: srv.URL,
		dim:      2,
		http:     srv.Client(),
	}
	vecs, err := e.Embed(context.Background(), []string{"hello", "world"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(vecs) != 2 {
		t.Fatalf("expected 2 vectors, got %d", len(vecs))
	}
}

func TestAllEmbedders_EmptyInput(t *testing.T) {
	tests := []struct {
		name     string
		embedder Embedder
	}{
		{"ollama", NewOllama("http://localhost:1", "test")},
		{"openai", NewOpenAI("key", "test")},
		{"voyage", NewVoyage("key", "test")},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := tt.embedder.Embed(context.Background(), nil)
			if err != ErrEmptyInput {
				t.Errorf("expected ErrEmptyInput, got %v", err)
			}
			_, err = tt.embedder.Embed(context.Background(), []string{})
			if err != ErrEmptyInput {
				t.Errorf("expected ErrEmptyInput, got %v", err)
			}
		})
	}
}
