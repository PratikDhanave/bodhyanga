// anthropic.go — Anthropic Messages API adapter.
//
// Overview:
//   Implements Client against the Anthropic Messages API using net/http (no SDK
//   dep). System prompts are sent as a cache_control: ephemeral block when
//   Request.CacheSystem is true. Structured outputs are delivered via a single
//   forced tool call; the tool's input JSON is returned in Response.Content.
//
// Key declarations:
//   - AnthropicEndpoint, AnthropicVersion: defaults for URL and api-version header
//   - NewAnthropic(apiKey, model) Client: constructor returning an anthropic-backed Client
//   - anthropicClient: unexported implementation
//
// See doc.go for package-level documentation.

package llm
