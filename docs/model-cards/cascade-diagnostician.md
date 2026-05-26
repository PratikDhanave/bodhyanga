# Model Card — `CascadeDiagnostician`

> **Card version:** 1.0.0 · **Last reviewed:** 2026-05-25 · **Author:** Bodh maintainers

## Model details

- **Variant name:** `CascadeDiagnostician`
- **Source:** [`agents/diagnostician_cascade.go`](../../agents/diagnostician_cascade.go)
- **Type:** Small → large model escalation with deterministic fallback
- **CLI:** `-diagnostician=llm-anthropic-cascade` (or `-openai`, `-ollama`)
- **Default LLM provider:** parent
- **Default cascade:** cheap/fast model → strong/slow model on low confidence
- **Provider-configurable:** yes
- **License:** Apache-2.0 (Bodh) / provider terms

## Intended use

- **Primary intended use:** A cheap small model tries first. If its `DiagnosisProposal.Confidence < EscalationThreshold` (default 0.7), the same call is retried against a larger model. Optimises mean cost while preserving accuracy on hard cases.
- **Primary intended users:** Operators with cost-sensitive deployments who want LLM-grade primary inference on hard cases without paying for it on easy ones.
- **Out-of-scope uses:**
  - Latency-sensitive flows where the second call's delay matters
  - Deployments without two different model tiers configured

## Factors

- **Cascade depth:** 2 models today (small → large). Could extend to 3 (tiny → medium → large) if cost data justifies.
- **Threshold tuning:** Default 0.7. Higher = more escalation = better accuracy + higher cost.
- **Model tiers:** Anthropic default `claude-haiku` → `claude-sonnet-4-6`; OpenAI `gpt-4o-mini` → `gpt-4o`; Ollama smaller llama variants.

## Metrics

The Pareto winner on Bodh's bench, typically:

| Metric | Cascade vs single-large |
|---|---|
| Accuracy | within 1-2pp |
| Mean cost | 30-50% less |
| Latency p50 | similar (most cases stop at first model) |
| Latency p99 | 2-3× (cases that escalate) |
| Pareto score | typically the highest |

Bench reproducibility: `go run ./cmd/bench -providers=llm-anthropic,llm-anthropic-cascade -manifest=data/cases/manifest.json`

## Evaluation data

Same as `LLMDiagnostician`. Cascade's value is most visible when the bench includes a mix of easy + hard cases; add fixtures that exercise both tails.

## Training data

Inherits both underlying LLMs. The cascade trigger is the small model's `Confidence` field — no fine-tuning.

## Ethical considerations

- **Risk: small model's confidence is miscalibrated.** A confidently-wrong small-model answer skips escalation. Mitigations: deterministic fallback still runs on confidence below `MinConfidence`; bench surfaces miscalibration regressions.
- **Risk: cost variance.** Cascade cost is bimodal (cheap on easy cases, expensive on hard). Capacity planning must use p99 cost, not mean.
- **Risk: latency variance.** Same — p99 includes the escalation case. SLO design must account for this.

## Caveats and recommendations

- **Tune `EscalationThreshold` per deployment.** 0.7 is the default; calibrate on your bench.
- **Monitor escalation rate.** If it's >50%, you're paying cascade overhead without saving; consider just using the large model. If it's <10%, you're under-utilising the cascade; consider lowering the threshold or removing the cascade entirely.
- **Cascade composes with HITL.** Pair with HITL gate to add human review on the escalated cases (where stakes are highest).
- **Cascade does NOT compose well with Reflexion or Debate.** Each is its own complexity layer; stack carefully.

## References

- [agents/diagnostician_cascade.go](../../agents/diagnostician_cascade.go)
- [docs/architecture.md § Agentic patterns](../architecture.md#agentic-patterns-debate-reflexion-cascade)
