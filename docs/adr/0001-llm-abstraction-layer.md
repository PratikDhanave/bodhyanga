# ADR 0001: LLM Abstraction Layer

**Status:** Accepted

**Date:** 2024-12-15

**Deciders:** Pratik Dhanave

---

## Context

Bodh is a clinical decision support platform that relies on Large Language Models for
diagnostic reasoning, care plan generation, and structured data extraction. The
platform needs to support multiple LLM providers simultaneously:

- **Anthropic** (Claude) for high-accuracy clinical reasoning in cloud deployments.
- **OpenAI** (GPT-4o) as an alternative cloud provider for cost comparison and
  redundancy.
- **Ollama** for local/air-gapped deployments where PHI cannot leave the facility
  network (a hard HIPAA requirement for some health systems).

Without an abstraction layer, every agent in the MARA pipeline would need
provider-specific code paths. Adding a fourth provider (e.g., Google Gemini, a
fine-tuned open model) would require touching every agent file. Testing agents
would require live API keys or per-provider mock implementations.

The business logic in agents (prompt construction, structured output parsing,
multi-turn tool loops) is identical regardless of which provider answers the
call. Only the HTTP wire format and authentication differ.

## Decision

Introduce a single Go interface in `pkg/llm` that all agents depend on:

```go
type Client interface {
    Provider() string
    Model() string
    Complete(ctx context.Context, req Request) (*Response, error)
}
```

Provider-specific implementations (`anthropicClient`, `openaiClient`,
`ollamaClient`) are unexported structs constructed by public factory functions
(`NewAnthropic`, `NewOpenAI`, `NewOllama`). The concrete provider is selected
once at startup in `cmd/care/main.go` based on a `-provider` flag or
`BODH_*_API_KEY` environment variables, and the resulting `Client` is injected
into every agent that needs LLM access.

Key design details:

- **Request/Response are provider-neutral.** `Request` carries system prompt,
  messages, temperature, max tokens, an optional `JSONSchema` for structured
  output, an optional `Tools` slice for function calling, and an optional
  `Grammar` spec for sampler-level constraints. Each provider adapter maps
  these to its wire format (Anthropic tool_use for structured output, OpenAI
  response_format, Ollama `format` field).

- **Structured output is first-class.** When `JSONSchema` is set, the adapter
  wires it to the provider's native structured output mechanism and extracts the
  JSON from any tool-use wrapper before returning it in `Response.Content`. This
  means agents never need to know whether the provider uses tool_use or
  response_format internally.

- **Token accounting is unified.** `Response` carries `InputTokens`,
  `OutputTokens`, and `CachedTokens` regardless of provider. The cost
  calculation in `pkg/llm/budget.go` consumes these fields directly.

- **A `Mock` client exists for tests.** `Mock` records every `Request` in a
  `Calls` slice and returns a configurable `Resp`/`Err`. Agents are tested
  without network calls or API keys.

- **Streaming is handled at the adapter level.** `anthropic_stream.go` and
  `openai_stream.go` implement SSE streaming internally when the provider
  supports it; the `Complete` method still returns a single `*Response` to the
  caller. Streaming to the end user (the Vue SPA) is a separate concern handled
  by the SSE endpoint in `cmd/care`.

## Consequences

### Positive

- **Adding a provider is a single file.** Each provider implementation
  (`anthropic.go`, `openai.go`, `ollama.go`) is self-contained. Adding a new
  provider requires one new file with a struct that satisfies `Client`, plus a
  constructor function. No agent code changes.

- **Agents are testable without LLM access.** The `Mock` client makes every
  agent unit-testable with deterministic responses and zero network calls. Test
  coverage across 23 packages confirms this works at scale.

- **Provider switching is a runtime flag.** Operators can switch from Anthropic
  to Ollama by changing a single environment variable and restarting. This is
  critical for health systems that mandate on-premises inference.

- **Cost tracking is automatic.** Because `Response` carries token counts from
  every provider, the budget tracker in `pkg/llm/budget.go` works uniformly.

- **Grammar support degrades gracefully.** Providers that support sampler-level
  grammars (Ollama/llama.cpp) enforce structural constraints at the token level.
  Providers that do not (Anthropic, OpenAI) emit a one-time warning and fall
  back to JSONSchema validation with retry-on-bad-JSON. The agent code is
  identical either way.

### Negative

- **One layer of indirection.** Developers reading agent code see a `Client`
  interface, not the concrete HTTP call. Understanding what happens on the wire
  requires navigating to the provider file. Mitigated by comprehensive doc
  comments on every adapter.

- **Lowest-common-denominator features.** The `Request` struct can only express
  capabilities all providers share (or that degrade gracefully). A
  provider-specific feature (e.g., Anthropic's prompt caching beta) requires
  either an optional field on `Request` (the `CacheSystem` boolean) or a
  provider-specific wrapper. The team chose optional fields over provider-specific
  wrappers to keep the agent surface uniform.

- **Adapter maintenance burden.** Each provider's API evolves independently.
  Breaking changes in the Anthropic Messages API or OpenAI chat completions
  API require updating the corresponding adapter file. This is manageable
  because the adapter files are small (200-400 lines each) and changes are
  isolated.

### Neutral

- The `Client` interface is intentionally minimal (three methods). Richer
  interfaces (batch completion, embedding, fine-tune management) live in
  separate packages (`pkg/finetune`, `pkg/knowledge/rerank`) rather than
  growing the core `Client`. This keeps the interface stable as the platform
  expands.

## References

- `pkg/llm/client.go` -- interface definition and shared types
- `pkg/llm/anthropic.go` -- Anthropic Messages API adapter
- `pkg/llm/openai.go` -- OpenAI chat completions adapter
- `pkg/llm/ollama.go` -- Ollama native /api/chat adapter
- `pkg/llm/mock.go` -- test mock
- `pkg/llm/budget.go` -- unified cost tracking
- `pkg/llm/grammar.go` -- GBNF grammar support
