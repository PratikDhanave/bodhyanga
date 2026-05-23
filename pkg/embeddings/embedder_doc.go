// embedder.go — provider-agnostic text-embedding interface.
//
// Overview:
//   Defines the Embedder interface used by pkg/knowledge and any other caller
//   that needs to turn text into vectors. The interface is intentionally small
//   — Provider/Model identify the backend, Dimension exposes the vector size,
//   and Embed batches text into [][]float32. ErrEmptyInput is the canonical
//   sentinel returned when callers pass no inputs.
//
// Key declarations:
//   - Embedder: interface implemented by every adapter
//   - ErrEmptyInput: sentinel returned by Embed when len(texts) == 0
//
// See doc.go for package-level documentation.

package embeddings
