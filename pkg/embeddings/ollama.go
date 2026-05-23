package embeddings

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

// OllamaDefaultHost is the local Ollama URL when none is configured.
const OllamaDefaultHost = "http://localhost:11434"

// ollamaEmbedPath is Ollama's native embeddings endpoint (0.5+).
const ollamaEmbedPath = "/api/embed"

// ollamaDimensions maps known Ollama embedding models to their vector size.
var ollamaDimensions = map[string]int{
	"nomic-embed-text":  768,
	"mxbai-embed-large": 1024,
}

// ollamaEmbedder implements Embedder against a local (or remote) Ollama
// server using its native /api/embed endpoint.
type ollamaEmbedder struct {
	host  string
	model string
	dim   int
	http  *http.Client
}

// NewOllama returns an Embedder backed by an Ollama server. host should be a
// scheme://host:port URL such as "http://localhost:11434" (empty falls back
// to OllamaDefaultHost; any trailing slash is stripped). model is required,
// e.g. "nomic-embed-text" or "mxbai-embed-large".
func NewOllama(host, model string) Embedder {
	if host == "" {
		host = OllamaDefaultHost
	}
	host = strings.TrimRight(host, "/")
	return &ollamaEmbedder{
		host:  host,
		model: model,
		dim:   ollamaDimensions[model],
		http:  &http.Client{Timeout: 120 * time.Second},
	}
}

func (e *ollamaEmbedder) Provider() string { return "ollama" }
func (e *ollamaEmbedder) Model() string    { return e.model }
func (e *ollamaEmbedder) Dimension() int   { return e.dim }

func (e *ollamaEmbedder) Embed(ctx context.Context, texts []string) ([][]float32, error) {
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

	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, e.host+ollamaEmbedPath, bytes.NewReader(buf))
	if err != nil {
		return nil, err
	}
	httpReq.Header.Set("Content-Type", "application/json")

	resp, err := e.http.Do(httpReq)
	if err != nil {
		return nil, fmt.Errorf("ollama: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}
	if resp.StatusCode >= 400 {
		return nil, fmt.Errorf("ollama: http %d: %s", resp.StatusCode, string(body))
	}

	var parsed struct {
		Embeddings [][]float32 `json:"embeddings"`
	}
	if err := json.Unmarshal(body, &parsed); err != nil {
		return nil, fmt.Errorf("ollama: decode: %w", err)
	}
	if len(parsed.Embeddings) != len(texts) {
		return nil, fmt.Errorf("ollama: expected %d vectors, got %d", len(texts), len(parsed.Embeddings))
	}
	if e.dim == 0 && len(parsed.Embeddings) > 0 {
		e.dim = len(parsed.Embeddings[0])
	}
	return parsed.Embeddings, nil
}
