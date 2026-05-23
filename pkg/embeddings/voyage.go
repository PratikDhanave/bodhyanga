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

// VoyageEndpoint is the default Voyage AI embeddings URL.
const VoyageEndpoint = "https://api.voyageai.com/v1/embeddings"

// voyageDimensions maps known Voyage embedding models to their vector size.
var voyageDimensions = map[string]int{
	"voyage-3":       1024,
	"voyage-3-lite":  512,
	"voyage-large-2": 1536,
}

// voyageDefaultDim is the assumed dimension for unknown Voyage models.
const voyageDefaultDim = 1024

// voyageEmbedder implements Embedder against the Voyage AI embeddings API.
type voyageEmbedder struct {
	apiKey   string
	model    string
	endpoint string
	dim      int
	http     *http.Client
}

// NewVoyage returns an Embedder backed by the Voyage AI embeddings API.
// model is required (e.g. "voyage-3"). apiKey is required.
func NewVoyage(apiKey, model string) Embedder {
	dim, ok := voyageDimensions[model]
	if !ok {
		dim = voyageDefaultDim
	}
	return &voyageEmbedder{
		apiKey:   apiKey,
		model:    model,
		endpoint: VoyageEndpoint,
		dim:      dim,
		http:     &http.Client{Timeout: 60 * time.Second},
	}
}

func (e *voyageEmbedder) Provider() string { return "voyage" }
func (e *voyageEmbedder) Model() string    { return e.model }
func (e *voyageEmbedder) Dimension() int   { return e.dim }

func (e *voyageEmbedder) Embed(ctx context.Context, texts []string) ([][]float32, error) {
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
		return nil, fmt.Errorf("voyage: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}
	if resp.StatusCode >= 400 {
		return nil, fmt.Errorf("voyage: http %d: %s", resp.StatusCode, string(body))
	}

	var parsed struct {
		Data []struct {
			Embedding []float32 `json:"embedding"`
			Index     int       `json:"index"`
		} `json:"data"`
	}
	if err := json.Unmarshal(body, &parsed); err != nil {
		return nil, fmt.Errorf("voyage: decode: %w", err)
	}
	if len(parsed.Data) != len(texts) {
		return nil, fmt.Errorf("voyage: expected %d vectors, got %d", len(texts), len(parsed.Data))
	}

	out := make([][]float32, len(texts))
	for i, d := range parsed.Data {
		idx := d.Index
		if idx == 0 && i != 0 && out[0] != nil {
			// Some responses may omit index; fall back to order.
			idx = i
		}
		if idx < 0 || idx >= len(out) {
			return nil, fmt.Errorf("voyage: bad index %d", idx)
		}
		out[idx] = d.Embedding
	}
	if len(out) > 0 && len(out[0]) > 0 {
		e.dim = len(out[0])
	}
	return out, nil
}
