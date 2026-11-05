# Model Card — `DebateDiagnostician`

> **Card version:** 1.0.0 · **Last reviewed:** 2026-05-25 · **Author:** Bodhyanga maintainers

## Model details

- **Variant name:** `DebateDiagnostician`
- **Source:** [`agents/diagnostician_debate.go`](../../agents/diagnostician_debate.go)
- **Type:** Multi-LLM-call agent (specialist personas + chair) with deterministic fallback
- **CLI:** `-diagnostician=llm-anthropic-debate` (or `-openai`, `-ollama`)
- **Default LLM provider:** same as parent `LLMDiagnostician`
- **Provider-configurable:** yes
- **License:** Apache-2.0 (Bodhyanga) / provider terms (LLM)

## Intended use

- **Primary intended use:** Three specialist personas (cardiologist, pulmonologist, infectious disease) each produce an independent `DiagnosisProposal` from the same `CaseState`. A chair persona synthesises a final answer that weighs the three. Higher cost than single-call; useful when disagreement signal matters and clinical evidence is borderline.
- **Primary intended users:** Researchers evaluating ensemble approaches; operators running Pareto comparisons.
- **Out-of-scope uses:**
  - Production deployments where latency matters (3-4× the single-call latency)
  - Cases where disagreement is unlikely (the synthesis cost is wasted)
  - Specialty selection that doesn't match the case (the personas are fixed; future PR adds dynamic selection)

## Factors

- **Relevant conditions:** Cases where multiple body systems could plausibly explain the presentation. CHF vs CAP vs PE in a dyspneic patient is the canonical example.
- **Persona diversity:** Three personas today; expandable in code. The personas are hand-coded with disclaimer comments — not validated specialists.
- **Latency:** 3-4× single LLM call (3 specialists + 1 chair).
- **Cost:** ~4× the token cost of single-call.

## Metrics

Run side-by-side bench:

```bash
go run ./cmd/bench -providers=llm-anthropic,llm-anthropic-debate -manifest=data/cases/manifest.json
```

Expect:
- **Accuracy:** typically +0-3pp vs single-call on the bench (depends on case mix)
- **Cost:** ~4× single-call
- **Latency:** ~3× single-call (parallelised specialists + sequential chair)
- **Pareto score:** typically *worse* than single-call on synthetic bench (extra cost not justified). Better on harder cases.

The Pareto score tells you whether debate is worth it for your case mix. **Don't deploy debate without bench evidence that it helps your specific distribution.**

## Evaluation data

Same as `LLMDiagnostician`. Add adversarial cases where multiple specialists could reasonably disagree.

## Training data

Inherits the underlying LLM's training. The persona prompts are hand-coded prefixes; no fine-tuning.

## Ethical considerations

- **Risk: chair persona overfits one specialist's view.** Mitigation: bench-driven evaluation per condition; future ablation studies.
- **Risk: false confidence from multi-specialist agreement.** Three personas all powered by the same underlying LLM may produce correlated errors. Mitigation: the rule-based fallback runs the deterministic logic that doesn't rely on LLM reasoning.
- **Risk: cost amplification.** A debate-diagnosed case is ~4× the cost. Mitigations: cost_guardian still enforces the per-case budget; the budget cap remains the upper bound.
- **Risk: PHI exposure × 4.** Every additional LLM call is another PHI surface. Mitigations: same BAA chain considerations as parent; prompt caching reduces repeated transmission.

## Caveats and recommendations

- **Deploy only with bench evidence.** Debate's value is empirical, not theoretical. The bench will tell you whether it pays back on your case mix.
- **Reserve for high-stakes ambiguous cases.** Use a cascade pattern (small model → debate on low-confidence) rather than debate-every-case.
- **HITL gate is non-optional.** Three specialists disagreeing is exactly the kind of case where clinician review adds value.

## References

- [agents/diagnostician_debate.go](../../agents/diagnostician_debate.go)
- [docs/architecture.md § Agentic patterns](../architecture.md#agentic-patterns-debate-reflexion-cascade)
