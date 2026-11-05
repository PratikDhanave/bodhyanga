# Model Card — `LLMDiagnostician`

> **Card version:** 1.0.0 · **Last reviewed:** 2026-05-25 · **Author:** Bodhyanga maintainers

## Model details

- **Variant name:** `LLMDiagnostician`
- **Source:** [`agents/diagnostician_llm.go`](../../agents/diagnostician_llm.go)
- **Type:** Single-LLM-call agent with deterministic rule-based fallback
- **Default LLM provider:** Anthropic / OpenAI / Ollama (selected via `-diagnostician=llm-{anthropic,openai,ollama}`)
- **Default LLM models:**
  - Anthropic: `claude-sonnet-4-6` (override via `BODHYANGA_ANTHROPIC_MODEL`)
  - OpenAI: `gpt-4o` (override via `BODHYANGA_OPENAI_MODEL`)
  - Ollama: `llama3.2` (override via `BODHYANGA_OLLAMA_MODEL`)
- **Provider-configurable:** yes
- **License:** Apache-2.0 (Bodhyanga) / provider terms (LLM)
- **Maintainer:** Bodhyanga project

## Intended use

- **Primary intended use:** Generate a `DiagnosisProposal` from a `CaseState` using a single LLM call. The LLM sees the case narrative, RPM alerts, prior questions/answers, ordered tests, and test results, and returns a structured JSON proposal matching the same schema as the rule-based diagnostician.
- **Primary intended users:** Researchers evaluating LLM-backed clinical reasoning; operators with API keys for one or more supported LLM providers.
- **Out-of-scope uses:**
  - Standalone clinical diagnosis without HITL gate
  - Use without the deterministic fallback wired in (the fallback IS the contract)
  - Deployment without a Business Associate Agreement covering the LLM provider when handling PHI

## Factors

- **Relevant conditions:** Any condition the underlying LLM's training data covers + RAG corpus when enabled. Empirically wider than the rule-based variant.
- **Patient demographics:** Inherits any biases in the underlying LLM. Equity audit pipeline planned.
- **Instrumentation:** Same `CaseState` inputs as rule-based; optional `BODHYANGA_KNOWLEDGE_CORPUS` enables RAG with `HybridStore` (vector + BM25 + reciprocal-rank fusion).
- **Environment:** Always invoked downstream of `cost_guardian`; always upstream of `reasoning_verifier`. Prompt caching enabled by default on Anthropic and OpenAI.

## Metrics

Bench reproducibility: `go run ./cmd/bench -manifest=data/cases/manifest.json -providers=llm-anthropic`

LLM-provider bench results are deployment-dependent (model version, API region, prompt rev). Run the bench in your environment and update this card with the values you measure.

| Metric | Expected range |
|---|---|
| Accuracy (exact-match) | 85-92% on Bodhyanga's 7-fixture bench (typical) |
| Mean cost USD | ~$240 (same workups as rule provider; test_planner is shared) |
| Mean message steps | 11.0 |
| Latency p50 | 2-5s per LLM call |
| Latency p99 | 10-15s per LLM call |
| Fallback rate | <5% target; alert at >10% |
| Prompt cache hit rate | 50-80% within TTL (Anthropic / OpenAI) |

## Evaluation data

- **Fixtures:** [`data/cases/manifest.json`](../../data/cases/manifest.json)
- **Red-team catalogue:** [`data/eval/redteam.json`](../../data/eval/redteam.json)
- **Knowledge corpus:** [`data/knowledge/corpus.json`](../../data/knowledge/corpus.json) — 15 synthetic teaching documents

## Training data

This variant composes with vendor LLMs. Refer to:

- [Anthropic Claude data handling](https://www.anthropic.com/legal/aup)
- [OpenAI training data policy](https://openai.com/policies/usage-policies/)
- Ollama: model-specific (local, no training data sent over the network)

The system prompt is cacheable; per-request user content varies. RAG-retrieved citations are first-class on the `DiagnosisProposal.Citations` field.

## Quantitative analyses

Run side-by-side with the rule provider:

```bash
go run ./cmd/bench -providers=rule,llm-anthropic,llm-openai,llm-ollama \
                   -manifest=data/cases/manifest.json
```

Compare the per-provider accuracy, mean cost, and Pareto score. The interesting question is per-condition: where does the LLM provider beat the rule provider, and by how much?

## Ethical considerations

- **Risk: hallucinated diagnosis.** Mitigations: deterministic fallback on low confidence; RAG grounding check (`pkg/safety.IsGroundedInCitations`); rule-based comparison via bench.
- **Risk: training data bias.** Mitigations: red-team catalogue exercises adversarial cases; equity audit pipeline planned.
- **Risk: PHI sent to vendor LLMs.** Mitigations: PHI redaction at logger seam (defence in depth); BAA chain documented in [docs/compliance.md](../compliance.md); OpenAI requires Zero Data Retention agreement before any PHI; Anthropic offers BAA via API on request.
- **Risk: prompt injection from clinical narrative.** Mitigations: `pkg/safety.IsRefusal` check; structured-output JSON schema constrains response; reasoning_verifier reviews the rationale.
- **Risk: provider outage.** Mitigations: deterministic rule-based fallback; cross-provider failover pattern documented (not yet wired).

## Caveats and recommendations

- **The fallback is the contract.** This variant must never be deployed without its deterministic fallback. The case always finalizes.
- **`MinConfidence` threshold (default 0.6) routes low-confidence LLM responses to the fallback.** Tune per deployment; don't disable.
- **BAA before any PHI.** Verify the BAA chain (cloud → LLM provider → SMS/email subcontractor) before going live.
- **Cache the system prompt.** Provider prompt-caching shaves token cost on repeated requests within the TTL.
- **Monitor the fallback rate.** A spike means the LLM provider is degraded — investigate before users notice.

## References

- [agents/diagnostician_llm.go](../../agents/diagnostician_llm.go)
- [bodhyanga-linkedin/04-fallback-is-the-contract.md](../../bodhyanga-linkedin/04-fallback-is-the-contract.md) — the fallback contract write-up
- [docs/compliance.md § HIPAA Privacy Rule](../compliance.md#hipaa-privacy-rule) — BAA chain
- [docs/ai-features.md](../ai-features.md) — RAG + prompt caching setup
