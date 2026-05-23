package embeddings

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"
)

// OpenAIEndpoint is the default OpenAI embeddings URL.
const OpenAIEndpoint = "https://api.openai.com/v1/embeddings"

// openaiDimensions maps known OpenAI embedding models to their vector size.
var openaiDimensions = map[string]int{
	"text-embedding-3-small": 1536,
	"text-embedding-3-large": 3072,
	"text-embedding-ada-002": 1536,
}

// openaiEmbedder implements Embedder against the OpenAI embeddings API.
type openaiEmbedder struct {
	apiKey   string
	model    string
	endpoint string
	dim      int
	http     *http.Client
}

// NewOpenAI returns an Embedder backed by the OpenAI embeddings API.
// model is required (e.g. "text-embedding-3-small"). apiKey is required.
func NewOpenAI(apiKey, model string) Embedder {
	return &openaiEmbedder{
		apiKey:   apiKey,
		model:    model,
		endpoint: OpenAIEndpoint,
		dim:      openaiDimensions[model],
		http:     &http.Client{Timeout: 60 * time.Second},
	}
}

func (e *openaiEmbedder) Provider() string { return "openai" }
func (e *openaiEmbedder) Model() string    { return e.model }
func (e *openaiEmbedder) Dimension() int   { return e.dim }

func (e *openaiEmbedder) Embed(ctx context.Context, texts []string) ([][]float32, error) {
	if len(texts) == 0 {
		return nil, ErrEmptyInput
	}

	payload := map[string]any{
		"model": e.model,
		"input": texts,
	}
	buf, err := json.Marshal(payload)
	if err != nil {
		return nil, err
	}

	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, e.endpoint, bytes.NewReader(buf))
	if err != nil {
		return nil, err
	}
	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set("Authorization", "Bearer "+e.apiKey)

	resp, err := e.http.Do(httpReq)
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

	var parsed struct {
		Data []struct {
			Embedding []float32 `json:"embedding"`
			Index     int       `json:"index"`
		} `json:"data"`
	}
	if err := json.Unmarshal(body, &parsed); err != nil {
		return nil, fmt.Errorf("openai: decode: %w", err)
	}
	if len(parsed.Data) != len(texts) {
		return nil, fmt.Errorf("openai: expected %d vectors, got %d", len(texts), len(parsed.Data))
	}

	out := make([][]float32, len(texts))
	for _, d := range parsed.Data {
		if d.Index < 0 || d.Index >= len(out) {
			return nil, fmt.Errorf("openai: bad index %d", d.Index)
		}
		out[d.Index] = d.Embedding
	}
	if e.dim == 0 && len(out) > 0 {
		e.dim = len(out[0])
	}
	return out, nil
}
