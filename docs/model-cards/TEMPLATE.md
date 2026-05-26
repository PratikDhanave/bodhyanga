# Model Card — [Agent Variant Name]

> **Card version:** 0.1.0 · **Last reviewed:** YYYY-MM-DD · **Author:** [Name]
>
> Template per Mitchell et al. (2018). Bodh's agent-variant model cards.

## Model details

- **Variant name:** [`AgentName`]
- **Source:** [`agents/path/to/file.go`](../../agents/path/to/file.go)
- **Type:** [rule-based | single-LLM-call | multi-LLM-call | tool-using | vision]
- **Default LLM provider:** [Anthropic | OpenAI | Ollama | none]
- **Default LLM model:** [e.g. `claude-sonnet-4-6`]
- **Provider-configurable:** [yes / no]
- **License:** Apache-2.0 (Bodh) / per-provider terms (LLM)
- **Maintainer:** Bodh project ([GitHub issues](https://github.com/PratikDhanave/bodh/issues))

## Intended use

- **Primary intended use:** [one-paragraph description anchored in a concrete clinical use case]
- **Primary intended users:** [clinicians / nurses / care coordinators / operators / researchers]
- **Out-of-scope uses:** [enumerate at least three things this variant should NOT do]

## Factors

Factors that may affect this variant's performance. Bodh's bench reports per-condition accuracy; demographic factors are tracked separately when equity audit is run.

- **Relevant conditions / phenotypes:** [e.g. CAP, CHF, ACS, sepsis, DKA — or "all" for non-specialised variants]
- **Patient demographics:** Synthetic only in CI; equity audit (stratified accuracy by age / sex / race / SES) planned per [ROADMAP Phase 7](../../ROADMAP.md)
- **Instrumentation:** RPM observations, lab feeds (when MCP client lands), imaging (vision variant only)
- **Environment:** Bodh's MARA bus + governance composer + HITL gate. **Not** tested standalone outside this substrate.

## Metrics

Bench-reported metrics. Run `go run ./cmd/bench -manifest=data/cases/manifest.json -providers=[variant]` to reproduce.

- **Accuracy (exact-match against gold_diagnosis):** [%]
- **Mean cost USD per case:** [$]
- **Mean message steps:** [n]
- **Under-budget rate:** [%]
- **RPM alert match rate:** [%]
- **Pareto score (accuracy / mean_cost × 100):** [score]
- **Fallback rate (LLM variants only):** [%] — fraction of calls that routed to the rule-based fallback

## Evaluation data

- **Fixtures:** [`data/cases/manifest.json`](../../data/cases/manifest.json)
- **Red-team catalogue:** [`data/eval/redteam.json`](../../data/eval/redteam.json) — six adversarial categories
- **Sample size:** [n] cases · [n] red-team cases
- **Synthetic only:** yes — opaque case IDs, no real PHI

## Training data

[For vendor LLMs] Bodh does not train custom models. This variant composes with a vendor LLM whose training data is governed by the provider's terms. Refer to:

- [Anthropic Trust & Transparency](https://www.anthropic.com/trust)
- [OpenAI Usage Policies](https://openai.com/policies/usage-policies/)
- [Ollama model documentation](https://ollama.com/library)

[For rule-based variants] No training data. Thresholds are clinical-textbook reasonable but **illustrative, not clinical guidelines**. Every threshold in the source carries an inline comment.

## Quantitative analyses

[Optional — populate as bench evidence accumulates.]

- **Per-condition accuracy stratified by demographic** — pending equity audit pipeline
- **Cost per condition** — see bench Pareto column
- **Latency p50/p95/p99** — from `[llm-trace]` records or OTel spans

## Ethical considerations

- **Risk: hallucinated diagnosis.** Mitigations: rule-based fallback (every LLM-backed agent has one), confidence threshold, RAG grounding check, safety guardrails (`pkg/safety`), HITL gate on the reasoning_verifier output.
- **Risk: overconfidence on out-of-distribution cases.** Mitigations: `MinConfidence` threshold routes to fallback; reasoning_verifier may reject low-evidence proposals.
- **Risk: PHI leakage via prompt or response.** Mitigations: PHI redaction at the logger seam (`pkg/phi`), opaque case IDs, no narrative in audit events, OTel spans use hashed case IDs.
- **Risk: biased recommendations.** Mitigations: red-team catalogue exercises adversarial cases; equity audit pipeline planned for stratified bench analysis.
- **Risk: cost gaming (under-ordering tests to stay within budget).** Mitigations: `cost_guardian` enforces per-case budget; reasoning_verifier checks the rationale-vs-tests alignment.

## Caveats and recommendations

- **Not a medical device.** Not approved for clinical use. Every deployment requires independent validation.
- **Thresholds are illustrative.** Every magic number in the source carries an "illustrative — educational demo, not a clinical guideline" comment.
- **Fallback contract.** This variant must NOT be deployed without its deterministic fallback. The fallback IS the contract — see [bodh-linkedin/04-fallback-is-the-contract.md](../../bodh-linkedin/04-fallback-is-the-contract.md) (in the sibling LinkedIn folder).
- **HITL is non-optional for clinical deployments.** The 21st Century Cures Act §3060 CDS carve-out criterion 4 ("independent review") requires it.
- **Audit gap = no deployment.** This variant must be deployed with `pkg/audit` wired into the orchestrator (PR #12). Otherwise §164.312(b) audit controls are not satisfied.
- **Production checklist:** see [docs/deployment.md](../deployment.md) and [docs/compliance.md](../compliance.md).

## References

- Mitchell, Wu, Zaldivar, et al. (2018) [*Model Cards for Model Reporting*](https://arxiv.org/abs/1810.03677)
- Bodh architecture: [docs/architecture.md](../architecture.md)
- Bodh agent reference: [docs/agents.md](../agents.md)
- Bodh evaluation: [docs/evaluation.md](../evaluation.md)
