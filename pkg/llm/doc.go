// Package llm provides a provider-agnostic client interface for large language
// models, used by Bodh agents that need generative reasoning (diagnostician,
// reasoning verifier, questioner).
//
// # Why a thin abstraction
//
// Different providers (Anthropic, OpenAI, Bedrock, local Ollama) speak slightly
// different wire formats, but the things an agent actually needs are small:
//
//   - send a system prompt + a list of user/assistant messages
//   - request a structured JSON response that matches a schema
//   - get back the text, token usage, and (where supported) cache-hit counts
//
// The Client interface in this package captures exactly that surface. Each
// adapter file (anthropic.go, openai.go) translates between this interface and
// the provider's HTTP API.
//
// # Prompt caching
//
// Request.CacheSystem marks the system prompt as cacheable. On Anthropic this
// becomes a cache_control block; on OpenAI the SDK handles it implicitly when
// the prefix is reused. The Response surfaces CachedTokens so the bench
// runner can report savings.
//
// # Structured outputs
//
// Request.JSONSchema lets the caller demand JSON matching a schema. On
// Anthropic this is implemented via a single "tool" the model must call; on
// OpenAI it uses the response_format: json_schema mode. Either way the caller
// gets back parseable JSON in Response.Content.
//
// # Multi-provider
//
// Constructors (NewAnthropic, NewOpenAI) take an API key + model name and
// return an llm.Client. Agents take llm.Client by interface and don't care
// which provider is on the other end. Tests use the Mock client.
//
// What this package does NOT do
//
//   - Retries / rate-limit backoff (the http.Client does basic timeouts).
//   - Streaming. The Bodh message bus is request/response per agent step.
//   - Embeddings. Add a separate Embedder interface when RAG lands.
package llm
