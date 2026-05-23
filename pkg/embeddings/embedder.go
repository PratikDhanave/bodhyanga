package embeddings

import (
	"context"
	"errors"
)

// Embedder is the provider-agnostic text-embedding interface.
type Embedder interface {
	// Provider returns a short identifier ("openai", "voyage", "ollama", "mock").
	Provider() string

	// Model returns the model identifier (e.g. "text-embedding-3-small",
	// "voyage-3", "nomic-embed-text").
	Model() string

	// Dimension returns the embedding vector size. Implementations should
	// learn this from the first Embed call, but may also hard-code per-model
	// defaults for callers that need it eagerly.
	Dimension() int

	// Embed turns each input string into a float32 vector. Length of the
	// returned slice equals len(texts); each inner slice has Dimension()
	// entries. Implementations should batch internally where the provider
	// supports it.
	Embed(ctx context.Context, texts []string) ([][]float32, error)
}

// ErrEmptyInput is returned when Embed is called with no inputs.
var ErrEmptyInput = errors.New("embeddings: no inputs to embed")
