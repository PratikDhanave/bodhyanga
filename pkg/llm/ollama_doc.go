// ollama.go — Ollama (local model server) adapter.
//
// Overview:
//   Implements Client against Ollama's native /api/chat endpoint. Ollama runs
//   open-weight models (Llama, Mistral, Qwen, etc.) locally with no API key.
//   Structured outputs use Ollama's `format` field (which accepts a JSON
//   Schema document in 0.5+). MaxTokens maps to the `num_predict` option and
//   temperature/system prompt are passed through normally. CachedTokens is
//   always 0 (Ollama does not surface prompt caching).
//
// Key declarations:
//   - OllamaDefaultHost: "http://localhost:11434" used when no host is configured
//   - NewOllama(host, model) Client: constructor returning an ollama-backed Client
//   - ollamaClient: unexported implementation
//
// See doc.go for package-level documentation.

package llm
