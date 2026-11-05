# Model Card — `ReflexionDiagnostician`

> **Card version:** 1.0.0 · **Last reviewed:** 2026-05-25 · **Author:** Bodhyanga maintainers

## Model details

- **Variant name:** `ReflexionDiagnostician`
- **Source:** [`agents/diagnostician_reflexion.go`](../../agents/diagnostician_reflexion.go)
- **Type:** Self-critique loop (produce → critique → revise) with deterministic fallback
- **CLI:** `-diagnostician=llm-anthropic-reflexion` (or `-openai`, `-ollama`)
- **Default LLM provider:** inherits parent
- **Default rounds:** 2 (configurable up to `MaxRounds`)
- **Provider-configurable:** yes
- **License:** Apache-2.0 (Bodhyanga) / provider terms

## Intended use

- **Primary intended use:** Produce a `DiagnosisProposal`, critique it against the supplied evidence, optionally revise. Stops early when the critique returns "no objections." Targets cases where first-pass LLM reasoning may overlook a less-obvious differential.
- **Primary intended users:** Researchers evaluating self-correction patterns.
- **Out-of-scope uses:**
  - Latency-sensitive flows (Reflexion adds 1-2× latency per round)
  - Cases where the first proposal is obviously correct (the critique step is wasted)

## Factors

- **Relevant conditions:** Cases where evidence is mixed, where common cognitive errors apply (anchoring, premature closure), or where uncommon presentations of common diseases matter.
- **Rounds:** Default 2. Each additional round adds ~1× single-call cost.
- **Stopping:** Early exit on "no objections" from critique; hard cap at `MaxRounds`.

## Metrics

Run side-by-side bench. Expect:
- **Accuracy:** +0-2pp vs single-call typically
- **Cost:** 1.5-2.5× single-call (with early-exit)
- **Latency:** 1.5-3× single-call
- **Pareto:** marginal — depends heavily on case mix

## Evaluation data

Same as `LLMDiagnostician`. Reflexion's value is on cases where critique catches first-pass errors; add bench fixtures with subtle differentials to surface that signal.

## Training data

Inherits underlying LLM. The critique prompt is hand-coded; no fine-tuning.

## Ethical considerations

- **Risk: self-confirming critique.** The same LLM is producing the proposal AND critiquing it; biases in the proposal may also be present in the critique. Mitigation: hallucination check still runs at the end; safety guardrails still apply.
- **Risk: cost amplification.** Each round is another full LLM call. Mitigation: cost_guardian enforces per-case budget.
- **Risk: revision drift.** A late round may revise toward a wrong answer. Mitigation: bench evaluation; HITL gate on final output.

## Caveats and recommendations

- **Bench the rounds value.** 2 is the default; 3-4 rounds rarely pay back. Stop-early-on-no-objections is the right shape.
- **Reflexion ≠ debate.** Reflexion is one model self-correcting; debate is multiple personas converging. They're complementary, not substitutes.
- **HITL on the final output.** Reflexion's last revision is what flows to reasoning_verifier — the gate is at the same point as for the single-call variant.

## References

- [agents/diagnostician_reflexion.go](../../agents/diagnostician_reflexion.go)
- [docs/architecture.md § Agentic patterns](../architecture.md#agentic-patterns-debate-reflexion-cascade)
- Shinn, Cassano, Gopinath, et al. (2023) [*Reflexion: Language Agents with Verbal Reinforcement Learning*](https://arxiv.org/abs/2303.11366) — the technique reference
