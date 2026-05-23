// Package embeddings provides a provider-agnostic text embedding interface
// used by pkg/knowledge for retrieval-augmented generation.
//
// Like pkg/llm, the interface is intentionally small:
//
//   - Embed: text(s) → vector(s)
//   - Dimension: the vector size (so callers can pre-allocate or check store compatibility)
//   - Provider/Model: identity for logs and bench
//
// Adapters
//
// Three are shipped:
//
//   - NewOpenAI(apiKey, model)   — text-embedding-3-small (1536d) by default
//   - NewVoyage(apiKey, model)   — Voyage AI; Anthropic's recommended embedder
//   - NewOllama(host, model)     — local Ollama, e.g. "nomic-embed-text" (768d)
//
// And a Mock for tests.
//
// Why a separate package from pkg/llm
//
// Embeddings have a different shape (vector out, not text), different cost
// profile, and often a different provider than the chat LLM. Splitting keeps
// each interface narrow.
package embeddings
