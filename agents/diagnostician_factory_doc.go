// diagnostician_factory.go — shared constructor for the diagnostician agent.
//
// Overview:
//   NewDiagnostician(provider) returns the right agent.Agent implementation
//   based on a provider string: "rule" for the deterministic DiagnosticianAgent
//   or "llm-anthropic" / "llm-openai" for LLMDiagnostician wrapping the matching
//   llm.Client. API keys are read from env (ANTHROPIC_API_KEY, OPENAI_API_KEY);
//   model names default to claude-sonnet-4-6 / gpt-4o and can be overridden via
//   BODH_ANTHROPIC_MODEL / BODH_OPENAI_MODEL. Used by cmd/demo, cmd/care, and
//   cmd/bench so the -diagnostician / -providers flags share one wiring path.
//
// Key declarations:
//   - ProviderRule, ProviderLLMAnthropic, ProviderLLMOpenAI: provider name constants
//   - DefaultAnthropicModel, DefaultOpenAIModel: default model identifiers
//   - NewDiagnostician(provider string) (agent.Agent, error): the factory
//
// See doc.go for package-level documentation.

package agents
