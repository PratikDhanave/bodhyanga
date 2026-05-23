// ollama.go — Ollama (local model server) embeddings adapter.
//
// Overview:
//   Implements Embedder against Ollama's native /api/embed endpoint (0.5+),
//   which accepts an array of inputs and returns a plural `embeddings` field.
//   Ollama runs open-weight embedding models (nomic-embed-text,
//   mxbai-embed-large, etc.) locally with no API key. The HTTP client uses a
//   120s timeout to tolerate cold-start latency.
//
// Key declarations:
//   - OllamaDefaultHost: "http://localhost:11434" used when no host is configured
//   - NewOllama(host, model) Embedder: constructor returning an ollama-backed Embedder
//   - ollamaEmbedder: unexported implementation
//
// See doc.go for package-level documentation.

package embeddings
