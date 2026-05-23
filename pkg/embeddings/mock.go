package embeddings

import "context"

// Mock is an in-memory Embedder for tests. Set Resp or Err to control
// behaviour; Calls records every batch of inputs received.
type Mock struct {
	// ProviderName overrides the provider string. Defaults to "mock".
	ProviderName string

	// ModelName overrides the model string. Defaults to "mock-embed".
	ModelName string

	// Dim is the dimension of generated vectors. Defaults to 8.
	Dim int

	// Resp, when non-nil, is returned verbatim from Embed.
	Resp [][]float32

	// Err, if set, is returned from Embed instead of Resp.
	Err error

	// Calls captures every batch the test made.
	Calls [][]string
}

// Provider implements Embedder.
func (m *Mock) Provider() string {
	if m.ProviderName != "" {
		return m.ProviderName
	}
	return "mock"
}

// Model implements Embedder.
func (m *Mock) Model() string {
	if m.ModelName != "" {
		return m.ModelName
	}
	return "mock-embed"
}

// Dimension implements Embedder.
func (m *Mock) Dimension() int {
	if m.Dim > 0 {
		return m.Dim
	}
	return 8
}

// Embed implements Embedder. Records the batch, then returns Resp / Err.
// When both are nil, returns deterministic pseudo-embeddings: the i-th vector
// is [float32(i), 0, 0, ..., 0] of length Dimension(). That is enough
// structure for simple cosine-similarity tests to distinguish inputs.
func (m *Mock) Embed(_ context.Context, texts []string) ([][]float32, error) {
	// Copy the batch so callers can mutate their input slice without
	// disturbing what we recorded.
	batch := make([]string, len(texts))
	copy(batch, texts)
	m.Calls = append(m.Calls, batch)

	if m.Err != nil {
		return nil, m.Err
	}
	if m.Resp != nil {
		return m.Resp, nil
	}

	dim := m.Dimension()
	out := make([][]float32, len(texts))
	for i := range texts {
		vec := make([]float32, dim)
		vec[0] = float32(i)
		out[i] = vec
	}
	return out, nil
}
